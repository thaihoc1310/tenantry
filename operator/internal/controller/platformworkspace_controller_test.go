package controller

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
	"github.com/thaihoc1310/tenantry/operator/internal/subreconciler"
)

func newWorkspace(name string) *v1.PlatformWorkspace {
	return &v1.PlatformWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1.PlatformWorkspaceSpec{
			OrganizationID: "org-01jabc", WorkspaceID: "ws-" + name, ClusterID: "cluster-a",
			NamespaceQuota: 3,
			Namespaces:     []string{"frontend"},
			Compute:        v1.Compute{Mode: v1.ComputeShared, SharedPoolID: testPool},
			StorageClasses: []string{"standard"},
			AccessBindings: []v1.AccessBinding{
				{GroupSubject: "oidc:org_01jabc:group_owner", Role: v1.RoleTenantOwner},
				{GroupSubject: "oidc:org_01jabc:group_dev", Role: v1.RoleDeveloper},
			},
		},
	}
}

func getWorkspace(t *testing.T, name string) *v1.PlatformWorkspace {
	t.Helper()
	ws := &v1.PlatformWorkspace{}
	if err := testClient.Get(context.Background(), client.ObjectKey{Name: name}, ws); err != nil {
		t.Fatal(err)
	}
	return ws
}

func condition(ws *v1.PlatformWorkspace, typ string) metav1.Condition {
	if c := meta.FindStatusCondition(ws.Status.Conditions, typ); c != nil {
		return *c
	}
	return metav1.Condition{Type: typ, Status: "absent"}
}

func owners(tenant *unstructured.Unstructured) []string {
	list, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "owners")
	var out []string
	for _, o := range list {
		m := o.(map[string]any)
		out = append(out, m["kind"].(string)+":"+m["name"].(string))
	}
	return out
}

// Create → Tenant with the right owners → Capsule Ready → Workspace Ready,
// its Namespace created; a second pass changes nothing.
func TestWorkspaceProvisioning(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	if err := testClient.Create(ctx, newWorkspace("acme-solar")); err != nil {
		t.Fatal(err)
	}

	if res := reconcileOnce(t, r, "acme-solar"); res.RequeueAfter == 0 {
		t.Error("waiting for Capsule should requeue")
	}
	ws := getWorkspace(t, "acme-solar")
	if !slices.Contains(ws.Finalizers, v1.WorkspaceFinalizer) {
		t.Errorf("finalizers %v", ws.Finalizers)
	}
	if ws.Status.Phase != v1.PhaseProvisioning || condition(ws, v1.ConditionCapsuleTenantReady).Reason != subreconciler.ReasonProgressing {
		t.Errorf("before Capsule: phase %s, %+v", ws.Status.Phase, condition(ws, v1.ConditionCapsuleTenantReady))
	}
	tenant := getTenant(t, "acme-solar")
	want := []string{"Group:oidc:org_01jabc:group_owner", "ServiceAccount:" + operatorSA}
	if got := owners(tenant); !slices.Equal(got, want) {
		t.Errorf("owners %v, want %v", got, want)
	}
	if refs := tenant.GetOwnerReferences(); len(refs) != 1 || refs[0].Name != "acme-solar" || refs[0].Controller == nil || !*refs[0].Controller {
		t.Errorf("owner references %+v", refs)
	}
	if prefix, _, _ := unstructured.NestedBool(tenant.Object, "spec", "forceTenantPrefix"); !prefix {
		t.Error("forceTenantPrefix not set")
	}

	capsuleReconciles(t, "acme-solar")
	if res := reconcileOnce(t, r, "acme-solar"); res.RequeueAfter != 0 {
		t.Errorf("a Ready Workspace should not requeue: %+v", res)
	}
	ws = getWorkspace(t, "acme-solar")
	for _, typ := range baseConditions {
		if c := condition(ws, typ); c.Status != metav1.ConditionTrue {
			t.Errorf("%s: %+v", typ, c)
		}
	}
	if ws.Status.Phase != v1.PhaseReady || ws.Status.ObservedGeneration != ws.Generation || ws.Status.CapsuleTenantRef != "acme-solar" {
		t.Errorf("status %+v", ws.Status)
	}
	ns := &corev1.Namespace{}
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-solar-frontend"}, ns); err != nil {
		t.Fatalf("Namespace of spec.namespaces: %v", err)
	}
	if ns.Labels[render.LabelCapsuleTenant] != "acme-solar" || ns.Labels[v1.LabelWorkspaceID] != "ws-acme-solar" {
		t.Errorf("Namespace labels %v", ns.Labels)
	}

	// Idempotent: applying again changes neither the Tenant nor the status.
	tenantVersion, wsVersion := getTenant(t, "acme-solar").GetResourceVersion(), getWorkspace(t, "acme-solar").ResourceVersion
	reconcileOnce(t, r, "acme-solar")
	reconcileOnce(t, r, "acme-solar")
	if v := getTenant(t, "acme-solar").GetResourceVersion(); v != tenantVersion {
		t.Errorf("Tenant changed on a second apply: %s → %s", tenantVersion, v)
	}
	if v := getWorkspace(t, "acme-solar").ResourceVersion; v != wsVersion {
		t.Errorf("PlatformWorkspace changed on a second pass: %s → %s", wsVersion, v)
	}

	// Someone edits the Tenant by hand: the next pass puts it back.
	tenant = getTenant(t, "acme-solar")
	_ = unstructured.SetNestedField(tenant.Object, false, "spec", "forceTenantPrefix")
	if err := testClient.Update(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-solar")
	if prefix, _, _ := unstructured.NestedBool(getTenant(t, "acme-solar").Object, "spec", "forceTenantPrefix"); !prefix {
		t.Error("drift on the Tenant was not restored")
	}
}

