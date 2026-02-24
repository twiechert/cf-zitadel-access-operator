package controller

import (
	"context"
	"fmt"
	"time"

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
	// DefaultIngressClass is the Ingress class for generated Ingresses.
	DefaultIngressClass string

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
			// Clean up Cloudflare Access Application.
			if app.Status.AccessApplicationID != "" {
				logger.Info("deleting Cloudflare Access Application", "appId", app.Status.AccessApplicationID)
				if err := r.Cloudflare.DeleteAccessApp(ctx, app.Status.AccessApplicationID); err != nil {
					logger.Error(err, "failed to delete Access Application, will retry")
					return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
				}
			}
			// Ingress is cleaned up via ownerReference GC.
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

	// 3. Reconcile Cloudflare Access Application with inline OIDC claim policies.
	roleClaim := fmt.Sprintf("urn:zitadel:iam:org:project:%s:roles", project.ID)

	accessAppID := app.Status.AccessApplicationID
	if accessAppID == "" {
		// Check if one already exists for this domain.
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

	// Build OIDC claim rules — one per role, OR'd together by Cloudflare.
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

	// 4. Reconcile Ingress (for CF tunnel routing only — no access annotations).
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
	}

	ingressResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		if err := controllerutil.SetControllerReference(&app, ingress, r.Scheme); err != nil {
			return err
		}

		className := r.Config.DefaultIngressClass
		if app.Spec.Ingress != nil && app.Spec.Ingress.ClassName != "" {
			className = app.Spec.Ingress.ClassName
		}
		ingress.Spec.IngressClassName = &className

		// Only user-specified annotations + backend protocol if needed.
		annotations := make(map[string]string)
		if app.Spec.Ingress != nil {
			for k, v := range app.Spec.Ingress.Annotations {
				annotations[k] = v
			}
		}
		if app.Spec.Backend.Protocol != "" {
			annotations[cfBackendProtocolAnnotation] = app.Spec.Backend.Protocol
		}
		ingress.Annotations = annotations

		pathType := networkingv1.PathTypePrefix
		if app.Spec.Ingress != nil && app.Spec.Ingress.PathType != "" {
			pathType = networkingv1.PathType(app.Spec.Ingress.PathType)
		}
		path := "/"
		if app.Spec.Ingress != nil && app.Spec.Ingress.Path != "" {
			path = app.Spec.Ingress.Path
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
										Name: app.Spec.Backend.ServiceName,
										Port: networkingv1.ServiceBackendPort{
											Number: app.Spec.Backend.ServicePort,
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
	if err != nil {
		return r.setCondition(ctx, &app, metav1.ConditionFalse, "IngressFailed", err.Error())
	}

	logger.Info("reconciled ingress", "name", ingress.Name, "operation", ingressResult)

	// 5. Update status.
	app.Status.ProjectID = project.ID
	app.Status.AccessApplicationID = accessAppID
	app.Status.AccessPolicyID = policy.ID
	app.Status.Ready = true
	return r.setCondition(ctx, &app, metav1.ConditionTrue, "Reconciled", "Access Application and Ingress are up to date")
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
		Complete(r)
}
