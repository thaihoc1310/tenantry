package provisioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/housekeeping"
	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// An organization acme with a grant on cluster-a and a group in Keycloak,
// Ann its OrgOwner.
const seed = `
	INSERT INTO users (id, issuer, subject) VALUES ('usr-ann', 'iss', 'ann');
	INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ('org-01', 'acme', 'ACME', 'usr-ann', 'Active');
	INSERT INTO organization_members (org_id, user_id, role) VALUES ('org-01', 'usr-ann', 'OrgOwner');
	INSERT INTO organization_entitlements (org_id, max_members, max_groups, max_workspaces_total) VALUES ('org-01', 10, 10, 5);
	INSERT INTO groups (id, org_id, display_name, machine_name, sync_status) VALUES ('grp-a', 'org-01', 'a', 'org_01:group_a', 'Synced');
	INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain)
		VALUES ('cluster-a', 'A', 'Ready', 'https://proxy', 'https://console', 'apps.example');
	INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio) VALUES ('shared-general', 'cluster-a', 'General', 2);
	INSERT INTO organization_cluster_grants (id, org_id, cluster_id, is_default, max_workspaces, max_namespaces_total,
		requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes)
		VALUES ('grant-1', 'org-01', 'cluster-a', true, 5, 10, 4000, 8589934592, 8000, 17179869184, 50, 21474836480);
	INSERT INTO cluster_grant_shared_pools (grant_id, pool_id) VALUES ('grant-1', 'shared-general');
	INSERT INTO network_pools (id, cluster_id, cidrs, default_prefix_length, allowed_prefix_lengths) VALUES ('pods', 'cluster-a', '{10.240.0.0/18}', 24, '{24}');
	INSERT INTO cluster_grant_network_pools (grant_id, pool_id) VALUES ('grant-1', 'pods');`

