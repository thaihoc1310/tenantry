package render

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

var opts = Options{
	OperatorServiceAccount: "system:serviceaccount:tenantry-operator:tenantry-operator",
	AllowedRegistries:      []string{"docker.io", "ghcr.io"},
}

// solar is the Workspace of PRODUCT-PLAN §14.2, with a Viewer group too.
func solar() *v1.PlatformWorkspace {
	return &v1.PlatformWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-solar"},
		Spec: v1.PlatformWorkspaceSpec{
			OrganizationID: "org-01jabc", WorkspaceID: "ws-01jsol", ClusterID: "cluster-a", DisplayName: "Solar",
			NamespaceQuota: 3,
			Namespaces:     []string{"frontend", "backend"},
			Resources:      v1.ResourceList{"requests.cpu": resource.MustParse("2")},
			Compute:        v1.Compute{Mode: v1.ComputeShared, SharedPoolID: "shared-general"},
			StorageClasses: []string{"standard"},
			// Out of order on purpose: the output is sorted.
			AccessBindings: []v1.AccessBinding{
				{GroupSubject: "oidc:org_01jabc:group_01jview", Role: v1.RoleViewer},
				{GroupSubject: "oidc:org_01jabc:group_01jdev", Role: v1.RoleDeveloper},
				{GroupSubject: "oidc:org_01jabc:group_01jops", Role: v1.RoleTenantOwner},
				{GroupSubject: "oidc:org_01jabc:group_01jadmin", Role: v1.RoleTenantOwner},
			},
		},
	}
}

func TestGolden(t *testing.T) {
	water := solar()
	water.Name, water.Spec.WorkspaceID = "acme-water", "ws-01jwat"
	water.Spec.Compute = v1.Compute{Mode: v1.ComputeDedicated, DedicatedNodeGrantID: "nodegrant-a1", NodeName: "worker-a-05"}
	cordoned := solar()
	cordoned.Spec.Cordoned = true
	cordoned.Spec.NamespaceQuota = 0
	cases := map[string]*unstructured.Unstructured{
		"tenant":                Tenant(solar(), opts, false),
		"tenant-revoked":        Tenant(solar(), opts, true),
		"tenant-cordoned":       Tenant(cordoned, opts, false),
		"namespace":             Namespace(solar(), "frontend"),
		"replication":           Replication(solar()),
		"tenant-dedicated":      Tenant(water, opts, false),
		"replication-dedicated": Replication(water),
	}
	for name, obj := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := yaml.Marshal(obj.Object)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".yaml")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/render -update)", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s differs from %s (go test ./internal/render -update rewrites it):\n%s", name, path, got)
			}
		})
	}
}

// Rendering twice gives the same object: applying it again is a no-op.
func TestDeterministic(t *testing.T) {
	a, _ := yaml.Marshal(Tenant(solar(), opts, false).Object)
	for range 20 {
		b, _ := yaml.Marshal(Tenant(solar(), opts, false).Object)
		if string(a) != string(b) {
			t.Fatal("Tenant output changes between calls")
		}
	}
}

func TestOrgULID(t *testing.T) {
	if got := OrgULID("org-01jabc"); got != "01jabc" {
		t.Fatalf("got %q", got)
	}
}
