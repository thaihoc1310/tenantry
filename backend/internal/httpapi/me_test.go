package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

func TestGetMe(t *testing.T) {
	h, db := newAPI(t)
	if _, err := db.Exec(context.Background(),
		`UPDATE users SET email = 'alice@example.com', display_name = 'Alice' WHERE id = 'usr-alice'`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		roles     []string
		wantAdmin bool
	}{
		{"plain user", nil, false},
		{"platform admin", []string{"platform-admin"}, true},
		{"one role among several", []string{"offline_access", "platform-admin"}, true},
		{"role names match exactly", []string{"platform-admins", "Platform-Admin"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := send(h, request(http.MethodGet, "/api/v1/me", newSession(t, db, "usr-alice", c.roles...), ""))
			if rec.Code != 200 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			var me oapi.Me
			if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
				t.Fatal(err)
			}
			if me.Id != "usr-alice" || me.DisplayName != "Alice" || me.Email == nil || *me.Email != "alice@example.com" ||
				me.IsPlatformAdmin != c.wantAdmin {
				t.Fatalf("got %+v (email %v), want admin=%v", me, me.Email, c.wantAdmin)
			}
		})
	}

	// No email from the identity provider: the field is left out, not "".
	rec := call(h, http.MethodGet, "/api/v1/me", "usr-dave")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "email") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// Every way of not having a usable session ends in the same 401.
func TestSessionRequired(t *testing.T) {
	h, db := newAPI(t)
	expired := newSession(t, db, "usr-alice")
	if _, err := db.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE id_hash = $1`, hashOf(t, expired)); err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{
		"no cookie":               "",
		"not base64":              "!!!",
		"wrong length":            "AAAA",
		"unknown session":         sessionOf("usr-ghost"),
		"expired session":         expired,
		"disabled user's session": sessionOf("usr-carol"),
	} {
		t.Run(name, func(t *testing.T) {
			rec := send(h, request(http.MethodGet, "/api/v1/me", token, ""))
			if rec.Code != 401 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if e := errorBody(t, rec); e.Code != oapi.UNAUTHENTICATED {
				t.Fatalf("code %s", e.Code)
			}
		})
	}

	// A disabled user's session is deleted, not only refused.
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id = 'usr-carol'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("carol still has %d sessions (%v)", n, err)
	}
}

// A request that can change something must echo the CSRF cookie in the
// header; reads need nothing.
func TestCSRF(t *testing.T) {
	h, _ := newAPI(t)
	const body = `{"slug":"dave","displayName":"Dave"}`
	post := func(cookies map[string]string, header string) int {
		t.Helper()
		req := bareRequest(http.MethodPost, "/api/v1/organizations", sessionOf("usr-dave"), body)
		for name, v := range cookies {
			req.AddCookie(&http.Cookie{Name: name, Value: v})
		}
		if header != "" {
			req.Header.Set(httpapi.CSRFHeader, header)
		}
		rec := send(h, req)
		if rec.Code == 403 {
			if e := errorBody(t, rec); e.Code != oapi.FORBIDDEN {
				t.Fatalf("code %s", e.Code)
			}
		}
		return rec.Code
	}

	for name, c := range map[string]struct {
		cookies map[string]string
		header  string
	}{
		"neither":          {nil, ""},
		"header only":      {nil, "x"},
		"cookie only":      {map[string]string{httpapi.CSRFCookie: "x"}, ""},
		"different values": {map[string]string{httpapi.CSRFCookie: "x"}, "y"},
		// What another tenantry.site subdomain could plant: no __Host- prefix.
		"unprefixed cookie": {map[string]string{"tenantry_csrf": "x"}, "x"},
	} {
		if got := post(c.cookies, c.header); got != 403 {
			t.Errorf("%s: status %d, want 403", name, got)
		}
	}
	if got := post(map[string]string{httpapi.CSRFCookie: "x"}, "x"); got != 201 {
		t.Fatalf("matching values: status %d, want 201", got)
	}
	if rec := send(h, bareRequest(http.MethodGet, "/api/v1/organizations", sessionOf("usr-dave"), "")); rec.Code != 200 {
		t.Fatalf("read without CSRF: status %d", rec.Code)
	}
	// Anonymous writes are told to log in, not that CSRF is missing.
	if rec := send(h, bareRequest(http.MethodPost, "/api/v1/organizations", "", body)); rec.Code != 401 {
		t.Fatalf("anonymous: status %d, want 401", rec.Code)
	}
}
