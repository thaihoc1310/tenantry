package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

// applicableConditions are the conditions a Workspace needs True to be Ready
// at this milestone (architecture/04 §1.2); later milestones add theirs.
var applicableConditions = []string{
	v1.ConditionComputeReady, v1.ConditionCapsuleTenantReady, v1.ConditionPolicyReady, v1.ConditionIdentityBindingsReady,
}

// phase derives status.phase from the conditions (architecture/04 §1.4).
// before is the phase of the previous pass: once a Workspace has been Ready,
// work under way is Updating and a failure is Degraded; before that both are
// Provisioning, unless an object was refused outright (Failed).
func phase(before v1.WorkspacePhase, conds []metav1.Condition) v1.WorkspacePhase {
	allTrue, failed, rejected := true, false, false
	for _, t := range applicableConditions {
		c := findCondition(conds, t)
		if c == nil || c.Status != metav1.ConditionTrue {
			allTrue = false
		}
		if c != nil && c.Status == metav1.ConditionFalse && c.Reason != subreconciler.ReasonProgressing {
			failed = true
			rejected = rejected || c.Reason == subreconciler.ReasonRejected
		}
	}
	wasReady := before == v1.PhaseReady || before == v1.PhaseUpdating || before == v1.PhaseDegraded
	switch {
	case allTrue:
		return v1.PhaseReady
	case wasReady && failed:
		return v1.PhaseDegraded
	case wasReady:
		return v1.PhaseUpdating
	case rejected:
		return v1.PhaseFailed
	default:
		return v1.PhaseProvisioning
	}
}

func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}
