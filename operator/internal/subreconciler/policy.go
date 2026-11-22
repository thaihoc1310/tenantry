package subreconciler

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Policy applies the replication of the LimitRange and reports PolicyReady
// (sub-reconciler policy, IMPLEMENTATION-PLAN §4.1): the quota rule and the
// Pod Security labels are in the Tenant the tenant step applied, so policy
// is Ready once Capsule has turned the quota into a GlobalResourceQuota with
// the current hard and has replicated the LimitRange. It returns that
// GlobalResourceQuota, nil when the Workspace has no quota.
func Policy(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace) (Result, *unstructured.Unstructured) {
	const t = v1.ConditionPolicyReady
	rep := render.Replication(ws)
	if err := controllerutil.SetControllerReference(ws, rep, scheme); err != nil {
		return failure(t, err), nil
	}
	if err := Apply(ctx, c, rep); err != nil {
		return failure(t, err), nil
	}
	if res, done := capsuleReady(t, rep, "LimitRange replication "+rep.GetName()); !done {
		return res, nil
	}

	if len(ws.Spec.Resources) == 0 {
		return ok(t), nil
	}
	quota := &unstructured.Unstructured{}
	quota.SetGroupVersionKind(render.QuotaGVK)
	switch err := c.Get(ctx, client.ObjectKey{Name: render.QuotaObjectName(ws)}, quota); {
	case apierrors.IsNotFound(err):
		return waiting(t, "waiting for Capsule to create GlobalResourceQuota "+render.QuotaObjectName(ws)), nil
	case err != nil:
		return failure(t, err), nil
	}
	if res, done := capsuleReady(t, quota, "GlobalResourceQuota "+quota.GetName()); !done {
		return res, quota
	}
	hard := quantities(quota, "status", "total", "hard")
	for k, want := range ws.Spec.Resources {
		if got, found := hard[k]; !found || got.Cmp(want) != 0 {
			return waiting(t, fmt.Sprintf("waiting for Capsule to apply %s=%s to GlobalResourceQuota %s", k, want.String(), quota.GetName())), quota
		}
	}
	return ok(t), quota
}

// capsuleReconciling is the reason Capsule gives Ready=False while it is
// still working on an object, which under load can last a minute: under
// way, not a failure.
const capsuleReconciling = "Reconciling"

// capsuleReady reads the Ready condition Capsule keeps on its objects, for
// the object's current generation when Capsule reports one.
func capsuleReady(t string, obj *unstructured.Unstructured, what string) (Result, bool) {
	if seen, found, _ := unstructured.NestedInt64(obj.Object, "status", "observedGeneration"); found && seen != obj.GetGeneration() {
		return waiting(t, "waiting for Capsule to reconcile "+what), false
	}
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, raw := range conds {
		cond, _ := raw.(map[string]any)
		if cond["type"] != "Ready" {
			continue
		}
		if cond["status"] == "True" {
			return Result{}, true
		}
		if cond["reason"] == capsuleReconciling {
			return waiting(t, "waiting for Capsule to reconcile "+what), false
		}
		return failed(t, ReasonCapsuleNotReady, fmt.Sprintf("Capsule: %s: %v: %v", what, cond["reason"], cond["message"])), false
	}
	return waiting(t, "waiting for Capsule to reconcile "+what), false
}

// Usage reads what the Workspace uses from its GlobalResourceQuota: the
// total, and each Namespace's share (status.usage, status.namespaces[].usage).
func Usage(quota *unstructured.Unstructured) (v1.ResourceList, map[string]v1.ResourceList) {
	if quota == nil {
		return nil, nil
	}
	perNamespace := map[string]v1.ResourceList{}
	byNs, _, _ := unstructured.NestedMap(quota.Object, "status", "namespaceUsage")
	for ns := range byNs {
		perNamespace[ns] = quantities(quota, "status", "namespaceUsage", ns, "used")
	}
	return quantities(quota, "status", "total", "used"), perNamespace
}

func quantities(obj *unstructured.Unstructured, path ...string) v1.ResourceList {
	raw, found, _ := unstructured.NestedStringMap(obj.Object, path...)
	if !found {
		return nil
	}
	out := v1.ResourceList{}
	for k, s := range raw {
		if q, err := resource.ParseQuantity(s); err == nil {
			out[k] = q
		}
	}
	return out
}
