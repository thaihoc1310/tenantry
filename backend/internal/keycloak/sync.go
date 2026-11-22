package keycloak

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/backoff"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// TenantUsers is the platform group of everyone with a Workspace binding;
// Capsule knows them as oidc:tenantry:tenant-users (PRODUCT-PLAN §9.3).
const TenantUsers = "tenantry:tenant-users"

const (
	PollInterval = 2 * time.Second
	BatchSize    = 20
	batchTimeout = 2 * time.Minute
	maxLastError = 500
)

// Worker brings Keycloak in line with the groups and group_members tables
// (04-state-machines §9.1). A live row must exist in Keycloak, a tombstone
// must not; a tombstone leaves the database only once Keycloak agrees.
// Groups go first, since a membership needs its group's Keycloak id. A
// Keycloak failure is recorded on the row and retried with backoff. Every
// call is idempotent, so a batch that died half-way (its transaction rolled
// back, the rows still due) is simply done again.
type Worker struct {
	db  *pgxpool.Pool
	q   *store.Queries
	kc  *Client
	log *slog.Logger

	tenantUsers string // Keycloak id of TenantUsers, once known
}

func NewWorker(db *pgxpool.Pool, kc *Client, log *slog.Logger) *Worker {
	return &Worker{db: db, q: store.New(db), kc: kc, log: log}
}

// Run syncs until ctx is cancelled; a full batch is followed straight away
// by the next one, otherwise the worker sleeps PollInterval.
func (w *Worker) Run(ctx context.Context) {
	for {
		batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), batchTimeout)
		n, err := w.RunOnce(batchCtx)
		cancel()
		if err != nil {
			w.log.Error("group sync batch failed", "err", err)
		}
		if n == BatchSize && err == nil && ctx.Err() == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(PollInterval):
		}
	}
}

// RunOnce syncs one batch of groups, then one of memberships, then one of
// accounts, and returns the largest batch size. Only database errors are returned.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	groups, err := w.syncGroups(ctx)
	if err != nil {
		return groups, err
	}
	members, err := w.syncMembers(ctx)
	if err != nil {
		return max(groups, members), err
	}
	users, err := w.syncUsers(ctx)
	return max(groups, members, users), err
}

