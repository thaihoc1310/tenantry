package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// loggedIn logs acct in at kc and returns the session and CSRF cookies.
func loggedIn(t *testing.T, kc *fakeKeycloak, h http.Handler, acct account) (session, csrf *http.Cookie) {
	t.Helper()
	rec := kc.login(h, "/", acct)
	if rec.Code != http.StatusFound {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	return cookie(t, rec, httpapi.SessionCookie), cookie(t, rec, httpapi.CSRFCookie)
}

// expireAccess makes the session's access token look 5 minutes old.
func expireAccess(t *testing.T, db *pgxpool.Pool, session string) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE id_hash = $1`, hashOf(t, session)); err != nil {
		t.Fatal(err)
	}
}

func getMe(t *testing.T, h http.Handler, session string) (int, oapi.Me) {
	t.Helper()
	rec := send(h, bareRequest(http.MethodGet, "/api/v1/me", session, ""))
	var me oapi.Me
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, me
}

type sessionRow struct {
	refreshEnc               []byte
	accessIn, expiresIn, age time.Duration
	exists                   bool
}

func readSession(t *testing.T, db *pgxpool.Pool, session string) sessionRow {
	t.Helper()
	var r sessionRow
	err := db.QueryRow(context.Background(), `
SELECT refresh_token_enc, access_expires_at - now(), expires_at - now(), now() - created_at
FROM sessions WHERE id_hash = $1`, hashOf(t, session)).Scan(&r.refreshEnc, &r.accessIn, &r.expiresIn, &r.age)
	r.exists = err == nil
	return r
}

// Roles come from the login until the access token expires; then the
// session is refreshed once and carries Keycloak's roles as of now.
func TestRefresh(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	sc, _ := loggedIn(t, kc, h, newcomer)
	session := sc.Value
	before := readSession(t, db, session)

	kc.setRoles("kc-1", "offline_access") // platform-admin removed at Keycloak
	if code, me := getMe(t, h, session); code != 200 || !me.IsPlatformAdmin {
		t.Fatalf("access token still valid: %d admin=%v, want the login's roles", code, me.IsPlatformAdmin)
	}
	if n, _, _ := kc.stats(); n != 0 {
		t.Fatalf("%d refreshes before the access token expired", n)
	}

	expireAccess(t, db, session)
	if code, me := getMe(t, h, session); code != 200 || me.IsPlatformAdmin {
		t.Fatalf("after expiry: %d admin=%v, want Keycloak's current roles", code, me.IsPlatformAdmin)
	}
	after := readSession(t, db, session)
	if after.accessIn < 4*time.Minute || after.accessIn > 5*time.Minute {
		t.Errorf("new access token good for %s, want ~5m", after.accessIn)
	}
	if after.expiresIn < 7*time.Hour || after.expiresIn > 8*time.Hour {
		t.Errorf("session now ends in %s, want refresh_expires_in (8h) from now", after.expiresIn)
	}
	if string(after.refreshEnc) == string(before.refreshEnc) {
		t.Error("the new refresh token was not stored")
	}
	var roles []string
	if err := db.QueryRow(context.Background(), `SELECT realm_roles FROM sessions WHERE id_hash = $1`, hashOf(t, session)).Scan(&roles); err != nil ||
		len(roles) != 1 || roles[0] != "offline_access" {
		t.Errorf("stored roles %v (%v)", roles, err)
	}

	// Renewed: the next request does not refresh again.
	if code, _ := getMe(t, h, session); code != 200 {
		t.Fatalf("status %d", code)
	}
	if n, _, _ := kc.stats(); n != 1 {
		t.Fatalf("%d refreshes, want 1", n)
	}

	// The next refresh uses the refresh token the first one returned.
	_, lastIssued, _ := kc.stats()
	kc.mu.Lock()
	for rt := range kc.refreshTokens {
		if rt != lastIssued {
			delete(kc.refreshTokens, rt) // only the newest token works now
		}
	}
	kc.mu.Unlock()
	expireAccess(t, db, session)
	if code, _ := getMe(t, h, session); code != 200 {
		t.Fatalf("second refresh: %d", code)
	}
}

// Concurrent requests of one session with an expired access token call
// Keycloak once: the others wait for the row lock, then see it renewed.
func TestRefreshOnce(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, _ := loggedIn(t, kc, h, newcomer)
	expireAccess(t, db, session.Value)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if rec := send(h, bareRequest(http.MethodGet, "/api/v1/me", session.Value, "")); rec.Code != 200 {
				t.Errorf("status %d: %s", rec.Code, rec.Body)
			}
		})
	}
	wg.Wait()
	if n, _, _ := kc.stats(); n != 1 {
		t.Fatalf("%d refreshes for one session, want 1", n)
	}
}

// A slow Keycloak holds no database connection: while one session's refresh
// waits on it, with more of the session's requests queued behind, the rest
// of the API answers. (Holding the session row's lock meanwhile, as before,
// took a pooled connection per waiting request and stalled every caller.)
func TestRefreshHoldsNoConnection(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, _ := loggedIn(t, kc, h, newcomer)
	other, _ := loggedIn(t, kc, h, account{sub: "kc-2", email: "omar@example.com", name: "Omar", emailVerified: true})
	expireAccess(t, db, session.Value)
	hold := make(chan struct{})
	kc.mu.Lock()
	kc.holdRefresh = hold
	kc.mu.Unlock()

	var wg sync.WaitGroup
	for range 3 * int(db.Config().MaxConns) {
		wg.Go(func() {
			if rec := send(h, bareRequest(http.MethodGet, "/api/v1/me", session.Value, "")); rec.Code != 200 {
				t.Errorf("waiting request: %d %s", rec.Code, rec.Body)
			}
		})
	}
	time.Sleep(300 * time.Millisecond) // the refresh is held at Keycloak, the others behind it
	done := make(chan int, 1)
	go func() { code, _ := getMe(t, h, other.Value); done <- code }()
	select {
	case code := <-done:
		if code != 200 {
			t.Errorf("another session: %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Error("another session waited for a connection while Keycloak was slow")
	}
	close(hold)
	wg.Wait()
	if n, _, _ := kc.stats(); n != 1 {
		t.Fatalf("%d refreshes for one session, want 1", n)
	}
}

// When Keycloak refuses the refresh token, the SSO session is over and so
// is the portal session.
func TestRefreshRefused(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, _ := loggedIn(t, kc, h, newcomer)
	kc.endSSO("kc-1")
	expireAccess(t, db, session.Value)

	rec := send(h, bareRequest(http.MethodGet, "/api/v1/me", session.Value, ""))
	if rec.Code != 401 || errorBody(t, rec).Code != oapi.UNAUTHENTICATED {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if readSession(t, db, session.Value).exists {
		t.Fatal("the session row outlived its SSO session")
	}
}

// A refresh token sealed with another session key (the key was rotated) ends
// the session: everyone logs in again, as the key's documentation says.
func TestRefreshWithRotatedKey(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, _ := loggedIn(t, kc, h, newcomer)
	if _, err := db.Exec(context.Background(), `UPDATE sessions SET refresh_token_enc = '\x00' WHERE id_hash = $1`, hashOf(t, session.Value)); err != nil {
		t.Fatal(err)
	}
	expireAccess(t, db, session.Value)
	if code, _ := getMe(t, h, session.Value); code != 401 {
		t.Fatalf("status %d", code)
	}
	if readSession(t, db, session.Value).exists {
		t.Fatal("session kept")
	}
}

// Keycloak being unreachable is not the end of the session: the request
// fails, the stale roles are not used, and a later request refreshes.
func TestRefreshWhileKeycloakDown(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, _ := loggedIn(t, kc, h, newcomer)
	expireAccess(t, db, session.Value)

	kc.mu.Lock()
	kc.down = true
	kc.mu.Unlock()
	if code, _ := getMe(t, h, session.Value); code != 500 {
		t.Fatalf("Keycloak down: status %d", code)
	}
	if r := readSession(t, db, session.Value); !r.exists || r.accessIn > 0 {
		t.Fatalf("session after a failed refresh: %+v", r)
	}

	kc.mu.Lock()
	kc.down = false
	kc.mu.Unlock()
	if code, _ := getMe(t, h, session.Value); code != 200 {
		t.Fatalf("Keycloak back: status %d", code)
	}
}

// Refreshing never stretches a session past 24 hours after its login, even
// when Keycloak's answer would.
func TestRefreshKeepsSessionMax(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, _ := loggedIn(t, kc, h, newcomer)
	if _, err := db.Exec(context.Background(),
		`UPDATE sessions SET created_at = now() - interval '23 hours' WHERE id_hash = $1`, hashOf(t, session.Value)); err != nil {
		t.Fatal(err)
	}
	expireAccess(t, db, session.Value)
	if code, _ := getMe(t, h, session.Value); code != 200 {
		t.Fatalf("status %d", code)
	}
	if r := readSession(t, db, session.Value); r.expiresIn > time.Hour || r.expiresIn < 59*time.Minute {
		t.Fatalf("session ends in %s, want what is left of 24h after login (1h)", r.expiresIn)
	}
}

func logoutRequest(session, csrf *http.Cookie, header string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	for _, c := range []*http.Cookie{session, csrf} {
		if c != nil {
			req.AddCookie(c)
		}
	}
	if header != "" {
		req.Header.Set(httpapi.CSRFHeader, header)
	}
	return req
}

// Logout deletes the session and its cookies and ends the SSO session at
// Keycloak with the session's current refresh token.
func TestLogout(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, csrf := loggedIn(t, kc, h, newcomer)
	expireAccess(t, db, session.Value)
	if code, _ := getMe(t, h, session.Value); code != 200 { // refreshed: a new refresh token
		t.Fatalf("status %d", code)
	}
	_, current, _ := kc.stats()

	rec := send(h, logoutRequest(session, csrf, csrf.Value))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	for _, name := range []string{httpapi.SessionCookie, httpapi.CSRFCookie} {
		if c := cookie(t, rec, name); c.MaxAge >= 0 || c.Value != "" {
			t.Errorf("%s not deleted: %+v", name, c)
		}
	}
	if readSession(t, db, session.Value).exists {
		t.Fatal("session row kept")
	}
	if _, _, out := kc.stats(); len(out) != 1 || out[0] != current {
		t.Fatalf("Keycloak logout called with %v, want the current refresh token", out)
	}
	kc.mu.Lock()
	ended := kc.refreshTokens[current].ended
	kc.mu.Unlock()
	if !ended {
		t.Fatal("the SSO session is still alive")
	}
	if code, _ := getMe(t, h, session.Value); code != 401 {
		t.Fatalf("old cookie after logout: %d", code)
	}
}

func TestLogoutNeedsCSRF(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	session, csrf := loggedIn(t, kc, h, newcomer)

	for name, req := range map[string]*http.Request{
		"no header":      logoutRequest(session, csrf, ""),
		"wrong header":   logoutRequest(session, csrf, "x"),
		"no CSRF cookie": logoutRequest(session, nil, csrf.Value),
	} {
		rec := send(h, req)
		if rec.Code != 403 || errorBody(t, rec).Code != oapi.FORBIDDEN {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if !readSession(t, db, session.Value).exists {
		t.Fatal("a forged logout ended the session")
	}
	if _, _, out := kc.stats(); len(out) != 0 {
		t.Fatal("a forged logout reached Keycloak")
	}
}

// Logout always ends with the cookies gone, whatever is left to end.
func TestLogoutWithNothingToEnd(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	csrf := &http.Cookie{Name: httpapi.CSRFCookie, Value: "c"}

	// The SSO session ended at Keycloak already (idle timeout).
	session, _ := loggedIn(t, kc, h, newcomer)
	kc.endSSO("kc-1")
	// Keycloak is down: the portal session still ends.
	other, _ := loggedIn(t, kc, h, newcomer)

	cases := []struct {
		name string
		req  *http.Request
		down bool
	}{
		{"no session cookie", logoutRequest(nil, nil, ""), false},
		{"unknown session", logoutRequest(&http.Cookie{Name: httpapi.SessionCookie, Value: sessionOf("usr-ghost")}, csrf, "c"), false},
		{"SSO session already over", logoutRequest(session, csrf, "c"), false},
		{"Keycloak down", logoutRequest(other, csrf, "c"), true},
	}
	for _, c := range cases {
		kc.mu.Lock()
		kc.down = c.down
		kc.mu.Unlock()
		rec := send(h, c.req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
			continue
		}
		if ck := cookie(t, rec, httpapi.SessionCookie); ck.MaxAge >= 0 {
			t.Errorf("%s: session cookie kept", c.name)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM sessions JOIN users ON users.id = user_id WHERE subject = 'kc-1'`); n != 0 {
		t.Fatalf("%d sessions left", n)
	}
	if rec := send(h, httptest.NewRequest(http.MethodGet, "/auth/logout", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /auth/logout: %d", rec.Code)
	}
}
