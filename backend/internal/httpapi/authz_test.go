package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The authorization matrix of PRODUCT-PLAN §16.1–16.2 (IMPLEMENTATION-PLAN
// M4.9): every endpoint of organizations, members, invitations, groups and
// ownership transfers, called by each kind of caller. One row per endpoint;
// the expected statuses are, in order:
//
//	anonymous, outsider, Platform Admin (not a member), OrgMember, OrgAdmin, OrgOwner
//
// The order of the checks is part of the contract: not a member is 404 (an
// outsider cannot tell the organization exists), then a missing role is 403.
// A Platform Admin is an outsider here: their rights are in /admin (§16.5).

type authzFixture struct {
	h                                     http.Handler
	org, group, invitation, token, transf string
	platformAdmin, owner                  string
}

// newAuthzFixture is the staffed organization zeta (owner zeta, OrgAdmin erin,
// OrgMember bob in developers; dave outside), with an invitation for
// dave@example.com and an ownership transfer from zeta to erin waiting.
func newAuthzFixture(t *testing.T) authzFixture {
	t.Helper()
	h, db, orgID, owner := staffedOrg(t)
	_, token := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)
	if token == "" {
		t.Fatal("no invitation")
	}
	f := authzFixture{h: h, org: orgID, group: groupID(t, db, orgID, "developers"), token: token, transf: "transfer-authz", owner: owner}
	ctx := context.Background()
	if err := db.QueryRow(ctx, `SELECT id FROM invitations WHERE org_id = $1`, orgID).Scan(&f.invitation); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO ownership_transfers (id, org_id, from_user_id, to_user_id, expires_at, state)
		VALUES ($1, $2, 'usr-zeta', 'usr-erin', $3, 'Proposed')`, f.transf, orgID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.platformAdmin = newSession(t, db, "usr-dave", "platform-admin")
	return f
}

func (f authzFixture) path(p string) string {
	return strings.NewReplacer("{org}", "zeta", "{group}", f.group, "{invitation}", f.invitation,
		"{token}", f.token, "{transfer}", f.transf).Replace(p)
}

// tokens of the callers, in the matrix's column order.
func (f authzFixture) callers() []string {
	return []string{"", sessionOf("usr-dave"), f.platformAdmin, sessionOf("usr-bob"), sessionOf("usr-erin"), f.owner}
}

var callerNames = []string{"anonymous", "outsider", "Platform Admin", "OrgMember", "OrgAdmin", "OrgOwner"}

func TestAuthorizationMatrix(t *testing.T) {
	rows := []struct {
		method, path, body string
		want               [6]int
	}{
		// 16.1 Organization
		{"GET", "/api/v1/organizations/{org}", "", [6]int{401, 404, 404, 200, 200, 200}},
		{"PATCH", "/api/v1/organizations/{org}", `{"description":"x"}`, [6]int{401, 404, 404, 403, 200, 200}},
		{"GET", "/api/v1/organizations/{org}/entitlement", "", [6]int{401, 404, 404, 403, 200, 200}},
		{"GET", "/api/v1/organizations/{org}/usage", "", [6]int{401, 404, 404, 403, 200, 200}},
		{"POST", "/api/v1/organizations/{org}:transfer-ownership", `{"toUserId":"usr-erin"}`, [6]int{401, 404, 404, 403, 403, 201}},
		// The waiting transfer shows to its owner and its recipient only.
		{"GET", "/api/v1/organizations/{org}/ownership-transfer", "", [6]int{401, 404, 404, 404, 200, 200}},
		{"POST", "/api/v1/ownership-transfers/{transfer}:accept", "", [6]int{401, 404, 404, 403, 200, 403}},
		{"DELETE", "/api/v1/ownership-transfers/{transfer}", "", [6]int{401, 404, 404, 403, 204, 204}},
		{"POST", "/api/v1/organizations/{org}/members/me:leave", "", [6]int{401, 404, 404, 204, 204, 409}},

		// 16.2 Members and invitations
		{"GET", "/api/v1/organizations/{org}/members", "", [6]int{401, 404, 404, 403, 200, 200}},
		{"PATCH", "/api/v1/organizations/{org}/members/usr-bob", `{"role":"OrgAdmin"}`, [6]int{401, 404, 404, 403, 200, 200}},
		{"PATCH", "/api/v1/organizations/{org}/members/usr-zeta", `{"role":"OrgMember"}`, [6]int{401, 404, 404, 403, 403, 403}},
		{"DELETE", "/api/v1/organizations/{org}/members/usr-bob", "", [6]int{401, 404, 404, 403, 204, 204}},
		{"DELETE", "/api/v1/organizations/{org}/members/usr-zeta", "", [6]int{401, 404, 404, 403, 403, 403}},
		{"GET", "/api/v1/organizations/{org}/invitations", "", [6]int{401, 404, 404, 403, 200, 200}},
		{"POST", "/api/v1/organizations/{org}/invitations", `{"email":"new@example.com","role":"OrgMember"}`, [6]int{401, 404, 404, 403, 201, 201}},
		{"DELETE", "/api/v1/invitations/{invitation}", "", [6]int{401, 404, 404, 403, 204, 204}},
		// Anyone logged in may look at an invitation they hold the link of;
		// only its invitee (dave@example.com) may accept it.
		{"GET", "/api/v1/invitations/{token}", "", [6]int{401, 200, 200, 200, 200, 200}},
		{"POST", "/api/v1/invitations/{token}:accept", "", [6]int{401, 200, 200, 403, 403, 403}},

		// 16.2 Groups
		{"GET", "/api/v1/organizations/{org}/groups", "", [6]int{401, 404, 404, 403, 200, 200}},
		{"POST", "/api/v1/organizations/{org}/groups", `{"name":"qa"}`, [6]int{401, 404, 404, 403, 201, 201}},
		{"PATCH", "/api/v1/groups/{group}", `{"name":"devs"}`, [6]int{401, 404, 404, 403, 200, 200}},
		{"DELETE", "/api/v1/groups/{group}", "", [6]int{401, 404, 404, 403, 204, 204}},
		{"PUT", "/api/v1/groups/{group}/members/usr-erin", "", [6]int{401, 404, 404, 403, 204, 204}},
		{"DELETE", "/api/v1/groups/{group}/members/usr-bob", "", [6]int{401, 404, 404, 403, 204, 204}},
	}
	// A few rows at a time: each opens several test databases.
	slots := make(chan struct{}, 4)
	for _, r := range rows {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			// Refusals change nothing, so they share one organization; each
			// caller that succeeds gets a fresh one, since success changes it.
			shared := newAuthzFixture(t)
			check := func(t *testing.T, f authzFixture, i, want int) {
				rec := asAdmin(f.h, f.callers()[i], r.method, f.path(r.path), newKey(), r.body)
				if rec.Code != want {
					t.Errorf("%s: %d, want %d: %s", callerNames[i], rec.Code, want, rec.Body)
				}
			}
			for i, want := range r.want {
				if want >= 300 {
					check(t, shared, i, want)
					continue
				}
				// Its own subtest, so its database goes as soon as it is checked.
				t.Run(callerNames[i], func(t *testing.T) { check(t, newAuthzFixture(t), i, want) })
			}
		})
	}
}

// Members, invitations and groups of an organization that is not approved
// yet answer 409 ORG_NOT_ACTIVE to its own owner, after the role check.
func TestAuthorizationOfAnOrganizationNotActive(t *testing.T) {
	h, db, _ := newReviewAPI(t)
	orgID := pendingRequest(t, db, "pending")
	var owner string
	if err := db.QueryRow(context.Background(), `SELECT owner_user_id FROM organizations WHERE id = $1`, orgID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	token := newSession(t, db, owner)
	for _, r := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/v1/organizations/%s", "", 200},
		{"PATCH", "/api/v1/organizations/%s", `{"description":"x"}`, 200},
		{"GET", "/api/v1/organizations/%s/members", "", 409},
		{"GET", "/api/v1/organizations/%s/invitations", "", 409},
		{"POST", "/api/v1/organizations/%s/invitations", `{"email":"new@example.com","role":"OrgMember"}`, 409},
		{"GET", "/api/v1/organizations/%s/groups", "", 409},
		{"POST", "/api/v1/organizations/%s/groups", `{"name":"qa"}`, 409},
		{"POST", "/api/v1/organizations/%s:transfer-ownership", `{"toUserId":"usr-erin"}`, 409},
		{"POST", "/api/v1/organizations/%s/members/me:leave", "", 409},
	} {
		rec := asAdmin(h, token, r.method, fmt.Sprintf(r.path, orgID), newKey(), r.body)
		if rec.Code != r.want {
			t.Errorf("%s %s: %d, want %d: %s", r.method, r.path, rec.Code, r.want, rec.Body)
			continue
		}
		if r.want == 409 && errorBody(t, rec).Code != "ORG_NOT_ACTIVE" && !strings.Contains(r.path, "leave") {
			t.Errorf("%s %s: %s, want ORG_NOT_ACTIVE", r.method, r.path, rec.Body)
		}
	}
}