type env struct {
	t       *testing.T
	db      *pgxpool.Pool
	cluster *fakeCluster
	outbox  *outbox.Worker
	syncer  *Syncer
	ws      *workspace.Service
	ops     *operation.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	url := testdb.New(t)
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	p, err := migrations.NewProvider(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(seed); err != nil {
		t.Fatal(err)
	}
	db := testdb.Pool(t, url)
	c := newFakeCluster()
	return &env{
		t: t, db: db, cluster: c,
		outbox: outbox.NewWorker(db, "cluster-a", Handler(c), quiet),
		syncer: NewSyncer(db, "cluster-a", c, 10*time.Minute, quiet),
		ws:     workspace.NewService(db, org.NewService(db)),
		ops:    operation.NewService(db, map[string]operation.Reapply{"Workspace": workspace.Reapply}),
	}
}

// poolNode gives shared-general a node, so Workspaces fit under its ceiling.
func (e *env) poolNode() {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable,
		allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, shared_pool_id)
		VALUES ('cluster-a', 'wk-pool', 'Worker', true, true, 8000, 17179869184, 110, 'shared-general') ON CONFLICT DO NOTHING`); err != nil {
		e.t.Fatal(err)
	}
}

// create makes Workspace slug with Namespace frontend and delivers its CR.
func (e *env) create(slug string) workspace.Created {
	e.t.Helper()
	e.poolNode()
	c, err := e.ws.Create(context.Background(), "usr-ann", "acme", workspace.New{
		Slug: slug, DisplayName: slug, ClusterID: "cluster-a", ComputeMode: workspace.ComputeShared, SharedPoolID: "shared-general",
		Resources: workspace.Resources{RequestsCPUMilli: 500, RequestsMemoryBytes: 1 << 30, LimitsCPUMilli: 1000,
			LimitsMemoryBytes: 1 << 30, Pods: 10, RequestsStorageBytes: 1 << 30, NamespaceQuota: 3},
		Namespaces: []string{"frontend"},
		Bindings:   []workspace.Binding{{GroupID: "grp-a", Role: workspace.RoleTenantOwner}},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.deliver()
	return c
}

func (e *env) deliver() {
	e.t.Helper()
	if _, err := e.outbox.RunOnce(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) sync() {
	e.t.Helper()
	if err := e.syncer.RunOnce(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) workspace(id string) store.Workspace {
	e.t.Helper()
	var w store.Workspace
	if err := e.db.QueryRow(context.Background(), `SELECT state, desired_generation, observed_generation, conditions FROM workspaces WHERE id = $1`, id).
		Scan(&w.State, &w.DesiredGeneration, &w.ObservedGeneration, &w.Conditions); err != nil {
		e.t.Fatal(err)
	}
	return w
}

func (e *env) op(id string) store.Operation {
	e.t.Helper()
	var o store.Operation
	if err := e.db.QueryRow(context.Background(), `SELECT state, error_code, error_message FROM operations WHERE id = $1`, id).
		Scan(&o.State, &o.ErrorCode, &o.ErrorMessage); err != nil {
		e.t.Fatal(err)
	}
	return o
}

// namespaces is the Workspace's rows as name → "source:state".
func (e *env) namespaces(wsID string) map[string]string {
	e.t.Helper()
	rows, err := e.db.Query(context.Background(), `SELECT name, source || ':' || state FROM workspace_namespaces WHERE workspace_id = $1`, wsID)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{}
	for rows.Next() {
		var n, s string
		if err := rows.Scan(&n, &s); err != nil {
			e.t.Fatal(err)
		}
		out[n] = s
	}
	return out
}

// report plays the controller: status of the CR's current generation.
func (e *env) report(name string, phase v1.WorkspacePhase, conds []metav1.Condition, nss ...v1.NamespaceStatus) {
	e.t.Helper()
	cr := e.cluster.get(k8s.Workspaces, name)
	e.cluster.setStatus(k8s.Workspaces, name, &v1.PlatformWorkspaceStatus{
		Phase: phase, ObservedGeneration: cr.GetGeneration(), Conditions: conds, Namespaces: nss,
	})
}

func cond(typ string, ok bool, reason, msg string) metav1.Condition {
	s := metav1.ConditionTrue
	if !ok {
		s = metav1.ConditionFalse
	}
	return metav1.Condition{Type: typ, Status: s, Reason: reason, Message: msg, LastTransitionTime: metav1.Now()}
}

var readyConds = []metav1.Condition{cond(v1.ConditionCapsuleTenantReady, true, "Reconciled", ""), cond(v1.ConditionIdentityBindingsReady, true, "Reconciled", "")}

func TestProvisioningToReady(t *testing.T) {
	e := newEnv(t)
	c := e.create("solar")
	ws := c.Workspace
	cr := e.cluster.get(k8s.Workspaces, "acme-solar")
	if cr == nil || cr.GetAnnotations()[workspace.AnnotationSpecVersion] != "1" || e.op(c.Operation.ID).State != "Running" {
		t.Fatalf("CR not applied, or operation not Running: %v", cr)
	}

	// No status yet, then a status of an older generation: nothing is trusted.
	e.sync()
	if w := e.workspace(ws.ID); w.State != "Provisioning" || w.ObservedGeneration != 0 {
		t.Fatalf("before any status: %+v", w)
	}
	e.cluster.setStatus(k8s.Workspaces, "acme-solar", &v1.PlatformWorkspaceStatus{Phase: v1.PhaseReady, ObservedGeneration: 7})
	e.sync()
	if w := e.workspace(ws.ID); w.State != "Provisioning" {
		t.Fatalf("trusted a status of another generation: %+v", w)
	}

	usage := v1.ResourceList{"requests.cpu": resource.MustParse("250m")}
	e.report("acme-solar", v1.PhaseReady, readyConds,
		v1.NamespaceStatus{Name: "acme-solar-frontend", Source: v1.SourcePortal, Usage: usage},
		v1.NamespaceStatus{Name: "acme-solar-scratch", Source: v1.SourceKubectl})
	e.sync()
	w := e.workspace(ws.ID)
	var conds []metav1.Condition
	if w.State != "Ready" || w.ObservedGeneration != 1 || json.Unmarshal(w.Conditions, &conds) != nil || len(conds) != 2 {
		t.Fatalf("after Ready: %+v %s", w, w.Conditions)
	}
	if o := e.op(c.Operation.ID); o.State != "Succeeded" {
		t.Fatalf("operation %+v", o)
	}
	if got := e.namespaces(ws.ID); got["acme-solar-frontend"] != "portal:Active" || got["acme-solar-scratch"] != "kubectl:Active" {
		t.Fatalf("namespaces %v", got)
	}
	var snapshot string
	if err := e.db.QueryRow(context.Background(), `SELECT usage_snapshot::text FROM workspace_namespaces WHERE name = 'acme-solar-frontend'`).Scan(&snapshot); err != nil ||
		snapshot != `{"requests.cpu": "250m"}` {
		t.Fatalf("usage %q %v", snapshot, err)
	}

	// The kubectl Namespace is deleted; the portal one missing for a moment
	// (the controller recreates declared ones) stays Active.
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	if got := e.namespaces(ws.ID); got["acme-solar-frontend"] != "portal:Active" || got["acme-solar-scratch"] != "kubectl:Deleted" {
		t.Fatalf("after both vanished from the status: %v", got)
	}

	// Drift: a condition goes False, then back.
	e.report("acme-solar", v1.PhaseDegraded, []metav1.Condition{cond(v1.ConditionCapsuleTenantReady, false, "TenantNotReady", "Capsule says no")})
	e.sync()
	if w := e.workspace(ws.ID); w.State != "Degraded" {
		t.Fatalf("drift: %s", w.State)
	}
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	if w := e.workspace(ws.ID); w.State != "Ready" {
		t.Fatalf("drift handled: %s", w.State)
	}
}

// A new spec version identical to the cluster's keeps the CR's generation:
// the status already there describes it, and the operation succeeds.
func TestUnchangedSpecSucceedsOnTheExistingStatus(t *testing.T) {
	e := newEnv(t)
	c := e.create("solar")
	e.report("acme-solar", v1.PhaseReady, readyConds, v1.NamespaceStatus{Name: "acme-solar-frontend", Source: v1.SourcePortal})
	e.sync()

	ctx := context.Background()
	q := store.New(e.db)
	ws, err := q.BeginWorkspaceUpdate(ctx, c.Workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	op, err := operation.Create(ctx, q, operation.New{OrgID: ws.OrgID, ResourceType: "Workspace", ResourceID: ws.ID, Type: "workspace.update"})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workspace.BuildCR(ctx, q, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(ctx, q, op, outbox.Event{TargetClusterID: "cluster-a", Type: workspace.EventApply, Payload: cr, Version: ws.DesiredGeneration}); err != nil {
		t.Fatal(err)
	}
	e.deliver()
	if got := e.cluster.get(k8s.Workspaces, "acme-solar"); got.GetGeneration() != 1 || got.GetAnnotations()[workspace.AnnotationSpecVersion] != "2" {
		t.Fatalf("generation %d, version %s", got.GetGeneration(), got.GetAnnotations()[workspace.AnnotationSpecVersion])
	}
	e.sync()
	if w := e.workspace(ws.ID); w.State != "Ready" || w.ObservedGeneration != 2 || e.op(op.ID).State != "Succeeded" {
		t.Fatalf("after an unchanged spec: %+v, operation %s", w, e.op(op.ID).State)
	}
}

// The worker stops after applying, before recording it: the event is
// delivered again and the cluster still holds one CR, same generation.
func TestRedeliveryCreatesNoDuplicate(t *testing.T) {
	e := newEnv(t)
	h := Handler(e.cluster)
	crash := outbox.NewWorker(e.db, "cluster-a", outbox.HandlerFunc(func(ctx context.Context, ev store.OutboxEvent) error {
		if err := h.Handle(ctx, ev); err != nil {
			return err
		}
		panic("worker killed after apply")
	}), quiet)
	e.poolNode()
	c, err := e.ws.Create(context.Background(), "usr-ann", "acme", workspace.New{
		Slug: "solar", DisplayName: "Solar", ClusterID: "cluster-a", ComputeMode: workspace.ComputeShared, SharedPoolID: "shared-general",
		Resources: workspace.Resources{RequestsCPUMilli: 500, RequestsMemoryBytes: 1 << 30, LimitsCPUMilli: 500,
			LimitsMemoryBytes: 1 << 30, Pods: 5, RequestsStorageBytes: 1 << 30, NamespaceQuota: 1},
		Bindings: []workspace.Binding{{GroupID: "grp-a", Role: workspace.RoleTenantOwner}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crash.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(context.Background(), `UPDATE outbox_events SET next_attempt_at = now()`); err != nil {
		t.Fatal(err)
	}
	e.deliver()
	if n := len(e.cluster.objs[k8s.Workspaces]); n != 1 || e.cluster.applies != 2 ||
		e.cluster.get(k8s.Workspaces, "acme-solar").GetGeneration() != 1 || e.op(c.Operation.ID).State != "Running" {
		t.Fatalf("%d CRs after %d applies, generation %d, operation %s", n, e.cluster.applies,
			e.cluster.get(k8s.Workspaces, "acme-solar").GetGeneration(), e.op(c.Operation.ID).State)
	}
}

// An operation the controller does not finish in time fails; a retry writes
// the CR again with a new retryNonce, and the Workspace gets Ready.
func TestTimeoutThenRetry(t *testing.T) {
	e := newEnv(t)
	c := e.create("solar")
	e.report("acme-solar", v1.PhaseProvisioning, []metav1.Condition{cond(v1.ConditionCapsuleTenantReady, false, "TenantNotReady", "waiting for Capsule")})
	e.sync()
	if w := e.workspace(c.Workspace.ID); w.State != "Provisioning" {
		t.Fatalf("before the timeout: %s", w.State)
	}
	if _, err := e.db.Exec(context.Background(), `UPDATE operations SET updated_at = now() - interval '11 minutes'`); err != nil {
		t.Fatal(err)
	}
	e.sync()
	o := e.op(c.Operation.ID)
	if o.State != "Failed" || o.ErrorCode != ReconcileTimeout || o.ErrorMessage == "" || e.workspace(c.Workspace.ID).State != "Failed" {
		t.Fatalf("after the timeout: %+v, workspace %s", o, e.workspace(c.Workspace.ID).State)
	}
	// A late Ready of the old generation does not revive a Failed Workspace.
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	if w := e.workspace(c.Workspace.ID); w.State != "Failed" {
		t.Fatalf("Failed revived without a retry: %s", w.State)
	}

	if _, err := e.ops.Retry(context.Background(), "usr-ann", false, c.Operation.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.workspace(c.Workspace.ID); w.State != "Provisioning" || w.DesiredGeneration != 2 {
		t.Fatalf("after retry: %+v", w)
	}
	e.deliver()
	cr := e.cluster.get(k8s.Workspaces, "acme-solar")
	if cr.GetGeneration() != 2 || cr.GetAnnotations()[workspace.AnnotationSpecVersion] != "2" {
		t.Fatalf("retried CR: generation %d", cr.GetGeneration())
	}
	if nonce, _, _ := unstructuredString(cr.Object, "spec", "retryNonce"); nonce == "" {
		t.Fatal("no retryNonce")
	}
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	if w := e.workspace(c.Workspace.ID); w.State != "Ready" || e.op(c.Operation.ID).State != "Succeeded" {
		t.Fatalf("after the retried CR got Ready: %s, operation %s", w.State, e.op(c.Operation.ID).State)
	}
}

func unstructuredString(obj map[string]any, path ...string) (string, bool, error) {
	cur := any(obj)
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false, nil
		}
		cur = m[p]
	}
	s, ok := cur.(string)
	return s, ok, nil
}

func TestNextState(t *testing.T) {
	for _, c := range []struct {
		state   string
		phase   v1.WorkspacePhase
		current bool
		want    string
	}{
		{"Provisioning", v1.PhaseReady, true, "Ready"},
		{"Provisioning", v1.PhaseReady, false, "Provisioning"},
		{"Provisioning", v1.PhaseProvisioning, true, "Provisioning"},
		{"Provisioning", v1.PhaseFailed, true, "Failed"},
		{"Updating", v1.PhaseReady, true, "Ready"},
		{"Updating", v1.PhaseDegraded, true, "Degraded"},
		{"Updating", v1.PhaseUpdating, true, "Updating"},
		{"Ready", v1.PhaseDegraded, true, "Degraded"},
		{"Degraded", v1.PhaseReady, true, "Ready"},
		{"Degraded", v1.PhaseReady, false, "Degraded"},
		{"Failed", v1.PhaseReady, true, "Failed"},
		{"Cordoned", v1.PhaseDegraded, true, "Cordoned"},
		{"Deleting", v1.PhaseReady, true, "Deleting"},
	} {
		if got := nextState(c.state, c.phase, c.current); got != c.want {
			t.Errorf("%s + %s (current %v) = %s, want %s", c.state, c.phase, c.current, got, c.want)
		}
	}
}

// A cordon keeps the state until the controller has done it, then Cordoned;
// an uncordon brings back what the controller reports.
func TestCordonOperations(t *testing.T) {
	e := newEnv(t)
	c := e.create("solar")
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	ch, err := e.ws.Cordon(context.Background(), "usr-ann", c.Workspace.ID, "")
	if err != nil || ch.Operation == nil {
		t.Fatalf("cordon: %v", err)
	}
	e.deliver()
	e.sync() // the status is of the older generation
	if w := e.workspace(c.Workspace.ID); w.State != "Ready" || e.op(ch.Operation.ID).State != "Running" {
		t.Fatalf("before the controller: %s, operation %s", w.State, e.op(ch.Operation.ID).State)
	}
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	if w := e.workspace(c.Workspace.ID); w.State != "Cordoned" || e.op(ch.Operation.ID).State != "Succeeded" {
		t.Fatalf("after the controller: %s, operation %s", w.State, e.op(ch.Operation.ID).State)
	}
	ch, err = e.ws.Uncordon(context.Background(), "usr-ann", c.Workspace.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	e.deliver()
	e.report("acme-solar", v1.PhaseReady, readyConds)
	e.sync()
	if w := e.workspace(c.Workspace.ID); w.State != "Ready" || e.op(ch.Operation.ID).State != "Succeeded" {
		t.Fatalf("after uncordon: %s, operation %s", w.State, e.op(ch.Operation.ID).State)
	}
}

// A delete: the CR is deleted; its finalizer stuck → Blocked; force marks
// the CR; the CR gone → the Workspace's row and share are released, the
// members of its groups counted again.
func TestDeleteRelease(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.create("solar")
	e.report("acme-solar", v1.PhaseReady, readyConds, v1.NamespaceStatus{Name: "acme-solar-frontend", Source: v1.SourcePortal})
	e.sync()
	if _, err := e.db.Exec(ctx, `INSERT INTO group_members (group_id, user_id, sync_status) VALUES ('grp-a', 'usr-ann', 'Synced')`); err != nil {
		t.Fatal(err)
	}
	del, err := e.ws.Delete(ctx, "usr-ann", c.Workspace.ID, "solar", "")
	if err != nil {
		t.Fatal(err)
	}
	// A real CR stays while its finalizer works; the fake deletes at once,
	// so put it back as the finalizer would hold it.
	cr := e.cluster.get(k8s.Workspaces, "acme-solar")
	e.deliver()
	if e.cluster.get(k8s.Workspaces, "acme-solar") == nil {
		if err := e.cluster.Apply(ctx, k8s.Workspaces, cr); err != nil {
			t.Fatal(err)
		}
	}
	e.cluster.setStatus(k8s.Workspaces, "acme-solar", &v1.PlatformWorkspaceStatus{Phase: v1.PhaseDeleting, Conditions: []metav1.Condition{
		cond(v1.ConditionDeleting, false, v1.ReasonBlockedByFinalizer, "Namespace acme-solar-frontend: PVC data"),
	}})
	e.sync()
	if o := e.op(del.Operation.ID); o.State != "Blocked" || o.ErrorMessage != "Namespace acme-solar-frontend: PVC data" {
		t.Fatalf("stuck delete: %+v", o)
	}
	if w := e.workspace(c.Workspace.ID); w.State != "Deleting" {
		t.Fatalf("Blocked changed the Workspace: %s", w.State)
	}
	if _, err := e.ws.Force(ctx, "usr-admin", true, del.Operation.ID, ""); err != nil {
		t.Fatal(err)
	}
	e.deliver()
	if got := e.cluster.get(k8s.Workspaces, "acme-solar"); got.GetAnnotations()[v1.AnnotationForceDelete] != "true" {
		t.Fatal("force did not mark the CR")
	}
	// The finalizer is done: the CR is gone.
	if err := e.cluster.Delete(ctx, k8s.Workspaces, "acme-solar"); err != nil {
		t.Fatal(err)
	}
	e.sync()
	if o := e.op(del.Operation.ID); o.State != "Succeeded" {
		t.Fatalf("delete operation %+v", o)
	}
	// Deleted, out of every sum; its block quarantined, its name held.
	if n := countRows(t, e, `SELECT count(*) FROM workspaces w JOIN network_allocations a ON a.workspace_id = w.id
		WHERE w.state = 'Deleted' AND a.state = 'Quarantined' AND a.quarantine_until > now()`); n != 1 {
		t.Fatal("not Deleted with a quarantined block")
	}
	if n := countRows(t, e, `SELECT count(*) FROM group_members WHERE sync_status = 'Pending'`); n != 1 {
		t.Fatal("the bound group's member is not counted again")
	}
	if n := countRows(t, e, `SELECT count(*) FROM audit_events WHERE action = 'workspace.deleted'`); n != 1 {
		t.Fatal("no audit of the release")
	}
	if _, err := e.ws.Create(ctx, "usr-ann", "acme", workspace.New{
		Slug: "solar", DisplayName: "Solar", ClusterID: "cluster-a", ComputeMode: workspace.ComputeShared, SharedPoolID: "shared-general",
		Resources: workspace.Resources{RequestsCPUMilli: 100, RequestsMemoryBytes: 1 << 20, LimitsCPUMilli: 100, LimitsMemoryBytes: 1 << 20,
			Pods: 1, RequestsStorageBytes: 1 << 20, NamespaceQuota: 1},
		Bindings: []workspace.Binding{{GroupID: "grp-a", Role: workspace.RoleTenantOwner}},
	}); err == nil || !strings.Contains(err.Error(), "SLUG_TAKEN") {
		t.Fatalf("the name during quarantine: %v", err)
	}
	// Past the quarantine, housekeeping frees the block and the name.
	if _, err := e.db.Exec(ctx, `UPDATE network_allocations SET quarantine_until = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if err := housekeeping.RunOnce(ctx, e.db, quiet); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, e, `SELECT count(*) FROM workspaces`); n != 0 {
		t.Fatal("the Workspace row outlived its quarantine")
	}
	if n := countRows(t, e, `SELECT count(*) FROM audit_events WHERE action = 'network_allocation.release'`); n != 1 {
		t.Fatal("no audit of the released block")
	}
	e.create("solar")
}

