// Package user is the platform's side of people's accounts (IMPLEMENTATION-PLAN
// §2): a Platform Admin lists them and disables or enables one (PRODUCT-PLAN
// §16.5). Disabling ends the portal sessions at once; the group sync worker
// then sets the account's enabled flag in Keycloak.
package user

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

const (
	Active   = "Active"
	Disabled = "Disabled"

	DefaultPageSize = 50
	MaxPageSize     = 200
)

var (
	ErrNotFound    = apperr.New(apperr.NotFound, "user not found")
	ErrDisableSelf = apperr.New(apperr.Forbidden, "a Platform Admin cannot disable their own account")
)

type User struct {
	ID          string
	Email       *string
	DisplayName string
	Status      string
	SyncStatus  string
	CreatedAt   time.Time
}

func from(u store.User) User {
	return User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: u.Status, SyncStatus: u.SyncStatus, CreatedAt: u.CreatedAt}
}

type Service struct {
	db *pgxpool.Pool
	q  *store.Queries
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db, q: store.New(db)} }

// Page is one page of List. NextCursor is empty on the last page.
type Page struct {
	Items      []User
	NextCursor string
}

// List returns the users who have logged in, by email; query keeps those
// whose email or display name contains it.
func (s *Service) List(ctx context.Context, query, cursor string, pageSize int) (Page, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return Page{}, apperr.Invalid("limit", fmt.Sprintf("must be between 1 and %d", MaxPageSize))
	}
	arg := store.ListUsersForAdminParams{PageSize: int32(pageSize + 1)}
	if q := strings.TrimSpace(query); q != "" {
		// LIKE wildcards in the query are matched literally.
		q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
		arg.Q = &q
	}
	if cursor != "" {
		email, id, err := decodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
		arg.CursorEmail, arg.Cursor = &email, &id
	}
	rows, err := s.q.ListUsersForAdmin(ctx, arg)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: make([]User, 0, len(rows))}
	for _, r := range rows {
		p.Items = append(p.Items, from(r))
	}
	if len(p.Items) > pageSize {
		p.Items = p.Items[:pageSize]
		last := p.Items[pageSize-1]
		p.NextCursor = encodeCursor(last)
	}
	return p, nil
}

// The cursor is the last user's place in the order: email, then id.
func encodeCursor(u User) string {
	email := ""
	if u.Email != nil {
		email = *u.Email
	}
	return base64.RawURLEncoding.EncodeToString([]byte(email + "\x00" + u.ID))
}

func decodeCursor(c string) (email, id string, err error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.SplitN(string(b), "\x00", 2)
	if err != nil || len(parts) != 2 || parts[1] == "" {
		return "", "", apperr.Invalid("cursor", "is not a cursor this list gave")
	}
	return parts[0], parts[1], nil
}

// Disable turns the account off: in one transaction its status, its portal
// sessions (all of them end) and the order to Keycloak. Disabling twice
// changes nothing.
func (s *Service) Disable(ctx context.Context, adminID, userID string) (User, error) {
	if userID == adminID {
		return User{}, ErrDisableSelf
	}
	return s.set(ctx, adminID, userID, Disabled)
}

// Enable turns a disabled account on again.
func (s *Service) Enable(ctx context.Context, adminID, userID string) (User, error) {
	return s.set(ctx, adminID, userID, Active)
}

func (s *Service) set(ctx context.Context, adminID, userID, status string) (User, error) {
	var out User
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		u, err := q.LockUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if u.Status == status {
			out = from(u)
			return nil
		}
		if u, err = q.SetUserStatus(ctx, store.SetUserStatusParams{ID: userID, Status: status}); err != nil {
			return err
		}
		ended := int64(0)
		action := audit.UserEnable
		if status == Disabled {
			action = audit.UserDisable
			if ended, err = q.DeleteUserSessions(ctx, userID); err != nil {
				return err
			}
		}
		out = from(u)
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, Action: action, TargetType: "User", TargetID: userID,
			Detail: map[string]any{"sessionsEnded": ended},
		})
	})
	return out, err
}
