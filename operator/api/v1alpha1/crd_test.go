package v1alpha1_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// The generated CRDs accept the PlatformWorkspace of PRODUCT-PLAN §14.2 as
// it is written there and give it back unchanged, and refuse what the
// schema forbids.
func TestCRDs(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set: run make operator-test")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ws := &v1.PlatformWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-solar", Labels: map[string]string{
			v1.LabelOrganizationID: "org-01jabc", v1.LabelWorkspaceID: "ws-01jsol", v1.LabelClusterID: "cluster-a",
		}},
		Spec: v1.PlatformWorkspaceSpec{
			OrganizationID: "org-01jabc", WorkspaceID: "ws-01jsol", ClusterID: "cluster-a", DisplayName: "Solar",
			NamespaceQuota: 3,
			Namespaces:     []string{"frontend", "backend"},
			Routes: []v1.WorkspaceRoute{{
				Name: "web", Namespace: "acme-solar-frontend", Host: "web-acme-solar.mvp.tenantry.site", Service: "web", Port: 80,
			}},
			Resources: v1.ResourceList{
				"requests.cpu": resource.MustParse("2"), "requests.memory": resource.MustParse("4Gi"),
				"limits.cpu": resource.MustParse("4"), "limits.memory": resource.MustParse("8Gi"),
				"pods": resource.MustParse("20"), "requests.storage": resource.MustParse("10Gi"),
			},
			Compute:        v1.Compute{Mode: v1.ComputeShared, SharedPoolID: "shared-general"},
			Network:        &v1.Network{AllocationRef: "netalloc-01j123", CIDR: "10.240.12.0/24"},
			StorageClasses: []string{"standard"},
			AccessBindings: []v1.AccessBinding{
				{GroupSubject: "oidc:org_01jabc:group_01jadmin", Role: v1.RoleTenantOwner},
				{GroupSubject: "oidc:org_01jabc:group_01jdev", Role: v1.RoleDeveloper},
			},
		},
	}
	want := ws.Spec.DeepCopy()
	if err := c.Create(ctx, ws); err != nil {
		t.Fatalf("create PlatformWorkspace: %v", err)
	}
	got := &v1.PlatformWorkspace{}
	if err := c.Get(ctx, client.ObjectKey{Name: "acme-solar"}, got); err != nil {
		t.Fatal(err)
	}
	if !equality.Semantic.DeepEqual(*want, got.Spec) {
		t.Errorf("spec changed on the way:\nwant %+v\ngot  %+v", *want, got.Spec)
	}

	got.Status = v1.PlatformWorkspaceStatus{
		Phase: v1.PhaseReady, ObservedGeneration: got.Generation, CapsuleTenantRef: "acme-solar",
		Conditions: []metav1.Condition{{
			Type: v1.ConditionCapsuleTenantReady, Status: metav1.ConditionTrue, Reason: "Reconciled", LastTransitionTime: metav1.Now(),
		}},
		Usage: v1.ResourceList{"pods": resource.MustParse("6")},
		Namespaces: []v1.NamespaceStatus{
			{Name: "acme-solar-frontend", Source: v1.SourcePortal, Usage: v1.ResourceList{"pods": resource.MustParse("4")}},
			{Name: "acme-solar-scratch", Source: v1.SourceKubectl},
		},
		Routes: []v1.RouteStatus{{Name: "web", Namespace: "acme-solar-frontend", Ready: true}},
	}
	if err := c.Status().Update(ctx, got); err != nil {
		t.Fatalf("status update: %v", err)
	}

	refused := map[string]client.Object{
		"compute mode outside the enum": &v1.PlatformWorkspace{
			ObjectMeta: metav1.ObjectMeta{Name: "acme-bad"},
			Spec:       v1.PlatformWorkspaceSpec{OrganizationID: "o", WorkspaceID: "w", ClusterID: "c", Compute: v1.Compute{Mode: "Mixed"}},
		},
		"role outside the enum": &v1.PlatformWorkspace{
			ObjectMeta: metav1.ObjectMeta{Name: "acme-bad"},
			Spec: v1.PlatformWorkspaceSpec{OrganizationID: "o", WorkspaceID: "w", ClusterID: "c", Compute: v1.Compute{Mode: v1.ComputeShared},
				AccessBindings: []v1.AccessBinding{{GroupSubject: "g", Role: "Admin"}}},
		},
		"shared pool named system": &v1.PlatformSharedPool{ObjectMeta: metav1.ObjectMeta{Name: v1.SystemPool}},
	}
	for name, obj := range refused {
		if err := c.Create(ctx, obj); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	grant := &v1.PlatformNodeGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "nodegrant-a1"},
		Spec:       v1.PlatformNodeGrantSpec{NodeName: "fdp-k8s-wk3", OrganizationID: "org-01jabc"},
	}
	if err := c.Create(ctx, grant); err != nil {
		t.Fatal(err)
	}
	grant.Spec.NodeName = "fdp-k8s-wk2"
	if err := c.Update(ctx, grant); err == nil {
		t.Error("PlatformNodeGrant: nodeName changed")
	}
	if err := c.Create(ctx, &v1.PlatformSharedPool{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-general"}, Spec: v1.PlatformSharedPoolSpec{Nodes: []string{"fdp-k8s-wk2"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, &v1.PlatformNodeInventory{ObjectMeta: metav1.ObjectMeta{Name: v1.NodeInventoryName}}); err != nil {
		t.Fatal(err)
	}
}
