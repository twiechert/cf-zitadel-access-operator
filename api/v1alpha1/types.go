package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.host`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.access.project`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SecuredApplication declares a Kubernetes service that should be exposed
// via an Ingress and protected with Zitadel role-based access through
// Cloudflare Access.
type SecuredApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecuredApplicationSpec   `json:"spec,omitempty"`
	Status SecuredApplicationStatus `json:"status,omitempty"`
}

type SecuredApplicationSpec struct {
	// Host is the public hostname for this application.
	Host string `json:"host"`

	// Backend defines the Kubernetes Service to route traffic to.
	Backend Backend `json:"backend"`

	// Access defines the Zitadel project and roles required to access this application.
	Access Access `json:"access"`

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

type Access struct {
	// Project is the Zitadel project name. The operator resolves this to a project ID.
	Project string `json:"project"`

	// Roles lists the Zitadel project roles allowed to access this application.
	// +kubebuilder:validation:MinItems=1
	Roles []string `json:"roles"`
}

type IngressConfig struct {
	// ClassName overrides the default Ingress class.
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
