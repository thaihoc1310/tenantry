// Package render turns a PlatformWorkspace into the objects it should have
// in the cluster (IMPLEMENTATION-PLAN §4.2). Pure functions: no client, no
// cluster, so golden files can pin the output. Capsule's Go module needs a
// newer Go than ours, hence unstructured objects.
package render

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

var (
	TenantGVK    = schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta2", Kind: "Tenant"}
	NamespaceGVK = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
)

const (
	// LabelCapsuleTenant is how Capsule ties a Namespace to its Tenant. The
	// operator owns every Tenant, so it names the Tenant on each Namespace
	// it creates.
	LabelCapsuleTenant = "capsule.clastix.io/tenant"

	ClusterRoleDeveloper = "tenantry:tenant-developer"
	ClusterRoleViewer    = "tenantry:tenant-viewer"
)

// Options are the parts of the output that come from the cluster, not the CR.
type Options struct {
	// OperatorServiceAccount is the operator's username,
	// system:serviceaccount:<namespace>:<name>: an owner of every Tenant, so
	// Capsule lets it create the Namespaces of spec.namespaces.
	OperatorServiceAccount string
	// AllowedRegistries tenants may pull images from (registry hosts).
	AllowedRegistries []string
}

// ManagedLabels go on every object rendered for ws (PRODUCT-PLAN §18).
func ManagedLabels(ws *v1.PlatformWorkspace) map[string]string {
	l := map[string]string{
		v1.LabelManaged:        "true",
		v1.LabelOrganizationID: ws.Spec.OrganizationID,
		v1.LabelWorkspaceID:    ws.Spec.WorkspaceID,
		v1.LabelClusterID:      ws.Spec.ClusterID,
		v1.LabelComputeMode:    strings.ToLower(string(ws.Spec.Compute.Mode)),
	}
	if ws.Spec.Compute.Mode == v1.ComputeShared && ws.Spec.Compute.SharedPoolID != "" {
		l[v1.LabelComputePool] = ws.Spec.Compute.SharedPoolID
	}
	return l
}

// OrgULID is the Organization id as Keycloak group names carry it:
// org-01jabc → 01jabc (org_01jabc:group_…).
func OrgULID(organizationID string) string {
	return strings.TrimPrefix(organizationID, "org-")
}

// NamespaceName is the full name of a spec.namespaces suffix.
func NamespaceName(ws *v1.PlatformWorkspace, suffix string) string {
	return ws.Name + "-" + suffix
}

// ForbiddenAnnotations a tenant may not set on its Namespaces: the ones the
// platform uses for IP pools and placement (architecture/05 §5).
var (
	forbiddenAnnotations = []any{
		"scheduler.alpha.kubernetes.io/node-selector",
		"scheduler.alpha.kubernetes.io/defaultTolerations",
		"scheduler.alpha.kubernetes.io/tolerationsWhitelist",
	}
	forbiddenAnnotationsRegex = `^cni\.projectcalico\.org/.*`
)

// Tenant is the Capsule Tenant of ws, named like it (sub-reconcilers tenant
// and rbac, IMPLEMENTATION-PLAN §4.1). It uses the fields Capsule 0.15.2
// marks deprecated but still enforces (namespaceOptions.additionalMetadata,
// forbiddenAnnotations, serviceOptions, containerRegistries); M7 moves them
// to spec.rules. Pass revoked to drop every owner but the operator and every
// RoleBinding: the first step of a delete (IMPLEMENTATION-PLAN §4.3).
func Tenant(ws *v1.PlatformWorkspace, o Options, revoked bool) *unstructured.Unstructured {
	owners := []any{}
	var bindings []any
	if !revoked {
		var ownerGroups []string
		for _, b := range ws.Spec.AccessBindings {
			switch b.Role {
			case v1.RoleTenantOwner:
				ownerGroups = append(ownerGroups, b.GroupSubject)
			case v1.RoleDeveloper:
				bindings = append(bindings, roleBinding(ClusterRoleDeveloper, b.GroupSubject))
			case v1.RoleViewer:
				bindings = append(bindings, roleBinding(ClusterRoleViewer, b.GroupSubject))
			}
		}
		sort.Strings(ownerGroups)
		for _, g := range ownerGroups {
			owners = append(owners, map[string]any{"kind": "Group", "name": g})
		}
		sort.SliceStable(bindings, func(i, j int) bool { return bindingKey(bindings[i]) < bindingKey(bindings[j]) })
	}
	owners = append(owners, map[string]any{"kind": "ServiceAccount", "name": o.OperatorServiceAccount})

	nsLabels := map[string]any{v1.LabelOrg: OrgULID(ws.Spec.OrganizationID)}
	for k, v := range ManagedLabels(ws) {
		nsLabels[k] = v
	}
	nsOptions := map[string]any{
		"additionalMetadata": map[string]any{"labels": nsLabels},
		"forbiddenAnnotations": map[string]any{
			"denied":      forbiddenAnnotations,
			"deniedRegex": forbiddenAnnotationsRegex,
		},
	}
	if ws.Spec.NamespaceQuota > 0 {
		nsOptions["quota"] = int64(ws.Spec.NamespaceQuota)
	}

	spec := map[string]any{
		"owners":            owners,
		"forceTenantPrefix": true,
		"cordoned":          ws.Spec.Cordoned,
		"namespaceOptions":  nsOptions,
		"storageClasses":    map[string]any{"allowed": strings2any(ws.Spec.StorageClasses)},
		// Services stay inside the cluster: Routes are the way out (PRODUCT-PLAN §7.6).
		"serviceOptions": map[string]any{"allowedServices": map[string]any{
			"nodePort": false, "loadBalancer": false, "externalName": false,
		}},
		"containerRegistries": map[string]any{"allowed": strings2any(o.AllowedRegistries)},
		// Every Pod states what it needs: BestEffort Pods would escape the
		// requests the Workspace's quota counts.
		"rules": []any{map[string]any{"enforce": map[string]any{
			"action":    "deny",
			"workloads": map[string]any{"qosClasses": []any{"BestEffort"}},
		}}},
	}
	if len(bindings) > 0 {
		spec["additionalRoleBindings"] = bindings
	}

	t := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	t.SetGroupVersionKind(TenantGVK)
	t.SetName(ws.Name)
	t.SetLabels(ManagedLabels(ws))
	return t
}

// Namespace is one Namespace of spec.namespaces (sub-reconciler namespaces).
func Namespace(ws *v1.PlatformWorkspace, suffix string) *unstructured.Unstructured {
	n := &unstructured.Unstructured{Object: map[string]any{}}
	n.SetGroupVersionKind(NamespaceGVK)
	n.SetName(NamespaceName(ws, suffix))
	l := ManagedLabels(ws)
	l[LabelCapsuleTenant] = ws.Name
	n.SetLabels(l)
	return n
}

func roleBinding(clusterRole, group string) any {
	return map[string]any{
		"clusterRoleName": clusterRole,
		"subjects": []any{map[string]any{
			"apiGroup": "rbac.authorization.k8s.io", "kind": "Group", "name": group,
		}},
	}
}

func bindingKey(b any) string {
	m := b.(map[string]any)
	return m["clusterRoleName"].(string) + "/" + m["subjects"].([]any)[0].(map[string]any)["name"].(string)
}

func strings2any(s []string) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
