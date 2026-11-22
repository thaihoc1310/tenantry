// Package operation tracks asynchronous work a user can follow, such as
// creating a Workspace: Pending until the change reaches the cluster, Running
// while the controller works, then Succeeded, Failed or Blocked
// (IMPLEMENTATION-PLAN M2 item 6).
//
// The functions that take a *store.Queries run inside the caller's
// transaction, so an operation is written together with the change it tracks.
package operation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

type State string

const (
	Pending   State = "Pending"   // waiting for its outbox events to reach the cluster
	Running   State = "Running"   // delivered; the controller is working on it
	Succeeded State = "Succeeded" // the controller reports the change done
	Failed    State = "Failed"    // delivery kept failing, or the controller gave up; can be retried
	Blocked   State = "Blocked"   // needs a Platform Admin, e.g. a delete stuck on a finalizer
)

// cameFrom lists, for each state, the states an operation may move to it
// from. Anything else is refused by MoveOperation's WHERE clause.
var cameFrom = map[State][]State{
	Running:   {Pending, Failed}, // Failed: a delivery still retrying in the background got through
	Succeeded: {Running},
	Failed:    {Pending, Running},
	Blocked:   {Running},
	Pending:   {Failed}, // user retry
}

// ApplyFailed is the error code of an operation whose events could not be
// delivered to the cluster.
const ApplyFailed = "APPLY_FAILED"

var (
	ErrNotFound    = apperr.New(apperr.NotFound, "operation not found")
	ErrForbidden   = apperr.New(apperr.Forbidden, "requires OrgOwner, OrgAdmin or Platform Admin")
	ErrNotRetrying = apperr.New(apperr.Conflict, "only a Failed operation can be retried")
)

// New describes an operation to start.
type New struct {
	OrgID         string // empty for an operation on a cluster itself (sharedpool.update)
	ResourceType  string // e.g. Workspace
	ResourceID    string
	Type          string // e.g. workspace.create
	CorrelationID string // request id of the call that started it
	CreatedBy     string // user id; empty when the platform started it
}

// Create starts an operation in Pending.
func Create(ctx context.Context, q *store.Queries, n New) (store.Operation, error) {
	arg := store.CreateOperationParams{
		ID: id.New(id.Operation), ResourceType: n.ResourceType, ResourceID: n.ResourceID,
		Type: n.Type, CorrelationID: n.CorrelationID,
	}
	if n.OrgID != "" {
		arg.OrgID = &n.OrgID
	}
	if n.CreatedBy != "" {
		arg.CreatedBy = &n.CreatedBy
	}
	return q.CreateOperation(ctx, arg)
}

// Move changes the state of operation id, clearing the error unless one is
// given. ok is false when the transition is not allowed from the current state.
func Move(ctx context.Context, q *store.Queries, opID string, to State, errCode, errMsg string) (op store.Operation, ok bool, err error) {
	from := make([]string, 0, len(cameFrom[to]))
	for _, s := range cameFrom[to] {
		from = append(from, string(s))
	}
	op, err = q.MoveOperation(ctx, store.MoveOperationParams{
		ID: opID, ToState: string(to), FromStates: from, ErrorCode: errCode, ErrorMessage: errMsg,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Operation{}, false, nil
	}
	return op, err == nil, err
}

// Reapply writes the change of a Failed operation to the outbox again, when
// every event of it was delivered and the controller reported the failure.
// It runs in the retry's transaction.
type Reapply func(ctx context.Context, q *store.Queries, op store.Operation) error

// Service serves operations to users.
type Service struct {
	db      *pgxpool.Pool
	q       *store.Queries
	reapply map[string]Reapply // by resource type
}

// NewService serves operations; reapply says, per resource type (Workspace),
// how to retry a failure the controller reported.
func NewService(db *pgxpool.Pool, reapply map[string]Reapply) *Service {
	return &Service{db: db, q: store.New(db), reapply: reapply}
}

// Get returns an operation to any member of its organization, and to a
// Platform Admin for every organization; others get ErrNotFound.
func (s *Service) Get(ctx context.Context, userID string, platformAdmin bool, opID string) (store.Operation, error) {
	row, err := s.find(ctx, userID, platformAdmin, opID)
	return row.Operation, err
}

// Retry puts a Failed operation back to Pending and has its undelivered events
// sent again at once (OrgOwner, OrgAdmin, or a Platform Admin for any
// organization). A failure the controller reported has no undelivered event
// left: the change is written again through the resource type's Reapply.
func (s *Service) Retry(ctx context.Context, userID string, platformAdmin bool, opID string) (store.Operation, error) {
	row, err := s.find(ctx, userID, platformAdmin, opID)
	if err != nil {
		return store.Operation{}, err
	}
	var role org.Role // empty for a Platform Admin who is not a member
	if row.Role != nil {
		role = org.Role(*row.Role)
	}
	if !platformAdmin && role != org.RoleOwner && role != org.RoleAdmin {
		return store.Operation{}, ErrForbidden
	}

	var op store.Operation
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		moved, ok, err := Move(ctx, q, opID, Pending, "", "")
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotRetrying
		}
		op = moved
		n, err := q.ResetOperationEvents(ctx, opID)
		if err != nil {
			return err
		}
		if reapply := s.reapply[op.ResourceType]; n == 0 && reapply != nil {
			if err := reapply(ctx, q, op); err != nil {
				return err
			}
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: deref(op.OrgID), Action: audit.OperationRetry, TargetType: "Operation", TargetID: op.ID,
			// A Platform Admin acting on an organization they are not a member of.
			Detail: map[string]any{"type": op.Type, "asPlatformAdmin": role == ""},
		})
	})
	return op, err
}

func (s *Service) find(ctx context.Context, userID string, platformAdmin bool, opID string) (store.GetOperationForCallerRow, error) {
	row, err := s.q.GetOperationForCaller(ctx, store.GetOperationForCallerParams{ID: opID, UserID: userID, PlatformAdmin: platformAdmin})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	return row, err
}

// Page is one page of AdminList. NextCursor is empty on the last page.
type Page struct {
	Items      []store.Operation
	NextCursor string
}

// AdminList returns the operations of every organization and of clusters,
// newest first; state "" means any (PRODUCT-PLAN §16.5, Platform Admin).
func (s *Service) AdminList(ctx context.Context, state, cursor string, pageSize int) (Page, error) {
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 200 {
		return Page{}, apperr.Invalid("limit", "must be between 1 and 200")
	}
	arg := store.ListOperationsForAdminParams{PageSize: int32(pageSize + 1)}
	if state != "" {
		arg.State = &state
	}
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListOperationsForAdmin(ctx, arg)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: rows}
	if len(rows) > pageSize {
		p.Items = rows[:pageSize]
		p.NextCursor = p.Items[pageSize-1].ID
	}
	return p, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
