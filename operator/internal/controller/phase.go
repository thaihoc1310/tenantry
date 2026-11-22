package controller

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

// baseConditions are the conditions every Workspace needs True to be Ready
// (architecture/04 §1.2); applicable adds those that depend on its spec.
var baseConditions = []string{
	v1.ConditionComputeReady, v1.ConditionCapsuleTenantReady, v1.ConditionPolicyReady, v1.ConditionIdentityBindingsReady,
}

// applicable: NetworkReady counts only for a Workspace with a network,
// ProxyAccessReady only when the operator probes Capsule Proxy.
func applicable(ws *v1.PlatformWorkspace, proxy bool) []string {
	out := slices.Clone(baseConditions)
	if ws.Spec.Network != nil {
		out = append(out, v1.ConditionNetworkReady)
	}
	if proxy {
		out = append(out, v1.ConditionProxyAccessReady)
	}
	return out
}

// phase derives status.phase from the conditions (architecture/04 §1.4).
// before is the phase of the previous pass: once a Workspace has been Ready,
// work under way is Updating and a failure is Degraded; before that both are
// Provisioning, unless an object was refused outright (Failed).
func phase(before v1.WorkspacePhase, conds []metav1.Condition, types []string) v1.WorkspacePhase {
	allTrue, failed, rejected := true, false, false
	for _, t := range types {
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
