package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Delete takes a Workspace away with everything in it: Namespaces,
// workloads, volumes (PRODUCT-PLAN §17.4). OrgOwner or OrgAdmin, typing the
// slug again. The Workspace is Deleting and keeps its share of the grant
// until the worker sees the cluster clean (the CR gone); only then is it
// Deleted and released.
func (s *Service) Delete(ctx context.Context, userID, wsID, confirm, correlationID string) (Changed, error) {
	ws, a, err := s.access(ctx, userID, wsID)
	if err != nil {
		return Changed{}, err
	}
	if !a.Manages() {
		return Changed{}, ErrForbidden
	}
	if confirm != ws.Slug {
		return Changed{}, &apperr.Error{Code: apperr.Validation, Message: "confirmName: type the Workspace's slug to delete it",
			Details: map[string]any{"field": "confirmName"}}
	}
	var out Changed
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockIn(ctx, q, wsID, StateReady, StateCordoned, StateDegraded, StateFailed)
		if err != nil {
			return err
		}
		if ws, err = q.SetWorkspaceState(ctx, store.SetWorkspaceStateParams{ID: ws.ID, State: StateDeleting}); err != nil {
			return err
		}
		// Still held, and counted, until the cluster is clean (04 §3, §4).
		if err := q.SetReservationState(ctx, store.SetReservationStateParams{WorkspaceID: ws.ID, State: "Releasing"}); err != nil {
			return err
		}
		if err := q.SetAllocationState(ctx, store.SetAllocationStateParams{WorkspaceID: ws.ID, State: "Releasing"}); err != nil {
			return err
		}
		op, err := newOperation(ctx, q, ws, "workspace.delete", userID, correlationID)
		if err != nil {
			return err
		}
		if _, err := outbox.Enqueue(ctx, q, op, outbox.Event{
			TargetClusterID: ws.ClusterID, Type: EventDelete, Payload: map[string]string{"name": ws.K8sName}, Version: ws.DesiredGeneration,
		}); err != nil {
			return err
		}
		out = Changed{Workspace: ws, Operation: &op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: audit.WorkspaceDelete, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{"slug": ws.Slug, "operationId": op.ID},
		})
	})
	return out, err
}

// Quarantine is how long a deleted Workspace's address block, and its
// k8s_name, stay unused (04-state-machines §4).
const Quarantine = 10 * time.Minute

// Release finishes a delete once the CR is gone (IMPLEMENTATION-PLAN §3.2):
// in one transaction the operation succeeds, the node reservation is
// Released and the Workspace Deleted, out of every sum; its block goes into
// quarantine, and housekeeping deletes the row after (ReleaseQuarantined).
// The members of the groups it bound are counted again for
// tenantry:tenant-users first.
func Release(ctx context.Context, q *store.Queries, ws store.Workspace, opID string) error {
	groups, err := q.BoundGroupIDs(ctx, ws.ID)
	if err != nil {
		return err
	}
	if _, err := q.ResyncGroupMembers(ctx, groups); err != nil {
		return err
	}
	if opID != "" {
		if _, _, err := operation.Move(ctx, q, opID, operation.Succeeded, "", ""); err != nil {
			return err
		}
	}
	if err := audit.Record(ctx, q, audit.Event{
		OrgID: ws.OrgID, Action: audit.WorkspaceDeleted, TargetType: "Workspace", TargetID: ws.ID,
		Detail: map[string]any{"slug": ws.Slug, "k8sName": ws.K8sName, "operationId": opID},
	}); err != nil {
		return err
	}
	res, err := q.GetNodeReservation(ctx, ws.ID)
	dedicated := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = q.GetNetworkAllocation(ctx, ws.ID)
	quarantine := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if quarantine {
		// The block waits out its quarantine, and the row with it, so the
		// k8s_name is not taken again meanwhile; out of every sum already.
		until := time.Now().Add(Quarantine)
		if err := q.SetAllocationState(ctx, store.SetAllocationStateParams{WorkspaceID: ws.ID, State: "Quarantined", QuarantineUntil: &until}); err != nil {
			return err
		}
		if err := q.MarkWorkspaceDeleted(ctx, ws.ID); err != nil {
			return err
		}
		if err := q.SetReservationState(ctx, store.SetReservationStateParams{WorkspaceID: ws.ID, State: "Released"}); err != nil {
			return err
		}
	} else if err := q.DeleteWorkspaceRow(ctx, ws.ID); err != nil {
		// A Workspace from before network allocations (M6–M8) goes at once,
		// its node reservation with it.
		return err
	}
	if dedicated {
		return reservation.RefreshNodeCapacity(ctx, q, res.NodeGrantID)
	}
	return nil
}

