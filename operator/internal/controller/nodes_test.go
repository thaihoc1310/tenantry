package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

func createNode(t *testing.T, name string, labels map[string]string, cpu string) {
	t.Helper()
	ctx := context.Background()
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	if err := testClient.Create(ctx, n); err != nil {
		t.Fatal(err)
	}
	n.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse("8Gi"), corev1.ResourcePods: resource.MustParse("110"),
	}
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
	if err := testClient.Status().Update(ctx, n); err != nil {
		t.Fatal(err)
	}
}

func nodeLabel(t *testing.T, name string) string {
	t.Helper()
	n := &corev1.Node{}
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, n); err != nil {
		t.Fatal(err)
	}
	return n.Labels[v1.LabelComputePool]
}

// eventually retries check for a few seconds: the manager reacts to watches.
func eventually(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type failure string

func (f failure) Error() string { return string(f) }

// The three small CRDs, through a running manager so their watches on
// Nodes are exercised too (IMPLEMENTATION-PLAN §4.5).
func TestNodeCRDs(t *testing.T) {
	needEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr, err := ctrl.NewManager(testCfg, ctrl.Options{Scheme: testScheme, Metrics: metricsserver.Options{BindAddress: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, setup := range []interface{ SetupWithManager(ctrl.Manager) error }{
		&PlatformSharedPoolReconciler{Client: mgr.GetClient()},
		&PlatformNodeGrantReconciler{Client: mgr.GetClient()},
		&PlatformNodeInventoryReconciler{Client: mgr.GetClient()},
	} {
		if err := setup.SetupWithManager(mgr); err != nil {
			t.Fatal(err)
		}
	}
	go func() { _ = mgr.Start(ctx) }()

	createNode(t, "cp1", map[string]string{labelControlPlane: ""}, "4")
	createNode(t, "wk1", map[string]string{v1.LabelComputePool: v1.SystemPool}, "3")
	createNode(t, "wk2", nil, "3")
	createNode(t, "wk3", nil, "3")
	createNode(t, "wk4", map[string]string{v1.LabelDedicatedOrg: "org-01jabc"}, "3")

	// Inventory: every node, classified.
	if err := testClient.Create(ctx, &v1.PlatformNodeInventory{ObjectMeta: metav1.ObjectMeta{Name: v1.NodeInventoryName}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "inventory", func() error {
		inv := &v1.PlatformNodeInventory{}
		if err := testClient.Get(ctx, client.ObjectKey{Name: v1.NodeInventoryName}, inv); err != nil {
			return err
		}
		kinds := map[string]v1.NodeKind{}
		for _, n := range inv.Status.Nodes {
			kinds[n.Name] = n.Kind
			if !n.Ready || !n.Schedulable || cpu(n.Allocatable) == "0" {
				return failure("node " + n.Name + " reported wrong")
			}
		}
		if len(kinds) != 5 || kinds["cp1"] != v1.NodeControlPlane || kinds["wk1"] != v1.NodeSystem || kinds["wk2"] != v1.NodeWorker {
			return failure("kinds " + kindsString(kinds))
		}
		return nil
	})

	// Shared pool: wk2 and wk3 get the label; cp1, wk1 and wk4 are refused.
	pool := &v1.PlatformSharedPool{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-general"},
		Spec:       v1.PlatformSharedPoolSpec{Nodes: []string{"cp1", "wk1", "wk2", "wk3", "wk4", "ghost"}},
	}
	if err := testClient.Create(ctx, pool); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pool labels", func() error {
		if nodeLabel(t, "wk2") != "shared-general" || nodeLabel(t, "wk3") != "shared-general" {
			return failure("wk2/wk3 not labelled")
		}
		p := &v1.PlatformSharedPool{}
		if err := testClient.Get(ctx, client.ObjectKey{Name: "shared-general"}, p); err != nil {
			return err
		}
		c := meta.FindStatusCondition(p.Status.Conditions, v1.ConditionReady)
		if c == nil || c.Reason != v1.ReasonNodeNotEligible || p.Status.ReadyNodes != 2 || cpu(p.Status.Allocatable) != "6" {
			return failure("pool status not there yet")
		}
		for _, want := range []string{"cp1 (control-plane node)", "wk1 (system node)", "wk4 (dedicated to org-01jabc)", "ghost (no such node)"} {
			if !strings.Contains(c.Message, want) {
				return failure("message " + c.Message + " lacks " + want)
			}
		}
		return nil
	})
	if nodeLabel(t, "wk1") != v1.SystemPool || nodeLabel(t, "cp1") != "" {
		t.Error("an ineligible node was relabelled")
	}

	// A second pool cannot take wk2.
	if err := testClient.Create(ctx, &v1.PlatformSharedPool{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-big"}, Spec: v1.PlatformSharedPoolSpec{Nodes: []string{"wk2"}},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "second pool refused", func() error {
		p := &v1.PlatformSharedPool{}
		if err := testClient.Get(ctx, client.ObjectKey{Name: "shared-big"}, p); err != nil {
			return err
		}
		if c := meta.FindStatusCondition(p.Status.Conditions, v1.ConditionReady); c == nil || !strings.Contains(c.Message, "wk2 (in pool shared-general)") {
			return failure("not refused")
		}
		return nil
	})

	// wk3 leaves the pool; then the pool goes and takes its label with it.
	if err := testClient.Get(ctx, client.ObjectKey{Name: "shared-general"}, pool); err != nil {
		t.Fatal(err)
	}
	pool.Spec.Nodes = []string{"wk2"}
	if err := testClient.Update(ctx, pool); err != nil {
		t.Fatal(err)
	}
	eventually(t, "wk3 unlabelled", func() error {
		if nodeLabel(t, "wk3") != "" {
			return failure("wk3 still labelled")
		}
		return nil
	})
	if err := testClient.Delete(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// wk2 is free again, so shared-big, which asked for it, now gets it.
	eventually(t, "pool deleted", func() error {
		if got := nodeLabel(t, "wk2"); got != "shared-big" {
			return failure("wk2 labelled " + got + ", want shared-big")
		}
		if err := testClient.Get(ctx, client.ObjectKey{Name: "shared-general"}, &v1.PlatformSharedPool{}); !apierrors.IsNotFound(err) {
			return failure("pool still there")
		}
		return nil
	})

	// Node grant: allocatable only (M6).
	if err := testClient.Create(ctx, &v1.PlatformNodeGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "nodegrant-a1"},
		Spec:       v1.PlatformNodeGrantSpec{NodeName: "wk4", OrganizationID: "org-01jabc"},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "grant allocatable", func() error {
		g := &v1.PlatformNodeGrant{}
		if err := testClient.Get(ctx, client.ObjectKey{Name: "nodegrant-a1"}, g); err != nil {
			return err
		}
		if cpu(g.Status.Allocatable) != "3" || g.Status.ObservedGeneration != g.Generation {
			return failure("allocatable not reported")
		}
		return nil
	})
}

func cpu(l v1.ResourceList) string {
	q := l["cpu"]
	return q.String()
}

func kindsString(k map[string]v1.NodeKind) string {
	return fmt.Sprint(k)
}