// Namespaces from kubectl show up as such; namespacesToDelete deletes only
// Namespaces of the Tenant.
func TestWorkspaceNamespaces(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	if err := testClient.Create(ctx, newWorkspace("acme-water")); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-water")
	capsuleReconciles(t, "acme-water")
	reconcileOnce(t, r, "acme-water")

	for name, labels := range map[string]map[string]string{
		"acme-water-scratch": {render.LabelCapsuleTenant: "acme-water"}, // kubectl create ns, Capsule labels it
		"acme-water-foreign": nil,                                       // not the Tenant's
	} {
		if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}); err != nil {
			t.Fatal(err)
		}
	}
	reconcileOnce(t, r, "acme-water")
	ws := getWorkspace(t, "acme-water")
	want := []v1.NamespaceStatus{
		{Name: "acme-water-frontend", Source: v1.SourcePortal},
		{Name: "acme-water-scratch", Source: v1.SourceKubectl},
	}
	if !equality.Semantic.DeepEqual(ws.Status.Namespaces, want) {
		t.Errorf("status.namespaces %+v, want %+v", ws.Status.Namespaces, want)
	}

	ws.Spec.Namespaces = []string{"frontend", "backend"}
	ws.Spec.NamespacesToDelete = []string{"acme-water-scratch", "acme-water-foreign"}
	if err := testClient.Update(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-water")
	ws = getWorkspace(t, "acme-water")
	if c := condition(ws, v1.ConditionCapsuleTenantReady); c.Reason != subreconciler.ReasonProgressing || !strings.Contains(c.Message, "acme-water-scratch") {
		t.Errorf("while a Namespace goes away: %+v", c)
	}
	if ws.Status.Phase != v1.PhaseUpdating {
		t.Errorf("phase %s, want Updating", ws.Status.Phase)
	}
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-water-backend"}, &corev1.Namespace{}); err != nil {
		t.Errorf("acme-water-backend: %v", err)
	}
	foreign := &corev1.Namespace{}
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-water-foreign"}, foreign); err != nil || foreign.DeletionTimestamp != nil {
		t.Errorf("a Namespace outside the Tenant was deleted: %v %v", err, foreign.DeletionTimestamp)
	}
	finishNamespaceDeletion(t, "acme-water-scratch")
	reconcileOnce(t, r, "acme-water")
	ws = getWorkspace(t, "acme-water")
	if ws.Status.Phase != v1.PhaseReady || len(ws.Status.Namespaces) != 2 {
		t.Errorf("after the delete: phase %s, namespaces %+v", ws.Status.Phase, ws.Status.Namespaces)
	}
}

