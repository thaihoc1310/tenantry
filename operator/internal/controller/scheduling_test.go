package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// testPod is a Pod in ns with the status the scheduler (or the kubelet)
// would have written; envtest runs neither.
func testPod(t *testing.T, ns, name string, phase corev1.PodPhase, scheduled *corev1.PodCondition) *corev1.Pod {
	t.Helper()
	ctx := context.Background()
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "docker.io/library/pause:3.10"}}},
	}
	if err := testClient.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.Status.Phase = phase
	if scheduled != nil {
		p.Status.Conditions = []corev1.PodCondition{*scheduled}
	}
	if err := testClient.Status().Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func unschedulableSince(at time.Time, message string) *corev1.PodCondition {
	return &corev1.PodCondition{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
		Message: message, LastTransitionTime: metav1.NewTime(at),
	}
}

func namespaceUnschedulable(ws *v1.PlatformWorkspace) map[string]int32 {
	out := map[string]int32{}
	for _, ns := range ws.Status.Namespaces {
		out[ns.Name] = ns.UnschedulablePods
	}
	return out
}

// Pods the scheduler cannot place are counted per Namespace and in total,
// with the message of the one waiting the longest; a gated Pod or one
// starting its containers is not. Placed or deleted, they leave the count,
// and at 0 status.scheduling goes. The Workspace stays Ready throughout, and
// a pass with nothing changed writes nothing.
func TestWorkspaceScheduling(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-sched")
	ws.Spec.Namespaces = []string{"frontend", "backend"}
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-sched")
	capsuleReconciles(t, "acme-sched")
	reconcileOnce(t, r, "acme-sched")
	capsuleReplicates(t, "acme-sched")
	reconcileOnce(t, r, "acme-sched")
	ws = getWorkspace(t, "acme-sched")
	if ws.Status.Phase != v1.PhaseReady || ws.Status.Scheduling != nil {
		t.Fatalf("before any Pod: phase %s, scheduling %+v", ws.Status.Phase, ws.Status.Scheduling)
	}
	conditionsBefore := ws.Status.Conditions

	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	oldest := testPod(t, "acme-sched-frontend", "web-0", corev1.PodPending, unschedulableSince(t0, "0/4 nodes are available: 2 Insufficient cpu."))
	second := testPod(t, "acme-sched-frontend", "web-1", corev1.PodPending, unschedulableSince(t0.Add(time.Minute), "0/4 nodes are available: 2 Insufficient memory."))
	other := testPod(t, "acme-sched-backend", "api-0", corev1.PodPending, unschedulableSince(t0.Add(2*time.Minute), "0/4 nodes are available: 2 Insufficient cpu."))
	testPod(t, "acme-sched-backend", "gated", corev1.PodPending, &corev1.PodCondition{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonSchedulingGated,
	})
	testPod(t, "acme-sched-backend", "starting", corev1.PodPending, &corev1.PodCondition{
		Type: corev1.PodScheduled, Status: corev1.ConditionTrue,
	})

	reconcileOnce(t, r, "acme-sched")
	ws = getWorkspace(t, "acme-sched")
	s := ws.Status.Scheduling
	if s == nil || s.UnschedulablePods != 3 {
		t.Fatalf("scheduling %+v, want 3 Pods", s)
	}
	if s.Message != "0/4 nodes are available: 2 Insufficient cpu." || s.Since == nil || !s.Since.Time.Equal(t0) {
		t.Errorf("message %q since %v, want web-0's, at %v", s.Message, s.Since, t0)
	}
	if got := namespaceUnschedulable(ws); got["acme-sched-frontend"] != 2 || got["acme-sched-backend"] != 1 {
		t.Errorf("per Namespace %v, want frontend 2 and backend 1", got)
	}
	if ws.Status.Phase != v1.PhaseReady {
		t.Errorf("phase %s with Pods unschedulable, want Ready", ws.Status.Phase)
	}
	for _, c := range conditionsBefore {
		if after := condition(ws, c.Type); after.Status != c.Status || after.Reason != c.Reason || !after.LastTransitionTime.Equal(&c.LastTransitionTime) {
			t.Errorf("condition %s changed: %+v → %+v", c.Type, c, after)
		}
	}

	// Nothing changed: nothing is written.
	rv := ws.ResourceVersion
	reconcileOnce(t, r, "acme-sched")
	if got := getWorkspace(t, "acme-sched").ResourceVersion; got != rv {
		t.Errorf("a pass with nothing changed wrote the status: resourceVersion %s → %s", rv, got)
	}

	// The oldest is placed: the next one's message and time.
	oldest.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}}
	if err := testClient.Status().Update(ctx, oldest); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-sched")
	s = getWorkspace(t, "acme-sched").Status.Scheduling
	if s == nil || s.UnschedulablePods != 2 || s.Message != "0/4 nodes are available: 2 Insufficient memory." {
		t.Errorf("after web-0 is placed: %+v", s)
	}

	// The others are deleted: back to nothing.
	for _, p := range []*corev1.Pod{second, other} {
		if err := testClient.Delete(ctx, p, client.GracePeriodSeconds(0)); err != nil {
			t.Fatal(err)
		}
	}
	reconcileOnce(t, r, "acme-sched")
	ws = getWorkspace(t, "acme-sched")
	if ws.Status.Scheduling != nil {
		t.Errorf("after the rest are deleted: scheduling %+v, want nil", ws.Status.Scheduling)
	}
	for ns, n := range namespaceUnschedulable(ws) {
		if n != 0 {
			t.Errorf("%s still counts %d", ns, n)
		}
	}
	if ws.Status.Phase != v1.PhaseReady {
		t.Errorf("phase %s, want Ready", ws.Status.Phase)
	}
}

