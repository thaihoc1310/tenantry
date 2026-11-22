package subreconciler

import (
	"testing"

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