// syncUsers sets the enabled flag of accounts a Platform Admin disabled or
// enabled (02-data-model §2). The Keycloak user id is the subject.
func (w *Worker) syncUsers(ctx context.Context) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, w.db, func(tx pgx.Tx) error {
		q := w.q.WithTx(tx)
		rows, err := q.ClaimDueUsers(ctx, BatchSize)
		if err != nil {
			return err
		}
		n = len(rows)
		for _, u := range rows {
			if kerr := w.kc.SetUserEnabled(ctx, u.Subject, u.Status == "Active"); kerr != nil {
				w.log.Warn("user sync failed", "user_id", u.ID, "attempts", u.SyncAttempts+1, "err", kerr)
				err = q.MarkUserFailed(ctx, store.MarkUserFailedParams{
					ID: u.ID, LastError: truncate(kerr.Error()), RetryInSeconds: backoff.After(u.SyncAttempts + 1).Seconds(),
				})
			} else {
				err = q.MarkUserSynced(ctx, u.ID)
				w.log.Info("user synced", "user_id", u.ID, "status", u.Status)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// syncGroups claims due groups and holds their row locks until it has
// recorded the outcome, so the API waits rather than change a row under it.
func (w *Worker) syncGroups(ctx context.Context) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, w.db, func(tx pgx.Tx) error {
		q := w.q.WithTx(tx)
		rows, err := q.ClaimDueGroups(ctx, BatchSize)
		if err != nil {
			return err
		}
		n = len(rows)
		for _, g := range rows {
			kcID, kerr := w.reconcileGroup(ctx, g)
			switch {
			case kerr != nil:
				w.log.Warn("group sync failed", "group_id", g.ID, "attempts", g.SyncAttempts+1, "err", kerr)
				err = q.MarkGroupFailed(ctx, store.MarkGroupFailedParams{
					ID: g.ID, LastError: truncate(kerr.Error()), RetryInSeconds: backoff.After(g.SyncAttempts + 1).Seconds(),
				})
			case g.DeletedAt == nil:
				err = q.MarkGroupSynced(ctx, store.MarkGroupSyncedParams{ID: g.ID, KeycloakGroupID: &kcID})
				w.log.Info("group synced", "group_id", g.ID, "keycloak_group_id", kcID)
			default:
				err = q.DeleteGroupRow(ctx, g.ID) // its group_members go with it (ON DELETE CASCADE)
				w.log.Info("group removed", "group_id", g.ID)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// reconcileGroup makes a live group exist in Keycloak and returns its id, or
// deletes a tombstone's group (with its memberships). A group still bound to
// a Workspace cannot be deleted (guarded from M6), so a deleted group gave
// nobody Kubernetes access and nobody is logged out.
func (w *Worker) reconcileGroup(ctx context.Context, g store.Group) (string, error) {
	if g.DeletedAt == nil {
		return w.kc.EnsureGroup(ctx, g.MachineName)
	}
	id := ""
	if g.KeycloakGroupID != nil {
		id = *g.KeycloakGroupID
	} else {
		// A run may have created the group, then died before recording its id.
		found, ok, err := w.kc.FindGroup(ctx, g.MachineName)
		if err != nil || !ok {
			return "", err
		}
		id = found
	}
	return "", w.kc.DeleteGroup(ctx, id)
}

func (w *Worker) syncMembers(ctx context.Context) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, w.db, func(tx pgx.Tx) error {
		q := w.q.WithTx(tx)
		rows, err := q.ClaimDueMembers(ctx, BatchSize)
		if err != nil {
			return err
		}
		n = len(rows)
		for _, m := range rows {
			kerr := w.reconcileMember(ctx, q, m)
			switch {
			case kerr != nil:
				w.log.Warn("group member sync failed", "group_id", m.GroupID, "user_id", m.UserID, "attempts", m.SyncAttempts+1, "err", kerr)
				err = q.MarkMemberFailed(ctx, store.MarkMemberFailedParams{
					GroupID: m.GroupID, UserID: m.UserID, LastError: truncate(kerr.Error()),
					RetryInSeconds: backoff.After(m.SyncAttempts + 1).Seconds(),
				})
			case m.DeletedAt == nil:
				err = q.MarkMemberSynced(ctx, store.MarkMemberSyncedParams{GroupID: m.GroupID, UserID: m.UserID})
			default:
				err = q.DeleteMemberRow(ctx, store.DeleteMemberRowParams{GroupID: m.GroupID, UserID: m.UserID})
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// reconcileMember adds a live membership in Keycloak or removes a
// tombstone's, then brings the person's TenantUsers membership up to date.
// The Keycloak user id is the subject of the person's tokens. A removal logs
// nobody out: Keycloak runs the group mapper again on every refresh, so the
// next token of the portal, Headlamp or kubelogin no longer names the group
// (architecture/06 §5).
func (w *Worker) reconcileMember(ctx context.Context, q *store.Queries, m store.ClaimDueMembersRow) error {
	user, group := m.Subject, *m.KeycloakGroupID // ClaimDueMembers only returns synced groups
	if m.DeletedAt == nil {
		if err := w.kc.AddMember(ctx, user, group); err != nil {
			return err
		}
	} else if err := w.kc.RemoveMember(ctx, user, group); err != nil {
		return err
	}
	return w.syncTenantUsers(ctx, q, m.UserID, user)
}

// syncTenantUsers puts the person in TenantUsers while they have a Workspace
// binding through a live group, and takes them out otherwise (§9.3). Every
// transaction that changes the set of bindings (create or delete a
// Workspace, PUT/DELETE a binding) sets the group_members of the groups
// concerned back to Pending, so this runs again for each person
// (IMPLEMENTATION-PLAN §5.2).
func (w *Worker) syncTenantUsers(ctx context.Context, q *store.Queries, userID, kcUser string) error {
	bound, err := q.UserHasWorkspaceBinding(ctx, userID)
	if err != nil {
		return err
	}
	if w.tenantUsers == "" {
		id, err := w.kc.EnsureGroup(ctx, TenantUsers)
		if err != nil {
			return err
		}
		w.tenantUsers = id
	}
	if bound {
		err = w.kc.AddMember(ctx, kcUser, w.tenantUsers)
	} else {
		err = w.kc.RemoveMember(ctx, kcUser, w.tenantUsers)
	}
	if err != nil {
		w.tenantUsers = "" // look it up again next time, in case it was deleted
	}
	return err
}

// truncate keeps last_error short, without cutting a character in two
// (PostgreSQL refuses invalid UTF-8).
func truncate(s string) string {
	if len(s) > maxLastError {
		return strings.ToValidUTF8(s[:maxLastError], "")
	}
	return s
}
