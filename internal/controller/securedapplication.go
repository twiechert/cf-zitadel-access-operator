package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	accessv1alpha1 "github.com/twiechert/zitadel-access-operator/api/v1alpha1"
	cfclient "github.com/twiechert/zitadel-access-operator/internal/cloudflare"
	"github.com/twiechert/zitadel-access-operator/internal/zitadel"
)

const (
	finalizerName = "access.zitadel.com/finalizer"

	cfBackendProtocolAnnotation = "cloudflare-tunnel-ingress-controller.strrl.dev/backend-protocol"
)

// Config holds operator-level configuration.
type Config struct {
	// CloudflareIdPID is the Cloudflare Access Identity Provider ID for Zitadel.
	CloudflareIdPID string

	// SessionDuration is the Cloudflare Access session duration (e.g. "24h").
	SessionDuration string
}

type SecuredApplicationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Zitadel    zitadel.Client
	Cloudflare cfclient.Client
	Config     Config
}

// +kubebuilder:rbac:groups=access.zitadel.com,resources=securedapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=access.zitadel.com,resources=securedapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=access.zitadel.com,resources=securedapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

func (r *SecuredApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var app accessv1alpha1.SecuredApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !app.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&app, finalizerName) {
			if !app.Spec.DeleteProtection {
				// Delete Zitadel OIDC app.
				if app.Status.ZitadelAppID != "" && app.Status.ProjectID != "" {
					logger.Info("deleting Zitadel OIDC app", "appId", app.Status.ZitadelAppID)
					if err := r.Zitadel.DeleteApp(ctx, app.Status.ProjectID, app.Status.ZitadelAppID); err != nil {
						logger.Error(err, "failed to delete Zitadel app, will retry")
						return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
					}
				}
				// Delete Cloudflare Access Application.
				if app.Status.AccessApplicationID != "" {
					logger.Info("deleting Cloudflare Access Application", "appId", app.Status.AccessApplicationID)
					if err := r.Cloudflare.DeleteAccessApp(ctx, app.Status.AccessApplicationID); err != nil {
						logger.Error(err, "failed to delete Access Application, will retry")
						return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
					}
				}
			} else {
				logger.Info("delete protection enabled, keeping external resources")
			}
			// Ingress + Secret cleaned up via ownerReference GC.
			controllerutil.RemoveFinalizer(&app, finalizerName)
			if err := r.Update(ctx, &app); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer.
	if !controllerutil.ContainsFinalizer(&app, finalizerName) {
		controllerutil.AddFinalizer(&app, finalizerName)
		if err := r.Update(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 1. Resolve Zitadel project name → ID.
	project, err := r.Zitadel.GetProjectByName(ctx, app.Spec.Access.Project)
	if err != nil {
		return r.setCondition(ctx, &app, metav1.ConditionFalse, "ProjectLookupFailed", err.Error())
	}
	if project == nil {
		return r.setCondition(ctx, &app, metav1.ConditionFalse, "ProjectNotFound",
			fmt.Sprintf("Zitadel project %q not found", app.Spec.Access.Project))
	}

	// 2. Validate that all requested roles exist.
	existingRoles, err := r.Zitadel.ListProjectRoles(ctx, project.ID)
	if err != nil {
		return r.setCondition(ctx, &app, metav1.ConditionFalse, "RoleLookupFailed", err.Error())
	}
	roleSet := make(map[string]bool, len(existingRoles))
	for _, role := range existingRoles {
		roleSet[role.Key] = true
	}
	for _, requested := range app.Spec.Access.Roles {
		if !roleSet[requested] {
			return r.setCondition(ctx, &app, metav1.ConditionFalse, "RoleNotFound",
				fmt.Sprintf("role %q does not exist in Zitadel project %q", requested, app.Spec.Access.Project))
		}
	}

	// 3. Reconcile Zitadel OIDC application (only when spec.oidc is set).
	if app.Spec.OIDC != nil {
		oidcApp, clientSecret, err := r.reconcileZitadelApp(ctx, &app, project.ID)
		if err != nil {
			return r.setCondition(ctx, &app, metav1.ConditionFalse, "ZitadelAppFailed", err.Error())
		}
		logger.Info("reconciled Zitadel OIDC app", "appId", oidcApp.ID, "clientId", oidcApp.ClientID)

		app.Status.ZitadelAppID = oidcApp.ID
		app.Status.ClientID = oidcApp.ClientID

		// Write credentials to K8s Secret (only on initial creation when we have the client secret).
		if clientSecret != "" {
			if err := r.writeCredentialSecret(ctx, &app, oidcApp.ClientID, clientSecret); err != nil {
				return r.setCondition(ctx, &app, metav1.ConditionFalse, "SecretFailed", err.Error())
			}
		}
	}

	// 4. Reconcile Cloudflare Access Application with inline OIDC claim policies.
	roleClaim := fmt.Sprintf("urn:zitadel:iam:org:project:%s:roles", project.ID)

	accessAppID := app.Status.AccessApplicationID
	if accessAppID == "" {
		existing, err := r.Cloudflare.FindAccessAppByDomain(ctx, app.Spec.Host)
		if err != nil {
			return r.setCondition(ctx, &app, metav1.ConditionFalse, "CloudflareLookupFailed", err.Error())
		}
		if existing != nil {
			logger.Info("adopting existing Access Application", "appId", existing.ID)
			accessAppID = existing.ID
		}
	}

	if accessAppID != "" {
		if err := r.Cloudflare.UpdateAccessApp(ctx, accessAppID, app.Name, app.Spec.Host, r.Config.SessionDuration); err != nil {
			return r.setCondition(ctx, &app, metav1.ConditionFalse, "CloudflareUpdateFailed", err.Error())
		}
	} else {
		created, err := r.Cloudflare.CreateAccessApp(ctx, app.Name, app.Spec.Host, r.Config.SessionDuration)
		if err != nil {
			return r.setCondition(ctx, &app, metav1.ConditionFalse, "CloudflareCreateFailed", err.Error())
		}
		accessAppID = created.ID
		logger.Info("created Access Application", "appId", accessAppID)
	}

	rules := make([]cfclient.OIDCClaimRule, len(app.Spec.Access.Roles))
	for i, role := range app.Spec.Access.Roles {
		rules[i] = cfclient.OIDCClaimRule{
			IdentityProviderID: r.Config.CloudflareIdPID,
			ClaimName:          roleClaim,
			ClaimValue:         role,
		}
	}

	policy, err := r.Cloudflare.UpsertAccessPolicy(ctx, accessAppID, app.Status.AccessPolicyID, rules)
	if err != nil {
		return r.setCondition(ctx, &app, metav1.ConditionFalse, "PolicyFailed", err.Error())
	}

	// 5. Reconcile Ingress if tunnel is configured.
	if app.Spec.Tunnel != nil {
		if err := r.reconcileIngress(ctx, &app); err != nil {
			return r.setCondition(ctx, &app, metav1.ConditionFalse, "IngressFailed", err.Error())
		}
		logger.Info("reconciled ingress", "name", app.Name)
	}

	// 6. Update status.
	app.Status.ProjectID = project.ID
	app.Status.AccessApplicationID = accessAppID
	app.Status.AccessPolicyID = policy.ID
	app.Status.Ready = true
	return r.setCondition(ctx, &app, metav1.ConditionTrue, "Reconciled", "All resources are up to date")
}

func (r *SecuredApplicationReconciler) reconcileZitadelApp(ctx context.Context, app *accessv1alpha1.SecuredApplication, projectID string) (*zitadel.App, string, error) {
	oidc := app.Spec.OIDC

	redirectURIs := []string{fmt.Sprintf("https://%s/callback", app.Spec.Host)}
	if len(oidc.RedirectURIs) > 0 {
		redirectURIs = oidc.RedirectURIs
	}

	config := zitadel.AppConfig{
		Name:                     app.Name,
		RedirectURIs:             redirectURIs,
		PostLogoutRedirectURIs:   oidc.PostLogoutRedirectURIs,
		ResponseTypes:            withDefault(oidc.ResponseTypes, []string{"OIDC_RESPONSE_TYPE_CODE"}),
		GrantTypes:               withDefault(oidc.GrantTypes, []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}),
		AppType:                  withDefault1(oidc.AppType, "OIDC_APP_TYPE_WEB"),
		AuthMethodType:           withDefault1(oidc.AuthMethodType, "OIDC_AUTH_METHOD_TYPE_BASIC"),
		AccessTokenType:          withDefault1(oidc.AccessTokenType, "OIDC_TOKEN_TYPE_BEARER"),
		DevMode:                  oidc.DevMode,
		IDTokenRoleAssertion:     oidc.IDTokenRoleAssertion,
		IDTokenUserinfoAssertion: oidc.IDTokenUserinfoAssertion,
		AccessTokenRoleAssertion: oidc.AccessTokenRoleAssertion,
	}

	// Check if app already exists.
	if app.Status.ZitadelAppID != "" {
		if err := r.Zitadel.UpdateApp(ctx, projectID, app.Status.ZitadelAppID, config); err != nil {
			return nil, "", err
		}
		return &zitadel.App{
			ID:       app.Status.ZitadelAppID,
			ClientID: app.Status.ClientID,
		}, "", nil
	}

	// Try to find by name (re-adoption).
	existing, err := r.Zitadel.GetAppByName(ctx, projectID, app.Name)
	if err != nil {
		return nil, "", err
	}
	if existing != nil {
		if err := r.Zitadel.UpdateApp(ctx, projectID, existing.ID, config); err != nil {
			return nil, "", err
		}
		return existing, "", nil
	}

	// Create new app — this is the only time we get the client secret.
	created, err := r.Zitadel.CreateApp(ctx, projectID, config)
	if err != nil {
		return nil, "", err
	}
	return created, created.ClientSecret, nil
}

func (r *SecuredApplicationReconciler) writeCredentialSecret(ctx context.Context, app *accessv1alpha1.SecuredApplication, clientID, clientSecret string) error {
	secretName := app.Name + "-oidc"
	if app.Spec.OIDC != nil && app.Spec.OIDC.ClientSecretRef != "" {
		secretName = app.Spec.OIDC.ClientSecretRef
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: app.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if err := controllerutil.SetControllerReference(app, secret, r.Scheme); err != nil {
			return err
		}
		secret.StringData = map[string]string{
			"clientId":     clientID,
			"clientSecret": clientSecret,
		}
		return nil
	})
	return err
}

