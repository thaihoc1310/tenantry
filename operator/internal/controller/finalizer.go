package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

// finalize is the in-cluster part of a delete, in the order of
// IMPLEMENTATION-PLAN §4.3. Every pass starts over from the top and skips
// what is already done, so a restart in the middle is harmless. Steps 5
// (IPPool, M9) and 6 (node membership label, M8) come with their milestones.
func (r *PlatformWorkspaceReconciler) finalize(ctx context.Context, ws *v1.PlatformWorkspace) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ws, v1.WorkspaceFinalizer) {
		return ctrl.Result{}, nil
	}
	before := ws.DeepCopy()
	ws.Status.Phase = v1.PhaseDeleting
	progress := func(msg string) (ctrl.Result, error) {
		setCondition(ws, metav1.Condition{Type: v1.ConditionDeleting, Status: metav1.ConditionTrue, Reason: subreconciler.ReasonProgressing, Message: msg})
		return ctrl.Result{RequeueAfter: recheck}, r.patchStatus(ctx, before, ws)
	}

	tenant := &unstructured.Unstructured{}
	tenant.SetGroupVersionKind(render.TenantGVK)
	err := r.Get(ctx, client.ObjectKey{Name: ws.Name}, tenant)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	tenantExists := err == nil

	if tenantExists {
		// 1. The tenant loses its rights first: owners but the operator and
		// every RoleBinding go, and Capsule removes them from the Namespaces.
		if !r.revoked(tenant) {
			revoked := ws.DeepCopy()
			revoked.Spec.Cordoned = subreconciler.TenantCordoned(tenant) // unchanged until step 2
			if _, err := subreconciler.ApplyTenant(ctx, r.Client, r.Scheme(), revoked, r.Render, true); err != nil {
				return ctrl.Result{}, err
			}
			return progress("removing the tenant's owners and RoleBindings")
		}
		if !subreconciler.TenantReconciled(tenant) {
			return progress("waiting for Capsule to remove the tenant's RoleBindings")
		}
		// 2. Uncordon: Capsule refuses even the operator's writes in a
		// cordoned Tenant, deleting its Namespaces included.
		if subreconciler.TenantCordoned(tenant) {
			uncordoned := ws.DeepCopy()
			uncordoned.Spec.Cordoned = false
			if _, err := subreconciler.ApplyTenant(ctx, r.Client, r.Scheme(), uncordoned, r.Render, true); err != nil {
				return ctrl.Result{}, err
			}
			return progress("uncordoning the Tenant")
		}
	}

	// 3. Every Namespace of the Tenant, from the portal or kubectl; PVCs and
	// Pods go with them.
	namespaces, err := subreconciler.TenantNamespaces(ctx, r.Client, ws)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(namespaces) > 0 {
		var names []string
		for i := range namespaces {
			names = append(names, namespaces[i].Name)
			if namespaces[i].DeletionTimestamp == nil {
				if err := r.Delete(ctx, &namespaces[i]); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
		}
		slices.Sort(names)
		switch {
		case ws.Annotations[v1.AnnotationForceDelete] == "true":
			// :force of a Platform Admin: stop waiting for the Namespaces;
			// deleting the Tenant below leaves them to the garbage collector.
			if tenantExists && tenant.GetDeletionTimestamp() == nil {
				r.Recorder.Eventf(ws, nil, corev1.EventTypeWarning, "ForceDelete", "Finalize",
					"force-delete: not waiting for Namespaces %s", strings.Join(names, ", "))
			}
		case time.Since(ws.DeletionTimestamp.Time) > r.BlockedAfter:
			setCondition(ws, metav1.Condition{
				Type: v1.ConditionDeleting, Status: metav1.ConditionFalse, Reason: v1.ReasonBlockedByFinalizer,
				Message: "Namespaces still terminating: " + describe(namespaces),
			})
			return ctrl.Result{RequeueAfter: 30 * time.Second}, r.patchStatus(ctx, before, ws)
		default:
			return progress("waiting for Namespaces to go away: " + strings.Join(names, ", "))
		}
	}

	// 4. The replication of the LimitRange, then the Tenant itself; owner
	// references would remove both too, but in no particular order.
	rep := &unstructured.Unstructured{}
	rep.SetGroupVersionKind(render.ReplicationGVK)
	switch err := r.Get(ctx, client.ObjectKey{Name: ws.Name}, rep); {
	case err == nil:
		if rep.GetDeletionTimestamp() == nil {
			if err := r.Delete(ctx, rep); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return progress("waiting for GlobalTenantResource " + ws.Name + " to go away")
	case !apierrors.IsNotFound(err):
		return ctrl.Result{}, err
	}
	if tenantExists {
		if tenant.GetDeletionTimestamp() == nil {
			if err := r.Delete(ctx, tenant); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return progress("waiting for Tenant " + ws.Name + " to go away")
	}

	// 6. Dedicated: the node keeps the Organization's label and taint and
	// the other Workspaces' labels, only this Workspace's goes.
	if ws.Spec.Compute.Mode == v1.ComputeDedicated && ws.Spec.Compute.NodeName != "" {
		node := &corev1.Node{}
		switch err := r.Get(ctx, client.ObjectKey{Name: ws.Spec.Compute.NodeName}, node); {
		case err == nil:
			if _, labelled := node.Labels[render.MembershipLabel(ws)]; labelled {
				if err := subreconciler.SetNodeLabel(ctx, r.Client, node.Name, render.MembershipLabel(ws), nil); err != nil {
					return ctrl.Result{}, err
				}
			}
		case !apierrors.IsNotFound(err):
			return ctrl.Result{}, err
		}
	}

	// 7. Done: the CR goes, and the worker releases what the Workspace held.
	controllerutil.RemoveFinalizer(ws, v1.WorkspaceFinalizer)
	return ctrl.Result{}, r.Update(ctx, ws)
}

// revoked is true once the live Tenant has no owner but the operator and no
// additional RoleBinding.
func (r *PlatformWorkspaceReconciler) revoked(tenant *unstructured.Unstructured) bool {
	owners, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "owners")
	bindings, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "additionalRoleBindings")
	if len(bindings) > 0 || len(owners) != 1 {
		return false
	}
	o, _ := owners[0].(map[string]any)
	return o["kind"] == "ServiceAccount" && o["name"] == r.Render.OperatorServiceAccount
}

// describe names each terminating Namespace with what Kubernetes says holds it.
func describe(namespaces []corev1.Namespace) string {
	var parts []string
	for _, ns := range namespaces {
		why := ""
		for _, c := range ns.Status.Conditions {
			if c.Status == corev1.ConditionTrue && c.Message != "" {
				why = c.Message
			}
		}
		if why == "" {
			parts = append(parts, ns.Name)
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", ns.Name, why))
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}
