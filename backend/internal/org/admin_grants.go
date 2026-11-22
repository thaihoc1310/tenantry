package org

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Cluster grants and the onboarding policy as a Platform Admin edits them
// (PRODUCT-PLAN §5.2.1, §5.5).

var (
	ErrNoSuchGrant = apperr.New(apperr.NotFound, "the organization has no grant on this cluster")
	ErrNoPolicy    = apperr.New(apperr.NotFound, "no onboarding policy is active")
)

// lockActive locks an organization for an Admin's change: it exists and is Active.
func lockActive(ctx context.Context, q *store.Queries, orgID string) (store.Organization, error) {
	o, err := q.LockOrganization(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && o.Status == "Deleted") {
		return o, ErrNoSuchOrg
	}
	if err != nil {
		return o, err
	}
	if o.Status != StatusActive {
		return o, ErrNotActive
	}
	return o, nil
}

// PutGrant gives an Active organization a grant on a cluster, or changes the
// one it has, under the grant's lock (§5.5). A new grant needs a Ready
// cluster, and is the default when it is the organization's only one. Every
// change is audited with the values before and after.
//
// ponytail: the guards that need Workspaces come with them: no number below
// what Workspaces hold (M7, CLUSTER_GRANT_BELOW_ALLOCATION), no shared pool
// taken away while a Shared Workspace uses it (M8), no network pool while a
// Workspace holds a CIDR in it (M9), and a storage class change re-renders
// every Workspace of the grant (M7).
func (s *Service) PutGrant(ctx context.Context, adminID, orgID string, g reservation.GrantSpec) (reservation.Grant, bool, error) {
	var out reservation.Grant
	created := false
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := lockActive(ctx, q, orgID)
		if err != nil {
			return err
		}
		cur, err := q.LockClusterGrant(ctx, store.LockClusterGrantParams{OrgID: o.ID, ClusterID: g.ClusterID})
		if errors.Is(err, pgx.ErrNoRows) {
			n, err := q.CountOrganizationGrants(ctx, o.ID)
			if err != nil {
				return err
			}
			if out, err = reservation.CreateGrant(ctx, q, o.ID, n == 0, g); err != nil {
				return err
			}
			created = true
			return audit.Record(ctx, q, audit.Event{
				Actor: adminID, OrgID: o.ID, Action: audit.ClusterGrantCreate, TargetType: "ClusterGrant", TargetID: out.ID,
				Detail: map[string]any{"after": out},
			})
		}
		if err != nil {
			return err
		}
		before, err := grantOf(ctx, q, o.ID, cur.ID)
		if err != nil {
			return err
		}
		if out, err = reservation.UpdateGrant(ctx, q, cur, g); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: o.ID, Action: audit.ClusterGrantUpdate, TargetType: "ClusterGrant", TargetID: out.ID,
			Detail: map[string]any{"before": before, "after": out},
		})
	})
	return out, created, err
}

func grantOf(ctx context.Context, q *store.Queries, orgID, grantID string) (reservation.Grant, error) {
	grants, err := reservation.Grants(ctx, q, orgID)
	if err != nil {
		return reservation.Grant{}, err
	}
	for _, g := range grants {
		if g.ID == grantID {
			return g, nil
		}
	}
	return reservation.Grant{}, ErrNoSuchGrant
}

// DeleteGrant takes an organization's grant on a cluster away. Not while it
// holds dedicated nodes there; deleting the default grant makes the oldest
// one left the default.
//
// ponytail: refusing while Workspaces use the grant comes with Workspaces (M7).
func (s *Service) DeleteGrant(ctx context.Context, adminID, orgID, clusterID string) error {
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := lockActive(ctx, q, orgID)
		if err != nil {
			return err
		}
		cur, err := q.LockClusterGrant(ctx, store.LockClusterGrantParams{OrgID: o.ID, ClusterID: clusterID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGrant
		}
		if err != nil {
			return err
		}
		nodes, err := q.CountLiveDedicatedNodes(ctx, store.CountLiveDedicatedNodesParams{OrgID: o.ID, ClusterID: clusterID})
		if err != nil {
			return err
		}
		if nodes > 0 {
			return &apperr.Error{Code: apperr.Conflict, Details: map[string]any{"dedicatedNodes": nodes},
				Message: fmt.Sprintf("the organization holds %d dedicated node(s) on this cluster: revoke them first", nodes)}
		}
		before, err := grantOf(ctx, q, o.ID, cur.ID)
		if err != nil {
			return err
		}
		if err := q.DeleteClusterGrant(ctx, cur.ID); err != nil {
			return err
		}
		if cur.IsDefault {
			if err := q.PromoteOldestGrant(ctx, o.ID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: o.ID, Action: audit.ClusterGrantDelete, TargetType: "ClusterGrant", TargetID: cur.ID,
			Detail: map[string]any{"before": before},
		})
	})
}

