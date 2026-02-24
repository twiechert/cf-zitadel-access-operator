package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.host`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.access.project`
// +kubebuilder:printcolumn:name="Client ID",type=string,JSONPath=`.status.clientId`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SecuredApplication registers an OIDC application in Zitadel, protects it
// with a Cloudflare Access policy based on Zitadel roles, and optionally
// creates an Ingress for routing.
type SecuredApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecuredApplicationSpec   `json:"spec,omitempty"`
	Status SecuredApplicationStatus `json:"status,omitempty"`
}

type SecuredApplicationSpec struct {
	// Host is the public hostname for this application.
	Host string `json:"host"`

	// Access defines the Zitadel project and roles required to access this application.
	Access Access `json:"access"`

	// OIDC configures the Zitadel OIDC application.
	// +optional
	OIDC *OIDCConfig `json:"oidc,omitempty"`

	// Tunnel creates an Ingress for routing (e.g. via Cloudflare Tunnel).
	// When omitted, no Ingress is created.
	// +optional
	Tunnel *TunnelConfig `json:"tunnel,omitempty"`
}

type Access struct {
	// Project is the Zitadel project name. The operator resolves this to a project ID.
	Project string `json:"project"`

	// Roles lists the Zitadel project roles allowed to access this application.
	// +kubebuilder:validation:MinItems=1
	Roles []string `json:"roles"`
}

type OIDCConfig struct {
	// RedirectURIs for the OIDC application.
	// Defaults to ["https://{host}/callback"] if not specified.
	// +optional
	RedirectURIs []string `json:"redirectURIs,omitempty"`

	// PostLogoutRedirectURIs for the OIDC application.
	// +optional
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectURIs,omitempty"`

	// ResponseTypes defaults to ["OIDC_RESPONSE_TYPE_CODE"].
	// +optional
	ResponseTypes []string `json:"responseTypes,omitempty"`

	// GrantTypes defaults to ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"].
	// +optional
	GrantTypes []string `json:"grantTypes,omitempty"`

	// AppType defaults to "OIDC_APP_TYPE_WEB".
	// +optional
	AppType string `json:"appType,omitempty"`

	// AuthMethodType defaults to "OIDC_AUTH_METHOD_TYPE_BASIC".
	// +optional
	AuthMethodType string `json:"authMethodType,omitempty"`

	// AccessTokenType defaults to "OIDC_TOKEN_TYPE_BEARER".
	// +optional
	AccessTokenType string `json:"accessTokenType,omitempty"`

	// DevMode enables development mode (allows http redirect URIs).
	// +optional
	DevMode bool `json:"devMode,omitempty"`

	// IDTokenRoleAssertion includes roles in the ID token.
	// +optional
	IDTokenRoleAssertion bool `json:"idTokenRoleAssertion,omitempty"`

	// IDTokenUserinfoAssertion includes userinfo in the ID token.
	// +optional
	IDTokenUserinfoAssertion bool `json:"idTokenUserinfoAssertion,omitempty"`

	// AccessTokenRoleAssertion includes roles in the access token.
	// +optional
	AccessTokenRoleAssertion bool `json:"accessTokenRoleAssertion,omitempty"`

	// ClientSecretRef is the name of the Kubernetes Secret to write OIDC
	// credentials to. Defaults to "{name}-oidc". The secret will contain
	// "clientId" and "clientSecret" keys.
	// +optional
	ClientSecretRef string `json:"clientSecretRef,omitempty"`
}

type TunnelConfig struct {
	// Backend defines the Kubernetes Service to route traffic to.
	Backend Backend `json:"backend"`

	// Ingress allows overriding generated Ingress settings.
	// +optional
	Ingress *IngressConfig `json:"ingress,omitempty"`
}

type Backend struct {
	// ServiceName is the name of the Kubernetes Service.
	ServiceName string `json:"serviceName"`

	// ServicePort is the port number on the Service.
	ServicePort int32 `json:"servicePort"`

	// Protocol overrides the backend protocol (e.g. "https").
	// +optional
	Protocol string `json:"protocol,omitempty"`
}

type IngressConfig struct {
	// ClassName overrides the default Ingress class (defaults to "cloudflare-tunnel").
	// +optional
	ClassName string `json:"className,omitempty"`

	// Annotations to add to the generated Ingress.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Path defaults to "/".
	// +optional
	Path string `json:"path,omitempty"`

	// PathType defaults to "Prefix".
	// +optional
	PathType string `json:"pathType,omitempty"`
}

type SecuredApplicationStatus struct {
	// ProjectID is the resolved Zitadel project ID.
	ProjectID string `json:"projectId,omitempty"`

	// ZitadelAppID is the Zitadel OIDC application ID.
	ZitadelAppID string `json:"zitadelAppId,omitempty"`

	// ClientID is the OIDC client ID.
	ClientID string `json:"clientId,omitempty"`

	// AccessApplicationID is the Cloudflare Access Application ID.
	AccessApplicationID string `json:"accessApplicationId,omitempty"`

	// AccessPolicyID is the Cloudflare Access Policy ID.
	AccessPolicyID string `json:"accessPolicyId,omitempty"`

	// Ready indicates the application is fully reconciled.
	Ready bool `json:"ready"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// SecuredApplicationList contains a list of SecuredApplication.
type SecuredApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecuredApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecuredApplication{}, &SecuredApplicationList{})
}
