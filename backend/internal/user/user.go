// Package user holds the rules about accounts. Keycloak owns the account; the
// users table is its projection (PRODUCT-PLAN §4.1).
package user

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// ErrUnknown answers both "no such user" and "disabled", like a login failure.
var ErrUnknown = apperr.New(apperr.Unauthenticated, "unknown or disabled user")

type Service struct{ q *store.Queries }

func NewService(q *store.Queries) *Service { return &Service{q: q} }

// Active returns the user with this id if it may use the portal.
func (s *Service) Active(ctx context.Context, id string) (store.User, error) {
	u, err := s.q.GetActiveUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, ErrUnknown
	}
	return u, err
}