// Only events that can change status.scheduling wake the Workspace.
func TestSchedulingChanged(t *testing.T) {
	pending := func(c *corev1.PodCondition) *corev1.Pod {
		p := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}
		if c != nil {
			p.Status.Conditions = []corev1.PodCondition{*c}
		}
		return p
	}
	stuck := pending(unschedulableSince(time.Now(), "0/4 nodes are available: 2 Insufficient cpu."))
	stuckOther := pending(unschedulableSince(time.Now(), "0/4 nodes are available: 2 Insufficient memory."))
	fresh := pending(nil)
	placed := pending(&corev1.PodCondition{Type: corev1.PodScheduled, Status: corev1.ConditionTrue})
	running := placed.DeepCopy()
	running.Status.Phase = corev1.PodRunning
	deleting := stuck.DeepCopy()
	deleting.DeletionTimestamp = &metav1.Time{Time: time.Now()}

	for _, tc := range []struct {
		name      string
		got, want bool
	}{
		{"a new Pod not yet looked at", schedulingChanged.Create(event.CreateEvent{Object: fresh}), false},
		{"a new unschedulable Pod (cache start)", schedulingChanged.Create(event.CreateEvent{Object: stuck}), true},
		{"becomes unschedulable", schedulingChanged.Update(event.UpdateEvent{ObjectOld: fresh, ObjectNew: stuck}), true},
		{"its message changes", schedulingChanged.Update(event.UpdateEvent{ObjectOld: stuck, ObjectNew: stuckOther}), true},
		{"unchanged", schedulingChanged.Update(event.UpdateEvent{ObjectOld: stuck, ObjectNew: stuck.DeepCopy()}), false},
		{"placed", schedulingChanged.Update(event.UpdateEvent{ObjectOld: stuck, ObjectNew: placed}), true},
		{"placed Pod starts", schedulingChanged.Update(event.UpdateEvent{ObjectOld: placed, ObjectNew: running}), false},
		{"being deleted", schedulingChanged.Update(event.UpdateEvent{ObjectOld: stuck, ObjectNew: deleting}), true},
		{"an unschedulable Pod goes", schedulingChanged.Delete(event.DeleteEvent{Object: stuck}), true},
		{"a placed Pod leaves Pending", schedulingChanged.Delete(event.DeleteEvent{Object: placed}), false},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// The manager's cache holds Pending Pods only: a Running one is never in it,
// and a Pod leaving Pending leaves it.
func TestPendingPodsOnlyCache(t *testing.T) {
	needEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := cache.New(testCfg, cache.Options{Scheme: testScheme, ByObject: map[client.Object]cache.ByObject{&corev1.Pod{}: PendingPodsOnly}})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = c.Start(ctx) }()
	if !c.WaitForCacheSync(ctx) {
		t.Fatal("cache did not sync")
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "pending-cache"}}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}
	testPod(t, ns.Name, "waiting", corev1.PodPending, unschedulableSince(time.Now(), "no room"))
	moving := testPod(t, ns.Name, "moving", corev1.PodPending, nil)
	testPod(t, ns.Name, "running", corev1.PodRunning, nil)
	cached := func() map[string]bool {
		var pods corev1.PodList
		if err := c.List(ctx, &pods, client.InNamespace(ns.Name)); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range pods.Items {
			out[p.Name] = true
		}
		return out
	}
	waitUntil := func(what string, ok func(map[string]bool) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			got := cached()
			if ok(got) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: cache holds %v", what, got)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	waitUntil("Pending Pods in, Running out", func(m map[string]bool) bool {
		return m["waiting"] && m["moving"] && !m["running"] && len(m) == 2
	})
	moving.Status.Phase = corev1.PodRunning
	if err := testClient.Status().Update(ctx, moving); err != nil {
		t.Fatal(err)
	}
	waitUntil("a Pod leaving Pending leaves the cache", func(m map[string]bool) bool {
		return m["waiting"] && !m["moving"] && len(m) == 1
	})
}