// The finalizer: rights go first, then the cordon, then the Namespaces, then
// the Tenant, then the finalizer itself (IMPLEMENTATION-PLAN §4.3).
func TestWorkspaceFinalizer(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-green")
	ws.Spec.Cordoned = true
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-green")
	capsuleReconciles(t, "acme-green")
	reconcileOnce(t, r, "acme-green")
	// Cordoned: Capsule would refuse the Namespace, so the operator waits.
	ws = getWorkspace(t, "acme-green")
	if c := condition(ws, v1.ConditionCapsuleTenantReady); c.Reason != subreconciler.ReasonCordoned {
		t.Errorf("cordoned with a Namespace pending: %+v", c)
	}
	if err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "acme-green-frontend", Labels: map[string]string{render.LabelCapsuleTenant: "acme-green"},
	}}); err != nil {
		t.Fatal(err)
	}

	if err := testClient.Delete(ctx, ws); err != nil {
		t.Fatal(err)
	}
	// 1. owners and RoleBindings go, the cordon stays.
	reconcileOnce(t, r, "acme-green")
	tenant := getTenant(t, "acme-green")
	if got := owners(tenant); !slices.Equal(got, []string{"ServiceAccount:" + operatorSA}) {
		t.Errorf("owners after step 1: %v", got)
	}
	if _, found, _ := unstructured.NestedSlice(tenant.Object, "spec", "additionalRoleBindings"); found {
		t.Error("additionalRoleBindings left after step 1")
	}
	if !subreconciler.TenantCordoned(tenant) {
		t.Error("uncordoned before Capsule removed the tenant's rights")
	}
	if len(tenant.GetOwnerReferences()) != 1 {
		t.Error("the revoked Tenant lost its owner reference")
	}
	if ws := getWorkspace(t, "acme-green"); ws.Status.Phase != v1.PhaseDeleting {
		t.Errorf("phase %s, want Deleting", ws.Status.Phase)
	}
	reconcileOnce(t, r, "acme-green") // Capsule has not caught up: nothing more
	if !subreconciler.TenantCordoned(getTenant(t, "acme-green")) {
		t.Error("uncordoned before Capsule reconciled")
	}
	// 2. uncordon.
	capsuleReconciles(t, "acme-green")
	reconcileOnce(t, r, "acme-green")
	if subreconciler.TenantCordoned(getTenant(t, "acme-green")) {
		t.Fatal("still cordoned after step 2")
	}
	// 3. Namespaces, the Tenant waits for them.
	capsuleReconciles(t, "acme-green")
	reconcileOnce(t, r, "acme-green")
	ns := &corev1.Namespace{}
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-green-frontend"}, ns); err != nil || ns.DeletionTimestamp == nil {
		t.Fatalf("Namespace not being deleted: %v", err)
	}
	if getTenant(t, "acme-green").GetDeletionTimestamp() != nil {
		t.Error("Tenant deleted before its Namespaces were gone")
	}
	// Past the threshold: BlockedByFinalizer, naming the Namespace.
	r.BlockedAfter = 0
	reconcileOnce(t, r, "acme-green")
	if c := condition(getWorkspace(t, "acme-green"), v1.ConditionDeleting); c.Status != metav1.ConditionFalse ||
		c.Reason != v1.ReasonBlockedByFinalizer || !strings.Contains(c.Message, "acme-green-frontend") {
		t.Errorf("Deleting condition %+v", c)
	}
	// 4. Namespace gone → replication deleted, then the Tenant (envtest has
	// no garbage collector for owner references to matter) → 7. finalizer
	// removed.
	finishNamespaceDeletion(t, "acme-green-frontend")
	reconcileOnce(t, r, "acme-green")
	rep := &unstructured.Unstructured{}
	rep.SetGroupVersionKind(render.ReplicationGVK)
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-green"}, rep); !apierrors.IsNotFound(err) {
		t.Fatalf("GlobalTenantResource still there: %v", err)
	}
	if getTenant(t, "acme-green").GetDeletionTimestamp() != nil {
		t.Error("Tenant deleted in the same pass as its replication")
	}
	reconcileOnce(t, r, "acme-green")
	tenant = &unstructured.Unstructured{}
	tenant.SetGroupVersionKind(render.TenantGVK)
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-green"}, tenant); !apierrors.IsNotFound(err) {
		t.Fatalf("Tenant still there: %v", err)
	}
	reconcileOnce(t, r, "acme-green")
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-green"}, &v1.PlatformWorkspace{}); !apierrors.IsNotFound(err) {
		t.Errorf("PlatformWorkspace still there: %v", err)
	}
}

