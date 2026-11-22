//go:build lab

// Package isolation is the cluster side of the isolation suite (PRODUCT-PLAN
// §24.4): one subtest per threat case of §20.3 that the cluster enforces,
// run against a real cluster (make operator-isolation KUBE_CONTEXT=...).
// It provisions two Workspaces of two Organizations through the operator,
// acts as their tenants by impersonation (direct API) and as the proxyaccess
// probe (Capsule Proxy), and deletes everything it made.
package isolation

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

const image = "registry.k8s.io/e2e-test-images/agnhost:2.53"

var (
	cfg    *rest.Config
	admin  client.Client
	scheme = runtime.NewScheme()
	ctx    = context.Background()
)

func TestMain(m *testing.M) {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	kubeContext := os.Getenv("KUBE_CONTEXT")
	var err error
	cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: kubeContext}).ClientConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if admin, err = client.New(cfg, client.Options{Scheme: scheme}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// tenant is an oidc: user of org, a member of its owner group, as Keycloak
// tokens name them (architecture/05 §8).
func tenant(t *testing.T, org string) client.Client {
	t.Helper()
	c := rest.CopyConfig(cfg)
	c.Impersonate = rest.ImpersonationConfig{
		UserName: "oidc:iso-owner-" + org,
		Groups:   []string{"oidc:org_" + org + ":group_owner", "oidc:tenantry:tenant-users", "system:authenticated"},
	}
	cl, err := client.New(c, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

func workspace(name, org, cidr string) *v1.PlatformWorkspace {
	return &v1.PlatformWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1.PlatformWorkspaceSpec{
			OrganizationID: "org-" + org, WorkspaceID: "ws-" + name, ClusterID: "cluster-a", DisplayName: name,
			NamespaceQuota: 3, Namespaces: []string{"web", "api"},
			Resources: v1.ResourceList{
				"requests.cpu": resource.MustParse("1"), "requests.memory": resource.MustParse("1Gi"),
				"pods": resource.MustParse("8"), "requests.storage": resource.MustParse("2Gi"),
			},
			Compute:        v1.Compute{Mode: v1.ComputeShared, SharedPoolID: "shared-general"},
			Network:        &v1.Network{AllocationRef: "netalloc-" + name, CIDR: cidr},
			StorageClasses: []string{"standard"},
			AccessBindings: []v1.AccessBinding{{GroupSubject: "oidc:org_" + org + ":group_owner", Role: v1.RoleTenantOwner}},
		},
	}
}

// refused: err must be an admission or authorization refusal.
func refused(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: allowed, want refused", what)
		return
	}
	if !apierrors.IsForbidden(err) && !apierrors.IsInvalid(err) && !apierrors.IsBadRequest(err) {
		t.Errorf("%s: %v, want a refusal", what, err)
		return
	}
	t.Logf("refused as wanted: %s: %s", what, firstLine(err.Error()))
}

func allowed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
	}
}

func firstLine(s string) string {
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.SplitN(s, "\n", 2)[0]
}

func pod(ns, name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name: "c", Image: image, Args: []string{"pause"},
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi"),
				}},
			}},
		},
	}
	for _, m := range mutate {
		m(p)
	}
	return p
}

