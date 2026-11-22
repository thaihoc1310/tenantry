package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	testPool     = "test-pool"
	testPoolNode = "test-pool-node"
)

// createReadyNode creates a Ready node with 4 CPU, 8Gi and 110 Pods.
func createReadyNode(name string, labels map[string]string) error {
	ctx := context.Background()
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	if err := testClient.Create(ctx, n); err != nil {
		return err
	}
	n.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi"), corev1.ResourcePods: resource.MustParse("110"),
	}
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
	return testClient.Status().Update(ctx, n)
}

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
	// The shared pool of the Workspaces in these tests: one Ready node.
	if err := createReadyNode(testPoolNode, map[string]string{v1.LabelComputePool: testPool}); err != nil {
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
		APIReader:    testClient,
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
	// The LimitRange replication too, once the policy step has applied it.
	rep := &unstructured.Unstructured{}
	rep.SetGroupVersionKind(render.ReplicationGVK)
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, rep); err != nil {
		return
	}
	rep.Object["status"] = map[string]any{"size": int64(0), "conditions": []any{readyCondition()}}
	if err := testClient.Status().Update(context.Background(), rep); err != nil {
		t.Fatalf("set GlobalTenantResource status: %v", err)
	}
}

func readyCondition() map[string]any {
	return map[string]any{
		"type": "Ready", "status": "True", "reason": "Succeeded", "message": "reconciled",
		"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
	}
}

// capsuleQuota plays Capsule for the quota rule: the GlobalResourceQuota
// <tenant>-workspace with hard and the usage given.
func capsuleQuota(t *testing.T, tenant string, hard map[string]any, used map[string]map[string]any) {
	t.Helper()
	ctx := context.Background()
	q := &unstructured.Unstructured{}
	q.SetGroupVersionKind(render.QuotaGVK)
	q.SetName(tenant + "-" + render.QuotaName)
	q.SetLabels(map[string]string{labelQuotaTenant: tenant})
	q.Object["spec"] = map[string]any{"quota": map[string]any{"hard": hard}}
	if err := testClient.Create(ctx, q); err != nil {
		t.Fatalf("create GlobalResourceQuota: %v", err)
	}
	total := map[string]any{}
	namespaceUsage := map[string]any{}
	for ns, u := range used {
		namespaceUsage[ns] = map[string]any{"used": u}
		for k, v := range u {
			total[k] = v // one Namespace per resource in the tests
		}
	}
	q.Object["status"] = map[string]any{
		"conditions":     []any{readyCondition()},
		"total":          map[string]any{"hard": hard, "used": total},
		"namespaceUsage": namespaceUsage,
	}
	if err := testClient.Status().Update(ctx, q); err != nil {
		t.Fatalf("set GlobalResourceQuota status: %v", err)
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