// The quota: PolicyReady waits until Capsule's GlobalResourceQuota carries
// the Workspace's hard, and its usage becomes status.usage.
func TestWorkspacePolicy(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-sun")
	ws.Spec.Resources = v1.ResourceList{"requests.cpu": resource.MustParse("2"), "pods": resource.MustParse("20")}
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-sun")
	quota, _, _ := unstructured.NestedSlice(getTenant(t, "acme-sun").Object, "spec", "rules")
	if q := quota[len(quota)-1].(map[string]any)["quota"]; q == nil {
		t.Fatalf("no quota rule in %v", quota)
	}
	capsuleReconciles(t, "acme-sun")
	reconcileOnce(t, r, "acme-sun")
	if c := condition(getWorkspace(t, "acme-sun"), v1.ConditionPolicyReady); c.Reason != subreconciler.ReasonProgressing {
		t.Errorf("before the GlobalResourceQuota: %+v", c)
	}

	// Capsule still has the old hard.
	capsuleQuota(t, "acme-sun", map[string]any{"requests.cpu": "1", "pods": "20"},
		map[string]map[string]any{"acme-sun-frontend": {"requests.cpu": "300m", "pods": "2"}})
	reconcileOnce(t, r, "acme-sun")
	if c := condition(getWorkspace(t, "acme-sun"), v1.ConditionPolicyReady); c.Reason != subreconciler.ReasonProgressing ||
		!strings.Contains(c.Message, "requests.cpu=2") {
		t.Errorf("with a stale hard: %+v", c)
	}

	q := &unstructured.Unstructured{}
	q.SetGroupVersionKind(render.QuotaGVK)
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-sun-workspace"}, q); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedField(q.Object, "2000m", "status", "total", "hard", "requests.cpu")
	if err := testClient.Status().Update(ctx, q); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-sun")
	ws = getWorkspace(t, "acme-sun")
	if c := condition(ws, v1.ConditionPolicyReady); c.Status != metav1.ConditionTrue || ws.Status.Phase != v1.PhaseReady {
		t.Errorf("PolicyReady %+v, phase %s", c, ws.Status.Phase)
	}
	cpu := ws.Status.Usage["requests.cpu"]
	if cpu.String() != "300m" || len(ws.Status.Namespaces) != 1 {
		t.Fatalf("usage %v, namespaces %+v", ws.Status.Usage, ws.Status.Namespaces)
	}
	if pods := ws.Status.Namespaces[0].Usage["pods"]; pods.String() != "2" {
		t.Errorf("Namespace usage %v", ws.Status.Namespaces[0].Usage)
	}
}

