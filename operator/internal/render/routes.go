package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

var IngressGVK = schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}

const (
	// IngressClass is the shared Traefik of the cluster (architecture/05 §7).
	IngressClass = "platform-shared"
	// LabelRoute names the Route an Ingress was rendered from.
	LabelRoute = "tenantry.io/route"
)

// Ingress is the Ingress of one Route (sub-reconciler routes, PRODUCT-PLAN
// §7.6), named like it in the Route's Namespace. No tls section: Traefik
// answers with its default certificate, the apps domain's wildcard, so no
// private key ever sits in a tenant's Namespace.
func Ingress(ws *v1.PlatformWorkspace, r v1.WorkspaceRoute) *unstructured.Unstructured {
	ing := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
		"ingressClassName": IngressClass,
		"rules": []any{map[string]any{
			"host": r.Host,
			"http": map[string]any{"paths": []any{map[string]any{
				"path": "/", "pathType": "Prefix",
				"backend": map[string]any{"service": map[string]any{
					"name": r.Service, "port": map[string]any{"number": int64(r.Port)},
				}},
			}}},
		}},
	}}}
	ing.SetGroupVersionKind(IngressGVK)
	ing.SetName(r.Name)
	ing.SetNamespace(r.Namespace)
	l := ManagedLabels(ws)
	l[LabelRoute] = r.Name
	ing.SetLabels(l)
	return ing
}
