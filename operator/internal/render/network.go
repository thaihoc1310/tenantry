package render

import (
	"net/netip"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

var (
	// IPPoolGVK goes through the Calico API server: deleting a pool there
	// releases its blocks (IMPLEMENTATION-PLAN §4.3 step 5).
	IPPoolGVK = schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "IPPool"}
	// IPAMBlockGVK is Calico's record of the addresses it gave out, read only.
	IPAMBlockGVK = schema.GroupVersionKind{Group: "crd.projectcalico.org", Version: "v1", Kind: "IPAMBlock"}
)

// AnnotationIPv4Pools picks the Calico pools of a Namespace's Pods.
const AnnotationIPv4Pools = "cni.projectcalico.org/ipv4pools"

// LabelNetwork marks the Namespaces of a Workspace with a network: the
// policy tenantry-tenant-network-ready holds their Pods until the baseline
// NetworkPolicies are there, and a policy binding selects by label only.
const LabelNetwork = "tenantry.io/network"

// blockSize is how much of a Workspace's CIDR a node claims at a time: four
// blocks in the default /24 (architecture/05 §5).
const blockSize = 26

// IPPool is the Calico pool of ws's CIDR, named like it (sub-reconciler
// network, PRODUCT-PLAN §7.3): Manual, so only Namespaces that name it get
// its addresses; held to the Workspace's nodes like its Pods.
func IPPool(ws *v1.PlatformWorkspace) *unstructured.Unstructured {
	size := int64(blockSize)
	// A CIDR smaller than a block (/27, /28) is one block of its own size.
	if p, err := netip.ParsePrefix(ws.Spec.Network.CIDR); err == nil && p.Bits() > blockSize {
		size = int64(p.Bits())
	}
	pool := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
		"cidr":           ws.Spec.Network.CIDR,
		"blockSize":      size,
		"assignmentMode": "Manual",
		"allowedUses":    []any{"Workload"},
		"ipipMode":       "Never",
		"vxlanMode":      "CrossSubnet",
		"natOutgoing":    true,
		"nodeSelector":   nodeSelectorKey(ws) + " == '" + nodeSelectorValue(ws) + "'",
	}}}
	pool.SetGroupVersionKind(IPPoolGVK)
	pool.SetName(ws.Name)
	pool.SetLabels(ManagedLabels(ws))
	return pool
}

// networkPolicies are the baseline of every Namespace of a Workspace with a
// network (architecture/05 §6): nothing in or out but the Workspace itself,
// DNS, Traefik, and the Internet without private ranges and the metadata
// endpoint. Selectors use only labels tenants cannot set: Capsule's tenant
// label and Kubernetes' namespace name.
func networkPolicies(ws *v1.PlatformWorkspace) []any {
	sameWorkspace := []any{map[string]any{"namespaceSelector": map[string]any{
		"matchLabels": map[string]any{LabelCapsuleTenant: ws.Name},
	}}}
	namespace := func(name string) map[string]any {
		return map[string]any{"matchLabels": map[string]any{"kubernetes.io/metadata.name": name}}
	}
	policy := func(name string, spec map[string]any) any {
		spec["podSelector"] = map[string]any{}
		return map[string]any{
			"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
			"metadata": map[string]any{"name": name},
			"spec":     spec,
		}
	}
	return []any{
		policy("tenant-default-deny", map[string]any{"policyTypes": []any{"Ingress", "Egress"}}),
		policy("tenant-allow-same-workspace", map[string]any{
			"policyTypes": []any{"Ingress", "Egress"},
			"ingress":     []any{map[string]any{"from": sameWorkspace}},
			"egress":      []any{map[string]any{"to": sameWorkspace}},
		}),
		policy("tenant-allow-dns", map[string]any{
			"policyTypes": []any{"Egress"},
			"egress": []any{map[string]any{
				"to": []any{map[string]any{
					"namespaceSelector": namespace("kube-system"),
					"podSelector":       map[string]any{"matchLabels": map[string]any{"k8s-app": "kube-dns"}},
				}},
				"ports": []any{
					map[string]any{"protocol": "UDP", "port": int64(53)},
					map[string]any{"protocol": "TCP", "port": int64(53)},
				},
			}},
		}),
		// Traefik runs with hostPort, so its packets carry its Pod address.
		policy("tenant-allow-ingress-controller", map[string]any{
			"policyTypes": []any{"Ingress"},
			"ingress":     []any{map[string]any{"from": []any{map[string]any{"namespaceSelector": namespace("ingress-system")}}}},
		}),
		policy("tenant-allow-internet-egress", map[string]any{
			"policyTypes": []any{"Egress"},
			"egress": []any{map[string]any{"to": []any{map[string]any{"ipBlock": map[string]any{
				"cidr":   "0.0.0.0/0",
				"except": []any{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.169.254/32"},
			}}}}},
		}),
	}
}
