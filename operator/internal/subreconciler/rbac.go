package subreconciler

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// RBAC reports IdentityBindingsReady (sub-reconciler rbac). The bindings are
// part of the Tenant the tenant step applied: owners for TenantOwner groups,
// additionalRoleBindings to tenantry:tenant-developer and
// tenantry:tenant-viewer for the others (render.Tenant). Capsule writes the
// RoleBindings into every Namespace of the Tenant, and reports the Tenant
// Ready once it has done so for the current generation.
func RBAC(tenant *unstructured.Unstructured) Result {
	if tenant == nil {
		return waiting(v1.ConditionIdentityBindingsReady, "the Tenant is not applied yet")
	}
	return TenantState(v1.ConditionIdentityBindingsReady, tenant)
}
