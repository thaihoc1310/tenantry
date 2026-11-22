package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

func getNode(t *testing.T, name string) *corev1.Node {
	t.Helper()
	n := &corev1.Node{}
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dedicatedTaint(n *corev1.Node) string {
	for _, t := range n.Spec.Taints {
		if t.Key == v1.LabelDedicatedOrg {
			return t.Value + ":" + string(t.Effect)
		}
	}
	return ""
}

func grantCondition(t *testing.T, name string) metav1.Condition {
	t.Helper()
	g := &v1.PlatformNodeGrant{}
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, g); err != nil {
		t.Fatal(err)
	}
	if c := meta.FindStatusCondition(g.Status.Conditions, v1.ConditionReady); c != nil {
		return *c
	}
	return metav1.Condition{Status: "absent"}
}

// A node is given only when fit and clean (architecture/04 §5.1), and given
// back on revoke.
func TestNodeGrant(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := &PlatformNodeGrantReconciler{Client: testClient, APIReader: testClient}
	for name, labels := range map[string]map[string]string{
		"ded-pool":  {v1.LabelComputePool: "shared-x"},
		"ded-clean": nil,
		"ded-busy":  nil,
		"ded-other": {v1.LabelDedicatedOrg: "org-other"},
		"ded-cp":    {labelControlPlane: ""},
	} {
		if err := createReadyNode(name, labels); err != nil {
			t.Fatal(err)
		}
	}
	// A tenant Pod still running on ded-busy.
	if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "acme-old-web", Labels: map[string]string{render.LabelCapsuleTenant: "acme-old"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "acme-old-web"},
		Spec:       corev1.PodSpec{NodeName: "ded-busy", Containers: []corev1.Container{{Name: "c", Image: "nginx"}}},
	}); err != nil {
		t.Fatal(err)
	}

	for node, want := range map[string]string{
		"ded-pool":  v1.ReasonNodeNotEligible,
		"ded-other": v1.ReasonNodeNotEligible,
		"ded-cp":    v1.ReasonNodeNotEligible,
		"ded-busy":  v1.ReasonNodeNotClean,
		"ghost":     v1.ReasonNodeNotReady,
		"ded-clean": "Dedicated",
	} {
		name := "grant-" + node
		if err := testClient.Create(ctx, &v1.PlatformNodeGrant{
			ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: v1.PlatformNodeGrantSpec{NodeName: node, OrganizationID: "org-01jabc"},
		}); err != nil {
			t.Fatal(err)
		}
		reconcileOnce(t, r, name)
		if c := grantCondition(t, name); c.Reason != want {
			t.Errorf("%s: %+v, want reason %s", node, c, want)
		}
	}
	if c := grantCondition(t, "grant-ded-busy"); !strings.Contains(c.Message, "acme-old-web/web") {
		t.Errorf("NodeNotClean does not name the Pod: %s", c.Message)
	}
	for _, node := range []string{"ded-pool", "ded-busy", "ded-cp"} {
		if n := getNode(t, node); n.Labels[v1.LabelDedicatedOrg] != "" || dedicatedTaint(n) != "" {
			t.Errorf("%s was given although refused", node)
		}
	}
	if n := getNode(t, "ded-other"); n.Labels[v1.LabelDedicatedOrg] != "org-other" {
		t.Error("another Organization's node was relabelled")
	}
	clean := getNode(t, "ded-clean")
	if clean.Labels[v1.LabelDedicatedOrg] != "org-01jabc" || dedicatedTaint(clean) != "org-01jabc:NoSchedule" {
		t.Fatalf("ded-clean: labels %v taints %v", clean.Labels, clean.Spec.Taints)
	}

	// Someone removes the taint: it comes back. The node is cordoned: the
	// grant stays, not Ready.
	clean.Spec.Taints = nil
	clean.Spec.Unschedulable = true
	if err := testClient.Update(ctx, clean); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "grant-ded-clean")
	if n := getNode(t, "ded-clean"); dedicatedTaint(n) != "org-01jabc:NoSchedule" {
		t.Error("taint not restored")
	}
	if c := grantCondition(t, "grant-ded-clean"); c.Reason != v1.ReasonNodeNotReady || !strings.Contains(c.Message, "cordoned") {
		t.Errorf("cordoned node: %+v", c)
	}

	// Revoke: label and taint go, other taints stay.
	n := getNode(t, "ded-clean")
	n.Spec.Taints = append(n.Spec.Taints, corev1.Taint{Key: "example.com/keep", Effect: corev1.TaintEffectNoSchedule})
	if err := testClient.Update(ctx, n); err != nil {
		t.Fatal(err)
	}
	g := &v1.PlatformNodeGrant{}
	if err := testClient.Get(ctx, client.ObjectKey{Name: "grant-ded-clean"}, g); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Delete(ctx, g); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "grant-ded-clean")
	n = getNode(t, "ded-clean")
	if n.Labels[v1.LabelDedicatedOrg] != "" || dedicatedTaint(n) != "" || len(n.Spec.Taints) != 1 {
		t.Errorf("after revoke: labels %v taints %v", n.Labels, n.Spec.Taints)
	}
}

