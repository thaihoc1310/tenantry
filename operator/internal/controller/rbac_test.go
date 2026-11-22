package controller

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// loadClusterRoles creates the ClusterRoles of a YAML file in envtest.
func loadClusterRoles(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		role := &rbacv1.ClusterRole{}
		if err := dec.Decode(role); err != nil {
			break
		}
		if role.Name == "" {
			continue
		}
		if err := testClient.Create(context.Background(), role); err != nil {
			t.Fatalf("ClusterRole %s: %v", role.Name, err)
		}
	}
}

type access struct {
	verb, group, resource, subresource string
	allowed                            bool
}

func checkAccess(t *testing.T, who string, user string, groups []string, ns string, cases []access) {
	t.Helper()
	for _, c := range cases {
		sar := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
			User: user, Groups: groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: ns, Verb: c.verb, Group: c.group, Resource: c.resource, Subresource: c.subresource,
			},
		}}
		if err := testClient.Create(context.Background(), sar); err != nil {
			t.Fatal(err)
		}
		if sar.Status.Allowed != c.allowed {
			t.Errorf("%s: can %s %s/%s/%s in %q = %v, want %v", who, c.verb, c.group, c.resource, c.subresource, ns, sar.Status.Allowed, c.allowed)
		}
	}
}

// The permission matrix of Developer and Viewer (architecture/06 §8), the
// same question `kubectl auth can-i` asks, against the chart's ClusterRoles
// bound the way Capsule binds them (additionalRoleBindings).
func TestTenantRoles(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	loadClusterRoles(t, filepath.Join("..", "..", "..", "deploy", "charts", "tenantry-operator", "files", "tenant-roles.yaml"))
	const ns = "acme-roles-frontend"
	if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	for role, group := range map[string]string{render.ClusterRoleDeveloper: "oidc:org_01jabc:group_dev", render.ClusterRoleViewer: "oidc:org_01jabc:group_view"} {
		if err := testClient.Create(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "capsule-" + role[len("tenantry:"):], Namespace: ns},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: group}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	checkAccess(t, "Developer", "oidc:joe", []string{"oidc:org_01jabc:group_dev", "system:authenticated"}, ns, []access{
		{"create", "apps", "deployments", "", true},
		{"delete", "", "pods", "", true},
		{"create", "", "persistentvolumeclaims", "", true},
		{"create", "", "services", "", true},
		{"get", "", "pods", "log", true},
		{"create", "", "pods", "exec", true},
		{"create", "batch", "jobs", "", true},
		{"create", "", "secrets", "", true},
		{"get", "", "secrets", "", false},
		{"list", "", "secrets", "", false},
		{"create", "rbac.authorization.k8s.io", "rolebindings", "", false},
		{"create", "networking.k8s.io", "ingresses", "", false},
		{"get", "networking.k8s.io", "ingresses", "", true},
		{"create", "networking.k8s.io", "networkpolicies", "", false},
		{"update", "", "resourcequotas", "", false},
		{"create", "cert-manager.io", "certificates", "", false},
		{"get", "", "pods", "", true},
	})
	checkAccess(t, "Developer outside its Namespace", "oidc:joe", []string{"oidc:org_01jabc:group_dev"}, "other", []access{
		{"get", "", "pods", "", false},
	})
	checkAccess(t, "Developer, cluster scope", "oidc:joe", []string{"oidc:org_01jabc:group_dev"}, "", []access{
		{"create", "", "namespaces", "", false},
		{"list", "", "nodes", "", false},
	})
	checkAccess(t, "Viewer", "oidc:bob", []string{"oidc:org_01jabc:group_view", "system:authenticated"}, ns, []access{
		{"get", "", "pods", "", true},
		{"list", "apps", "deployments", "", true},
		{"get", "", "pods", "log", true},
		{"get", "", "configmaps", "", true},
		{"create", "", "pods", "exec", false},
		{"get", "", "secrets", "", false},
		{"create", "apps", "deployments", "", false},
		{"delete", "", "pods", "", false},
		{"patch", "apps", "deployments", "scale", false},
	})
}

// The operator's own ClusterRole, as generated from the markers in rbac.go:
// what architecture/06 §7 gives it and a few things it must not have.
func TestOperatorRole(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	loadClusterRoles(t, filepath.Join("..", "..", "config", "rbac", "role.yaml"))
	if err := testClient.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "tenantry-operator"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "tenantry-operator"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "tenantry-operator", Namespace: "tenantry-operator"}},
	}); err != nil {
		t.Fatal(err)
	}
	groups := []string{"system:serviceaccounts", "system:serviceaccounts:tenantry-operator", "system:authenticated"}
	checkAccess(t, "operator", operatorSA, groups, "", []access{
		{"create", "capsule.clastix.io", "tenants", "", true},
		{"patch", "", "nodes", "", true},
		{"create", "", "namespaces", "", true},
		{"update", "tenantry.io", "platformworkspaces", "status", true},
		{"update", "tenantry.io", "platformworkspaces", "finalizers", true},
		{"create", "projectcalico.org", "ippools", "", true},
		{"list", "crd.projectcalico.org", "ipamblocks", "", true},
		{"create", "tenantry.io", "platformworkspaces", "", false},
		{"patch", "tenantry.io", "platformnodeinventories", "", false},
		{"create", "cert-manager.io", "certificates", "", false},
		{"get", "", "secrets", "", false},
		{"create", "rbac.authorization.k8s.io", "rolebindings", "", false},
		{"delete", "", "nodes", "", false},
		{"create", "", "pods", "", false},
	})
}
