package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type auditRow struct {
	Actor, Org, Action, TargetType, TargetID, Result string
	Detail                                           map[string]any
}

// auditTrail returns the events of action, oldest first.
func auditTrail(t *testing.T, db *pgxpool.Pool, action string) []auditRow {
	t.Helper()
	rows, err := db.Query(context.Background(), `
SELECT coalesce(actor_user_id, ''), coalesce(org_id, ''), action, target_type, target_id, result, detail
FROM audit_events WHERE action = $1 ORDER BY id`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		var detail []byte
		if err := rows.Scan(&r.Actor, &r.Org, &r.Action, &r.TargetType, &r.TargetID, &r.Result, &detail); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(detail, &r.Detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAuditOrganizationChanges(t *testing.T) {
	h, db := newAPI(t)

	rec := callBody(h, http.MethodPost, "/api/v1/organizations", "usr-dave", `{"slug":"dave","displayName":"Dave","description":"mine"}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Id string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	got := auditTrail(t, db, "organization.create")
	want := []auditRow{{"usr-dave", created.Id, "organization.create", "Organization", created.Id, "Succeeded",
		map[string]any{"slug": "dave", "displayName": "Dave", "description": "mine", "status": "Pending"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create audited as %+v", got)
	}

	// Only the fields that change are recorded, with their old and new values.
	patches := []struct{ user, body string }{
		{"usr-erin", `{"displayName":"ACME Corp","description":""}`}, // description unchanged
		{"usr-alice", `{"displayName":"ACME Corp"}`},                 // nothing changes: no event
		{"usr-bob", `{"displayName":"Bob's"}`},                       // refused: no event
		{"usr-alice", `{"description":"Rockets"}`},
	}
	for _, p := range patches {
		callBody(h, http.MethodPatch, "/api/v1/organizations/acme", p.user, p.body)
	}
	got = auditTrail(t, db, "organization.update")
	want = []auditRow{
		{"usr-erin", "org-acme", "organization.update", "Organization", "org-acme", "Succeeded", map[string]any{
			"before": map[string]any{"displayName": "ACME"}, "after": map[string]any{"displayName": "ACME Corp"}}},
		{"usr-alice", "org-acme", "organization.update", "Organization", "org-acme", "Succeeded", map[string]any{
			"before": map[string]any{"description": ""}, "after": map[string]any{"description": "Rockets"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates audited as %+v", got)
	}
}

func TestAuditRetry(t *testing.T) {
	h, db := newAPI(t)
	if _, err := db.Exec(context.Background(), opSeed); err != nil {
		t.Fatal(err)
	}
	// A Platform Admin outside the organization.
	admin := newSession(t, db, "usr-dave", "platform-admin")
	if rec := send(h, request(http.MethodPost, "/api/v1/operations/op-failed:retry", admin, "")); rec.Code != 200 {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body)
	}
	call(h, http.MethodPost, "/api/v1/operations/op-failed:retry", "usr-alice") // 409: no event
	got := auditTrail(t, db, "operation.retry")
	want := []auditRow{{"usr-dave", "org-acme", "operation.retry", "Operation", "op-failed", "Succeeded",
		map[string]any{"type": "workspace.create", "asPlatformAdmin": true}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retry audited as %+v", got)
	}
}

func TestAuditLoginLogout(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, csrf := loggedIn(t, kc, h, newcomer)
	unverified := newcomer
	unverified.sub, unverified.emailVerified = "kc-2", false
	kc.login(h, "/", unverified) // refused: no event

	var userID string
	if err := db.QueryRow(context.Background(), `SELECT id FROM users WHERE subject = 'kc-1'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if got := auditTrail(t, db, "user.login"); len(got) != 1 || got[0].Actor != userID || got[0].TargetID != userID || got[0].Org != "" {
		t.Fatalf("logins audited as %+v", got)
	}
	if rec := send(h, logoutRequest(session, csrf, csrf.Value)); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	send(h, logoutRequest(session, csrf, csrf.Value)) // nothing left to end: no event
	if got := auditTrail(t, db, "user.logout"); len(got) != 1 || got[0].Actor != userID {
		t.Fatalf("logouts audited as %+v", got)
	}
}
