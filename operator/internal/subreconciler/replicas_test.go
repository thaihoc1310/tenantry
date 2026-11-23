package subreconciler

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

func replicaWorkspace(network bool) *v1.PlatformWorkspace {
	ws := &v1.PlatformWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-solar"},
		Spec: v1.PlatformWorkspaceSpec{
			OrganizationID: "org-01jabc", WorkspaceID: "ws-solar", ClusterID: "cluster-a",
			Compute: v1.Compute{Mode: v1.ComputeShared, SharedPoolID: "shared-general"},
		},
	}
	if network {
		ws.Spec.Network = &v1.Network{AllocationRef: "netalloc-solar", CIDR: "10.240.0.0/24"}
	}
	return ws
}

// Without a network only the LimitRange is copied, so only it is expected;
// with one, the baseline NetworkPolicies too.
func TestReplicaItems(t *testing.T) {
	names := func(ws *v1.PlatformWorkspace) []string {
		var out []string
		for _, it := range items(render.Replication(ws)) {
			out = append(out, it.gvk.Kind+"/"+it.name)
		}
		return out
	}
	if got := names(replicaWorkspace(false)); len(got) != 1 || got[0] != "LimitRange/"+render.LimitRangeName {
		t.Errorf("no network: %v, want only the LimitRange", got)
	}
	got := names(replicaWorkspace(true))
	policies := 0
	for _, n := range got {
		if strings.HasPrefix(n, "NetworkPolicy/") {
			policies++
		}
	}
	if policies == 0 || !strings.Contains(strings.Join(got, ","), "NetworkPolicy/tenant-default-deny") {
		t.Errorf("with a network: %v, want the baseline NetworkPolicies", got)
	}
}

func replicaClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func tenantNamespace(name string, uid string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: name, UID: k8stypes.UID(uid), Labels: map[string]string{render.LabelCapsuleTenant: "acme-solar"},
	}}
}

// A Namespace being deleted is skipped; a live one without its copies is
// named, looked at again every 2 s for a minute, then every 30 s; once its
// copies are there it is complete and the wait starts over next time.
func TestReplicasMissing(t *testing.T) {
	ctx := context.Background()
	ws := replicaWorkspace(true)
	rep := render.Replication(ws)

	gone := tenantNamespace("acme-solar-old", "uid-old")
	gone.Finalizers = []string{"kubernetes"}
	gone.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	live := tenantNamespace("acme-solar-web", "uid-web")
	c := replicaClient(t, gone, live)
	r := &Replicas{Reader: c}

	t0 := time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)
	defer func() { now = time.Now }()
	now = func() time.Time { return t0 }

	missing, again, err := r.Missing(ctx, c, ws, rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(missing, "acme-solar-old") {
		t.Errorf("a Namespace being deleted is named: %s", missing)
	}
	if !strings.Contains(missing, "acme-solar-web: LimitRange/"+render.LimitRangeName) || !strings.Contains(missing, "NetworkPolicy/tenant-default-deny") {
		t.Errorf("missing %q", missing)
	}
	if again != replicaSoon {
		t.Errorf("first look: again %s, want %s", again, replicaSoon)
	}

	now = func() time.Time { return t0.Add(replicaSoonFor + time.Second) }
	if _, again, _ = r.Missing(ctx, c, ws, rep); again != replicaRetry {
		t.Errorf("after a minute: again %s, want %s", again, replicaRetry)
	}

	// Capsule copies everything into the live Namespace.
	for _, it := range items(rep) {
		var obj client.Object
		switch it.gvk.Kind {
		case "LimitRange":
			obj = &corev1.LimitRange{}
		case "NetworkPolicy":
			obj = &networkingv1.NetworkPolicy{}
		default:
			t.Fatalf("unexpected kind %s", it.gvk.Kind)
		}
		obj.SetName(it.name)
		obj.SetNamespace(live.Name)
		if err := c.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}
	if missing, again, err = r.Missing(ctx, c, ws, rep); err != nil || missing != "" || again != 0 {
		t.Errorf("with every copy: %q %s %v", missing, again, err)
	}

	// A new Namespace later: the wait starts over at 2 s.
	if err := c.Create(ctx, tenantNamespace("acme-solar-api", "uid-api")); err != nil {
		t.Fatal(err)
	}
	if missing, again, _ = r.Missing(ctx, c, ws, rep); !strings.HasPrefix(missing, "acme-solar-api:") || again != replicaSoon {
		t.Errorf("a new Namespace: %q, again %s", missing, again)
	}
}
