package subreconciler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// TenantNamespaces lists the Namespaces Capsule ties to the Tenant of ws,
// whoever created them.
func TenantNamespaces(ctx context.Context, c client.Client, ws *v1.PlatformWorkspace) ([]corev1.Namespace, error) {
	var list corev1.NamespaceList
	if err := c.List(ctx, &list, client.MatchingLabels{render.LabelCapsuleTenant: ws.Name}); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// Namespaces creates the Namespaces of spec.namespaces and deletes those of
// spec.namespacesToDelete (sub-reconciler namespaces). Its result folds into
// CapsuleTenantReady. Capsule refuses both while the Tenant is cordoned.
func Namespaces(ctx context.Context, c client.Client, ws *v1.PlatformWorkspace, cordoned bool) Result {
	const t = v1.ConditionCapsuleTenantReady
	existing, err := TenantNamespaces(ctx, c, ws)
	if err != nil {
		return failure(t, err)
	}
	ours := map[string]corev1.Namespace{}
	for _, ns := range existing {
		ours[ns.Name] = ns
	}

	var create, remove, gone []string
	for _, suffix := range ws.Spec.Namespaces {
		name := render.NamespaceName(ws, suffix)
		if slices.Contains(ws.Spec.NamespacesToDelete, name) {
			continue // the delete wins: the API never asks for both
		}
		create = append(create, suffix)
	}
	for _, name := range ws.Spec.NamespacesToDelete {
		ns, found := ours[name]
		switch {
		case !found:
			// Gone already, or never ours: only Namespaces Capsule ties to
			// this Tenant are deleted.
		case ns.DeletionTimestamp != nil:
			gone = append(gone, name)
		default:
			remove = append(remove, name)
		}
	}
	if cordoned {
		var pending []string
		for _, s := range create {
			if _, found := ours[render.NamespaceName(ws, s)]; !found {
				pending = append(pending, render.NamespaceName(ws, s))
			}
		}
		pending = append(pending, remove...)
		if len(pending) > 0 {
			return failed(t, ReasonCordoned, "the Workspace is cordoned; waiting to create or delete "+strings.Join(pending, ", "))
		}
		return ok(t)
	}

	for _, s := range create {
		name := render.NamespaceName(ws, s)
		if ns, found := ours[name]; found && ns.DeletionTimestamp != nil {
			gone = append(gone, name) // created again once the old one is gone
			continue
		}
		if err := Apply(ctx, c, render.Namespace(ws, s)); err != nil {
			if IsTransient(err) {
				return failure(t, err)
			}
			return failed(t, ReasonNamespaceFailed, fmt.Sprintf("creating Namespace %s: %v", name, err))
		}
	}
	for _, name := range remove {
		ns := ours[name]
		if err := c.Delete(ctx, &ns); err != nil && !apierrors.IsNotFound(err) {
			if IsTransient(err) {
				return failure(t, err)
			}
			return failed(t, ReasonNamespaceFailed, fmt.Sprintf("deleting Namespace %s: %v", name, err))
		}
		gone = append(gone, name)
	}
	if len(gone) > 0 {
		return waiting(t, "waiting for Namespaces to go away: "+strings.Join(gone, ", "))
	}
	return ok(t)
}
