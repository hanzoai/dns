package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PagesProjectSpec defines a Cloudflare Pages site managed declaratively.
// It composes the DNS layer: when CustomDomain+ZoneRef are set the operator
// emits a CNAME DnsRecord pointing the domain at the project's pages.dev host.
type PagesProjectSpec struct {
	// AccountID is the Cloudflare account that owns the project.
	AccountID string `json:"accountId"`

	// ProjectName is the Cloudflare Pages project name. Defaults to metadata.name.
	// +optional
	ProjectName string `json:"projectName,omitempty"`

	// ProductionBranch for git-connected projects.
	// +kubebuilder:default="main"
	ProductionBranch string `json:"productionBranch,omitempty"`

	// CustomDomain to attach to the project (e.g. "hanzo.bot").
	// +optional
	CustomDomain string `json:"customDomain,omitempty"`

	// ZoneRef is the DnsZone that owns CustomDomain, used to emit the CNAME.
	// Required when CustomDomain is set.
	// +optional
	ZoneRef string `json:"zoneRef,omitempty"`
}

// PagesProjectStatus reports the observed state of the Pages project.
type PagesProjectStatus struct {
	// Phase represents the current lifecycle phase.
	// +kubebuilder:validation:Enum=Pending;Ready;Error
	Phase string `json:"phase,omitempty"`

	// Subdomain is the project's canonical *.pages.dev host.
	// +optional
	Subdomain string `json:"subdomain,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Domain",type=string,JSONPath=`.spec.customDomain`
// +kubebuilder:printcolumn:name="Subdomain",type=string,JSONPath=`.status.subdomain`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// PagesProject is a Cloudflare Pages site deployed and domain-wired by the operator.
type PagesProject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PagesProjectSpec   `json:"spec,omitempty"`
	Status PagesProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PagesProjectList contains a list of PagesProject.
type PagesProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PagesProject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PagesProject{}, &PagesProjectList{})
}
