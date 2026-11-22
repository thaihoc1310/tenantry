// Package org holds the organization rules: who may read or change an
// organization and what a valid change is. It knows nothing about HTTP: rule
// violations are *apperr.Error values the HTTP layer turns into responses.
package org

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

type Role string

const (
	RoleOwner  Role = "OrgOwner"
	RoleAdmin  Role = "OrgAdmin"
	RoleMember Role = "OrgMember"
)

const StatusRejected = "Rejected"

// Field limits, kept equal to openapi/tenantry.v1.yaml.
const (
	maxDisplayName = 80
	maxDescription = 500

	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Unique indexes from migrations/0001_users_orgs.sql.
const (
	slugIndex       = "organizations_slug_uq"
	onePendingIndex = "organizations_one_pending_per_user_uq"
)

var (
	// ErrNotFound covers "does not exist" and "you are not a member" alike,
	// so callers cannot probe which organizations exist.
	ErrNotFound  = apperr.New(apperr.NotFound, "organization not found")
	ErrForbidden = apperr.New(apperr.Forbidden, "requires OrgOwner or OrgAdmin")
	ErrClosed    = apperr.New(apperr.Conflict, "organization request was rejected and is read-only")

	ErrSlugTaken = &apperr.Error{
		Code: apperr.SlugTaken, Message: "slug is already in use", Details: map[string]any{"field": "slug"},
	}
	ErrRequestPending = apperr.New(apperr.OrgRequestPending, "you already have an organization request waiting for review")
)

// Org is an organization as seen by one of its members.
type Org struct {
	store.Organization
	Role Role
}

type Service struct {
	db *pgxpool.Pool // for transactions
	q  *store.Queries
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db, q: store.New(db)} }

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

// Page is one page of a list, newest first. NextCursor is empty on the last page.
type Page struct {
	Items      []Org
	NextCursor string
}

// List returns the organizations userID belongs to, including their own
// Pending and Rejected requests. pageSize 0 means DefaultPageSize; cursor ""
// starts from the newest.
func (s *Service) List(ctx context.Context, userID, cursor string, pageSize int) (Page, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return Page{}, apperr.Invalid("limit", fmt.Sprintf("must be between 1 and %d", MaxPageSize))
	}
	arg := store.ListOrganizationsForMemberParams{UserID: userID, PageSize: int32(pageSize + 1)} // one extra row tells whether a next page exists
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListOrganizationsForMember(ctx, arg)
	if err != nil {
		return Page{}, err
	}

	p := Page{Items: make([]Org, 0, len(rows))}
	for _, r := range rows {
		p.Items = append(p.Items, Org{Organization: r.Organization, Role: Role(r.Role)})
	}
	if len(p.Items) > pageSize {
		p.Items = p.Items[:pageSize]
		p.NextCursor = p.Items[pageSize-1].ID
	}
	return p, nil
}

// Request is what a user submits to ask for a new organization.
type Request struct {
	Slug        string
	DisplayName string
	Description string
}

