package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

// Capsule losing its own race on a new Namespace (AlreadyExists) does not
// degrade a Ready Workspace; a real replication error does.
func TestReplicationRaceDoesNotDegrade(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-race")
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-race")
	capsuleReconciles(t, "acme-race")
	reconcileOnce(t, r, "acme-race")
	if got := getWorkspace(t, "acme-race").Status.Phase; got != v1.PhaseReady {
		t.Fatalf("phase %s", got)
	}
	failWith := func(message string) {
		t.Helper()
		rep := &unstructured.Unstructured{}
		rep.SetGroupVersionKind(render.ReplicationGVK)
		if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-race"}, rep); err != nil {
			t.Fatal(err)
		}
		rep.Object["status"] = map[string]any{
			"size": int64(0),
			"conditions": []any{map[string]any{
				"type": "Ready", "status": "False", "reason": "Failed", "message": "applying of 1 resources failed",
				"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
			}},
			"processedItems": []any{map[string]any{
				"group": "", "version": "v1", "kind": "LimitRange", "name": render.LimitRangeName, "namespace": "acme-race-frontend",
				"origin": "0/raw-0", "status": map[string]any{"status": "False", "type": "Ready", "message": message},
			}},
		}
		if err := testClient.Status().Update(ctx, rep); err != nil {
			t.Fatal(err)
		}
	}
	failWith(`apply failed for item 0/raw-0: creating target: limitranges "tenantry-defaults" already exists`)
	reconcileOnce(t, r, "acme-race")
	ws = getWorkspace(t, "acme-race")
	if c := condition(ws, v1.ConditionPolicyReady); c.Reason != subreconciler.ReasonProgressing || ws.Status.Phase == v1.PhaseDegraded {
		t.Errorf("Capsule's own race: %+v, phase %s", c, ws.Status.Phase)
	}
	failWith("apply failed for item 0/raw-0: admission webhook denied the request")
	reconcileOnce(t, r, "acme-race")
	ws = getWorkspace(t, "acme-race")
	if c := condition(ws, v1.ConditionPolicyReady); c.Reason != subreconciler.ReasonCapsuleNotReady || ws.Status.Phase != v1.PhaseDegraded {
		t.Errorf("a real replication error: %+v, phase %s", c, ws.Status.Phase)
	}
}

// Capsule's Ready=False from its own race keeps until its next resync, 60 s
// later; the copies actually in the Namespaces decide instead, but only for
// a status about the current spec and only race errors (namespace-latency.md).
func TestReplicationRaceTrustsCopies(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	if err := testClient.Create(ctx, newWorkspace("acme-trust")); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-trust")
	capsuleReconciles(t, "acme-trust")
	reconcileOnce(t, r, "acme-trust")
	capsuleReplicates(t, "acme-trust")
	reconcileOnce(t, r, "acme-trust")
	if got := getWorkspace(t, "acme-trust").Status.Phase; got != v1.PhaseReady {
		t.Fatalf("phase %s", got)
	}
	// status sets the replication Ready=False for one item failing with
	// message, its status and condition observing generation seen.
	status := func(seen int64, message string) {
		t.Helper()
		rep := &unstructured.Unstructured{}
		rep.SetGroupVersionKind(render.ReplicationGVK)
		if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-trust"}, rep); err != nil {
			t.Fatal(err)
		}
		if seen == 0 {
			seen = rep.GetGeneration()
		}
		rep.Object["status"] = map[string]any{
			"size": int64(0), "observedGeneration": seen,
			"conditions": []any{map[string]any{
				"type": "Ready", "status": "False", "reason": "Failed", "message": "applying of 1 resources failed",
				"observedGeneration": seen, "lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
			}},
			"processedItems": []any{map[string]any{
				"group": "", "version": "v1", "kind": "LimitRange", "name": render.LimitRangeName, "namespace": "acme-trust-frontend",
				"origin": "0/raw-0", "status": map[string]any{"status": "False", "type": "Ready", "message": message},
			}},
		}
		if err := testClient.Status().Update(ctx, rep); err != nil {
			t.Fatal(err)
		}
	}
	const race = `apply failed for item 0/raw-0: creating target: limitranges "tenantry-defaults" already exists`
	policy := func() (metav1.Condition, v1.WorkspacePhase) {
		t.Helper()
		reconcileOnce(t, r, "acme-trust")
		ws := getWorkspace(t, "acme-trust")
		return condition(ws, v1.ConditionPolicyReady), ws.Status.Phase
	}

	// A race only, every copy in place: Ready, no 60 s wait.
	status(0, race)
	if c, phase := policy(); c.Status != metav1.ConditionTrue || phase != v1.PhaseReady {
		t.Errorf("race only, copies in place: %+v, phase %s", c, phase)
	}

	// A status about an older spec: not trusted, wait. The spec changes (a new
	// generation) and the race report is about the generation before.
	rep := &unstructured.Unstructured{}
	rep.SetGroupVersionKind(render.ReplicationGVK)
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-trust"}, rep); err != nil {
		t.Fatal(err)
	}
	old := rep.GetGeneration()
	if err := unstructured.SetNestedField(rep.Object, "61s", "spec", "resyncPeriod"); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Update(ctx, rep); err != nil {
		t.Fatal(err)
	}
	status(old, race)
	if c, _ := policy(); c.Reason != subreconciler.ReasonProgressing {
		t.Errorf("stale generation: %+v", c)
	}

	// A real replication error: Degraded as before, copies or not.
	status(0, "apply failed for item 0/raw-0: admission webhook denied the request")
	if c, phase := policy(); c.Reason != subreconciler.ReasonCapsuleNotReady || phase != v1.PhaseDegraded {
		t.Errorf("a real error: %+v, phase %s", c, phase)
	}

	// A race with a Namespace still missing its copies: wait for them.
	ws := getWorkspace(t, "acme-trust")
	ws.Spec.Namespaces = []string{"frontend", "backend"}
	if err := testClient.Update(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-trust") // creates acme-trust-backend
	status(0, race)
	if c, _ := policy(); c.Reason != subreconciler.ReasonProgressing || !strings.Contains(c.Message, "acme-trust-backend") {
		t.Errorf("race with a copy missing: %+v", c)
	}
	capsuleReplicates(t, "acme-trust")
	if c, phase := policy(); c.Status != metav1.ConditionTrue || phase != v1.PhaseReady {
		t.Errorf("race, then every copy: %+v, phase %s", c, phase)
	}
}
