package subreconciler

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Capsule's Ready=False with reason Reconciling is work under way, any other
// reason a failure.
func TestCapsuleReady(t *testing.T) {
	obj := func(reason string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "False", "reason": reason, "message": "x"},
		}}}}
	}
	if res, done := capsuleReady("PolicyReady", obj("Reconciling"), "o"); done || res.Condition.Reason != ReasonProgressing {
		t.Errorf("Reconciling: %+v", res.Condition)
	}
	if res, done := capsuleReady("PolicyReady", obj("Failed"), "o"); done || res.Condition.Reason != ReasonCapsuleNotReady {
		t.Errorf("Failed: %+v", res.Condition)
	}
	if res := TenantState("CapsuleTenantReady", obj("Reconciling")); res.Condition.Reason != ReasonProgressing {
		t.Errorf("Tenant Reconciling: %+v", res.Condition)
	}
}

// A replication Ready=False from Capsule's own race (AlreadyExists) or with
// a condition behind its items is under way for 2 minutes, then a failure;
// any other item error is a failure at once.
func TestReplicationRace(t *testing.T) {
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	now = func() time.Time { return at }
	defer func() { now = time.Now }()
	gtr := func(since time.Duration, messages ...string) *unstructured.Unstructured {
		items := []any{map[string]any{"kind": "LimitRange", "status": map[string]any{"status": "True", "type": "Ready"}}}
		for _, m := range messages {
			items = append(items, map[string]any{"kind": "NetworkPolicy", "status": map[string]any{"status": "False", "type": "Ready", "message": m}})
		}
		return &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
			"conditions": []any{map[string]any{
				"type": "Ready", "status": "False", "reason": "Failed", "message": "applying of 1 resources failed",
				"lastTransitionTime": at.Add(-since).Format(time.RFC3339),
			}},
			"processedItems": items,
		}}}
	}
	conflict := "apply failed for item 0/raw-3: applying object failed: Operation cannot be fulfilled on networkpolicies.networking.k8s.io \"tenant-allow-dns\": the object has been modified; please apply your changes to the latest version and try again"
	raced := "apply failed for item 0/raw-1: creating target: networkpolicies \"tenant-default-deny\" already exists"
	for name, c := range map[string]struct {
		obj  *unstructured.Unstructured
		want string
	}{
		"only already exists":         {gtr(10*time.Second, raced), ReasonProgressing},
		"already exists and conflict": {gtr(10*time.Second, raced, conflict), ReasonProgressing},
		"another error":               {gtr(10*time.Second, raced, "apply failed for item 0/raw-2: admission webhook denied"), ReasonCapsuleNotReady},
		"condition behind its items":  {gtr(10 * time.Second), ReasonProgressing},
		"race past 2 minutes":         {gtr(3*time.Minute, raced), ReasonCapsuleNotReady},
	} {
		res, done := capsuleReady("PolicyReady", c.obj, "LimitRange replication x")
		if done || res.Condition.Reason != c.want {
			t.Errorf("%s: %+v, want %s", name, res.Condition, c.want)
		}
		if c.want == ReasonCapsuleNotReady && len(c.obj.Object["status"].(map[string]any)["processedItems"].([]any)) > 1 &&
			!strings.Contains(res.Condition.Message, "apply failed for item") {
			t.Errorf("%s: the items' messages are missing from %q", name, res.Condition.Message)
		}
	}
}
