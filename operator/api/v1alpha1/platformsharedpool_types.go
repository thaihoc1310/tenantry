package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PlatformSharedPoolSpec lists the nodes of a shared pool (IMPLEMENTATION-PLAN §4.5).
type PlatformSharedPoolSpec struct {
	// Nodes that get the label tenantry.io/compute-pool=<metadata.name>.
	// +listType=set
	// +optional
	Nodes []string `json:"nodes,omitempty"`
}

type PlatformSharedPoolStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Ready is False with reason NodeNotEligible (naming the nodes) when a
	// listed node is a control-plane or system node, dedicated, or in
	// another pool; the other nodes are still labelled.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ReadyNodes counts the pool's labelled nodes that are Ready.
	// +optional
	ReadyNodes int32 `json:"readyNodes"`
	// Allocatable sums the pool's labelled nodes: cpu, memory, pods.
	// +optional
	Allocatable ResourceList `json:"allocatable,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name != 'system'",message="system is the label value of the system pool"
// +kubebuilder:printcolumn:name="Ready nodes",type=integer,JSONPath=`.status.readyNodes`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// PlatformSharedPool is a shared compute pool; its name is the pool id.
type PlatformSharedPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec PlatformSharedPoolSpec `json:"spec,omitzero"`
	// +optional
	Status PlatformSharedPoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type PlatformSharedPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PlatformSharedPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PlatformSharedPool{}, &PlatformSharedPoolList{})
		return nil
	})
}