// A Dedicated reservation is Active once the Workspace is Ready, comes down
// after a smaller resize only once the cluster has it, and frees the node
// when the Workspace is deleted.
func TestDedicatedReservationLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.db.Exec(ctx, `INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods)
		VALUES ('cluster-a', 'wk3', 'Worker', true, true, 3000, 12348030976, 110);
		INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods,
			system_reserve_cpu_milli, system_reserve_memory_bytes, system_reserve_pods, node_state)
		VALUES ('nodegrant-wk3', 'org-01', 'cluster-a', 'wk3', 3000, 12348030976, 110, 500, 1610612736, 10, 'Ready')`); err != nil {
		t.Fatal(err)
	}
	reservation := func() (string, int64, string) {
		var state, capacity string
		var cpu int64
		if err := e.db.QueryRow(ctx, `SELECT coalesce((SELECT state FROM workspace_node_reservations), 'none'),
			coalesce((SELECT reserved_cpu_milli FROM workspace_node_reservations), 0),
			(SELECT capacity_state FROM dedicated_node_grants WHERE id = 'nodegrant-wk3')`).Scan(&state, &cpu, &capacity); err != nil {
			t.Fatal(err)
		}
		return state, cpu, capacity
	}
	c, err := e.ws.Create(ctx, "usr-ann", "acme", workspace.New{
		Slug: "water", DisplayName: "Water", ClusterID: "cluster-a", ComputeMode: workspace.ComputeDedicated, NodeGrantID: "nodegrant-wk3",
		Resources: workspace.Resources{RequestsCPUMilli: 1000, RequestsMemoryBytes: 1 << 30, Pods: 10, RequestsStorageBytes: 1 << 30, NamespaceQuota: 1},
		Bindings:  []workspace.Binding{{GroupID: "grp-a", Role: workspace.RoleTenantOwner}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s, cpu, cap := reservation(); s != "Pending" || cpu != 1000 || cap != "PartiallyAllocated" {
		t.Fatalf("after create: %s %d %s", s, cpu, cap)
	}
	e.deliver()
	e.report("acme-water", v1.PhaseReady, readyConds)
	e.sync()
	if s, _, _ := reservation(); s != "Active" {
		t.Fatalf("after Ready: %s", s)
	}
	small := workspace.Resources{RequestsCPUMilli: 500, RequestsMemoryBytes: 1 << 30, Pods: 10, RequestsStorageBytes: 1 << 30, NamespaceQuota: 1}
	if _, err := e.ws.Update(ctx, "usr-ann", c.Workspace.ID, workspace.Patch{Resources: &small}); err != nil {
		t.Fatal(err)
	}
	if _, cpu, _ := reservation(); cpu != 1000 {
		t.Fatalf("lowered before the cluster has it: %d", cpu)
	}
	e.deliver()
	e.report("acme-water", v1.PhaseReady, readyConds)
	e.sync()
	if _, cpu, _ := reservation(); cpu != 500 {
		t.Fatalf("after the smaller quota is Ready: %d", cpu)
	}
	del, err := e.ws.Delete(ctx, "usr-ann", c.Workspace.ID, "water", "")
	if err != nil {
		t.Fatal(err)
	}
	if s, _, _ := reservation(); s != "Releasing" {
		t.Fatalf("while deleting: %s", s)
	}
	e.deliver() // the fake deletes the CR at once
	e.sync()
	if s, _, cap := reservation(); s != "Released" || cap != "Available" || e.op(del.Operation.ID).State != "Succeeded" {
		t.Fatalf("after delete: %s %s", s, cap)
	}
}
