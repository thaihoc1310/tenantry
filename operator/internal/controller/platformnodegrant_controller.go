package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// nodeGrantFinalizer takes the Organization's label and taint off the node
// when the grant is revoked.
const nodeGrantFinalizer = "tenantry.io/node-grant"

// PlatformNodeGrantReconciler gives a node to one Organization
// (IMPLEMENTATION-PLAN §4.5, architecture/04 §5.1): once the node is Ready,
// schedulable, in no pool, not a control-plane or system node and runs no
// tenant Pod, it gets the label and taint tenantry.io/dedicated-org=<org>.
// From then on only that Organization's Dedicated Workspaces land there.
type PlatformNodeGrantReconciler struct {
	client.Client
	// APIReader lists the Pods of one node straight from the API server, so
	// the operator does not cache every Pod of the cluster.
	APIReader client.Reader
}

func (r *PlatformNodeGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	grant := &v1.PlatformNodeGrant{}
	if err := r.Get(ctx, req.NamespacedName, grant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	org := grant.Spec.OrganizationID
	node := &corev1.Node{}
	err := r.Get(ctx, client.ObjectKey{Name: grant.Spec.NodeName}, node)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	nodeExists := err == nil

	if !grant.DeletionTimestamp.IsZero() {
		// Revoke: the worker only deletes the CR once no reservation holds
		// the node (architecture/04 §5.1). Another Organization's label is
		// never touched.
		if nodeExists && node.Labels[v1.LabelDedicatedOrg] == org {
			if err := setDedicated(ctx, r.Client, node, ""); err != nil {
				return ctrl.Result{}, err
			}
		}
		if controllerutil.RemoveFinalizer(grant, nodeGrantFinalizer) {
			return ctrl.Result{}, r.Update(ctx, grant)
		}
		return ctrl.Result{}, nil
	}
	if controllerutil.AddFinalizer(grant, nodeGrantFinalizer) {
		if err := r.Update(ctx, grant); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := grant.DeepCopy()
	cond := metav1.Condition{Type: v1.ConditionReady, Status: metav1.ConditionFalse, ObservedGeneration: grant.Generation}
	grant.Status.Allocatable = nil
	switch {
	case !nodeExists:
		cond.Reason, cond.Message = v1.ReasonNodeNotReady, "no node "+grant.Spec.NodeName
	case node.Labels[v1.LabelDedicatedOrg] != "" && node.Labels[v1.LabelDedicatedOrg] != org:
		cond.Reason, cond.Message = v1.ReasonNodeNotEligible, "dedicated to "+node.Labels[v1.LabelDedicatedOrg]
	default:
		grant.Status.Allocatable = allocatable(node)
		if node.Labels[v1.LabelDedicatedOrg] != org {
			// First time: the node must be fit and clean before it is given.
			if why := ineligibleForGrant(node); why != "" {
				cond.Reason, cond.Message = v1.ReasonNodeNotEligible, why
				break
			}
			if why := unhealthy(node); why != "" {
				cond.Reason, cond.Message = v1.ReasonNodeNotReady, why
				break
			}
			pods, err := r.tenantPods(ctx, node.Name)
			if err != nil {
				return ctrl.Result{}, err
			}
			if len(pods) > 0 {
				cond.Reason, cond.Message = v1.ReasonNodeNotClean, "tenant Pods still on the node: "+strings.Join(pods, ", ")
				break
			}
		}
		// Given (or given now): keep the label and the taint in place.
		if err := setDedicated(ctx, r.Client, node, org); err != nil {
			return ctrl.Result{}, err
		}
		if why := unhealthy(node); why != "" {
			cond.Reason, cond.Message = v1.ReasonNodeNotReady, why
		} else {
			cond.Status, cond.Reason = metav1.ConditionTrue, "Dedicated"
		}
	}
	meta.SetStatusCondition(&grant.Status.Conditions, cond)
	grant.Status.ObservedGeneration = grant.Generation
	if !equality.Semantic.DeepEqual(before.Status, grant.Status) {
		if err := r.Status().Patch(ctx, grant, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
	}
	if cond.Status != metav1.ConditionTrue {
		// Pods leave a node without telling us: look again.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// ineligibleForGrant says why a node cannot become dedicated, or "".
func ineligibleForGrant(n *corev1.Node) string {
	switch pool := n.Labels[v1.LabelComputePool]; {
	case nodeKind(n) == v1.NodeControlPlane:
		return "control-plane node"
	case pool == v1.SystemPool:
		return "system node"
	case pool != "":
		return "still in shared pool " + pool + ": take it out of the pool first"
	}
	return ""
}

// unhealthy says why a node cannot take new Pods, or "".
func unhealthy(n *corev1.Node) string {
	switch {
	case !nodeReady(n):
		return "node is not Ready"
	case n.Spec.Unschedulable:
		return "node is cordoned"
	}
	return ""
}

// tenantPods lists the Pods on node that belong to a Tenant and still run.
func (r *PlatformNodeGrantReconciler) tenantPods(ctx context.Context, node string) ([]string, error) {
	var pods corev1.PodList
	if err := r.APIReader.List(ctx, &pods, client.MatchingFields{"spec.nodeName": node}); err != nil {
		return nil, err
	}
	var tenantNamespaces corev1.NamespaceList
	if err := r.List(ctx, &tenantNamespaces, client.HasLabels{render.LabelCapsuleTenant}); err != nil {
		return nil, err
	}
	isTenant := map[string]bool{}
	for _, ns := range tenantNamespaces.Items {
		isTenant[ns.Name] = true
	}
	var out []string
	for _, p := range pods.Items {
		if isTenant[p.Namespace] && p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			out = append(out, p.Namespace+"/"+p.Name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// setDedicated puts the label and NoSchedule taint tenantry.io/dedicated-org=org
// on the node, or takes them off when org is "". Other labels and taints stay.
func setDedicated(ctx context.Context, c client.Client, node *corev1.Node, org string) error {
	orig := node.DeepCopy()
	taints := slices.DeleteFunc(slices.Clone(node.Spec.Taints), func(t corev1.Taint) bool { return t.Key == v1.LabelDedicatedOrg })
	if org == "" {
		delete(node.Labels, v1.LabelDedicatedOrg)
	} else {
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
		node.Labels[v1.LabelDedicatedOrg] = org
		taints = append(taints, corev1.Taint{Key: v1.LabelDedicatedOrg, Value: org, Effect: corev1.TaintEffectNoSchedule})
	}
	node.Spec.Taints = taints
	if equality.Semantic.DeepEqual(orig.Labels, node.Labels) && equality.Semantic.DeepEqual(orig.Spec.Taints, node.Spec.Taints) {
		return nil
	}
	if err := c.Patch(ctx, node, client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("node %s: %w", node.Name, err)
	}
	return nil
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
