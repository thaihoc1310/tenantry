package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// PlatformNodeGrantReconciler reports the node's allocatable. Checking the
// node and putting the Organization's label and taint on it come in M8
// (IMPLEMENTATION-PLAN §4.5); until then it sets no condition.
type PlatformNodeGrantReconciler struct {
	client.Client
}

func (r *PlatformNodeGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	grant := &v1.PlatformNodeGrant{}
	if err := r.Get(ctx, req.NamespacedName, grant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	before := grant.DeepCopy()
	node := &corev1.Node{}
	switch err := r.Get(ctx, client.ObjectKey{Name: grant.Spec.NodeName}, node); {
	case apierrors.IsNotFound(err):
		grant.Status.Allocatable = nil
	case err != nil:
		return ctrl.Result{}, err
	default:
		grant.Status.Allocatable = allocatable(node)
	}
	grant.Status.ObservedGeneration = grant.Generation
	if equality.Semantic.DeepEqual(before.Status, grant.Status) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Patch(ctx, grant, client.MergeFrom(before))
}

func (r *PlatformNodeGrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	grantsOfNode := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		var grants v1.PlatformNodeGrantList
		if err := r.List(ctx, &grants); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for _, g := range grants.Items {
			if g.Spec.NodeName == o.GetName() {
				reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKey{Name: g.Name}})
			}
		}
		return reqs
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.PlatformNodeGrant{}).
		Watches(&corev1.Node{}, grantsOfNode).
		Named("platformnodegrant").
		Complete(r)
}
