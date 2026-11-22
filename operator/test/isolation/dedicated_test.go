//go:build lab

package isolation

import (
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

const dedicatedNode = "fdp-k8s-wk3"

type sharedPool struct {
	ID                        string         `json:"id"`
	Name                      string         `json:"name"`
	AllocationOvercommitRatio any            `json:"allocationOvercommitRatio"`
	SystemReserve             map[string]any `json:"systemReserve"`
	Nodes                     []string       `json:"nodes"`
}

// setPoolNodes gives shared-general these nodes, through the admin API.
func setPoolNodes(t *testing.T, admin *portal, nodes func([]string) []string) {
	t.Helper()
	var pools struct {
		Items []sharedPool `json:"items"`
	}
	admin.api(t, http.MethodGet, "/admin/clusters/cluster-a/shared-pools", nil, &pools)
	for _, p := range pools.Items {
		if p.ID != "shared-general" {
			continue
		}
		want := nodes(p.Nodes)
		if strings.Join(want, ",") == strings.Join(p.Nodes, ",") {
			return
		}
		var out struct {
			Operation *struct{ ID string } `json:"operation"`
		}
		if code := admin.api(t, http.MethodPut, "/admin/clusters/cluster-a/shared-pools/shared-general", map[string]any{
			"name": p.Name, "allocationOvercommitRatio": p.AllocationOvercommitRatio, "systemReserve": p.SystemReserve, "nodes": want,
		}, &out); code != http.StatusOK {
			t.Fatalf("set the nodes of shared-general: %d", code)
		}
		if out.Operation != nil {
			admin.operation(t, out.Operation.ID)
		}
		return
	}
	t.Fatal("no shared pool shared-general")
}

// The Dedicated cases of the isolation suite (PRODUCT-PLAN §24.4), set up
// through the portal as a Platform Admin and an owner would: wk3 leaves the
// shared pool and goes to a throwaway Organization, which makes a Dedicated
// Workspace on it. Everything goes back at the end, wk3 to the pool.
func TestIsolationDedicated(t *testing.T) {
	adm := adminPortal(t)
	owner := signUp(t)
	slug := "optest" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomString(4)))
	var org struct {
		Organization struct{ ID string } `json:"organization"`
	}
	if code := adm.api(t, http.MethodPost, "/admin/organizations", map[string]any{
		"slug": slug, "displayName": "Optest " + slug, "description": "Isolation suite, Dedicated cases.", "ownerEmail": owner.Email,
		"entitlement": map[string]any{"maxMembers": 5, "maxGroups": 5, "maxWorkspacesTotal": 3},
		"clusterGrant": map[string]any{
			"clusterId": "cluster-a", "maxWorkspaces": 3, "maxNamespacesTotal": 6,
			"resources":     map[string]any{"requestsCpu": "2", "requestsMemory": "4Gi", "limitsCpu": "4", "limitsMemory": "8Gi", "pods": 20, "requestsStorage": "10Gi"},
			"sharedPoolIds": []string{"shared-general"}, "networkPoolIds": []string{"onboarding-pod-network"}, "allowedStorageClasses": []string{"standard"},
		},
	}, &org); code != http.StatusCreated {
		t.Fatalf("create Organization %s: %d", slug, code)
	}
	orgID := org.Organization.ID
	// Undo in reverse order, whatever happened.
	t.Cleanup(func() {
		var users struct {
			Items []struct{ ID string } `json:"items"`
		}
		adm.api(t, http.MethodGet, "/admin/users?q="+owner.Email, nil, &users)
		if len(users.Items) == 1 {
			adm.api(t, http.MethodPost, "/admin/users/"+users.Items[0].ID+":disable", nil, nil)
		}
	})
	t.Cleanup(func() { owner.api(t, http.MethodDelete, "/organizations/"+orgID, nil, nil) })
	t.Cleanup(func() { setPoolNodes(t, adm, func(n []string) []string { return appendMissing(n, dedicatedNode) }) })
	t.Cleanup(func() {
		var out struct {
			Operations []struct{ ID string } `json:"operations"`
		}
		adm.api(t, http.MethodPut, "/admin/organizations/"+orgID+"/cluster-grants/cluster-a/dedicated-nodes", map[string]any{"nodes": []any{}}, &out)
		for _, op := range out.Operations {
			adm.operation(t, op.ID)
		}
	})

	var admins struct{ ID, MachineName string }
	waitFor(t, "group admins synced", 3*time.Minute, func() (bool, error) {
		var groups struct {
			Items []struct{ ID, Name, MachineName, SyncStatus string } `json:"items"`
		}
		owner.api(t, http.MethodGet, "/organizations/"+slug+"/groups", nil, &groups)
		for _, g := range groups.Items {
			if g.Name == "admins" && g.SyncStatus == "Synced" {
				admins.ID, admins.MachineName = g.ID, g.MachineName
				return true, nil
			}
		}
		return false, nil
	})

	setPoolNodes(t, adm, func(n []string) []string { return without(n, dedicatedNode) })
	var give struct {
		Items      []struct{ ID string } `json:"items"`
		Operations []struct{ ID string } `json:"operations"`
	}
	if code := adm.api(t, http.MethodPut, "/admin/organizations/"+orgID+"/cluster-grants/cluster-a/dedicated-nodes", map[string]any{
		"nodes": []any{map[string]any{"name": dedicatedNode, "systemReserve": map[string]any{"cpu": "500m", "memory": "1536Mi", "pods": 10}}},
	}, &give); code != http.StatusOK {
		t.Fatalf("grant %s: %d", dedicatedNode, code)
	}
	for _, op := range give.Operations {
		adm.operation(t, op.ID)
	}
	grantID := give.Items[0].ID

	var ws struct {
		Workspace struct{ ID string } `json:"workspace"`
		Operation struct{ ID string } `json:"operation"`
	}
	if code := owner.api(t, http.MethodPost, "/organizations/"+slug+"/workspaces", map[string]any{
		"slug": "water", "displayName": "water", "clusterId": "cluster-a",
		"compute":    map[string]any{"mode": "Dedicated", "dedicatedNodeGrantId": grantID},
		"resources":  map[string]any{"requestsCpu": "1", "requestsMemory": "2Gi", "pods": 10, "requestsStorage": "1Gi", "namespaceQuota": 1},
		"namespaces": []string{"app"},
		"bindings":   []any{map[string]any{"groupId": admins.ID, "role": "TenantOwner"}},
	}, &ws); code != http.StatusAccepted {
		t.Fatalf("create the Dedicated Workspace: %d", code)
	}
	t.Cleanup(func() {
		var del struct {
			Operation struct{ ID string } `json:"operation"`
		}
		if owner.api(t, http.MethodDelete, "/workspaces/"+ws.Workspace.ID+"?confirmName=water", nil, &del) == http.StatusAccepted {
			owner.operation(t, del.Operation.ID)
		}
	})
	owner.operation(t, ws.Operation.ID)
	ns := slug + "-water-app"
	tenantOwner := tenantAs(t, "oidc:optest-dedicated", "oidc:"+admins.MachineName)
	guaranteed := func(name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
		return pod(ns, name, append([]func(*corev1.Pod){func(p *corev1.Pod) {
			q := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}
			p.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: q, Limits: q}
		}}, mutate...)...)
	}
	waitFor(t, "the Namespace's NetworkPolicies", 2*time.Minute, func() (bool, error) {
		err := tenantOwner.Create(ctx, guaranteed("probe"), client.DryRunAll)
		return err == nil, err
	})

	t.Run("D1 a Dedicated Pod runs on its node only", func(t *testing.T) {
		here := guaranteed("here")
		if err := tenantOwner.Create(ctx, here); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "scheduled", 2*time.Minute, func() (bool, error) {
			err := admin.Get(ctx, client.ObjectKeyFromObject(here), here)
			return here.Spec.NodeName != "", err
		})
		if here.Spec.NodeName != dedicatedNode {
			t.Errorf("the Dedicated Pod runs on %s", here.Spec.NodeName)
		}
		t.Logf("scheduled on %s, QoS %s", here.Spec.NodeName, here.Status.QOSClass)
		refused(t, "Pod naming another node", tenantOwner.Create(ctx, guaranteed("there", func(p *corev1.Pod) { p.Spec.NodeName = "fdp-k8s-wk2" }), client.DryRunAll))
		elsewhere := guaranteed("elsewhere", func(p *corev1.Pod) {
			p.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": "fdp-k8s-wk2"}
		})
		if err := tenantOwner.Create(ctx, elsewhere); err != nil {
			t.Logf("refused as wanted: Pod selecting another node: %s", firstLine(err.Error()))
		} else {
			time.Sleep(15 * time.Second)
			_ = admin.Get(ctx, client.ObjectKeyFromObject(elsewhere), elsewhere)
			if elsewhere.Spec.NodeName != "" {
				t.Errorf("a Pod selecting wk2 runs on %s", elsewhere.Spec.NodeName)
			} else {
				t.Logf("Pod selecting wk2 stays Pending: the Namespace's node selector keeps it on %s", dedicatedNode)
			}
		}
	})

	t.Run("D2 Guaranteed QoS: requests below limits refused", func(t *testing.T) {
		refused(t, "Pod with requests below limits", tenantOwner.Create(ctx, guaranteed("burst", func(p *corev1.Pod) {
			p.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("200m")
		}), client.DryRunAll))
	})

	t.Run("D3 a node of the Organization takes no other tenant's Pod", func(t *testing.T) {
		other := workspace("optest-iso-shared", "01isoshared", "10.240.253.0/24")
		other.Spec.Network = nil
		other.Spec.Namespaces = []string{"web"}
		if err := admin.Create(ctx, other); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = admin.Delete(ctx, other); waitFor(t, "delete", 3*time.Minute, gone(other.Name)) })
		waitFor(t, "Ready", 3*time.Minute, ready(other.Name))
		stranger := tenant(t, "01isoshared")
		refused(t, "Pod tolerating the Organization's taint", stranger.Create(ctx, pod("optest-iso-shared-web", "intruder", func(p *corev1.Pod) {
			p.Spec.Tolerations = []corev1.Toleration{{Key: v1.LabelDedicatedOrg, Operator: corev1.TolerationOpExists}}
		}), client.DryRunAll))
		refused(t, "Pod naming the dedicated node", stranger.Create(ctx, pod("optest-iso-shared-web", "intruder2", func(p *corev1.Pod) {
			p.Spec.NodeName = dedicatedNode
		}), client.DryRunAll))
	})

	t.Run("D4 reservations stay within the node", func(t *testing.T) {
		var out map[string]any
		code := owner.api(t, http.MethodPost, "/organizations/"+slug+"/workspaces", map[string]any{
			"slug": "earth", "displayName": "earth", "clusterId": "cluster-a",
			"compute":    map[string]any{"mode": "Dedicated", "dedicatedNodeGrantId": grantID},
			"resources":  map[string]any{"requestsCpu": "2", "requestsMemory": "2Gi", "pods": 10, "requestsStorage": "1Gi", "namespaceQuota": 1},
			"namespaces": []string{"app"},
			"bindings":   []any{map[string]any{"groupId": admins.ID, "role": "TenantOwner"}},
		}, &out)
		if code != http.StatusConflict || out["code"] != "DEDICATED_NODE_INSUFFICIENT_CAPACITY" {
			t.Errorf("a Workspace beyond the node's capacity: %d %v", code, out["code"])
		} else {
			t.Logf("refused as wanted (409): %v", out["message"])
		}
	})
}

// tenantAs impersonates an oidc: user with the given groups.
func tenantAs(t *testing.T, user string, groups ...string) client.Client {
	t.Helper()
	c := rest.CopyConfig(cfg)
	c.Impersonate = rest.ImpersonationConfig{UserName: user, Groups: append(groups, "oidc:tenantry:tenant-users", "system:authenticated")}
	cl, err := client.New(c, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

func without(s []string, x string) []string {
	var out []string
	for _, v := range s {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func appendMissing(s []string, x string) []string {
	for _, v := range s {
		if v == x {
			return s
		}
	}
	return append(s, x)
}
