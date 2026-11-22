package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/observe"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

// recheck is how soon a pass that waits on something looks again; watches
// usually wake it earlier.
const recheck = 5 * time.Second

// labelQuotaTenant is the label Capsule puts on a GlobalResourceQuota it
// made from a Tenant's quota rule, naming that Tenant.
const labelQuotaTenant = "projectcapsule.dev/tenant"

// PlatformWorkspaceReconciler runs the sub-reconcilers of a Workspace in
// order and writes its status (IMPLEMENTATION-PLAN §4.1).
type PlatformWorkspaceReconciler struct {
	client.Client
	Render   render.Options
	Recorder events.EventRecorder
	// BlockedAfter is how long the finalizer waits for Namespaces before it
	// reports BlockedByFinalizer (IMPLEMENTATION-PLAN §12, decision 8).
	BlockedAfter time.Duration
}

func (r *PlatformWorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ws := &v1.PlatformWorkspace{}
	if err := r.Get(ctx, req.NamespacedName, ws); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ws.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, ws)
	}
	if controllerutil.AddFinalizer(ws, v1.WorkspaceFinalizer) {
		if err := r.Update(ctx, ws); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := ws.DeepCopy()
	// 1. tenant (owners, namespace options, storage classes, guardrails and,
	// for rbac, the RoleBindings: one object, one apply).
	tenantRes, tenant := subreconciler.Tenant(ctx, r.Client, r.Scheme(), ws, r.Render)
	if tenantRes.Condition.Reason == subreconciler.ReasonApplyFailed {
		return ctrl.Result{}, r.fail(ctx, before, ws, tenantRes.Condition)
	}
	// 2. namespaces, folded into CapsuleTenantReady: the Tenant must exist
	// for Capsule to accept them.
	results := []subreconciler.Result{tenantRes}
	if tenant != nil {
		nsRes := subreconciler.Namespaces(ctx, r.Client, ws, subreconciler.TenantCordoned(tenant))
		if nsRes.Condition.Reason == subreconciler.ReasonApplyFailed {
			return ctrl.Result{}, r.fail(ctx, before, ws, nsRes.Condition)
		}
		if tenantRes.Condition.Status == metav1.ConditionTrue || nsRes.Condition.Reason != subreconciler.ReasonReconciled {
			results[0] = merge(tenantRes, nsRes)
		}
	}
	// 3. policy: the quota and Pod Security labels are in the Tenant; the
	// LimitRange is replicated by Capsule.
	policyRes, quota := subreconciler.Policy(ctx, r.Client, r.Scheme(), ws)
	if policyRes.Condition.Reason == subreconciler.ReasonApplyFailed {
		return ctrl.Result{}, r.fail(ctx, before, ws, policyRes.Condition)
	}
	// 4. rbac.
	results = append(results, policyRes, subreconciler.RBAC(tenant))

	existing, err := subreconciler.TenantNamespaces(ctx, r.Client, ws)
	if err != nil {
		return ctrl.Result{}, err
	}
	usage, perNamespace := subreconciler.Usage(quota)
	ws.Status.Usage = usage
	ws.Status.Namespaces = observe.Namespaces(ws, existing, perNamespace)
	if tenant != nil {
		ws.Status.CapsuleTenantRef = tenant.GetName()
	}
	wait := false
	for _, res := range results {
		setCondition(ws, res.Condition)
		wait = wait || res.Wait
	}
	ws.Status.Phase = phase(before.Status.Phase, ws.Status.Conditions)
	ws.Status.ObservedGeneration = ws.Generation
	if err := r.patchStatus(ctx, before, ws); err != nil {
		return ctrl.Result{}, err
	}
	if wait {
		return ctrl.Result{RequeueAfter: recheck}, nil
	}
	return ctrl.Result{}, nil
}

// merge folds the namespaces step into CapsuleTenantReady: the worse of the two.
func merge(tenant, ns subreconciler.Result) subreconciler.Result {
	if tenant.Condition.Status != metav1.ConditionTrue && tenant.Condition.Reason != subreconciler.ReasonProgressing {
		return tenant
	}
	if ns.Condition.Status == metav1.ConditionTrue {
		return tenant
	}
	return ns
}

// fail records a step the API server could not answer and hands the error
// back to controller-runtime, which retries with backoff. observedGeneration
// stays where it was: the pass did not finish.
func (r *PlatformWorkspaceReconciler) fail(ctx context.Context, before, ws *v1.PlatformWorkspace, c metav1.Condition) error {
	setCondition(ws, c)
	if err := r.patchStatus(ctx, before, ws); err != nil {
		return err
	}
	return fmt.Errorf("%s: %s", c.Type, c.Message)
}

func setCondition(ws *v1.PlatformWorkspace, c metav1.Condition) {
	c.ObservedGeneration = ws.Generation
	meta.SetStatusCondition(&ws.Status.Conditions, c)
}

func (r *PlatformWorkspaceReconciler) patchStatus(ctx context.Context, before, ws *v1.PlatformWorkspace) error {
	if equality.Semantic.DeepEqual(before.Status, ws.Status) {
		return nil
	}
	return r.Status().Patch(ctx, ws, client.MergeFrom(before))
}

func (r *PlatformWorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	tenant := &unstructured.Unstructured{}
	tenant.SetGroupVersionKind(render.TenantGVK)
	// A Namespace belongs to the Workspace named like its Tenant.
	byTenantLabel := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
		if name := o.GetLabels()[render.LabelCapsuleTenant]; name != "" {
			return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name}}}
		}
		return nil
	})
	hasTenantLabel := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[render.LabelCapsuleTenant] != ""
	})
	replication := &unstructured.Unstructured{}
	replication.SetGroupVersionKind(render.ReplicationGVK)
	// Capsule's GlobalResourceQuota names its Tenant in a label; its usage
	// becomes status.usage.
	quota := &unstructured.Unstructured{}
	quota.SetGroupVersionKind(render.QuotaGVK)
	byQuotaTenant := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
		if name := o.GetLabels()[labelQuotaTenant]; name != "" {
			return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name}}}
		}
		return nil
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.PlatformWorkspace{}).
		Owns(tenant).
		Owns(replication).
		Watches(quota, byQuotaTenant).
		Watches(&corev1.Namespace{}, byTenantLabel, builder.WithPredicates(hasTenantLabel)).
		Named("platformworkspace").
		Complete(r)
}
