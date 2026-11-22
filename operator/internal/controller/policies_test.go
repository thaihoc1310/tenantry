package controller

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// applyChartPolicies renders templates/policies.yaml of the operator chart
// with helm, as the lab values do, and creates its objects in envtest.
func applyChartPolicies(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	chart := filepath.Join("..", "..", "..", "deploy", "charts", "tenantry-operator")
	out, err := exec.Command("helm", "template", "tenantry-operator", chart, "--namespace", "tenantry-operator",
		"--set", "nodeLabelAdmins={kubeadm:cluster-admins}", "--show-only", "templates/policies.yaml").Output()
	if err != nil {
		t.Fatalf("helm template: %v", err)
	}
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := dec.Decode(&obj.Object); err != nil {
			break
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := testClient.Create(context.Background(), obj); err != nil {
			t.Fatalf("%s %s: %v", obj.GetKind(), obj.GetName(), err)
		}
		// The envtest admin is neither the operator nor an admin group: the
		// other tests, which relabel nodes as admin, run without the policies.
		t.Cleanup(func() { _ = testClient.Delete(context.Background(), obj) })
	}
}

// as is a client acting as user with groups; the group test:writers gets
// write rights below, so what refuses it is the policy, not RBAC.
func as(t *testing.T, user string, groups ...string) client.Client {
	t.Helper()
	cfg := rest.CopyConfig(testCfg)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: user, Groups: append(groups, "test:writers", "system:authenticated")}
	c, err := client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// expectRefused retries while the API server picks the policy up.
func expectRefused(t *testing.T, what string, try func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := try()
		if apierrors.IsForbidden(err) && strings.Contains(err.Error(), "ValidatingAdmissionPolicy") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: got %v, want a refusal by a ValidatingAdmissionPolicy", what, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func expectAllowed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
	}
}

