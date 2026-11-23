package subreconciler

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

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
// GlobalResourceQuota, nil when the Workspace has no quota. With replicas,
// it is Ready only once every live Namespace holds the replicated copies.
func Policy(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace, replicas *Replicas) (Result, *unstructured.Unstructured) {
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
		return replicated(ctx, c, ws, rep, replicas, nil)
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
	return replicated(ctx, c, ws, rep, replicas, quota)
}

// replicated is PolicyReady's last check: the copies are in every Namespace.
// While they are not, it looks again every replicaSoon (Wait would wait the
// controller's 5 s recheck).
func replicated(ctx context.Context, c client.Client, ws *v1.PlatformWorkspace, rep *unstructured.Unstructured,
	replicas *Replicas, quota *unstructured.Unstructured) (Result, *unstructured.Unstructured) {
	const t = v1.ConditionPolicyReady
	if replicas == nil {
		return ok(t), quota
	}
	missing, err := replicas.Missing(ctx, c, ws, rep)
	if err != nil {
		return failure(t, err), quota
	}
	if missing != "" {
		r := waiting(t, "waiting for Capsule to copy the Namespace defaults into "+missing)
		r.Wait, r.After = false, replicaSoon
		return r, quota
	}
	return ok(t), quota
}

// capsuleReconciling is the reason Capsule gives Ready=False while it is
// still working on an object, which under load can last a minute: under
// way, not a failure.
const capsuleReconciling = "Reconciling"

// replicationRaceGrace is how long a GlobalTenantResource may stay
// Ready=False from a race before it counts as a failure. Capsule 0.15.2
// replicates into a new Namespace from two controllers at once and the one
// that loses the race fails: AlreadyExists when both create an object
// (createTarget), Conflict ("the object has been modified") when both update
// it; its next reconcile, a few seconds later, succeeds (seen on the lab). Past this, Capsule is stuck for
// real and the Workspace must say so.
const replicationRaceGrace = 2 * time.Minute

// raceMessages are the item errors of those races, all seen on the lab:
// both controllers create an object, both update it, or one writes into a
// Namespace being deleted (Capsule reads its state from a cache).
var raceMessages = []string{
	"already exists",
	"the object has been modified",
	"because it is being terminated",
}

// now is time.Now, replaced in tests.
var now = time.Now

// replicationRace looks at a replication's items when it is Ready=False:
// retrying is true while, within replicationRaceGrace, every failed item
// failed only because another writer created or changed it first, or no
// item failed at all (the condition is behind its items). detail carries the items' own
// messages either way.
func replicationRace(obj *unstructured.Unstructured, cond map[string]any) (retrying bool, detail string) {
	items, found, _ := unstructured.NestedSlice(obj.Object, "status", "processedItems")
	if !found {
		return false, ""
	}
	var messages []string
	raced := true
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		st, _ := item["status"].(map[string]any)
		if st == nil || st["status"] != "False" {
			continue
		}
		msg, _ := st["message"].(string)
		messages = append(messages, msg)
		raced = raced && slices.ContainsFunc(raceMessages, func(m string) bool { return strings.Contains(msg, m) })
	}
	detail = strings.Join(messages, "; ")
	if len(messages) == 0 {
		detail = "every item is replicated, Capsule's condition is behind"
	}
	since, err := time.Parse(time.RFC3339, fmt.Sprint(cond["lastTransitionTime"]))
	if err != nil || now().Sub(since) > replicationRaceGrace {
		return false, detail
	}
	return raced, detail
}

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
		if retrying, detail := replicationRace(obj, cond); retrying {
			return waiting(t, "waiting for Capsule to retry "+what+": "+detail), false
		} else if detail != "" {
			return failed(t, ReasonCapsuleNotReady, fmt.Sprintf("Capsule: %s: %v: %v: %s", what, cond["reason"], cond["message"], detail)), false
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
