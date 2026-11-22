// Package render turns a PlatformWorkspace into the objects it should have
// in the cluster (IMPLEMENTATION-PLAN §4.2). Pure functions: no client, no
// cluster, so golden files can pin the output. Capsule's Go module needs a
// newer Go than ours, hence unstructured objects.
package render

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

var (
	TenantGVK    = schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta2", Kind: "Tenant"}
	NamespaceGVK = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	// ReplicationGVK copies objects into a Tenant's Namespaces.
	ReplicationGVK = schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta2", Kind: "GlobalTenantResource"}
	// QuotaGVK is what Capsule makes of a quota rule.
	QuotaGVK = schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta2", Kind: "GlobalResourceQuota"}
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

// MembershipLabel puts a Dedicated Workspace's Pods on its node.
func MembershipLabel(ws *v1.PlatformWorkspace) string {
	return v1.WorkspaceMemberLabelPrefix + ws.Spec.WorkspaceID
}

func nodeSelectorKey(ws *v1.PlatformWorkspace) string {
	if ws.Spec.Compute.Mode == v1.ComputeDedicated {
		return MembershipLabel(ws)
	}
	return v1.LabelComputePool
}

func nodeSelectorValue(ws *v1.PlatformWorkspace) string {
	if ws.Spec.Compute.Mode == v1.ComputeDedicated {
		return "true"
	}
	return ws.Spec.Compute.SharedPoolID
}

// Annotations of PodNodeSelector and PodTolerationRestriction
// (architecture/05 §2).
const (
	AnnotationNodeSelector         = "scheduler.alpha.kubernetes.io/node-selector"
	AnnotationDefaultTolerations   = "scheduler.alpha.kubernetes.io/defaultTolerations"
	AnnotationTolerationsWhitelist = "scheduler.alpha.kubernetes.io/tolerationsWhitelist"
)

// conditionTaints are the node.kubernetes.io taints Kubernetes itself puts
// on nodes and tolerates in Pods: a whitelist without them refuses every Pod.
var conditionTaints = []string{
	"not-ready", "unreachable", "disk-pressure", "memory-pressure", "pid-pressure", "network-unavailable", "unschedulable",
}

// NamespaceAnnotations hold a Namespace to the Workspace's nodes: the node
// selector, and the tolerations its Pods may carry. A Dedicated Workspace's
// Pods tolerate their Organization's taint by default; nothing else can be
// tolerated in either mode. With a network, the Calico pool too.
func NamespaceAnnotations(ws *v1.PlatformWorkspace) map[string]string {
	type toleration struct {
		Key      string `json:"key"`
		Operator string `json:"operator"`
		Value    string `json:"value,omitempty"`
		Effect   string `json:"effect,omitempty"`
	}
	var defaults, whitelist []toleration
	if ws.Spec.Compute.Mode == v1.ComputeDedicated {
		org := toleration{Key: v1.LabelDedicatedOrg, Operator: "Equal", Value: ws.Spec.OrganizationID, Effect: "NoSchedule"}
		defaults, whitelist = append(defaults, org), append(whitelist, org)
	}
	for _, t := range conditionTaints {
		whitelist = append(whitelist, toleration{Key: "node.kubernetes.io/" + t, Operator: "Exists"})
	}
	defaultsJSON, _ := json.Marshal(defaults)
	if defaults == nil {
		defaultsJSON = []byte("[]")
	}
	whitelistJSON, _ := json.Marshal(whitelist)
	a := map[string]string{
		AnnotationNodeSelector:         nodeSelectorKey(ws) + "=" + nodeSelectorValue(ws),
		AnnotationDefaultTolerations:   string(defaultsJSON),
		AnnotationTolerationsWhitelist: string(whitelistJSON),
	}
	// Sub-reconciler network: Pods take their addresses from the
	// Workspace's IPPool.
	if ws.Spec.Network != nil {
		poolJSON, _ := json.Marshal([]string{ws.Name})
		a[AnnotationIPv4Pools] = string(poolJSON)
	}
	return a
}

// anyValue matches every value of a metadata key.
var anyValue = map[string]any{"values": []any{map[string]any{"exp": ".*"}}}

// Tenant is the Capsule Tenant of ws, named like it: sub-reconcilers tenant,
// compute, rbac and policy write their parts of this one object
// (IMPLEMENTATION-PLAN §4.1). Everything is spec.rules, the API of Capsule
// 0.15, but the fields rules do not replace (owners, nodeSelector, storage
// classes, Namespace quota). Pass revoked to drop every owner but the
// operator and every RoleBinding: the first step of a delete
// (IMPLEMENTATION-PLAN §4.3).
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

	nsOptions := map[string]any{}
	if ws.Spec.NamespaceQuota > 0 {
		nsOptions["quota"] = int64(ws.Spec.NamespaceQuota)
	}

	// Calico pool annotations would pick another Workspace's addresses
	// (architecture/05 §5): ipv4pools is a managed value once the Workspace
	// has a network, refused before; ipv6pools always.
	deniedPools := map[string]any{"cni.projectcalico.org/ipv6pools": anyValue}
	if ws.Spec.Network == nil {
		deniedPools[AnnotationIPv4Pools] = anyValue
	}
	rules := []any{
		// Every Namespace of the Tenant, whoever creates it, carries these
		// labels and annotations, and nobody else can change them (PRODUCT-PLAN
		// §18): Capsule puts the managed value back.
		map[string]any{"enforce": map[string]any{
			"action": "allow",
			"metadata": []any{map[string]any{
				"apiGroups": []any{"v1"}, "kinds": []any{"Namespace"},
				"labels":      managedValues(NamespaceLabels(ws)),
				"annotations": managedValues(NamespaceAnnotations(ws)),
			}},
		}},
		map[string]any{"enforce": map[string]any{
			"action": "deny",
			"metadata": []any{map[string]any{
				"apiGroups": []any{"v1"}, "kinds": []any{"Namespace"},
				"annotations": deniedPools,
			}},
		}},
		map[string]any{"enforce": map[string]any{
			"action":    "allow",
			"workloads": map[string]any{"registries": registryMatchers(o.AllowedRegistries)},
		}},
		// Every Pod states what it needs (the LimitRange fills in defaults):
		// BestEffort Pods would escape the requests the quota counts.
		map[string]any{"enforce": map[string]any{
			"action":    "deny",
			"workloads": map[string]any{"qosClasses": []any{"BestEffort"}},
		}},
		// Services stay inside the cluster: Routes are the way out (PRODUCT-PLAN §7.6).
		map[string]any{"enforce": map[string]any{
			"action":   "deny",
			"services": map[string]any{"types": []any{"NodePort", "LoadBalancer", "ExternalName"}},
		}},
	}
	// Sub-reconciler policy: one quota shared by every Namespace, checked
	// when a Pod is admitted (PRODUCT-PLAN §5.4).
	if len(ws.Spec.Resources) > 0 {
		hard := map[string]any{}
		for k, q := range ws.Spec.Resources {
			hard[k] = q.String()
		}
		rules = append(rules, map[string]any{"quota": []any{map[string]any{"name": QuotaName, "hard": hard}}})
	}

	spec := map[string]any{
		"owners":            owners,
		"forceTenantPrefix": true,
		"cordoned":          ws.Spec.Cordoned,
		"namespaceOptions":  nsOptions,
		"storageClasses":    map[string]any{"allowed": strings2any(ws.Spec.StorageClasses)},
		"rules":             rules,
		// Sub-reconciler compute: Pods of the Workspace stay on its nodes.
		"nodeSelector": map[string]any{nodeSelectorKey(ws): nodeSelectorValue(ws)},
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

// NamespaceLabels go on every Namespace of the Tenant: the managed labels,
// the Organization's ULID for the RoleBinding VAP, and the Pod Security
// level (sub-reconciler policy): baseline enforced, restricted reported.
func NamespaceLabels(ws *v1.PlatformWorkspace) map[string]string {
	l := ManagedLabels(ws)
	l[v1.LabelOrg] = OrgULID(ws.Spec.OrganizationID)
	l["pod-security.kubernetes.io/enforce"] = "baseline"
	l["pod-security.kubernetes.io/warn"] = "restricted"
	l["pod-security.kubernetes.io/audit"] = "restricted"
	return l
}

func managedValues(l map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range l {
		out[k] = map[string]any{"managed": v}
	}
	return out
}

// QuotaName names the Workspace's quota inside its Tenant; Capsule turns it
// into the GlobalResourceQuota <tenant>-<QuotaName>.
const QuotaName = "workspace"

// QuotaObjectName is the GlobalResourceQuota Capsule makes from the quota.
func QuotaObjectName(ws *v1.PlatformWorkspace) string { return ws.Name + "-" + QuotaName }

// LimitRangeName is the LimitRange every Namespace of a Tenant gets.
const LimitRangeName = "tenantry-defaults"

// Defaults of a container that states no resources: it becomes Burstable
// and counts against the quota.
var (
	defaultRequest = map[string]any{"cpu": "100m", "memory": "128Mi"}
	defaultLimit   = map[string]any{"cpu": "500m", "memory": "512Mi"}
)

// Replication is the GlobalTenantResource that copies the LimitRange into
// every Namespace of the Tenant, kubectl-made ones included (sub-reconciler
// policy), and with a network the baseline NetworkPolicies (sub-reconciler
// network). Capsule protects the copies from the tenant and puts back
// deleted ones every resyncPeriod (60s by default). Cluster-scoped like the
// PlatformWorkspace that owns it, named like it.
func Replication(ws *v1.PlatformWorkspace) *unstructured.Unstructured {
	limit := map[string]any{"type": "Container", "defaultRequest": defaultRequest, "default": defaultLimit}
	if ws.Spec.Compute.Mode == v1.ComputeDedicated {
		// Guaranteed QoS: limits = requests in every container, so what the
		// node reserved is what Pods may use (PRODUCT-PLAN §5.4).
		limit = map[string]any{
			"type": "Container", "defaultRequest": defaultRequest, "default": defaultRequest,
			"maxLimitRequestRatio": map[string]any{"cpu": "1", "memory": "1"},
		}
	}
	items := []any{map[string]any{
		"apiVersion": "v1", "kind": "LimitRange",
		"metadata": map[string]any{"name": LimitRangeName},
		"spec":     map[string]any{"limits": []any{limit}},
	}}
	if ws.Spec.Network != nil {
		items = append(items, networkPolicies(ws)...)
	}
	r := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
		"scope":           "Namespace",
		"pruningOnDelete": true,
		"tenantSelector":  map[string]any{"matchLabels": map[string]any{v1.LabelWorkspaceID: ws.Spec.WorkspaceID}},
		"resources":       []any{map[string]any{"rawItems": items}},
	}}}
	r.SetGroupVersionKind(ReplicationGVK)
	r.SetName(ws.Name)
	r.SetLabels(ManagedLabels(ws))
	return r
}

// registryMatchers allow images from the given registries. Capsule matches
// the image reference as written, without Docker's normalization, so
// nginx:1.27 and bitnami/redis (Docker Hub short names: no registry host in
// front) need matchers of their own when docker.io is allowed; checked on
// Capsule 0.15.2 of the lab.
func registryMatchers(registries []string) []any {
	hosts := make([]string, 0, len(registries))
	dockerHub := false
	for _, r := range registries {
		hosts = append(hosts, regexp.QuoteMeta(r))
		dockerHub = dockerHub || r == "docker.io"
	}
	out := []any{map[string]any{"exp": "^(" + strings.Join(hosts, "|") + ")/.+"}}
	if dockerHub {
		out = append(out,
			map[string]any{"exp": `^[^/]+$`},         // nginx:1.27
			map[string]any{"exp": `^[^./:]+/[^/]+$`}, // bitnami/redis:7
		)
	}
	return out
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
