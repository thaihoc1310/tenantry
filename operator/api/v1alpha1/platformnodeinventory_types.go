package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// NodeInventoryName is the name of the only PlatformNodeInventory of a
// cluster, created by the chart tenantry-operator.
const NodeInventoryName = "cluster"

// NodeKind matches cluster_nodes.kind (architecture/02 §3).
// +kubebuilder:validation:Enum=ControlPlane;System;Worker
type NodeKind string

const (
	NodeControlPlane NodeKind = "ControlPlane"
	NodeSystem       NodeKind = "System"
	NodeWorker       NodeKind = "Worker"
)

type PlatformNodeInventoryStatus struct {
	// Nodes of the cluster, sorted by name.
	// +listType=map
	// +listMapKey=name
	// +optional
	Nodes []InventoryNode `json:"nodes,omitempty"`
}

type InventoryNode struct {
	Name        string   `json:"name"`
	Kind        NodeKind `json:"kind"`
	Ready       bool     `json:"ready"`
	Schedulable bool     `json:"schedulable"`
	// Allocatable: cpu, memory, pods.
	// +optional
	Allocatable ResourceList `json:"allocatable,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// PlatformNodeInventory reports the cluster's nodes to the central plane,
// which never reads Node itself (IMPLEMENTATION-PLAN §4.5). It has no spec.
type PlatformNodeInventory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Status PlatformNodeInventoryStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type PlatformNodeInventoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PlatformNodeInventory `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PlatformNodeInventory{}, &PlatformNodeInventoryList{})
		return nil
	})
}