var (
	ErrNoOperation = apperr.New(apperr.NotFound, "operation not found")
	ErrNotForcible = apperr.New(apperr.Conflict, "only a Blocked Workspace delete can be forced")
	ErrNotAdmin    = apperr.New(apperr.Forbidden, "only a Platform Admin can force a delete")
)

// Force has a Blocked delete stop waiting for what holds its Namespaces
// (PRODUCT-PLAN §17.4): the worker marks the CR tenantry.io/force-delete
// and the finalizer goes on to delete the Tenant. Platform Admins only,
// always audited; volumes left behind are theirs to clean up.
func (s *Service) Force(ctx context.Context, adminID string, platformAdmin bool, opID, correlationID string) (store.Operation, error) {
	if !platformAdmin {
		return store.Operation{}, ErrNotAdmin
	}
	var out store.Operation
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		op, err := q.LockOperation(ctx, opID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoOperation
		}
		if err != nil {
			return err
		}
		if op.Type != "workspace.delete" || op.State != string(operation.Blocked) {
			return ErrNotForcible
		}
		ws, err := q.LockWorkspace(ctx, op.ResourceID)
		if err != nil {
			return err
		}
		if out, _, err = operation.Move(ctx, q, op.ID, operation.Running, "", ""); err != nil {
			return err
		}
		if _, err := outbox.Enqueue(ctx, q, out, outbox.Event{
			TargetClusterID: ws.ClusterID, Type: EventForce, Payload: map[string]string{"name": ws.K8sName}, Version: ws.DesiredGeneration,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: ws.OrgID, Action: audit.OperationForce, TargetType: "Operation", TargetID: op.ID,
			Detail: map[string]any{"workspaceId": ws.ID, "k8sName": ws.K8sName, "blockedBy": op.ErrorMessage, "correlationId": correlationID},
		})
	})
	return out, err
}

// Settled records that the cluster has a Dedicated Workspace's quota: its
// reservation is Active (from Pending, the first time) and, after a
// smaller resize, down to the new numbers (04-state-machines §3). A Shared
// Workspace has nothing to settle.
func Settled(ctx context.Context, q *store.Queries, ws store.Workspace) error {
	if ws.ComputeMode != ComputeDedicated {
		return nil
	}
	res, err := q.GetNodeReservation(ctx, ws.ID)
	if err != nil {
		return err
	}
	if err := q.SetReservationState(ctx, store.SetReservationStateParams{WorkspaceID: ws.ID, State: "Active"}); err != nil {
		return err
	}
	if res.ReservedCpuMilli == ws.RequestsCpuMilli && res.ReservedMemoryBytes == ws.RequestsMemoryBytes && res.ReservedPods == ws.Pods {
		return nil
	}
	if err := q.SetReservationAmount(ctx, store.SetReservationAmountParams{
		WorkspaceID: ws.ID, ReservedCpuMilli: ws.RequestsCpuMilli, ReservedMemoryBytes: ws.RequestsMemoryBytes, ReservedPods: ws.Pods,
	}); err != nil {
		return err
	}
	return reservation.RefreshNodeCapacity(ctx, q, res.NodeGrantID)
}
