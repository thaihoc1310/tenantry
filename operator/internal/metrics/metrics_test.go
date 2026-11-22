package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

func TestCollector(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.AddKnownTypeWithName(render.IPAMBlockGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(render.IPAMBlockGVK.GroupVersion().WithKind("IPAMBlockList"), &unstructured.UnstructuredList{})
	block := func(name, cidr string, allocations ...any) *unstructured.Unstructured {
		b := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"cidr": cidr, "allocations": allocations}}}
		b.SetGroupVersionKind(render.IPAMBlockGVK)
		b.SetName(name)
		return b
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1.PlatformWorkspace{
			ObjectMeta: metav1.ObjectMeta{Name: "acme-solar"},
			Spec:       v1.PlatformWorkspaceSpec{Network: &v1.Network{AllocationRef: "a", CIDR: "10.240.64.0/24"}},
			Status: v1.PlatformWorkspaceStatus{Phase: v1.PhaseReady, Routes: []v1.RouteStatus{
				{Name: "web", Ready: true}, {Name: "api", Ready: false},
			}},
		},
		&v1.PlatformWorkspace{ObjectMeta: metav1.ObjectMeta{Name: "acme-water"}, Status: v1.PlatformWorkspaceStatus{Phase: v1.PhaseProvisioning}},
		&v1.PlatformSharedPool{
			ObjectMeta: metav1.ObjectMeta{Name: "shared-general"},
			Status:     v1.PlatformSharedPoolStatus{ReadyNodes: 2, Allocatable: v1.ResourceList{"cpu": resource.MustParse("6")}},
		},
		block("b1", "10.240.64.0/26", int64(0), nil, int64(1)),
		block("b2", "10.240.70.0/26", int64(0)),
	).Build()

	want := `
# HELP tenantry_routes Routes by readiness.
# TYPE tenantry_routes gauge
tenantry_routes{ready="false"} 1
tenantry_routes{ready="true"} 1
# HELP tenantry_shared_pool_allocatable Allocatable of a shared pool.
# TYPE tenantry_shared_pool_allocatable gauge
tenantry_shared_pool_allocatable{pool="shared-general",resource="cpu"} 6
# HELP tenantry_shared_pool_ready_nodes Ready nodes of a shared pool.
# TYPE tenantry_shared_pool_ready_nodes gauge
tenantry_shared_pool_ready_nodes{pool="shared-general"} 2
# HELP tenantry_workspace_ip_capacity Addresses in a Workspace's CIDR.
# TYPE tenantry_workspace_ip_capacity gauge
tenantry_workspace_ip_capacity{workspace="acme-solar"} 256
# HELP tenantry_workspace_ip_used Addresses Calico gave out in a Workspace's CIDR.
# TYPE tenantry_workspace_ip_used gauge
tenantry_workspace_ip_used{workspace="acme-solar"} 2
# HELP tenantry_workspaces PlatformWorkspaces by phase.
# TYPE tenantry_workspaces gauge
tenantry_workspaces{phase="Provisioning"} 1
tenantry_workspaces{phase="Ready"} 1
`
	if err := testutil.CollectAndCompare(&collector{reader: c}, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}
