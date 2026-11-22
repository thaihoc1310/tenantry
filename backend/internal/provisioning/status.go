package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
)

// ReconcileTimeout is the error code of an operation the controller did not
// finish within the timeout.
const ReconcileTimeout = "RECONCILE_TIMEOUT"

// SyncEvery is how often the watch cache is copied into the database;
// RebuildEvery how often the pool and node CRs are applied from it again
// (IMPLEMENTATION-PLAN §5.1, the 5-minute resync).
const (
	SyncEvery    = 2 * time.Second
	RebuildEvery = 5 * time.Minute
)

// Syncer copies the status of its cluster's PlatformWorkspaces into the
// database and fails operations the controller does not finish in time.
type Syncer struct {
	db        *pgxpool.Pool
	q         *store.Queries
	clusterID string
	cluster   Cluster
	timeout   time.Duration
	log       *slog.Logger
}

func NewSyncer(db *pgxpool.Pool, clusterID string, c Cluster, timeout time.Duration, log *slog.Logger) *Syncer {
	return &Syncer{db: db, q: store.New(db), clusterID: clusterID, cluster: c, timeout: timeout, log: log}
}

// Run syncs until ctx ends, rebuilding the pool and node CRs first and
// every RebuildEvery.
func (s *Syncer) Run(ctx context.Context) {
	var rebuilt time.Time
	for {
		if time.Since(rebuilt) >= RebuildEvery {
			if err := s.Rebuild(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("rebuilding pool and node CRs failed", "err", err)
			} else {
				rebuilt = time.Now()
			}
		}
		if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("status sync failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(SyncEvery):
		}
	}
}

// RunOnce copies every PlatformWorkspace's status, fails operations past
// the timeout, then copies the nodes and pools. Each Workspace is its own
// transaction.
func (s *Syncer) RunOnce(ctx context.Context) error {
	crs, err := s.cluster.Workspaces()
	if err != nil {
		return err
	}
	var errs []error
	for _, cr := range crs {
		if cr.Spec.ClusterID != s.clusterID {
			continue
		}
		if err := s.syncWorkspace(ctx, cr); err != nil {
			errs = append(errs, fmt.Errorf("workspace %s: %w", cr.Spec.WorkspaceID, err))
		}
	}
	if err := s.syncDeletes(ctx, crs); err != nil {
		errs = append(errs, err)
	}
	if err := s.failTimedOut(ctx, crs); err != nil {
		errs = append(errs, err)
	}
	if err := s.syncNodes(ctx); err != nil {
		errs = append(errs, fmt.Errorf("nodes: %w", err))
	}
	return errors.Join(errs...)
}

// trusted is the spec version a CR's status describes; ok is false while
// the controller has not finished reconciling the CR's current generation
// (IMPLEMENTATION-PLAN §5.1). A spec applied again unchanged keeps its
// generation, so a status already caught up describes the new version too.
func trusted(cr v1.PlatformWorkspace) (version int64, ok bool) {
	if cr.Generation == 0 || cr.Status.ObservedGeneration != cr.Generation {
		return 0, false
	}
	v, err := strconv.ParseInt(cr.Annotations[workspace.AnnotationSpecVersion], 10, 64)
	return v, err == nil
}

