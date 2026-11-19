package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

func TestGetMe(t *testing.T) {
	h, db := newAPI(t, httpapi.Options{DevAuth: true})
	if _, err := db.Exec(context.Background(),
		`UPDATE users SET email = 'alice@example.com', display_name = 'Alice' WHERE id = 'usr-alice'`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, user, roles string
		status            int
		wantAdmin         bool
	}{
		{"plain user", "usr-alice", "", 200, false},
		{"platform admin", "usr-alice", "platform-admin", 200, true},
		{"one role among several", "usr-alice", "offline_access, platform-admin", 200, true},
		{"role names match exactly", "usr-alice", "platform-admins,Platform-Admin", 200, false},
		{"roles without a user", "", "platform-admin", 401, false},
		{"disabled user", "usr-carol", "platform-admin", 401, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			if c.user != "" {
				req.Header.Set(httpapi.DevUserHeader, c.user)
			}
			if c.roles != "" {
				req.Header.Set(httpapi.DevRolesHeader, c.roles)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			if c.status != 200 {
				if e := errorBody(t, rec); e.Code != oapi.UNAUTHENTICATED {
					t.Fatalf("code %s", e.Code)
				}
				return
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
