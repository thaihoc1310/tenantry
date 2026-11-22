// Package v1alpha1 holds the tenantry.io/v1alpha1 API: PlatformWorkspace and
// the three small CRDs of IMPLEMENTATION-PLAN §4.5. The backend imports it to
// build CRs, so it depends on k8s.io/apimachinery only, never on
// controller-runtime (IMPLEMENTATION-PLAN §2.1).
// +kubebuilder:object:generate=true
// +groupName=tenantry.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: "tenantry.io", Version: "v1alpha1"}
	GroupVersion       = SchemeGroupVersion

	SchemeBuilder = runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
		return nil
	})
	AddToScheme = SchemeBuilder.AddToScheme
)

// Names shared by the controller and the central plane.
const (
	// Finalizer of PlatformWorkspace: the in-cluster part of a delete
	// (IMPLEMENTATION-PLAN §4.3).
	WorkspaceFinalizer = "tenantry.io/workspace"
	// AnnotationForceDelete is set by the worker on :force; the finalizer
	// stops waiting for PVCs.
	AnnotationForceDelete = "tenantry.io/force-delete"

	// Labels of a PlatformWorkspace, and managed labels of every object the
	// controller renders (PRODUCT-PLAN §18).
	LabelManaged        = "tenantry.io/managed"
	LabelOrganizationID = "tenantry.io/organization-id"
	LabelWorkspaceID    = "tenantry.io/workspace-id"
	LabelClusterID      = "tenantry.io/cluster-id"
	LabelComputeMode    = "tenantry.io/compute-mode"
	LabelComputePool    = "tenantry.io/compute-pool"
	// LabelOrg on a tenant Namespace holds the Organization's ULID as it
	// appears in Keycloak group names (org_<ulid>), for the RoleBinding VAP.
	LabelOrg = "tenantry.io/org"
	// LabelDedicatedOrg (label and taint) marks a node granted to an Organization.
	LabelDedicatedOrg = "tenantry.io/dedicated-org"
	// WorkspaceMemberLabelPrefix + <workspace id> = "true" puts a Dedicated
	// Workspace on its node.
	WorkspaceMemberLabelPrefix = "workspaces.tenantry.io/"

	// SystemPool is the compute-pool label value of the system pool.
	SystemPool = "system"
)

// Condition types (architecture/04 §1.2). The controller sets the first
// group; ClusterReady, ClusterGrantValid, ResourcesReserved and
// DedicatedNodeCapacityReserved belong to the API and never appear on a CR.
const (
	ConditionCapsuleTenantReady    = "CapsuleTenantReady"
	ConditionIdentityBindingsReady = "IdentityBindingsReady"
	ConditionPolicyReady           = "PolicyReady"
	ConditionComputeReady          = "ComputeReady"
	ConditionNetworkReady          = "NetworkReady"
	ConditionProxyAccessReady      = "ProxyAccessReady"
	// ConditionDeleting is False with ReasonBlockedByFinalizer when a delete
	// is stuck; the worker maps it to Operation Blocked.
	ConditionDeleting        = "Deleting"
	ReasonBlockedByFinalizer = "BlockedByFinalizer"

	// ConditionReady of PlatformNodeGrant and PlatformSharedPool.
	ConditionReady        = "Ready"
	ReasonNodeNotReady    = "NodeNotReady"
	ReasonNodeNotEligible = "NodeNotEligible"
	ReasonNodeNotClean    = "NodeNotClean"
)
