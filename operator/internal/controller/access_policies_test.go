package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// The tenant's Ingress and RoleBinding policies (architecture/05 §7):
// Routes are the only way in, and an owner binds only their Organization's
// groups and the Workspace's ServiceAccounts.
// Called by TestChartPolicies, which loads the policies.
func checkAccessPolicies(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := testClient.Create(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "test-access-writers"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"create", "update", "delete"}},
			{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"rolebindings"}, Verbs: []string{"create", "update"}},
			{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"clusterroles"}, Verbs: []string{"bind"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test-access-writers"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "test-access-writers"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: "test:writers"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, ns := range []*corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: "acme-acc-web", Labels: map[string]string{render.LabelCapsuleTenant: "acme-acc", v1.LabelOrg: "01jabc"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "acme-acc-nolabel", Labels: map[string]string{render.LabelCapsuleTenant: "acme-acc"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "acc-system"}},
	} {
		if err := testClient.Create(ctx, ns); err != nil {
			t.Fatal(err)
		}
	}
	tenant := as(t, "oidc:alice", "oidc:org_01jabc:group_owner")
	operator := as(t, operatorSA, "system:serviceaccounts", "system:serviceaccounts:tenantry-operator")
	gc := as(t, "system:serviceaccount:kube-system:generic-garbage-collector")

	ingress := func(ns, name string) *networkingv1.Ingress {
		class := render.IngressClass
		return &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       networkingv1.IngressSpec{IngressClassName: &class, Rules: []networkingv1.IngressRule{{Host: name + ".mvp.tenantry.site"}}},
		}
	}
	expectRefused(t, "a tenant writes an Ingress", func() error { return tenant.Create(ctx, ingress("acme-acc-web", "mine")) })
	expectAllowed(t, "the operator writes an Ingress", operator.Create(ctx, ingress("acme-acc-web", "route")))
	expectRefused(t, "a tenant deletes the Route's Ingress", func() error { return tenant.Delete(ctx, ingress("acme-acc-web", "route")) })
	expectAllowed(t, "the garbage collector deletes it", gc.Delete(ctx, ingress("acme-acc-web", "route")))
	expectAllowed(t, "a tenant writes an Ingress outside the Workspaces", tenant.Create(ctx, ingress("acc-system", "elsewhere")))

	binding := func(ns, name string, subjects ...rbacv1.Subject) *rbacv1.RoleBinding {
		return &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "view"},
			Subjects:   subjects,
		}
	}
	group := func(name string) rbacv1.Subject {
		return rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: "Group", Name: name}
	}
	sa := func(ns, name string) rbacv1.Subject {
		return rbacv1.Subject{Kind: "ServiceAccount", Namespace: ns, Name: name}
	}
	expectAllowed(t, "an owner binds a group of the Organization",
		tenant.Create(ctx, binding("acme-acc-web", "own-group", group("oidc:org_01jabc:group_dev"))))
	expectAllowed(t, "an owner binds a ServiceAccount of the Workspace",
		tenant.Create(ctx, binding("acme-acc-web", "own-sa", sa("acme-acc-api", "ci"))))
	for name, subject := range map[string]rbacv1.Subject{
		"another Organization's group": group("oidc:org_01jzzz:group_dev"),
		"every tenant user":            group("oidc:tenantry:tenant-users"),
		"a group named like a prefix":  group("oidc:org_01jabcd:group_dev"),
		"a user":                       {APIGroup: rbacv1.GroupName, Kind: "User", Name: "oidc:mallory"},
		"another Workspace's account":  sa("acme-other-api", "ci"),
		"a system ServiceAccount":      sa("kube-system", "default"),
	} {
		expectRefused(t, "an owner binds "+name, func() error {
			return tenant.Create(ctx, binding("acme-acc-web", "bad", group("oidc:org_01jabc:group_dev"), subject))
		})
	}
	expectRefused(t, "a Namespace without its Organization label refuses every binding", func() error {
		return tenant.Create(ctx, binding("acme-acc-nolabel", "x", group("oidc:org_01jabc:group_dev")))
	})
	expectAllowed(t, "Capsule (not a tenant caller) binds anyone",
		as(t, "system:serviceaccount:capsule-system:capsule").Create(ctx, binding("acme-acc-web", "capsule", group("oidc:tenantry:tenant-users"))))
}