// Create files userID's request for a new organization (PRODUCT-PLAN §4.2): it
// starts Pending with the requester as its OrgOwner until a Platform Admin
// approves or rejects it.
func (s *Service) Create(ctx context.Context, userID string, r Request) (Org, error) {
	if err := checkSlug(r.Slug); err != nil {
		return Org{}, err
	}
	name, err := clean("displayName", r.DisplayName, maxDisplayName, true)
	if err != nil {
		return Org{}, err
	}
	desc, err := clean("description", r.Description, maxDescription, false)
	if err != nil {
		return Org{}, err
	}

	// The organization, its owner membership and the audit event exist
	// together or not at all.
	var created store.Organization
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := q.CreateOrganizationRequest(ctx, store.CreateOrganizationRequestParams{
			ID: id.New(id.Organization), Slug: r.Slug, DisplayName: name, Description: desc, OwnerUserID: userID,
		})
		if err != nil {
			return err
		}
		created = o
		if err := q.AddOrganizationMember(ctx, store.AddOrganizationMemberParams{
			OrgID: created.ID, UserID: userID, Role: string(RoleOwner),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: o.ID, Action: audit.OrganizationCreate, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{"slug": o.Slug, "displayName": o.DisplayName, "description": o.Description, "status": o.Status},
		})
	})
	// No "is it free?" query beforehand: two concurrent requests would both
	// pass it. The unique indexes decide, and losing the race is a 409.
	if c, ok := store.UniqueViolation(err); ok {
		switch c {
		case slugIndex:
			return Org{}, ErrSlugTaken
		case onePendingIndex:
			return Org{}, ErrRequestPending
		}
	}
	if err != nil {
		return Org{}, err
	}
	return Org{Organization: created, Role: RoleOwner}, nil
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
		v, err := clean("displayName", *p.DisplayName, maxDisplayName, true)
		if err != nil {
			return Org{}, err
		}
		p.DisplayName = &v
	}
	if p.Description != nil {
		v, err := clean("description", *p.Description, maxDescription, false)
		if err != nil {
			return Org{}, err
		}
		p.Description = &v
	}
	if p.DisplayName == nil && p.Description == nil {
		return o, nil
	}

	updated := o.Organization
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.LockOrganization(ctx, o.ID)
		if err != nil {
			return err
		}
		if cur.Status != "Pending" && cur.Status != "Active" {
			return ErrClosed // rejected or deleted since Get
		}
		// Only fields that really change are written and audited.
		before, after := map[string]any{}, map[string]any{}
		if p.DisplayName != nil && *p.DisplayName != cur.DisplayName {
			before["displayName"], after["displayName"] = cur.DisplayName, *p.DisplayName
		} else {
			p.DisplayName = nil
		}
		if p.Description != nil && *p.Description != cur.Description {
			before["description"], after["description"] = cur.Description, *p.Description
		} else {
			p.Description = nil
		}
		if len(after) == 0 {
			updated = cur
			return nil
		}
		if updated, err = q.UpdateOrganization(ctx, store.UpdateOrganizationParams{
			ID: o.ID, DisplayName: p.DisplayName, Description: p.Description,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: o.ID, Action: audit.OrganizationUpdate, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{"before": before, "after": after},
		})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrClosed // rejected or deleted since Get
	}
	if err != nil {
		return Org{}, err
	}
	return Org{Organization: updated, Role: o.Role}, nil
}

// No hyphen in an organization slug: <org>-<ws> then names exactly one
// Workspace of one organization, and two organizations' names never nest
// (acme-x of org "acme" against workspace "x" of an org "acme-x") (PRODUCT-PLAN §4.2).
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// reservedSlugs could be mistaken for platform objects (IMPLEMENTATION-PLAN §12,
// decision 4). "kube" also keeps Namespaces out of Kubernetes' kube- prefix.
var reservedSlugs = []string{"kube", "default", "system", "capsule", "tenantry", "admin"}

// checkSlug enforces the slug rules. The slug prefixes every Kubernetes name
// the organization owns (<org>-<ws>-<ns> must fit a 63-char DNS label).
func checkSlug(s string) error {
	if n := len(s); n < 3 || n > 20 {
		return apperr.Invalid("slug", "must be 3 to 20 characters")
	}
	if !slugPattern.MatchString(s) {
		return apperr.Invalid("slug", "must be lowercase letters and digits, starting with a letter")
	}
	if slices.Contains(reservedSlugs, s) {
		return apperr.Invalid("slug", "is reserved")
	}
	return nil
}

// clean trims a free-text field and checks its length in characters.
func clean(field, v string, maxLen int, required bool) (string, error) {
	v = strings.TrimSpace(v)
	if required && v == "" {
		return "", apperr.Invalid(field, "must not be blank")
	}
	if utf8.RuneCountInString(v) > maxLen {
		return "", apperr.Invalid(field, fmt.Sprintf("must be at most %d characters", maxLen))
	}
	return v, nil
}
