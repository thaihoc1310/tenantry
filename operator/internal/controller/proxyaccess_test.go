package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

// fakeProxy plays Capsule Proxy: it answers the Namespace list it is given,
// or status when not 200, and counts the probes and the tokens they carry.
type fakeProxy struct {
	mu     sync.Mutex
	status int
	names  map[string]string // Namespace → tenant label
	calls  int
	tokens map[string]bool
}

func (f *fakeProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] = true
	if r.URL.Path != "/api/v1/namespaces" || f.status != http.StatusOK {
		w.WriteHeader(f.status)
		return
	}
	list := corev1.NamespaceList{}
	for name, tenant := range f.names {
		list.Items = append(list.Items, corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{render.LabelCapsuleTenant: tenant}}})
	}
	_ = json.NewEncoder(w).Encode(list)
}

func (f *fakeProxy) set(status int, names map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.names = status, names
}

func (f *fakeProxy) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func proxySettings(t *testing.T, ws string) []unstructured.Unstructured {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(render.ProxySettingGVK.GroupVersion().WithKind("ProxySettingList"))
	if err := testClient.List(context.Background(), list, client.MatchingLabels{v1.LabelWorkspaceID: "ws-" + ws}); err != nil {
		t.Fatal(err)
	}
	return list.Items
}

// Sub-reconciler proxyaccess: a ProxySetting for the Developer and Viewer
// groups and the probe, and ProxyAccessReady from the probe's view through
// the proxy: everything of the Workspace, nothing else; probes paced.
func TestWorkspaceProxyAccess(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenantry-operator"}}); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "tenantry-proxy-probe", Namespace: "tenantry-operator"}}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProxy{status: http.StatusOK, tokens: map[string]bool{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	r := newWorkspaceReconciler()
	r.Proxy = &subreconciler.ProxyProber{Client: testClient, URL: server.URL, Namespace: "tenantry-operator", Name: "tenantry-proxy-probe", HTTP: server.Client()}

	ws := newWorkspace("acme-px")
	ws.Spec.Namespaces = []string{"web", "api"}
	ws.Spec.AccessBindings = append(ws.Spec.AccessBindings, v1.AccessBinding{GroupSubject: "oidc:org_01jabc:group_view", Role: v1.RoleViewer})
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	// The probe sees other Workspaces too: it is in their ProxySettings.
	other := newWorkspace("acme-pxo")
	if err := testClient.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	fake.set(http.StatusOK, map[string]string{"acme-px-api": "acme-px", "acme-px-web": "acme-px", "acme-pxo-frontend": "acme-pxo"})
	reconcileOnce(t, r, "acme-px")
	capsuleReconciles(t, "acme-px")
	res := reconcileOnce(t, r, "acme-px")
	ws = getWorkspace(t, "acme-px")
	if c := condition(ws, v1.ConditionProxyAccessReady); c.Status != metav1.ConditionTrue || ws.Status.Phase != v1.PhaseReady {
		t.Fatalf("ProxyAccessReady %+v, phase %s", c, ws.Status.Phase)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("a passing probe should be repeated later: %+v", res)
	}
	settings := proxySettings(t, "acme-px")
	if len(settings) != 1 || settings[0].GetNamespace() != "acme-px-api" {
		t.Fatalf("ProxySettings %v", settings)
	}
	subjects, _, _ := unstructured.NestedSlice(settings[0].Object, "spec", "subjects")
	want := `[map[kind:Group name:oidc:org_01jabc:group_dev] map[kind:Group name:oidc:org_01jabc:group_view] map[kind:ServiceAccount name:system:serviceaccount:tenantry-operator:tenantry-proxy-probe]]`
	if got := strings.Join(strings.Fields(strings.TrimSpace(fmt.Sprint(subjects))), " "); got != want {
		t.Errorf("subjects %s, want %s", got, want)
	}
	if len(fake.tokens) != 1 || fake.tokens[""] {
		t.Errorf("the probe did not carry one token: %v", fake.tokens)
	}

	// Paced: within five minutes and the same generation, no new probe.
	calls := fake.count()
	reconcileOnce(t, r, "acme-px")
	if fake.count() != calls {
		t.Error("probed again within five minutes")
	}

	// The proxy shows another Tenant's Namespace: not filtering. A new
	// generation probes at once.
	fake.set(http.StatusOK, map[string]string{"acme-px-api": "acme-px", "acme-px-web": "acme-px", "globex-x-web": "globex-x"})
	ws = getWorkspace(t, "acme-px")
	ws.Spec.DisplayName = "Px 2"
	if err := testClient.Update(ctx, ws); err != nil {
		t.Fatal(err)
	}
	res = reconcileOnce(t, r, "acme-px")
	ws = getWorkspace(t, "acme-px")
	if c := condition(ws, v1.ConditionProxyAccessReady); c.Reason != subreconciler.ReasonProxyNotFiltering || !strings.Contains(c.Message, "globex-x-web") ||
		ws.Status.Phase != v1.PhaseDegraded {
		t.Errorf("not filtering: %+v, phase %s", c, ws.Status.Phase)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > 30*time.Second {
		t.Errorf("a failing probe is retried in at most 30s: %+v", res)
	}

	// The proxy is down.
	fake.set(http.StatusServiceUnavailable, nil)
	ws.Spec.DisplayName = "Px 3"
	if err := testClient.Update(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-px")
	if c := condition(getWorkspace(t, "acme-px"), v1.ConditionProxyAccessReady); c.Reason != subreconciler.ReasonProxyUnreachable {
		t.Errorf("proxy down: %+v", c)
	}

	// The first Namespace goes: the ProxySetting moves to the next one.
	fake.set(http.StatusOK, map[string]string{"acme-px-web": "acme-px"})
	ws = getWorkspace(t, "acme-px")
	ws.Spec.Namespaces = []string{"web"}
	ws.Spec.NamespacesToDelete = []string{"acme-px-api"}
	if err := testClient.Update(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-px")
	finishNamespaceDeletion(t, "acme-px-api")
	reconcileOnce(t, r, "acme-px")
	if settings := proxySettings(t, "acme-px"); len(settings) != 1 || settings[0].GetNamespace() != "acme-px-web" {
		t.Errorf("ProxySetting did not move: %v", settings)
	}
	if c := condition(getWorkspace(t, "acme-px"), v1.ConditionProxyAccessReady); c.Status != metav1.ConditionTrue {
		t.Errorf("after the move: %+v", c)
	}

	// No Namespace at all: nothing to show, True.
	nsWS := newWorkspace("acme-px0")
	nsWS.Spec.Namespaces = nil
	if err := testClient.Create(ctx, nsWS); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-px0")
	capsuleReconciles(t, "acme-px0")
	reconcileOnce(t, r, "acme-px0")
	if c := condition(getWorkspace(t, "acme-px0"), v1.ConditionProxyAccessReady); c.Status != metav1.ConditionTrue || c.Reason != subreconciler.ReasonNoNamespace {
		t.Errorf("no Namespace: %+v", c)
	}
}