// The chart's policies (architecture/05 §2 and §5, 06 §7): only the
// operator and the admin group change tenantry.io/* labels and taints on
// Nodes; a tenant can neither name a node for its Pods nor pick their
// addresses.
func TestChartPolicies(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	applyChartPolicies(t)
	if err := testClient.Create(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "test-writers"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"create"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"create"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test-writers"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "test-writers"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: "test:writers"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := createReadyNode("vap-node", map[string]string{v1.LabelComputePool: "shared-general"}); err != nil {
		t.Fatal(err)
	}
	relabel := func(c client.Client, mutate func(*corev1.Node)) func() error {
		return func() error {
			n := &corev1.Node{}
			if err := testClient.Get(ctx, client.ObjectKey{Name: "vap-node"}, n); err != nil {
				return err
			}
			mutate(n)
			return c.Update(ctx, n)
		}
	}
	setLabel := func(k, v string) func(*corev1.Node) { return func(n *corev1.Node) { n.Labels[k] = v } }

	someSA := as(t, "system:serviceaccount:default:someone", "system:serviceaccounts")
	tenant := as(t, "oidc:mallory", "oidc:org_01jabc:group_owner")
	// Policies take a moment to load: wait with a dry run, which changes nothing.
	expectRefused(t, "the node policy loads", func() error {
		n := &corev1.Node{}
		if err := testClient.Get(ctx, client.ObjectKey{Name: "vap-node"}, n); err != nil {
			return err
		}
		n.Labels[v1.LabelComputePool] = "probe"
		return someSA.Update(ctx, n, client.DryRunAll)
	})
	expectRefused(t, "another ServiceAccount moves the node to another pool",
		relabel(someSA, setLabel(v1.LabelComputePool, "shared-big")))
	expectRefused(t, "another ServiceAccount adds a membership label",
		relabel(someSA, setLabel("workspaces.tenantry.io/ws-x", "true")))
	expectRefused(t, "a tenant gives the node to an Organization",
		relabel(tenant, setLabel(v1.LabelDedicatedOrg, "org-01jabc")))
	expectRefused(t, "another ServiceAccount removes the pool label",
		relabel(someSA, func(n *corev1.Node) { delete(n.Labels, v1.LabelComputePool) }))
	expectRefused(t, "another ServiceAccount adds a dedicated-org taint",
		relabel(someSA, func(n *corev1.Node) {
			n.Spec.Taints = append(n.Spec.Taints, corev1.Taint{Key: v1.LabelDedicatedOrg, Value: "org-x", Effect: corev1.TaintEffectNoSchedule})
		}))
	expectAllowed(t, "another ServiceAccount sets an unrelated label",
		relabel(someSA, setLabel("example.com/rack", "r1"))())
	expectAllowed(t, "the operator moves the node",
		relabel(as(t, operatorSA, "system:serviceaccounts", "system:serviceaccounts:tenantry-operator"), setLabel(v1.LabelComputePool, "shared-big"))())
	expectAllowed(t, "the kubeadm admin group moves it back",
		relabel(as(t, "kubernetes-admin", "kubeadm:cluster-admins"), setLabel(v1.LabelComputePool, "shared-general"))())

	// spec.nodeName in a tenant Namespace.
	if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "acme-vap-web", Labels: map[string]string{render.LabelCapsuleTenant: "acme-vap"},
	}}); err != nil {
		t.Fatal(err)
	}
	pod := func(name, node string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "acme-vap-web"},
			Spec:       corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "c", Image: "nginx"}}},
		}
	}
	expectRefused(t, "a tenant names a node", func() error { return tenant.Create(ctx, pod("pinned", "vap-node")) })
	expectAllowed(t, "a tenant Pod without nodeName", tenant.Create(ctx, pod("free", "")))
	expectAllowed(t, "a controller names a node (not a tenant caller)",
		as(t, "system:serviceaccount:kube-system:daemon-set-controller").Create(ctx, pod("ds", "vap-node")))
	deploy := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "pinned", "namespace": "acme-vap-web"},
		"spec": map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"app": "x"}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "x"}},
				"spec":     map[string]any{"nodeName": "vap-node", "containers": []any{map[string]any{"name": "c", "image": "nginx"}}},
			},
		},
	}}
	expectRefused(t, "a tenant Deployment names a node", func() error { return tenant.Create(ctx, deploy.DeepCopy()) })

	// Calico annotations on Pods.
	annotated := func(name, key string) *corev1.Pod {
		p := pod(name, "")
		p.Annotations = map[string]string{key: `["acme-other"]`}
		return p
	}
	for i, key := range []string{"ipv4pools", "ipv6pools", "ipAddrs", "ipAddrsNoIpam", "floatingIPs"} {
		expectRefused(t, "a tenant Pod picks its addresses with "+key, func() error {
			return tenant.Create(ctx, annotated(fmt.Sprintf("addr-%d", i), "cni.projectcalico.org/"+key))
		})
	}
	expectAllowed(t, "a tenant Pod with another annotation", tenant.Create(ctx, annotated("other", "example.com/note")))
	expectAllowed(t, "a controller's Pod with a pool annotation (not a tenant caller)",
		as(t, "system:serviceaccount:kube-system:replicaset-controller").Create(ctx, annotated("rs", "cni.projectcalico.org/ipv4pools")))
	poolDeploy := deploy.DeepCopy()
	poolDeploy.SetName("pool")
	unstructured.RemoveNestedField(poolDeploy.Object, "spec", "template", "spec", "nodeName")
	_ = unstructured.SetNestedStringMap(poolDeploy.Object, map[string]string{"cni.projectcalico.org/ipv4pools": `["acme-other"]`},
		"spec", "template", "metadata", "annotations")
	expectRefused(t, "a tenant Deployment picks its addresses", func() error { return tenant.Create(ctx, poolDeploy.DeepCopy()) })
	plain := deploy.DeepCopy()
	plain.SetName("plain")
	unstructured.RemoveNestedField(plain.Object, "spec", "template", "spec", "nodeName")
	expectAllowed(t, "a tenant Deployment without them", tenant.Create(ctx, plain))
}