// A Platform Admin's :force stops waiting for Namespaces.
func TestWorkspaceForceDelete(t *testing.T) {
	needEnv(t)
	ctx := context.Background()
	r := newWorkspaceReconciler()
	ws := newWorkspace("acme-wind")
	ws.Annotations = map[string]string{v1.AnnotationForceDelete: "true"}
	if err := testClient.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-wind")
	capsuleReconciles(t, "acme-wind")
	reconcileOnce(t, r, "acme-wind")
	if err := testClient.Delete(ctx, getWorkspace(t, "acme-wind")); err != nil {
		t.Fatal(err)
	}
	// Capsule 0.15 keeps a Tenant until its Namespaces are gone.
	tenant := getTenant(t, "acme-wind")
	tenant.SetFinalizers([]string{"controller.projectcapsule.dev/finalize"})
	if err := testClient.Update(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-wind") // step 1
	capsuleReconciles(t, "acme-wind")
	reconcileOnce(t, r, "acme-wind") // Namespace deleted, not waited for; replication deleted
	reconcileOnce(t, r, "acme-wind") // Tenant deleted, not waited for either; the CR goes
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-wind"}, &v1.PlatformWorkspace{}); !apierrors.IsNotFound(err) {
		reconcileOnce(t, r, "acme-wind")
		if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-wind"}, &v1.PlatformWorkspace{}); !apierrors.IsNotFound(err) {
			t.Fatalf("PlatformWorkspace still there: %v", err)
		}
	}
	tenant = getTenant(t, "acme-wind")
	if tenant.GetDeletionTimestamp() == nil {
		t.Error("Tenant not deleted under force-delete")
	}
	// What is left is out of the tenant's reach: owners and RoleBindings went first.
	if got := owners(tenant); !slices.Equal(got, []string{"ServiceAccount:" + operatorSA}) {
		t.Errorf("owners of the Tenant left behind: %v", got)
	}
	if _, found, _ := unstructured.NestedSlice(tenant.Object, "spec", "additionalRoleBindings"); found {
		t.Error("RoleBindings left on the Tenant left behind")
	}
	ns := &corev1.Namespace{}
	if err := testClient.Get(ctx, client.ObjectKey{Name: "acme-wind-frontend"}, ns); err != nil || ns.DeletionTimestamp == nil {
		t.Errorf("the Namespace is not being deleted: %v", err)
	}
	var leftovers string
	for len(leftovers) == 0 {
		select {
		case e := <-r.Recorder.(*events.FakeRecorder).Events:
			if strings.Contains(e, "ForceDeleteLeftovers") {
				leftovers = e
			}
		default:
			t.Fatal("no ForceDeleteLeftovers Event")
		}
	}
	if !strings.Contains(leftovers, "acme-wind-frontend") || !strings.Contains(leftovers, "Tenant acme-wind") {
		t.Errorf("Event %q does not name what is left", leftovers)
	}

	// A new Workspace of the same name waits for the old Tenant, slowly.
	again := newWorkspace("acme-wind")
	if err := testClient.Create(ctx, again); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-wind")
	res := reconcileOnce(t, r, "acme-wind")
	if c := condition(getWorkspace(t, "acme-wind"), v1.ConditionCapsuleTenantReady); c.Reason != subreconciler.ReasonTenantTerminating {
		t.Errorf("CapsuleTenantReady %+v, want TenantTerminating", c)
	}
	if res.RequeueAfter != 30*time.Second {
		t.Errorf("requeue %v, want 30s", res.RequeueAfter)
	}
	// Capsule finishes: the old Tenant goes, the new one is made.
	tenant.SetFinalizers(nil)
	if err := testClient.Update(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r, "acme-wind")
	if fresh := getTenant(t, "acme-wind"); fresh.GetDeletionTimestamp() != nil || len(fresh.GetOwnerReferences()) != 1 {
		t.Errorf("new Tenant %v", fresh.GetOwnerReferences())
	}
}

func TestPhase(t *testing.T) {
	cond := func(typ, status, reason string) metav1.Condition {
		return metav1.Condition{Type: typ, Status: metav1.ConditionStatus(status), Reason: reason}
	}
	allTrue := []metav1.Condition{
		cond(v1.ConditionComputeReady, "True", "Reconciled"), cond(v1.ConditionCapsuleTenantReady, "True", "Reconciled"), cond(v1.ConditionPolicyReady, "True", "Reconciled"),
		cond(v1.ConditionIdentityBindingsReady, "True", "Reconciled"),
	}
	progressing := []metav1.Condition{
		cond(v1.ConditionCapsuleTenantReady, "False", subreconciler.ReasonProgressing), cond(v1.ConditionIdentityBindingsReady, "True", "Reconciled"),
	}
	broken := []metav1.Condition{
		cond(v1.ConditionCapsuleTenantReady, "False", subreconciler.ReasonCapsuleNotReady), cond(v1.ConditionIdentityBindingsReady, "True", "Reconciled"),
	}
	rejected := []metav1.Condition{cond(v1.ConditionCapsuleTenantReady, "False", subreconciler.ReasonRejected)}
	for _, c := range []struct {
		before v1.WorkspacePhase
		conds  []metav1.Condition
		want   v1.WorkspacePhase
	}{
		{"", nil, v1.PhaseProvisioning},
		{"", progressing, v1.PhaseProvisioning},
		{"", broken, v1.PhaseProvisioning},
		{"", rejected, v1.PhaseFailed},
		{v1.PhaseFailed, progressing, v1.PhaseProvisioning},
		{v1.PhaseProvisioning, allTrue, v1.PhaseReady},
		{v1.PhaseReady, progressing, v1.PhaseUpdating},
		{v1.PhaseReady, broken, v1.PhaseDegraded},
		{v1.PhaseUpdating, broken, v1.PhaseDegraded},
		{v1.PhaseDegraded, progressing, v1.PhaseUpdating},
		{v1.PhaseDegraded, allTrue, v1.PhaseReady},
	} {
		if got := phase(c.before, c.conds, baseConditions); got != c.want {
			t.Errorf("phase(%q, %v) = %s, want %s", c.before, c.conds, got, c.want)
		}
	}
}