func (s *Syncer) syncWorkspace(ctx context.Context, cr v1.PlatformWorkspace) error {
	version, ok := trusted(cr)
	if !ok {
		return nil
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := q.LockWorkspace(ctx, cr.Spec.WorkspaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not ours, or gone
		}
		if err != nil {
			return err
		}
		if ws.ClusterID != s.clusterID || ws.K8sName != cr.Name {
			return nil
		}
		if err := s.syncNamespaces(ctx, q, ws, cr); err != nil {
			return err
		}
		usage, err := usageJSON(cr.Status.Usage)
		if err != nil {
			return err
		}
		if err := q.RecordWorkspaceUsage(ctx, store.RecordWorkspaceUsageParams{ID: ws.ID, UsageSnapshot: usage}); err != nil {
			return err
		}

		conditions, err := json.Marshal(nonNil(cr.Status.Conditions))
		if err != nil {
			return err
		}
		current := version == ws.DesiredGeneration
		state := nextState(ws.State, cr.Status.Phase, current)
		// A node holding more than it can is drift too: the Workspace runs on,
		// Degraded, until an Admin fixes the node (04-state-machines §5.3).
		if state == workspace.StateReady && ws.ComputeMode == workspace.ComputeDedicated && (ws.State == workspace.StateReady || ws.State == workspace.StateDegraded) {
			over, err := q.WorkspaceNodeOvercommitted(ctx, ws.ID)
			if err != nil {
				return err
			}
			if over {
				state = workspace.StateDegraded
			}
		}
		op, err := q.ActiveWorkspaceOperation(ctx, ws.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			op, err = store.ActiveWorkspaceOperationRow{}, nil
		}
		if err != nil {
			return err
		}
		// A cordon keeps the Workspace's state until it is done, then Cordoned;
		// an uncordon leaves Cordoned for what the controller reports (04 §1.1).
		cordonDone := current && op.State == "Running" &&
			(op.Type == "workspace.cordon" || op.Type == "workspace.uncordon") &&
			(cr.Status.Phase == v1.PhaseReady || cr.Status.Phase == v1.PhaseDegraded)
		if cordonDone {
			switch {
			case op.Type == "workspace.cordon":
				state = workspace.StateCordoned
			case cr.Status.Phase == v1.PhaseReady:
				state = workspace.StateReady
			default:
				state = workspace.StateDegraded
			}
		}
		if _, err := q.RecordWorkspaceStatus(ctx, store.RecordWorkspaceStatusParams{
			ID: ws.ID, Conditions: conditions, ObservedGeneration: version, State: state,
		}); err != nil {
			return err
		}
		if cordonDone {
			_, _, err := operation.Move(ctx, q, op.ID, operation.Succeeded, "", "")
			return err
		}
		// A change to a Cordoned Workspace (its grant's storage classes)
		// leaves it Cordoned and ends when the controller has it.
		if current && op.State == "Running" && ws.State == workspace.StateCordoned && cr.Status.Phase == v1.PhaseReady {
			_, _, err := operation.Move(ctx, q, op.ID, operation.Succeeded, "", "")
			return err
		}
		if !current || op.ID == "" || (ws.State != workspace.StateProvisioning && ws.State != workspace.StateUpdating) {
			return nil
		}
		// The operation that wrote this version ends with it.
		switch state {
		case workspace.StateReady:
			if err := workspace.Settled(ctx, q, ws); err != nil {
				return err
			}
			_, _, err = operation.Move(ctx, q, op.ID, operation.Succeeded, "", "")
		case workspace.StateFailed, workspace.StateDegraded:
			_, _, err = operation.Move(ctx, q, op.ID, operation.Failed,
				"RECONCILE_"+strings.ToUpper(string(cr.Status.Phase)), failedConditions(cr.Status.Conditions))
		}
		return err
	})
}

// nextState is the Workspace state a trusted status leads to (04 §1.1).
// current tells whether the status describes the latest spec version.
func nextState(state string, phase v1.WorkspacePhase, current bool) string {
	if !current {
		return state // a newer spec is on its way
	}
	switch state {
	case workspace.StateProvisioning:
		switch phase {
		case v1.PhaseReady:
			return workspace.StateReady
		case v1.PhaseFailed:
			return workspace.StateFailed
		}
	case workspace.StateUpdating:
		switch phase {
		case v1.PhaseReady:
			return workspace.StateReady
		case v1.PhaseFailed, v1.PhaseDegraded:
			return workspace.StateDegraded
		}
	case workspace.StateReady:
		if phase == v1.PhaseDegraded || phase == v1.PhaseFailed {
			return workspace.StateDegraded // drift
		}
	case workspace.StateDegraded:
		if phase == v1.PhaseReady {
			return workspace.StateReady // drift handled
		}
	}
	// Failed waits for a retry; Cordoned and Deleting have their own operations.
	return state
}

// syncNamespaces brings workspace_namespaces in line with what the
// controller observed (04-state-machines §9.2): requested Namespaces that
// exist become Active, ones deleted from the portal or created with kubectl
// and gone become Deleted, and ones created with kubectl appear. A portal
// Namespace missing from the status stays Active: it is declared, so the
// controller creates it again (only the portal deletes it). Usage is copied
// for every one.
func (s *Syncer) syncNamespaces(ctx context.Context, q *store.Queries, ws store.Workspace, cr v1.PlatformWorkspace) error {
	observed := map[string]v1.NamespaceStatus{}
	for _, n := range cr.Status.Namespaces {
		observed[n.Name] = n
	}
	rows, err := q.ListWorkspaceNamespaces(ctx, ws.ID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		obs, present := observed[row.Name]
		delete(observed, row.Name)
		state := row.State
		switch {
		case present && row.State != "Deleting":
			state = "Active"
		case !present && (row.State == "Deleting" || (row.State == "Active" && row.Source == workspace.SourceKubectl)):
			state = "Deleted"
		}
		usage, err := usageJSON(obs.Usage)
		if err != nil {
			return err
		}
		if state == row.State && string(usage) == string(row.UsageSnapshot) {
			continue
		}
		if err := q.RecordNamespace(ctx, store.RecordNamespaceParams{ID: row.ID, State: state, UsageSnapshot: usage}); err != nil {
			return err
		}
	}
	for name, obs := range observed {
		if !strings.HasPrefix(name, ws.K8sName+"-") {
			continue // Capsule's prefix rule makes this impossible; never record a stranger's name
		}
		usage, err := usageJSON(obs.Usage)
		if err != nil {
			return err
		}
		if err := q.CreateObservedNamespace(ctx, store.CreateObservedNamespaceParams{
			ID: id.New(id.Namespace), WorkspaceID: ws.ID, ClusterID: ws.ClusterID, Name: name,
			Source: string(obs.Source), UsageSnapshot: usage,
		}); err != nil {
			return err
		}
	}
	return nil
}

