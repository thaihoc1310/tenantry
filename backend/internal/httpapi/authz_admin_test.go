package httpapi_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The Platform Admin side of the authorization matrix (PRODUCT-PLAN §16.5,
// IMPLEMENTATION-PLAN M5): every operation under /admin/ in the OpenAPI
// document, so one added later is covered too. Anonymous is 401; an
// OrgOwner, an OrgAdmin and an OrgMember are 403, before any validation (a
// body that makes no sense still gets 403); a Platform Admin gets past
// authorization (neither 401 nor 403).
func TestAdminAuthorizationMatrix(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../openapi/tenantry.v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	h, db, admin := newReviewAPI(t)
	// alice owns acme, erin is its OrgAdmin, bob an OrgMember (the seed).
	callers := []struct {
		name, token string
	}{
		{"OrgOwner", sessionOf("usr-alice")},
		{"OrgAdmin", sessionOf("usr-erin")},
		{"OrgMember", sessionOf("usr-bob")},
	}
	params := strings.NewReplacer("{orgId}", "org-acme", "{clusterId}", "cluster-a", "{poolId}", "shared-general", "{userId}", "usr-carol") // not a caller: the Admin disables and enables them

	type op struct{ method, path string }
	var ops []op
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/admin/") {
			continue
		}
		for method := range item.Operations() {
			ops = append(ops, op{method, "/api/v1" + params.Replace(path)})
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].path+ops[i].method < ops[j].path+ops[j].method })
	if len(ops) < 25 {
		t.Fatalf("only %d admin operations in the document", len(ops))
	}
	for _, o := range ops {
		body := ""
		if o.method != http.MethodGet && o.method != http.MethodDelete {
			body = `{"nonsense":1}`
		}
		// With the Idempotency-Key a client sends: the generated router checks a
		// required header before any middleware, so without it the answer is 400.
		anon := bareRequest(o.method, o.path, "", body)
		anon.Header.Set("Idempotency-Key", newKey())
		if rec := send(h, anon); rec.Code != 401 {
			t.Errorf("anonymous %s %s: %d", o.method, o.path, rec.Code)
		}
		for _, c := range callers {
			if rec := asAdmin(h, c.token, o.method, o.path, newKey(), body); rec.Code != 403 {
				t.Errorf("%s %s %s: %d %s", c.name, o.method, o.path, rec.Code, rec.Body)
			}
		}
		// Read-only calls as the Admin; a change gets past authorization to validation.
		if rec := asAdmin(h, admin, o.method, o.path, newKey(), body); rec.Code == 401 || rec.Code == 403 {
			t.Errorf("Platform Admin %s %s: %d %s", o.method, o.path, rec.Code, rec.Body)
		}
	}
	// No refused call changed anything.
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE actor_user_id <> 'usr-dave'`); n != 0 {
		t.Fatalf("%d audit events by someone without the role", n)
	}
}
