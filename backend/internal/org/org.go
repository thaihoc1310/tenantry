// Package org holds the organization rules: who may read or change an
// organization and what a valid change is. It knows nothing about HTTP: rule
// violations are *apperr.Error values the HTTP layer turns into responses.
package org

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

type Role string

const (
	RoleOwner  Role = "OrgOwner"
	RoleAdmin  Role = "OrgAdmin"
	RoleMember Role = "OrgMember"
)

const StatusRejected = "Rejected"

var (
	// ErrNotFound covers "does not exist" and "you are not a member" alike,
	// so callers cannot probe which organizations exist.
	ErrNotFound  = apperr.New(apperr.NotFound, "organization not found")
	ErrForbidden = apperr.New(apperr.Forbidden, "requires OrgOwner or OrgAdmin")
	ErrClosed    = apperr.New(apperr.Conflict, "organization request was rejected and is read-only")
)

// Org is an organization as seen by one of its members.
type Org struct {
	store.Organization
	Role Role
}

type Service struct{ q *store.Queries }

func NewService(q *store.Queries) *Service { return &Service{q: q} }

// Get resolves idOrSlug for userID; non-members get ErrNotFound.
func (s *Service) Get(ctx context.Context, userID, idOrSlug string) (Org, error) {
	row, err := s.q.GetOrganizationForMember(ctx, store.GetOrganizationForMemberParams{UserID: userID, IDOrSlug: idOrSlug})
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	if err != nil {
		return Org{}, err
	}
	return Org{Organization: row.Organization, Role: Role(row.Role)}, nil
}

// Patch holds the fields to change; nil keeps the current value.
type Patch struct {
	DisplayName *string
	Description *string
}

// Update changes display name and description (OrgOwner or OrgAdmin, PRODUCT-PLAN §9.2).
// A Pending request can still be edited while it waits for review; a Rejected one cannot.
func (s *Service) Update(ctx context.Context, userID, idOrSlug string, p Patch) (Org, error) {
	o, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return Org{}, err
	}
	if o.Role != RoleOwner && o.Role != RoleAdmin {
		return Org{}, ErrForbidden
	}
	if o.Status == StatusRejected {
		return Org{}, ErrClosed
	}
	if p.DisplayName != nil {
		v := strings.TrimSpace(*p.DisplayName)
		if v == "" {
			return Org{}, apperr.Invalid("displayName", "must not be blank")
		}
		p.DisplayName = &v
	}
	if p.Description != nil {
		v := strings.TrimSpace(*p.Description)
		p.Description = &v
	}
	if p.DisplayName == nil && p.Description == nil {
		return o, nil
	}

	updated, err := s.q.UpdateOrganization(ctx, store.UpdateOrganizationParams{
		ID: o.ID, DisplayName: p.DisplayName, Description: p.Description,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrClosed // rejected or deleted since Get
	}
	if err != nil {
		return Org{}, err
	}
	return Org{Organization: updated, Role: o.Role}, nil
}