// failTimedOut fails the operations delivered longer ago than the timeout
// whose Workspace the controller has not brought to their version
// (IMPLEMENTATION-PLAN §5.1). crs gives the reason the controller reports.
func (s *Syncer) failTimedOut(ctx context.Context, crs []v1.PlatformWorkspace) error {
	ops, err := s.q.TimedOutOperations(ctx, store.TimedOutOperationsParams{ClusterID: s.clusterID, TimeoutSeconds: s.timeout.Seconds()})
	if err != nil {
		return err
	}
	byID := map[string]v1.PlatformWorkspace{}
	for _, cr := range crs {
		byID[cr.Spec.WorkspaceID] = cr
	}
	for _, op := range ops {
		if op.ResourceType != "Workspace" {
			msg := fmt.Sprintf("the operator did not finish within %s", s.timeout)
			if _, _, err := operation.Move(ctx, s.q, op.ID, operation.Failed, ReconcileTimeout, msg); err != nil {
				return fmt.Errorf("operation %s: %w", op.ID, err)
			}
			continue
		}
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			ws, err := q.LockWorkspace(ctx, op.ResourceID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			msg := fmt.Sprintf("the controller did not finish within %s", s.timeout)
			if cr, ok := byID[ws.ID]; ok {
				if why := failedConditions(cr.Status.Conditions); why != "" {
					msg += ": " + why
				}
			}
			if _, ok, err := operation.Move(ctx, q, op.ID, operation.Failed, ReconcileTimeout, msg); err != nil || !ok {
				return err // moved meanwhile
			}
			s.log.Warn("operation timed out", "operation_id", op.ID, "workspace_id", ws.ID, "reason", msg)
			next := ws.State
			switch ws.State {
			case workspace.StateProvisioning:
				next = workspace.StateFailed
			case workspace.StateUpdating:
				next = workspace.StateDegraded
			}
			if next == ws.State {
				return nil
			}
			_, err = q.RecordWorkspaceStatus(ctx, store.RecordWorkspaceStatusParams{
				ID: ws.ID, Conditions: ws.Conditions, ObservedGeneration: ws.ObservedGeneration, State: next,
			})
			return err
		})
		if err != nil {
			return fmt.Errorf("operation %s: %w", op.ID, err)
		}
	}
	return nil
}

// failedConditions names the conditions that are not True, for people.
func failedConditions(cs []metav1.Condition) string {
	var parts []string
	for _, c := range cs {
		if c.Status != metav1.ConditionTrue && c.Type != v1.ConditionDeleting {
			parts = append(parts, fmt.Sprintf("%s %s: %s", c.Type, c.Reason, c.Message))
		}
	}
	return strings.Join(parts, "; ")
}

func usageJSON(u v1.ResourceList) ([]byte, error) {
	out := map[string]string{}
	for k, q := range u {
		out[k] = q.String()
	}
	return json.Marshal(out)
}

func nonNil(cs []metav1.Condition) []metav1.Condition {
	if cs == nil {
		return []metav1.Condition{}
	}
	return cs
}

// syncDeletes finishes deletes on this cluster (IMPLEMENTATION-PLAN §5.1):
// a Deleting Workspace whose CR is gone after its delete was delivered is
// released; one whose finalizer reports itself stuck has its operation
// Blocked (04-state-machines §2).
func (s *Syncer) syncDeletes(ctx context.Context, crs []v1.PlatformWorkspace) error {
	byName := map[string]v1.PlatformWorkspace{}
	for _, cr := range crs {
		byName[cr.Name] = cr
	}
	deleting, err := s.q.ListDeletingWorkspaces(ctx, s.clusterID)
	if err != nil {
		return err
	}
	for _, ws := range deleting {
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			ws, err := q.LockWorkspace(ctx, ws.ID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && ws.State != workspace.StateDeleting) {
				return nil
			}
			if err != nil {
				return err
			}
			op, err := q.ActiveWorkspaceOperation(ctx, ws.ID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && op.State == "Pending") {
				return nil // not delivered yet: the CR may simply not exist yet
			}
			if err != nil {
				return err
			}
			cr, present := byName[ws.K8sName]
			if !present {
				return workspace.Release(ctx, q, ws, op.ID)
			}
			c := meta.FindStatusCondition(cr.Status.Conditions, v1.ConditionDeleting)
			if c != nil && c.Status == metav1.ConditionFalse && c.Reason == v1.ReasonBlockedByFinalizer && op.State == "Running" {
				_, _, err := operation.Move(ctx, q, op.ID, operation.Blocked, "BLOCKED_BY_FINALIZER", c.Message)
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("workspace %s: %w", ws.ID, err)
		}
	}
	return nil
}
