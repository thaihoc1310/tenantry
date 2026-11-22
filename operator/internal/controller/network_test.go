package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

func getIPPool(t *testing.T, name string) (*unstructured.Unstructured, error) {
	t.Helper()
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(render.IPPoolGVK)
	return pool, testClient.Get(context.Background(), client.ObjectKey{Name: name}, pool)
}

// ipamBlock plays Calico: a block of cidr with used addresses given out.
func ipamBlock(t *testing.T, name, cidr string, used int) *unstructured.Unstructured {
	t.Helper()
	allocations := make([]any, 4)
	for i := range used {
		allocations[i] = int64(i)
	}
	b := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
		"cidr": cidr, "allocations": allocations, "attributes": []any{}, "strictAffinity": false, "unallocated": []any{},
	}}}
	b.SetGroupVersionKind(render.IPAMBlockGVK)
	b.SetName(name)
	return b
}

// A Workspace with a network: its IPPool, NetworkReady among the conditions
// it needs, the Namespace annotation and the baseline policies; on delete
// the pool goes only once Calico gives out none of its addresses, and the
// Workspace waits for the blocks to go too (IMPLEMENTATION-PLAN §4.3 step 5).
func TestWorkspaceNetwork(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-net")
	ws.Spec.Namespaces = nil
	ws.Spec.Network = &v1.Network{AllocationRef: "netalloc-1", CIDR: "10.240.70.0/24"}
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-net")
	capsuleReconciles(t, "acme-net")
	reconcileOnce(t, r, "acme-net")
	ws = getWorkspace(t, "acme-net")
	if c := condition(ws, v1.ConditionNetworkReady); c.Status != metav1.ConditionTrue || ws.Status.Phase != v1.PhaseReady {
		t.Errorf("NetworkReady %+v, phase %s", c, ws.Status.Phase)
	}
	pool, err := getIPPool(t, "acme-net")
	if err != nil {
		t.Fatal(err)
	}
	spec, _, _ := unstructured.NestedMap(pool.Object, "spec")
	if spec["cidr"] != "10.240.70.0/24" || spec["assignmentMode"] != "Manual" || spec["nodeSelector"] != "tenantry.io/compute-pool == '"+testPool+"'" {
		t.Errorf("IPPool spec %v", spec)
	}
	if len(pool.GetOwnerReferences()) != 1 {
		t.Error("IPPool has no owner reference")
	}
	tenant := getTenant(t, "acme-net")
	if !strings.Contains(strings.Join(tenantRuleStrings(tenant), " "), `managed:["acme-net"]`) {
		t.Errorf("Tenant rules lack the ipv4pools managed value: %v", tenantRuleStrings(tenant))
	}

	// Someone changes the pool: it is put back.
	pool.Object["spec"].(map[string]any)["assignmentMode"] = "Automatic"
	if err := testClient.Update(ctx, pool); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-net")
	if pool, _ = getIPPool(t, "acme-net"); pool.Object["spec"].(map[string]any)["assignmentMode"] != "Manual" {
		t.Error("IPPool drift not restored")
	}

	// Delete: a Pod's address still given out holds the pool.
	if err := testClient.Create(ctx, ipamBlock(t, "10-240-70-0-26", "10.240.70.0/26", 1)); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, ipamBlock(t, "10-240-71-0-26", "10.240.71.0/26", 2)); err != nil { // another CIDR
		t.Fatal(err)
	}
	if err := testClient.Delete(ctx, getWorkspace(t, "acme-net")); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-net") // 1. rights
	capsuleReconciles(t, "acme-net")
	reconcileOnce(t, r, "acme-net") // 4. replication
	reconcileOnce(t, r, "acme-net") // 4. Tenant
	reconcileOnce(t, r, "acme-net") // 5. addresses in use
	if c := condition(getWorkspace(t, "acme-net"), v1.ConditionDeleting); !strings.Contains(c.Message, "10.240.70.0/26 (1 addresses)") ||
		strings.Contains(c.Message, "10.240.71") {
		t.Errorf("Deleting %+v", c)
	}
	if _, err := getIPPool(t, "acme-net"); err != nil {
		t.Fatal("IPPool deleted while an address is in use")
	}
	// Calico frees the address: the pool goes, the block is still there.
	if err := testClient.Delete(ctx, ipamBlock(t, "10-240-70-0-26", "", 0)); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, ipamBlock(t, "10-240-70-0-26", "10.240.70.0/26", 0)); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-net")
	if _, err := getIPPool(t, "acme-net"); err == nil {
		t.Fatal("IPPool not deleted")
	}
	reconcileOnce(t, r, "acme-net")
	if c := condition(getWorkspace(t, "acme-net"), v1.ConditionDeleting); !strings.Contains(c.Message, "release the blocks of 10.240.70.0/24: 10-240-70-0-26") {
		t.Errorf("Deleting %+v", c)
	}
	// The block goes: done.
	if err := testClient.Delete(ctx, ipamBlock(t, "10-240-70-0-26", "", 0)); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-net")
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-net"}, &v1.PlatformWorkspace{}); err == nil {
		t.Fatal("PlatformWorkspace still there")
	}
}

// tenantRuleStrings flattens the Tenant's metadata rules to key:value words.
func tenantRuleStrings(tenant *unstructured.Unstructured) []string {
	var out []string
	rules, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "rules")
	for _, raw := range rules {
		md, _, _ := unstructured.NestedSlice(raw.(map[string]any), "enforce", "metadata")
		for _, m := range md {
			ann, _, _ := unstructured.NestedMap(m.(map[string]any), "annotations")
			if v, found := ann[render.AnnotationIPv4Pools].(map[string]any); found {
				for k, val := range v {
					s, _ := val.(string)
					out = append(out, k+":"+s)
				}
			}
		}
	}
	return out
}