func waitFor(t *testing.T, what string, timeout time.Duration, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		done, err := check()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out (%v)", what, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// connects runs agnhost connect from a Pod in ns and says whether it got through.
func connects(t *testing.T, ns, name, target string) bool {
	t.Helper()
	p := pod(ns, name, func(p *corev1.Pod) {
		p.Spec.Containers[0].Args = []string{"connect", target, "--timeout=3s"}
	})
	if err := admin.Create(ctx, p); err != nil {
		t.Fatalf("probe Pod %s/%s: %v", ns, name, err)
	}
	defer func() { _ = admin.Delete(ctx, p) }()
	var phase corev1.PodPhase
	waitFor(t, "probe Pod "+name, 2*time.Minute, func() (bool, error) {
		if err := admin.Get(ctx, client.ObjectKeyFromObject(p), p); err != nil {
			return false, err
		}
		phase = p.Status.Phase
		return phase == corev1.PodSucceeded || phase == corev1.PodFailed, nil
	})
	return phase == corev1.PodSucceeded
}

func TestIsolation(t *testing.T) {
	a, b := workspace("optest-iso-a", "01isoa", "10.240.250.0/24"), workspace("optest-iso-b", "01isob", "10.240.251.0/24")
	for _, ws := range []*v1.PlatformWorkspace{a, b} {
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = admin.Delete(ctx, ws)
			waitFor(t, "delete "+ws.Name, 3*time.Minute, func() (bool, error) {
				err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, &v1.PlatformWorkspace{})
				return apierrors.IsNotFound(err), err
			})
		})
	}
	for _, ws := range []*v1.PlatformWorkspace{a, b} {
		waitFor(t, ws.Name+" Ready", 3*time.Minute, func() (bool, error) {
			got := &v1.PlatformWorkspace{}
			err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, got)
			return got.Status.Phase == v1.PhaseReady, err
		})
	}
	ownerA, ownerB := tenant(t, "01isoa"), tenant(t, "01isob")
	aWeb, aAPI, bWeb := "optest-iso-a-web", "optest-iso-a-api", "optest-iso-b-web"

	t.Run("T08 owner binds outsiders in a RoleBinding", func(t *testing.T) {
		bind := func(name string, s rbacv1.Subject) error {
			return ownerA.Create(ctx, &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: aWeb},
				RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "view"},
				Subjects:   []rbacv1.Subject{s},
			}, client.DryRunAll)
		}
		group := func(n string) rbacv1.Subject {
			return rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: "Group", Name: n}
		}
		allowed(t, "own Organization's group", bind("own", group("oidc:org_01isoa:group_dev")))
		refused(t, "another Organization's group", bind("other-org", group("oidc:org_01isob:group_dev")))
		refused(t, "every tenant user", bind("all", group("oidc:tenantry:tenant-users")))
		refused(t, "a user outside", bind("user", rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: "User", Name: "oidc:mallory"}))
		refused(t, "another Tenant's ServiceAccount", bind("sa", rbacv1.Subject{Kind: "ServiceAccount", Namespace: bWeb, Name: "default"}))
	})

	t.Run("T13 T14 read another Workspace, Nodes, PVs", func(t *testing.T) {
		refused(t, "get B's Namespace", ownerA.Get(ctx, client.ObjectKey{Name: bWeb}, &corev1.Namespace{}))
		refused(t, "list B's Pods", ownerA.List(ctx, &corev1.PodList{}, client.InNamespace(bWeb)))
		refused(t, "list B's Secrets", ownerA.List(ctx, &corev1.SecretList{}, client.InNamespace(bWeb)))
		refused(t, "list every Namespace", ownerA.List(ctx, &corev1.NamespaceList{}))
		refused(t, "list Nodes", ownerA.List(ctx, &corev1.NodeList{}))
		refused(t, "list PersistentVolumes", ownerA.List(ctx, &corev1.PersistentVolumeList{}))
		allowed(t, "list own Pods", ownerA.List(ctx, &corev1.PodList{}, client.InNamespace(aWeb)))
	})

	t.Run("T15 cross-Workspace network", func(t *testing.T) {
		for _, s := range []struct{ ns, name string }{{aWeb, "srv-a"}, {bWeb, "srv-b"}} {
			srv := pod(s.ns, s.name, func(p *corev1.Pod) {
				p.Spec.RestartPolicy = corev1.RestartPolicyAlways
				p.Spec.Containers[0].Args = []string{"netexec", "--http-port=8080"}
			})
			if err := admin.Create(ctx, srv); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = admin.Delete(ctx, srv) })
			waitFor(t, s.name+" Ready", 2*time.Minute, func() (bool, error) {
				err := admin.Get(ctx, client.ObjectKeyFromObject(srv), srv)
				return srv.Status.PodIP != "" && srv.Status.Phase == corev1.PodRunning, err
			})
		}
		ipOf := func(ns, name string) string {
			p := &corev1.Pod{}
			_ = admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, p)
			return p.Status.PodIP + ":8080"
		}
		if !connects(t, aAPI, "a-to-a", ipOf(aWeb, "srv-a")) {
			t.Error("A cannot reach A in another Namespace")
		}
		if connects(t, aAPI, "a-to-b", ipOf(bWeb, "srv-b")) {
			t.Error("A reaches B")
		}
		if connects(t, bWeb, "b-to-a", ipOf(aWeb, "srv-a")) {
			t.Error("B reaches A")
		}
		if connects(t, aAPI, "a-to-metadata", "169.254.169.254:80") {
			t.Error("A reaches the metadata endpoint")
		}
	})

	t.Run("T16 Calico annotation, node selector, toleration, node name", func(t *testing.T) {
		ns := &corev1.Namespace{}
		if err := ownerA.Get(ctx, client.ObjectKey{Name: aWeb}, ns); err == nil {
			ns.Annotations["cni.projectcalico.org/ipv4pools"] = `["optest-iso-b"]`
			_ = ownerA.Update(ctx, ns)
		}
		_ = admin.Get(ctx, client.ObjectKey{Name: aWeb}, ns)
		if got := ns.Annotations["cni.projectcalico.org/ipv4pools"]; got != `["optest-iso-a"]` {
			t.Errorf("Namespace pool annotation is %s after the tenant's change", got)
		}
		refused(t, "Pod with ipv4pools", ownerA.Create(ctx, pod(aWeb, "x1", func(p *corev1.Pod) {
			p.Annotations = map[string]string{"cni.projectcalico.org/ipv4pools": `["optest-iso-b"]`}
		}), client.DryRunAll))
		refused(t, "Pod with ipAddrs", ownerA.Create(ctx, pod(aWeb, "x2", func(p *corev1.Pod) {
			p.Annotations = map[string]string{"cni.projectcalico.org/ipAddrs": `["10.240.251.9"]`}
		}), client.DryRunAll))
		refused(t, "Pod on the system nodes", ownerA.Create(ctx, pod(aWeb, "x3", func(p *corev1.Pod) {
			p.Spec.NodeSelector = map[string]string{v1.LabelComputePool: v1.SystemPool}
		}), client.DryRunAll))
		refused(t, "Pod tolerating the system taint", ownerA.Create(ctx, pod(aWeb, "x4", func(p *corev1.Pod) {
			p.Spec.Tolerations = []corev1.Toleration{{Key: v1.LabelComputePool, Operator: corev1.TolerationOpExists}}
		}), client.DryRunAll))
		refused(t, "Pod naming a node", ownerA.Create(ctx, pod(aWeb, "x5", func(p *corev1.Pod) { p.Spec.NodeName = "fdp-k8s-wk1" }), client.DryRunAll))
	})

	t.Run("T17 privileged Pod and NodePort", func(t *testing.T) {
		yes := true
		refused(t, "privileged Pod", ownerA.Create(ctx, pod(aWeb, "priv", func(p *corev1.Pod) {
			p.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: &yes}
		}), client.DryRunAll))
		refused(t, "hostNetwork Pod", ownerA.Create(ctx, pod(aWeb, "hostnet", func(p *corev1.Pod) { p.Spec.HostNetwork = true }), client.DryRunAll))
		for _, typ := range []corev1.ServiceType{corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer} {
			refused(t, string(typ)+" Service", ownerA.Create(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: strings.ToLower(string(typ)), Namespace: aWeb},
				Spec:       corev1.ServiceSpec{Type: typ, Ports: []corev1.ServicePort{{Port: 80}}},
			}, client.DryRunAll))
		}
	})

	t.Run("T18 Ingress and Certificate", func(t *testing.T) {
		class := "platform-shared"
		refused(t, "Ingress for another host", ownerA.Create(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "steal", Namespace: aWeb},
			Spec:       networkingv1.IngressSpec{IngressClassName: &class, Rules: []networkingv1.IngressRule{{Host: "web-optest-iso-b.mvp.tenantry.site"}}},
		}, client.DryRunAll))
		cert := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
			"metadata": map[string]any{"name": "steal", "namespace": aWeb},
			"spec": map[string]any{"secretName": "steal", "dnsNames": []any{"portal.tenantry.site"},
				"issuerRef": map[string]any{"kind": "ClusterIssuer", "name": "letsencrypt"}},
		}}
		refused(t, "Certificate for a platform host", ownerA.Create(ctx, cert, client.DryRunAll))
	})

	t.Run("T19 quotas: CPU, Pods, storage, StorageClass, Namespaces", func(t *testing.T) {
		refused(t, "Pod above the CPU quota", ownerA.Create(ctx, pod(aWeb, "big", func(p *corev1.Pod) {
			p.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")
		})))
		standard := "standard"
		pvc := func(name, class, size string) *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: aWeb},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: &class,
					Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)}},
				},
			}
		}
		refused(t, "PVC above the storage quota", ownerA.Create(ctx, pvc("big", standard, "5Gi")))
		refused(t, "PVC of a StorageClass outside the allow-list", ownerA.Create(ctx, pvc("other", "fast", "1Gi")))
		// namespaceQuota 3 with 2 Namespaces: one more, then refused.
		third := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "optest-iso-a-third"}}
		allowed(t, "third Namespace", ownerA.Create(ctx, third))
		t.Cleanup(func() { _ = admin.Delete(ctx, third) })
		time.Sleep(3 * time.Second) // Capsule counts Namespaces from the Tenant status
		refused(t, "fourth Namespace", ownerA.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "optest-iso-a-fourth"}}))
		refused(t, "Namespace in B's name", ownerA.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "optest-iso-b-mine"}}))
	})

	t.Run("T15b B cannot use A's names either", func(t *testing.T) {
		refused(t, "B lists A's Pods", ownerB.List(ctx, &corev1.PodList{}, client.InNamespace(aWeb)))
	})
}
