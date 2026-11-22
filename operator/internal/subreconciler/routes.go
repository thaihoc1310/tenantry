package subreconciler

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Routes applies one Ingress per entry of spec.routes and deletes the
// Workspace's Ingresses no Route asks for any more (sub-reconciler routes,
// IMPLEMENTATION-PLAN §4.1). No condition: each Route's state is its own
// entry of status.routes, and one broken Route does not degrade the
// Workspace. A Route is ready once its Ingress is applied and its Service
// has the port. A transient API error is returned for a retry.
func Routes(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace) ([]v1.RouteStatus, error) {
	out := []v1.RouteStatus{}
	wanted := map[string]bool{}
	for _, r := range ws.Spec.Routes {
		wanted[r.Namespace+"/"+r.Name] = true
		msg, err := route(ctx, c, scheme, ws, r)
		if err != nil && IsTransient(err) {
			return nil, err
		}
		if err != nil {
			msg = err.Error()
		}
		out = append(out, v1.RouteStatus{Name: r.Name, Namespace: r.Namespace, Ready: msg == "", Message: msg})
	}

	var ingresses networkingv1.IngressList
	if err := c.List(ctx, &ingresses, client.MatchingLabels{v1.LabelManaged: "true", v1.LabelWorkspaceID: ws.Spec.WorkspaceID}); err != nil {
		return nil, err
	}
	for i := range ingresses.Items {
		ing := &ingresses.Items[i]
		if _, fromRoute := ing.Labels[render.LabelRoute]; !fromRoute || wanted[ing.Namespace+"/"+ing.Name] || ing.DeletionTimestamp != nil {
			continue
		}
		if err := c.Delete(ctx, ing); err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	slices.SortFunc(out, func(a, b v1.RouteStatus) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out, nil
}

// route applies one Route's Ingress; msg says why it is not ready.
func route(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace, r v1.WorkspaceRoute) (string, error) {
	// The Namespace must be the Workspace's: the operator writes Ingresses
	// anywhere, the Route must not point it at another tenant.
	ns := &corev1.Namespace{}
	switch err := c.Get(ctx, client.ObjectKey{Name: r.Namespace}, ns); {
	case apierrors.IsNotFound(err):
		return "Namespace " + r.Namespace + " does not exist", nil
	case err != nil:
		return "", err
	}
	if ns.Labels[render.LabelCapsuleTenant] != ws.Name {
		return "Namespace " + r.Namespace + " is not a Namespace of this Workspace", nil
	}
	if ns.DeletionTimestamp != nil {
		return "Namespace " + r.Namespace + " is being deleted", nil
	}
	ing := render.Ingress(ws, r)
	if err := controllerutil.SetControllerReference(ws, ing, scheme); err != nil {
		return "", err
	}
	if err := Apply(ctx, c, ing); err != nil {
		return "", err
	}
	svc := &corev1.Service{}
	switch err := c.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: r.Service}, svc); {
	case apierrors.IsNotFound(err):
		return "Service " + r.Service + " does not exist in " + r.Namespace, nil
	case err != nil:
		return "", err
	}
	for _, p := range svc.Spec.Ports {
		if p.Port == r.Port {
			return "", nil
		}
	}
	return fmt.Sprintf("Service %s has no port %d", r.Service, r.Port), nil
}
