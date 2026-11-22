package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PlatformNodeGrantSpec gives one node to one Organization as a Dedicated
// node (IMPLEMENTATION-PLAN §4.5).
type PlatformNodeGrantSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="nodeName is immutable"
	NodeName string `json:"nodeName"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="organizationId is immutable"
	OrganizationID string `json:"organizationId"`
}

type PlatformNodeGrantStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Ready says whether the node carries the Organization's label and
	// taint; reasons NodeNotReady, NodeNotEligible, NodeNotClean.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Allocatable of the node: cpu, memory, pods.
	// +optional
	Allocatable ResourceList `json:"allocatable,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeName`
// +kubebuilder:printcolumn:name="Organization",type=string,JSONPath=`.spec.organizationId`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// PlatformNodeGrant is a DedicatedNodeGrant on this cluster; its name is the grant id.
type PlatformNodeGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec PlatformNodeGrantSpec `json:"spec"`
	// +optional
	Status PlatformNodeGrantStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type PlatformNodeGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PlatformNodeGrant `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PlatformNodeGrant{}, &PlatformNodeGrantList{})
		return nil
	})
}
