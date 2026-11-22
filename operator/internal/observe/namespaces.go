// Package observe turns what the cluster holds into PlatformWorkspace status
// (IMPLEMENTATION-PLAN §4.4).
package observe

import (
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Namespaces is status.namespaces: every Namespace of the Tenant, sorted by
// name, source portal when spec.namespaces asked for it and kubectl
// otherwise. Usage comes with the quota (M7).
func Namespaces(ws *v1.PlatformWorkspace, existing []corev1.Namespace) []v1.NamespaceStatus {
	var out []v1.NamespaceStatus
	for _, ns := range existing {
		src := v1.SourceKubectl
		if slices.ContainsFunc(ws.Spec.Namespaces, func(s string) bool { return render.NamespaceName(ws, s) == ns.Name }) {
			src = v1.SourcePortal
		}
		out = append(out, v1.NamespaceStatus{Name: ns.Name, Source: src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
