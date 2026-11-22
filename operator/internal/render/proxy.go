package render

import (
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

var ProxySettingGVK = schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta1", Kind: "ProxySetting"}

// ProxySettingName is the operator's ProxySetting in a Workspace.
const ProxySettingName = "tenantry"

// ProxySetting lets the Workspace's Developer and Viewer groups, and the
// probe of sub-reconciler proxyaccess, list its Namespaces through Capsule
// Proxy (owners already can). It grants nothing else: what they may do
// inside stays with RBAC. One per Workspace, in namespace, one of its own.
func ProxySetting(ws *v1.PlatformWorkspace, namespace, probe string) *unstructured.Unstructured {
	var groups []string
	for _, b := range ws.Spec.AccessBindings {
		if b.Role == v1.RoleDeveloper || b.Role == v1.RoleViewer {
			groups = append(groups, b.GroupSubject)
		}
	}
	sort.Strings(groups)
	subjects := []any{}
	for _, g := range groups {
		subjects = append(subjects, map[string]any{"kind": "Group", "name": g})
	}
	subjects = append(subjects, map[string]any{"kind": "ServiceAccount", "name": probe})
	s := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"subjects": subjects}}}
	s.SetGroupVersionKind(ProxySettingGVK)
	s.SetName(ProxySettingName)
	s.SetNamespace(namespace)
	s.SetLabels(ManagedLabels(ws))
	return s
}
