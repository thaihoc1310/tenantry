// Package audit writes the audit trail (PRODUCT-PLAN §21.3). Record runs in
// the caller's transaction, so an event exists exactly when the change it
// records was committed.
package audit

import (
	"context"
	"encoding/json"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Actions recorded so far; the list grows with the endpoints.
const (
	Login              = "user.login"
	Logout             = "user.logout"
	OrganizationCreate = "organization.create"
	OrganizationUpdate = "organization.update"
	OperationRetry     = "operation.retry"
)

// Succeeded is the only result for now: every event is written by the
// change it records. Refused attempts worth keeping get their own result.
const Succeeded = "Succeeded"

type Event struct {
	Actor      string // user id; empty when the platform acted on its own
	OrgID      string // empty outside any organization
	Action     string
	TargetType string // e.g. Organization
	TargetID   string
	Detail     map[string]any // e.g. {"before": {...}, "after": {...}}; never a token or a secret
}

func Record(ctx context.Context, q *store.Queries, e Event) error {
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	if e.Detail == nil {
		detail = []byte("{}")
	}
	return q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ActorUserID: nullable(e.Actor),
		OrgID:       nullable(e.OrgID),
		Action:      e.Action,
		TargetType:  e.TargetType,
		TargetID:    e.TargetID,
		Result:      Succeeded,
		Detail:      detail,
	})
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
