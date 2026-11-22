package subreconciler

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// ReasonTenantTerminating: the Tenant of a previous Workspace of the same
// name is still being deleted.
const ReasonTenantTerminating = "TenantTerminating"

// Tenant applies the Capsule Tenant of ws and reports CapsuleTenantReady
// (sub-reconciler tenant). It returns the Tenant as the API server has it.
func Tenant(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace, o render.Options) (Result, *unstructured.Unstructured) {
	// A force-deleted Workspace of the same name may have left its Tenant
	// terminating behind a stuck Pod: Capsule finishes it once the Pod goes.
	// No watch maps the old Tenant to this CR, hence a slow look again.
	live := &unstructured.Unstructured{}
	live.SetGroupVersionKind(render.TenantGVK)
	if err := c.Get(ctx, client.ObjectKey{Name: ws.Name}, live); err == nil && live.GetDeletionTimestamp() != nil {
		r := failed(v1.ConditionCapsuleTenantReady, ReasonTenantTerminating,
			"Tenant "+ws.Name+" of a previous Workspace of this name is still being deleted; it goes once its Namespaces are gone")
		r.Wait, r.After = false, 30*time.Second
		return r, nil
	}
	t, err := ApplyTenant(ctx, c, scheme, ws, o, false)
	if err != nil {
		return failure(v1.ConditionCapsuleTenantReady, err), nil
	}
	return TenantState(v1.ConditionCapsuleTenantReady, t), t
}

// ApplyTenant applies render.Tenant, owned by ws.
func ApplyTenant(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace, o render.Options, revoked bool) (*unstructured.Unstructured, error) {
	t := render.Tenant(ws, o, revoked)
	// A safety net if the CR goes away without its finalizer; the finalizer
	// deletes the Tenant itself, in order (IMPLEMENTATION-PLAN §4.2).
	if err := controllerutil.SetControllerReference(ws, t, scheme); err != nil {
		return nil, err
	}
	return t, Apply(ctx, c, t)
}

// TenantState reads Capsule's verdict on the Tenant's current generation:
// Ready, or still on its way, or not Ready with Capsule's message.
func TenantState(conditionType string, t *unstructured.Unstructured) Result {
	if !TenantReconciled(t) {
		return waiting(conditionType, "waiting for Capsule to reconcile Tenant "+t.GetName())
	}
	conds, _, _ := unstructured.NestedSlice(t.Object, "status", "conditions")
	for _, raw := range conds {
		cond, _ := raw.(map[string]any)
		if cond["type"] != "Ready" {
			continue
		}
		if cond["status"] == "True" {
			return ok(conditionType)
		}
		return failed(conditionType, ReasonCapsuleNotReady, fmt.Sprintf("Capsule: %v: %v", cond["reason"], cond["message"]))
	}
	return waiting(conditionType, "Tenant "+t.GetName()+" has no Ready condition yet")
}

// TenantReconciled is true once Capsule has acted on the Tenant's current spec.
func TenantReconciled(t *unstructured.Unstructured) bool {
	seen, found, _ := unstructured.NestedInt64(t.Object, "status", "observedGeneration")
	return found && seen == t.GetGeneration()
}

// TenantCordoned reads spec.cordoned of a live Tenant.
func TenantCordoned(t *unstructured.Unstructured) bool {
	c, _, _ := unstructured.NestedBool(t.Object, "spec", "cordoned")
	return c
}
