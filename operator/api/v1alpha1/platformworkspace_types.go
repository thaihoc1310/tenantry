package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ResourceList maps a quota key (requests.cpu, limits.memory, pods,
// requests.storage) to a quantity. It is corev1.ResourceList without the
// k8s.io/api import, so the backend can build a CR from this package alone.
type ResourceList map[string]resource.Quantity

// ComputeMode is how a Workspace gets its nodes (PRODUCT-PLAN §6).
// +kubebuilder:validation:Enum=Shared;Dedicated
type ComputeMode string

const (
	ComputeShared    ComputeMode = "Shared"
	ComputeDedicated ComputeMode = "Dedicated"
)

// WorkspaceRole is the role a group holds in a Workspace (PRODUCT-PLAN §10).
// +kubebuilder:validation:Enum=TenantOwner;Developer;Viewer
type WorkspaceRole string

const (
	RoleTenantOwner WorkspaceRole = "TenantOwner"
	RoleDeveloper   WorkspaceRole = "Developer"
	RoleViewer      WorkspaceRole = "Viewer"
)

// WorkspacePhase is derived from the conditions (architecture/04 §1.4).
// +kubebuilder:validation:Enum=Provisioning;Ready;Updating;Degraded;Failed;Deleting
type WorkspacePhase string

const (
	PhaseProvisioning WorkspacePhase = "Provisioning"
	PhaseReady        WorkspacePhase = "Ready"
	PhaseUpdating     WorkspacePhase = "Updating"
	PhaseDegraded     WorkspacePhase = "Degraded"
	PhaseFailed       WorkspacePhase = "Failed"
	PhaseDeleting     WorkspacePhase = "Deleting"
)

// NamespaceSource tells whether a Namespace was asked for on the portal
// (it is in spec.namespaces) or created some other way (kubectl, Headlamp).
// +kubebuilder:validation:Enum=portal;kubectl
type NamespaceSource string

const (
	SourcePortal  NamespaceSource = "portal"
	SourceKubectl NamespaceSource = "kubectl"
)

// PlatformWorkspaceSpec is the desired state the central plane writes
// (PRODUCT-PLAN §14.2). Namespace names: spec.namespaces holds suffixes,
// every other field holds full names (<metadata.name>-<suffix>).
type PlatformWorkspaceSpec struct {
	// +kubebuilder:validation:MinLength=1
	OrganizationID string `json:"organizationId"`
	// +kubebuilder:validation:MinLength=1
	WorkspaceID string `json:"workspaceId"`
	// +kubebuilder:validation:MinLength=1
	ClusterID string `json:"clusterId"`
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Cordoned freezes the Tenant (PRODUCT-PLAN §17.3).
	// +optional
	Cordoned bool `json:"cordoned,omitempty"`
	// RetryNonce changes when an operation is retried after a failure the
	// controller reported: a new generation, nothing else changes.
	// +optional
	RetryNonce string `json:"retryNonce,omitempty"`

	// NamespaceQuota is how many Namespaces the Tenant may hold, whoever creates them.
	// +kubebuilder:validation:Minimum=1
	// +optional
	NamespaceQuota int32 `json:"namespaceQuota,omitempty"`
	// Namespaces the portal asked for, as suffixes: frontend → <metadata.name>-frontend.
	// +listType=set
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
	// NamespacesToDelete holds full names, including ones created with kubectl.
	// +listType=set
	// +optional
	NamespacesToDelete []string `json:"namespacesToDelete,omitempty"`
	// Routes publish a Service on the apps domain (PRODUCT-PLAN §7.6).
	// +listType=map
	// +listMapKey=name
	// +optional
	Routes []WorkspaceRoute `json:"routes,omitempty"`

	// Resources is the quota shared by every Namespace of the Workspace (PRODUCT-PLAN §5.4).
	// +optional
	Resources ResourceList `json:"resources,omitempty"`
	Compute   Compute      `json:"compute"`
	// Network is set from M9 on; Workspaces created before have none.
	// +optional
	Network *Network `json:"network,omitempty"`
	// StorageClasses the Tenant's PVCs may use.
	// +listType=set
	// +optional
	StorageClasses []string `json:"storageClasses,omitempty"`
	// AccessBindings give organization groups a role in the Workspace.
	// +listType=map
	// +listMapKey=groupSubject
	// +optional
	AccessBindings []AccessBinding `json:"accessBindings,omitempty"`
}

type WorkspaceRoute struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace is a full name.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// Host is complete, the API puts it together.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`
	// +kubebuilder:validation:MinLength=1
	Service string `json:"service"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

type Compute struct {
	Mode ComputeMode `json:"mode"`
	// Shared mode.
	// +optional
	SharedPoolID string `json:"sharedPoolId,omitempty"`
	// Dedicated mode.
	// +optional
	DedicatedNodeGrantID string `json:"dedicatedNodeGrantId,omitempty"`
	// Dedicated mode.
	// +optional
	NodeName string `json:"nodeName,omitempty"`
}

type Network struct {
	// +kubebuilder:validation:MinLength=1
	AllocationRef string `json:"allocationRef"`
	// +kubebuilder:validation:MinLength=1
	CIDR string `json:"cidr"`
}

type AccessBinding struct {
	// GroupSubject is the group as kube-apiserver sees it: oidc:org_<ulid>:group_<ulid>.
	// +kubebuilder:validation:MinLength=1
	GroupSubject string        `json:"groupSubject"`
	Role         WorkspaceRole `json:"role"`
}

// PlatformWorkspaceStatus is what the controller observed.
type PlatformWorkspaceStatus struct {
	// +optional
	Phase WorkspacePhase `json:"phase,omitempty"`
	// ObservedGeneration is set once a whole reconcile of that generation is done.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	CapsuleTenantRef string `json:"capsuleTenantRef,omitempty"`
	// Usage sums the Workspace's ResourceQuotas.
	// +optional
	Usage ResourceList `json:"usage,omitempty"`
	// Namespaces of the Tenant, including ones created with kubectl.
	// +listType=map
	// +listMapKey=name
	// +optional
	Namespaces []NamespaceStatus `json:"namespaces,omitempty"`
	// +listType=map
	// +listMapKey=name
	// +optional
	Routes []RouteStatus `json:"routes,omitempty"`
}

type NamespaceStatus struct {
	Name   string          `json:"name"`
	Source NamespaceSource `json:"source"`
	// +optional
	Usage ResourceList `json:"usage,omitempty"`
}

type RouteStatus struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     bool   `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=pws
// +kubebuilder:printcolumn:name="Workspace",type=string,JSONPath=`.spec.workspaceId`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PlatformWorkspace is one Workspace on this cluster; its name is
// <org-slug>-<ws-slug>, the name of the Capsule Tenant too.
type PlatformWorkspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec PlatformWorkspaceSpec `json:"spec"`
	// +optional
	Status PlatformWorkspaceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type PlatformWorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PlatformWorkspace `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PlatformWorkspace{}, &PlatformWorkspaceList{})
		return nil
	})
}
