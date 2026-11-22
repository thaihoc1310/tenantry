package controller

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// PlatformNodeInventoryReconciler reports every node of the cluster in the
// status of the inventory the chart created (IMPLEMENTATION-PLAN §4.5). It
// never creates one: without it, it has nothing to do.
type PlatformNodeInventoryReconciler struct {
	client.Client
}

func (r *PlatformNodeInventoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	inv := &v1.PlatformNodeInventory{}
	if err := r.Get(ctx, req.NamespacedName, inv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return ctrl.Result{}, err
	}
	before := inv.DeepCopy()
	inv.Status.Nodes = make([]v1.InventoryNode, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		inv.Status.Nodes = append(inv.Status.Nodes, v1.InventoryNode{
			Name: n.Name, Kind: nodeKind(&n), Ready: nodeReady(&n), Schedulable: !n.Spec.Unschedulable,
			Allocatable: allocatable(&n),
		})
	}
	sort.Slice(inv.Status.Nodes, func(i, j int) bool { return inv.Status.Nodes[i].Name < inv.Status.Nodes[j].Name })
	if equality.Semantic.DeepEqual(before.Status, inv.Status) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Patch(ctx, inv, client.MergeFrom(before))
}

func (r *PlatformNodeInventoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	inventory := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: v1.NodeInventoryName}}}
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.PlatformNodeInventory{}).
		Watches(&corev1.Node{}, inventory).
		Named("platformnodeinventory").
		Complete(r)
}