// A Dedicated Workspace: membership label on its granted node, the Tenant's
// node selector and toleration annotations, Guaranteed LimitRange; the
// finalizer takes only its own label off.
func TestWorkspaceDedicated(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	if err := createReadyNode("ded-water", map[string]string{v1.LabelDedicatedOrg: "org-01jabc", "workspaces.tenantry.io/ws-green": "true"}); err != nil {
		t.Fatal(err)
	}
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-water2")
	ws.Spec.Namespaces = nil
	ws.Spec.Compute = v1.Compute{Mode: v1.ComputeDedicated, DedicatedNodeGrantID: "nodegrant-1", NodeName: "ded-water"}
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-water2")
	capsuleReconciles(t, "acme-water2")
	reconcileOnce(t, r, "acme-water2")
	ws = getWorkspace(t, "acme-water2")
	if c := condition(ws, v1.ConditionComputeReady); c.Status != metav1.ConditionTrue || ws.Status.Phase != v1.PhaseReady {
		t.Errorf("ComputeReady %+v, phase %s", c, ws.Status.Phase)
	}
	if got := getNode(t, "ded-water").Labels["workspaces.tenantry.io/ws-acme-water2"]; got != "true" {
		t.Errorf("membership label %q", got)
	}
	tenant := getTenant(t, "acme-water2")
	if sel, _, _ := unstructured.NestedStringMap(tenant.Object, "spec", "nodeSelector"); sel["workspaces.tenantry.io/ws-acme-water2"] != "true" || len(sel) != 1 {
		t.Errorf("Tenant nodeSelector %v", sel)
	}
	rep := &unstructured.Unstructured{}
	rep.SetGroupVersionKind(render.ReplicationGVK)
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-water2"}, rep); err != nil {
		t.Fatal(err)
	}
	items, _, _ := unstructured.NestedSlice(rep.Object, "spec", "resources")
	if !strings.Contains(fmt.Sprint(items), "maxLimitRequestRatio") {
		t.Errorf("Dedicated LimitRange lacks maxLimitRequestRatio: %v", items)
	}

	// The node loses its grant: ComputeReady fails, the Workspace degrades.
	n := getNode(t, "ded-water")
	delete(n.Labels, v1.LabelDedicatedOrg)
	if err := testClient.Update(ctx, n); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-water2")
	ws = getWorkspace(t, "acme-water2")
	if c := condition(ws, v1.ConditionComputeReady); c.Reason != subreconciler.ReasonNodeNotGranted || ws.Status.Phase != v1.PhaseDegraded {
		t.Errorf("without the grant: %+v, phase %s", c, ws.Status.Phase)
	}
	n = getNode(t, "ded-water")
	n.Labels[v1.LabelDedicatedOrg] = "org-01jabc"
	if err := testClient.Update(ctx, n); err != nil {
		t.Fatal(err)
	}

	// Delete: step 6 takes this Workspace's label off, the rest stays.
	if err := testClient.Delete(ctx, getWorkspace(t, "acme-water2")); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-water2") // 1. rights
	capsuleReconciles(t, "acme-water2")
	reconcileOnce(t, r, "acme-water2") // 3. no Namespace; 4. replication
	reconcileOnce(t, r, "acme-water2") // 4. Tenant
	reconcileOnce(t, r, "acme-water2") // 6. membership label, 7. finalizer
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-water2"}, &v1.PlatformWorkspace{}); err == nil {
		t.Fatal("PlatformWorkspace still there")
	}
	labels := getNode(t, "ded-water").Labels
	if _, still := labels["workspaces.tenantry.io/ws-acme-water2"]; still {
		t.Error("membership label left on the node")
	}
	if labels[v1.LabelDedicatedOrg] != "org-01jabc" || labels["workspaces.tenantry.io/ws-green"] != "true" {
		t.Errorf("the finalizer touched other labels: %v", labels)
	}
}
