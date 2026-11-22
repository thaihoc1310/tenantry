package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// sharedPoolFinalizer takes the pool label off its nodes when a pool goes.
const sharedPoolFinalizer = "tenantry.io/shared-pool"

// PlatformSharedPoolReconciler owns the label tenantry.io/compute-pool=<pool>
// on nodes, following spec.nodes (IMPLEMENTATION-PLAN §4.5). It never evicts
// a Pod: a node leaving the pool only stops receiving new Shared Pods.
type PlatformSharedPoolReconciler struct {
	client.Client
}

func (r *PlatformSharedPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pool := &v1.PlatformSharedPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return ctrl.Result{}, err
	}

	if !pool.DeletionTimestamp.IsZero() {
		for _, n := range nodes.Items {
			if n.Labels[v1.LabelComputePool] == pool.Name {
				if err := setNodeLabel(ctx, r.Client, n.Name, v1.LabelComputePool, nil); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
		if controllerutil.RemoveFinalizer(pool, sharedPoolFinalizer) {
			return ctrl.Result{}, r.Update(ctx, pool)
		}
		return ctrl.Result{}, nil
	}
	if controllerutil.AddFinalizer(pool, sharedPoolFinalizer) {
		if err := r.Update(ctx, pool); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := pool.DeepCopy()
	var refused []string
	ready, total := int32(0), v1.ResourceList{}
	for _, n := range nodes.Items {
		wanted := slices.Contains(pool.Spec.Nodes, n.Name)
		current := n.Labels[v1.LabelComputePool]
		switch {
		case wanted && current != pool.Name:
			if why := ineligible(&n); why != "" {
				refused = append(refused, n.Name+" ("+why+")")
				continue
			}
			value := pool.Name
			if err := setNodeLabel(ctx, r.Client, n.Name, v1.LabelComputePool, &value); err != nil {
				return ctrl.Result{}, err
			}
		case !wanted && current == pool.Name:
			if err := setNodeLabel(ctx, r.Client, n.Name, v1.LabelComputePool, nil); err != nil {
				return ctrl.Result{}, err
			}
			continue
		case !wanted:
			continue
		}
		if nodeReady(&n) {
			ready++
		}
		addTo(total, allocatable(&n))
	}
	for _, name := range pool.Spec.Nodes {
		if !slices.ContainsFunc(nodes.Items, func(n corev1.Node) bool { return n.Name == name }) {
			refused = append(refused, name+" (no such node)")
		}
	}

	cond := metav1.Condition{Type: v1.ConditionReady, Status: metav1.ConditionTrue, Reason: "NodesLabelled", ObservedGeneration: pool.Generation}
	if len(refused) > 0 {
		slices.Sort(refused)
		cond.Status, cond.Reason = metav1.ConditionFalse, v1.ReasonNodeNotEligible
		cond.Message = "not in the pool: " + strings.Join(refused, ", ")
	}
	meta.SetStatusCondition(&pool.Status.Conditions, cond)
	pool.Status.ReadyNodes, pool.Status.Allocatable = ready, total
	pool.Status.ObservedGeneration = pool.Generation
	if equality.Semantic.DeepEqual(before.Status, pool.Status) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Patch(ctx, pool, client.MergeFrom(before))
}

// ineligible says why a node cannot join a shared pool, or "" if it can:
// control-plane and system nodes stay out, a dedicated node belongs to one
// Organization, and a node is in one pool at most (PRODUCT-PLAN §6.2).
func ineligible(n *corev1.Node) string {
	switch pool := n.Labels[v1.LabelComputePool]; {
	case nodeKind(n) == v1.NodeControlPlane:
		return "control-plane node"
	case pool == v1.SystemPool:
		return "system node"
	case n.Labels[v1.LabelDedicatedOrg] != "":
		return "dedicated to " + n.Labels[v1.LabelDedicatedOrg]
	case pool != "":
		return fmt.Sprintf("in pool %s", pool)
	}
	return ""
}

func (r *PlatformSharedPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Any node change can matter to any pool: there are a handful of each.
	allPools := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		var pools v1.PlatformSharedPoolList
		if err := r.List(ctx, &pools); err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(pools.Items))
		for _, p := range pools.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKey{Name: p.Name}})
		}
		return reqs
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.PlatformSharedPool{}).
		Watches(&corev1.Node{}, allPools).
		Named("platformsharedpool").
		Complete(r)
}
