package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

func routeStatus(t *testing.T, ws, name string) v1.RouteStatus {
	t.Helper()
	for _, r := range getWorkspace(t, ws).Status.Routes {
		if r.Name == name {
			return r
		}
	}
	return v1.RouteStatus{Message: "absent"}
}

// Routes (PRODUCT-PLAN §7.6): one Ingress per Route in its Namespace, ready
// once the Service has the port; never in another tenant's Namespace; gone
// with the Route. A broken Route does not degrade the Workspace.
func TestWorkspaceRoutes(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "acme-other-web", Labels: map[string]string{render.LabelCapsuleTenant: "acme-other"},
	}}); err != nil {
		t.Fatal(err)
	}
	ws := newWorkspace("acme-rt")
	ws.Spec.Routes = []v1.WorkspaceRoute{
		{Name: "web", Namespace: "acme-rt-frontend", Host: "web-acme-rt.mvp.tenantry.site", Service: "web", Port: 80},
		{Name: "steal", Namespace: "acme-other-web", Host: "steal-acme-rt.mvp.tenantry.site", Service: "web", Port: 80},
	}
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-rt")
	capsuleReconciles(t, "acme-rt")
	reconcileOnce(t, r, "acme-rt")

	if got := routeStatus(t, "acme-rt", "web"); got.Ready || !strings.Contains(got.Message, "Service web does not exist") {
		t.Errorf("web before its Service: %+v", got)
	}
	if got := routeStatus(t, "acme-rt", "steal"); got.Ready || !strings.Contains(got.Message, "not a Namespace of this Workspace") {
		t.Errorf("steal: %+v", got)
	}
	if err := testClient.Get(ctx, client.ObjectKey{Namespace: "acme-other-web", Name: "steal"}, &networkingv1.Ingress{}); !apierrors.IsNotFound(err) {
		t.Errorf("Ingress written into another tenant's Namespace: %v", err)
	}
	ing := &networkingv1.Ingress{}
	if err := testClient.Get(ctx, client.ObjectKey{Namespace: "acme-rt-frontend", Name: "web"}, ing); err != nil {
		t.Fatal(err)
	}
	if *ing.Spec.IngressClassName != render.IngressClass || len(ing.Spec.TLS) != 0 || ing.Spec.Rules[0].Host != "web-acme-rt.mvp.tenantry.site" ||
		len(ing.OwnerReferences) != 1 {
		t.Errorf("Ingress %+v", ing)
	}
	if ws := getWorkspace(t, "acme-rt"); ws.Status.Phase != v1.PhaseReady {
		t.Errorf("a broken Route degraded the Workspace: %s", ws.Status.Phase)
	}

	// The Service comes, on another port first.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "acme-rt-frontend"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}}},
	}
	if err := testClient.Create(ctx, svc); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-rt")
	if got := routeStatus(t, "acme-rt", "web"); got.Ready || !strings.Contains(got.Message, "has no port 80") {
		t.Errorf("web with a wrong port: %+v", got)
	}
	svc.Spec.Ports = []corev1.ServicePort{{Port: 80}}
	if err := testClient.Update(ctx, svc); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-rt")
	if got := routeStatus(t, "acme-rt", "web"); !got.Ready || got.Message != "" {
		t.Errorf("web: %+v", got)
	}

	// Someone edits the Ingress: put back. The Route goes: so does its Ingress.
	ing.Spec.Rules[0].Host = "evil.example.com"
	if err := testClient.Update(ctx, ing); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-rt")
	if err := testClient.Get(ctx, client.ObjectKey{Namespace: "acme-rt-frontend", Name: "web"}, ing); err != nil || ing.Spec.Rules[0].Host != "web-acme-rt.mvp.tenantry.site" {
		t.Errorf("Ingress drift not restored: %v %v", err, ing.Spec.Rules)
	}
	ws = getWorkspace(t, "acme-rt")
	ws.Spec.Routes = nil
	if err := testClient.Update(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-rt")
	if err := testClient.Get(ctx, client.ObjectKey{Namespace: "acme-rt-frontend", Name: "web"}, &networkingv1.Ingress{}); !apierrors.IsNotFound(err) {
		t.Errorf("Ingress of a removed Route: %v", err)
	}
	if got := getWorkspace(t, "acme-rt").Status.Routes; len(got) != 0 {
		t.Errorf("status.routes %+v", got)
	}
}