func (r *SecuredApplicationReconciler) reconcileIngress(ctx context.Context, app *accessv1alpha1.SecuredApplication) error {
	tunnel := app.Spec.Tunnel

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		if err := controllerutil.SetControllerReference(app, ingress, r.Scheme); err != nil {
			return err
		}

		className := "cloudflare-tunnel"
		if tunnel.Ingress != nil && tunnel.Ingress.ClassName != "" {
			className = tunnel.Ingress.ClassName
		}
		ingress.Spec.IngressClassName = &className

		annotations := make(map[string]string)
		if tunnel.Ingress != nil {
			for k, v := range tunnel.Ingress.Annotations {
				annotations[k] = v
			}
		}
		if tunnel.Backend.Protocol != "" {
			annotations[cfBackendProtocolAnnotation] = tunnel.Backend.Protocol
		}
		ingress.Annotations = annotations

		pathType := networkingv1.PathTypePrefix
		if tunnel.Ingress != nil && tunnel.Ingress.PathType != "" {
			pathType = networkingv1.PathType(tunnel.Ingress.PathType)
		}
		path := "/"
		if tunnel.Ingress != nil && tunnel.Ingress.Path != "" {
			path = tunnel.Ingress.Path
		}

		ingress.Spec.Rules = []networkingv1.IngressRule{
			{
				Host: app.Spec.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     path,
								PathType: &pathType,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: tunnel.Backend.ServiceName,
										Port: networkingv1.ServiceBackendPort{
											Number: tunnel.Backend.ServicePort,
										},
									},
								},
							},
						},
					},
				},
			},
		}

		return nil
	})

	return err
}

func (r *SecuredApplicationReconciler) setCondition(ctx context.Context, app *accessv1alpha1.SecuredApplication, status metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {
	meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if status != metav1.ConditionTrue {
		app.Status.Ready = false
	}
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}
	if status != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	return ctrl.Result{}, nil
}

func (r *SecuredApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&accessv1alpha1.SecuredApplication{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func withDefault(val, def []string) []string {
	if len(val) == 0 {
		return def
	}
	return val
}

func withDefault1(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
