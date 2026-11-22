package controller

import (
	"context"
	"testing"
	"time"

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
