package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// One envtest API server for the package: our CRDs plus Capsule's Tenant
// (config/crd/external). No Capsule runs, so tests play its part: they set
// the Tenant's status and finish Namespace deletions themselves.
var (
	testCfg    *rest.Config
	testClient client.Client
	testScheme = runtime.NewScheme()
)

const operatorSA = "system:serviceaccount:tenantry-operator:tenantry-operator"

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		// go test ./... without make operator-test: the envtest tests skip.
		os.Exit(m.Run())
	}
	if err := clientgoscheme.AddToScheme(testScheme); err != nil {
		panic(err)
	}
	if err := v1.AddToScheme(testScheme); err != nil {
		panic(err)
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases"), filepath.Join("..", "..", "config", "crd", "external")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		panic(err)
	}
	testCfg = cfg
	testClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

func needEnv(t *testing.T) {
	t.Helper()
	if testClient == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set: run make operator-test")
	}
}

func newWorkspaceReconciler() *PlatformWorkspaceReconciler {
	return &PlatformWorkspaceReconciler{
		Client:       testClient,
		Recorder:     events.NewFakeRecorder(100),
		Render:       render.Options{OperatorServiceAccount: operatorSA, AllowedRegistries: []string{"docker.io"}},
		BlockedAfter: 10 * time.Minute,
	}
}

func reconcileOnce(t *testing.T, r interface {
	Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)
}, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: name}})
	if err != nil {
		t.Fatalf("reconcile %s: %v", name, err)
	}
	return res
}

func getTenant(t *testing.T, name string) *unstructured.Unstructured {
	t.Helper()
	tenant := &unstructured.Unstructured{}
	tenant.SetGroupVersionKind(render.TenantGVK)
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, tenant); err != nil {
		t.Fatalf("get Tenant %s: %v", name, err)
	}
	return tenant
}

// capsuleReconciles plays Capsule: the Tenant's current generation is seen
// and Ready.
func capsuleReconciles(t *testing.T, name string) {
	t.Helper()
	tenant := getTenant(t, name)
	tenant.Object["status"] = map[string]any{
		"observedGeneration": tenant.GetGeneration(),
		"state":              "Active",
		"size":               int64(0),
		"conditions": []any{map[string]any{
			"type": "Ready", "status": "True", "reason": "Succeeded", "message": "reconciled",
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
		}},
	}
	if err := testClient.Status().Update(context.Background(), tenant); err != nil {
		t.Fatalf("set Tenant status: %v", err)
	}
}

// finishNamespaceDeletion plays the namespace controller, which envtest lacks.
func finishNamespaceDeletion(t *testing.T, name string) {
	t.Helper()
	ns := &corev1.Namespace{}
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, ns); err != nil {
		t.Fatalf("get Namespace %s: %v", name, err)
	}
	ns.Spec.Finalizers = nil
	if err := testClient.SubResource("finalize").Update(context.Background(), ns); err != nil {
		t.Fatalf("finalize Namespace %s: %v", name, err)
	}
}