// GrantRow is a grant with its organization, for the Admin's list of every grant.
type GrantRow struct {
	reservation.Grant
	OrgSlug, OrgDisplayName string
	Allocation              reservation.Allocation
}

type GrantPage struct {
	Items      []GrantRow
	NextCursor string
}

// AllGrants lists the grants of every organization, optionally of one cluster.
func (s *Service) AllGrants(ctx context.Context, clusterID, cursor string, pageSize int) (GrantPage, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return GrantPage{}, apperr.Invalid("limit", fmt.Sprintf("must be between 1 and %d", MaxPageSize))
	}
	arg := store.ListAllClusterGrantsParams{PageSize: int32(pageSize + 1)}
	if clusterID != "" {
		arg.ClusterID = &clusterID
	}
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListAllClusterGrants(ctx, arg)
	if err != nil {
		return GrantPage{}, err
	}
	p := GrantPage{Items: make([]GrantRow, 0, len(rows))}
	for _, r := range rows {
		g := reservation.FromRow(r.OrganizationClusterGrant, r.SharedPoolIds, r.NetworkPoolIds)
		p.Items = append(p.Items, GrantRow{Grant: g, OrgSlug: r.OrgSlug, OrgDisplayName: r.OrgDisplayName,
			Allocation: reservation.Allocations([]reservation.Grant{g})[0]})
	}
	if len(p.Items) > pageSize {
		p.Items = p.Items[:pageSize]
		p.NextCursor = p.Items[pageSize-1].ID
	}
	return p, nil
}

// SetPolicy replaces the active onboarding policy with entitlement and grant,
// stored as the templates the approval form takes: grantTemplate is grant in
// the API's shape, without its cluster (the policy's default cluster). What
// it proposes must be grantable to any organization: pools on its cluster,
// network pools shared. Grants already given are copies and do not change.
func (s *Service) SetPolicy(ctx context.Context, adminID string, e Entitlement, grant reservation.GrantSpec, grantTemplate []byte) error {
	if err := e.check(); err != nil {
		return err
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// No organization owns what the policy proposes: "" admits shared network pools only.
		if _, err := reservation.CheckGrant(ctx, q, "", grant); err != nil {
			return err
		}
		ent, err := json.Marshal(e)
		if err != nil {
			return err
		}
		before, _, err := reservation.ActivePolicy(ctx, q)
		if err != nil {
			return err
		}
		if _, err := q.UpdateActiveOnboardingPolicy(ctx, store.UpdateActiveOnboardingPolicyParams{
			DefaultClusterID: grant.ClusterID, EntitlementTemplate: ent, GrantTemplate: grantTemplate,
		}); errors.Is(err, pgx.ErrNoRows) {
			if _, err := q.CreateOnboardingPolicy(ctx, store.CreateOnboardingPolicyParams{
				DefaultClusterID: grant.ClusterID, EntitlementTemplate: ent, GrantTemplate: grantTemplate,
			}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, Action: audit.PolicyUpdate, TargetType: "OnboardingPolicy", TargetID: "default",
			Detail: map[string]any{
				"before": map[string]any{"defaultClusterId": before.DefaultClusterID,
					"entitlement": json.RawMessage(orNull(before.EntitlementTemplate)), "clusterGrant": json.RawMessage(orNull(before.GrantTemplate))},
				"after": map[string]any{"defaultClusterId": grant.ClusterID,
					"entitlement": json.RawMessage(ent), "clusterGrant": json.RawMessage(grantTemplate)},
			},
		})
	})
}

func orNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}
