package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// account is who logs in at the fake Keycloak.
type account struct {
	sub, email, name string
	emailVerified    bool
	roles            []string
}

// fakeKeycloak is an OIDC issuer in the test process: discovery and keys
// from go-oidc's oidctest, plus the endpoints Keycloak has for a
// confidential client: redeem a code (client secret, PKCE, single use),
// refresh, and log out.
type fakeKeycloak struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu       sync.Mutex
	down     bool               // every endpoint answers 503
	codes    map[string]grant   // issued, not yet redeemed
	accounts map[string]account // by sub, as of now: tests change roles here
	// refreshTokens maps every refresh token issued to its SSO session. A
	// refresh issues a new one and the old one keeps working, as with the
	// realm's Revoke Refresh Token off.
	refreshTokens map[string]*ssoSession
	refreshes     int      // successful refresh grants
	issued        []string // refresh tokens, in order
	loggedOut     []string // refresh tokens the logout endpoint was called with
	// Tamper with the next tokens.
	wrongNonce, wrongAudience, noRefreshToken bool
	// holdRefresh, when set, keeps every refresh waiting until it is closed.
	holdRefresh chan struct{}
}

type grant struct {
	acct             account
	nonce, challenge string
}

type ssoSession struct {
	sub   string
	ended bool
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeKeycloak{t: t, key: key, codes: map[string]grant{}, accounts: map[string]account{}, refreshTokens: map[string]*ssoSession{}}
	keys := &oidctest.Server{PublicKeys: []oidctest.PublicKey{{PublicKey: key.Public(), KeyID: "k1", Algorithm: "RS256"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", f.token)
	mux.HandleFunc("POST /logout", f.logout)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// oidctest's discovery plus the logout endpoint, which it leaves out.
		rec := httptest.NewRecorder()
		keys.ServeHTTP(rec, r)
		var disc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &disc); err != nil {
			t.Error(err)
		}
		disc["end_session_endpoint"] = f.srv.URL + "/logout"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(disc)
	})
	mux.Handle("/", keys)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		down := f.down
		f.mu.Unlock()
		if down {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	keys.SetIssuer(f.srv.URL)
	return f
}

// authorize plays the Keycloak login page: it accepts the authorization
// request in authURL for acct and returns the code it would redirect back with.
func (f *fakeKeycloak) authorize(authURL string, acct account) (code, state string) {
	f.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		f.t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": "portal", "response_type": "code", "redirect_uri": publicURL + "/auth/callback",
		"code_challenge_method": "S256", "scope": "openid email profile",
	}
	for k, v := range want {
		if q.Get(k) != v {
			f.t.Fatalf("authorization request %s=%q, want %q (%s)", k, q.Get(k), v, authURL)
		}
	}
	if !strings.HasPrefix(authURL, f.srv.URL+"/auth?") || q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		f.t.Fatalf("authorization request %s", authURL)
	}
	code = rand.Text()
	f.mu.Lock()
	f.codes[code] = grant{acct: acct, nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	f.accounts[acct.sub] = acct
	f.mu.Unlock()
	return code, q.Get("state")
}

// oauthError answers like Keycloak does when it refuses a request.
func oauthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func clientAuthenticated(r *http.Request) bool {
	id, secret, ok := r.BasicAuth()
	return ok && id == "portal" && secret == "portal-secret"
}

func (f *fakeKeycloak) token(w http.ResponseWriter, r *http.Request) {
	refuse := func(code string) { oauthError(w, http.StatusBadRequest, code) }
	if !clientAuthenticated(r) {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if r.FormValue("grant_type") == "refresh_token" {
		f.refresh(w, r)
		return
	}
	f.mu.Lock()
	g, ok := f.codes[r.FormValue("code")]
	delete(f.codes, r.FormValue("code"))
	wrongNonce, wrongAudience, noRefresh := f.wrongNonce, f.wrongAudience, f.noRefreshToken
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
	if !ok || r.FormValue("grant_type") != "authorization_code" || r.FormValue("redirect_uri") != publicURL+"/auth/callback" ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		refuse("invalid_grant")
		return
	}

	now := time.Now()
	nonce, aud := g.nonce, "portal"
	if wrongNonce {
		nonce = "other"
	}
	if wrongAudience {
		aud = "someone-else"
	}
	idToken := map[string]any{
		"iss": f.srv.URL, "aud": aud, "sub": g.acct.sub, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": nonce, "email": g.acct.email, "email_verified": g.acct.emailVerified, "name": g.acct.name,
		"preferred_username": "user-" + g.acct.sub,
	}
	resp := f.tokens(g.acct, &ssoSession{sub: g.acct.sub})
	resp["id_token"] = f.sign(idToken)
	if noRefresh {
		delete(resp, "refresh_token")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// refresh is the refresh_token grant: new tokens with the account's roles
// as of now, as long as the SSO session lives.
func (f *fakeKeycloak) refresh(w http.ResponseWriter, r *http.Request) {
	// Slow enough that concurrent requests of one session would all get
	// here if the API did not serialize them.
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	hold := f.holdRefresh
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sso, ok := f.refreshTokens[r.FormValue("refresh_token")]
	if !ok || sso.ended {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	f.refreshes++
	resp := f.tokensLocked(f.accounts[sso.sub], sso)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// logout is Keycloak's logout endpoint called by a confidential client with
// a refresh token: it ends the SSO session behind it.
func (f *fakeKeycloak) logout(w http.ResponseWriter, r *http.Request) {
	if !clientAuthenticated(r) {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rt := r.FormValue("refresh_token")
	f.loggedOut = append(f.loggedOut, rt)
	sso, ok := f.refreshTokens[rt]
	if !ok || sso.ended {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	sso.ended = true
	w.WriteHeader(http.StatusNoContent)
}

// endSSO ends sub's SSO sessions the way an idle timeout or an Admin would.
func (f *fakeKeycloak) endSSO(sub string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sso := range f.refreshTokens {
		if sso.sub == sub {
			sso.ended = true
		}
	}
}

func (f *fakeKeycloak) setRoles(sub string, roles ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.accounts[sub]
	a.roles = roles
	f.accounts[sub] = a
}

// stats returns the successful refreshes, the last refresh token issued and
// the refresh tokens logged out with.
func (f *fakeKeycloak) stats() (refreshes int, lastIssued string, loggedOut []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes, f.issued[len(f.issued)-1], slices.Clone(f.loggedOut)
}

func (f *fakeKeycloak) tokens(acct account, sso *ssoSession) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokensLocked(acct, sso)
}

// tokensLocked issues an access token and a new refresh token of sso.
func (f *fakeKeycloak) tokensLocked(acct account, sso *ssoSession) map[string]any {
	now := time.Now()
	accessToken := map[string]any{
		"iss": f.srv.URL, "aud": []string{"portal-api", "account"}, "sub": acct.sub, "azp": "portal",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"realm_access": map[string]any{"roles": acct.roles},
	}
	rt := "refresh-" + rand.Text()
	f.refreshTokens[rt] = sso
	f.issued = append(f.issued, rt)
	return map[string]any{
		"token_type": "Bearer", "expires_in": 300, "refresh_expires_in": 28800,
		"access_token": f.sign(accessToken), "refresh_token": rt,
	}
}

func (f *fakeKeycloak) sign(claims map[string]any) string {
	b, err := json.Marshal(claims)
	if err != nil {
		f.t.Fatal(err)
	}
	return oidctest.SignIDToken(f.key, "k1", "RS256", string(b))
}

// login runs the whole browser round trip and returns the callback's response.
func (f *fakeKeycloak) login(h http.Handler, returnTo string, acct account) *httptest.ResponseRecorder {
	f.t.Helper()
	start := send(h, httptest.NewRequest(http.MethodGet, "/auth/login?returnTo="+url.QueryEscape(returnTo), nil))
	if start.Code != http.StatusFound {
		f.t.Fatalf("login: %d %s", start.Code, start.Body)
	}
	code, state := f.authorize(start.Header().Get("Location"), acct)
	return callbackWith(h, cookie(f.t, start, "__Host-tenantry_login"), "code="+code+"&state="+url.QueryEscape(state))
}

func callbackWith(h http.Handler, login *http.Cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?"+query, nil)
	if login != nil {
		req.AddCookie(login)
	}
	return send(h, req)
}

// cookie returns the cookie rec sets under name.
func cookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", name, rec.Header().Values("Set-Cookie"))
	return nil
}

func count(t *testing.T, db *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var newcomer = account{sub: "kc-1", email: "nina@example.com", name: "Nina", emailVerified: true, roles: []string{"offline_access", "platform-admin"}}

func TestLogin(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)

	rec := kc.login(h, "/orgs/acme?tab=members", newcomer)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/orgs/acme?tab=members" {
		t.Fatalf("callback: %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	session := cookie(t, rec, httpapi.SessionCookie)
	csrf := cookie(t, rec, httpapi.CSRFCookie)
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" || session.MaxAge != 86400 {
		t.Errorf("session cookie %+v", session)
	}
	if csrf.HttpOnly || !csrf.Secure || csrf.Value == "" || csrf.MaxAge != 86400 {
		t.Errorf("CSRF cookie %+v: the SPA must be able to read it", csrf)
	}
	if c := cookie(t, rec, "__Host-tenantry_login"); c.MaxAge >= 0 {
		t.Errorf("login cookie not deleted: %+v", c)
	}

	// The account now exists, identified by issuer and subject.
	var userID, email, name string
	if err := db.QueryRow(context.Background(), `SELECT id, email, display_name FROM users WHERE issuer = $1 AND subject = 'kc-1'`, kc.srv.URL).
		Scan(&userID, &email, &name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(userID, "usr-") || email != "nina@example.com" || name != "Nina" {
		t.Fatalf("user %s %s %s", userID, email, name)
	}
	// Only the hash of the cookie is stored, and the refresh token is encrypted.
	var roles []string
	var refreshEnc []byte
	var expiresIn time.Duration
	if err := db.QueryRow(context.Background(),
		`SELECT realm_roles, refresh_token_enc, expires_at - now() FROM sessions WHERE id_hash = $1 AND user_id = $2`,
		hashOf(t, session.Value), userID).Scan(&roles, &refreshEnc, &expiresIn); err != nil {
		t.Fatal(err)
	}
	if strings.Join(roles, ",") != "offline_access,platform-admin" || strings.Contains(string(refreshEnc), "refresh-") {
		t.Fatalf("roles %v, refresh token stored as %q", roles, refreshEnc)
	}
	if expiresIn < 7*time.Hour || expiresIn > 8*time.Hour {
		t.Fatalf("session lasts %s, want Keycloak's refresh_expires_in (8h)", expiresIn)
	}

	// The cookies work: read as the new user, then write with the CSRF pair.
	req := bareRequest(http.MethodGet, "/api/v1/me", session.Value, "")
	var me oapi.Me
	if rec := send(h, req); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &me) != nil || me.Id != userID || !me.IsPlatformAdmin {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	req = bareRequest(http.MethodPost, "/api/v1/organizations", session.Value, `{"slug":"nina","displayName":"Nina's"}`)
	req.AddCookie(csrf)
	req.Header.Set(httpapi.CSRFHeader, csrf.Value)
	if rec := send(h, req); rec.Code != 201 {
		t.Fatalf("create organization: %d %s", rec.Code, rec.Body)
	}
}

// Logging in again, even twice at once, keeps one account and refreshes its
// profile; each login is its own session.
func TestLoginIsIdempotent(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)

	if rec := kc.login(h, "/", newcomer); rec.Code != http.StatusFound {
		t.Fatalf("first login: %d %s", rec.Code, rec.Body)
	}
	renamed := newcomer
	renamed.email, renamed.name = "nina@new.example", ""
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if rec := kc.login(h, "/", renamed); rec.Code != http.StatusFound {
				t.Errorf("login: %d %s", rec.Code, rec.Body)
			}
		})
	}
	wg.Wait()

	if n := count(t, db, `SELECT count(*) FROM users WHERE subject = 'kc-1'`); n != 1 {
		t.Fatalf("%d users for one identity", n)
	}
	var email, name string
	if err := db.QueryRow(context.Background(), `SELECT email, display_name FROM users WHERE subject = 'kc-1'`).Scan(&email, &name); err != nil {
		t.Fatal(err)
	}
	// No name claim: preferred_username stands in.
	if email != "nina@new.example" || name != "user-kc-1" {
		t.Fatalf("profile %s %s", email, name)
	}
	if n := count(t, db, `SELECT count(*) FROM sessions JOIN users ON users.id = user_id WHERE subject = 'kc-1'`); n != 3 {
		t.Fatalf("%d sessions, want 3", n)
	}
}

// A refused login shows a page saying why and leaves no account, no session
// and no session cookie behind.
func TestLoginRefused(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, db := newAPIWithIssuer(t, kc.srv.URL)
	if _, err := db.Exec(context.Background(),
		`INSERT INTO users (id, issuer, subject, status) VALUES ('usr-gone', $1, 'kc-disabled', 'Disabled')`, kc.srv.URL); err != nil {
		t.Fatal(err)
	}
	// begin starts a login and returns its cookie and the code Keycloak hands back.
	begin := func(acct account) (*http.Cookie, string, string) {
		start := send(h, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
		code, state := kc.authorize(start.Header().Get("Location"), acct)
		return cookie(t, start, "__Host-tenantry_login"), code, state
	}
	tamper := func(set func()) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			kc.mu.Lock()
			set()
			kc.mu.Unlock()
			defer func() {
				kc.mu.Lock()
				kc.wrongNonce, kc.wrongAudience, kc.noRefreshToken = false, false, false
				kc.mu.Unlock()
			}()
			return kc.login(h, "/", newcomer)
		}
	}

	cases := []struct {
		name   string
		do     func() *httptest.ResponseRecorder
		status int
		says   string
	}{
		{"email not verified", func() *httptest.ResponseRecorder {
			unverified := newcomer
			unverified.emailVerified = false
			return kc.login(h, "/", unverified)
		}, 403, "not verified"},
		{"disabled account", func() *httptest.ResponseRecorder {
			return kc.login(h, "/", account{sub: "kc-disabled", email: "x@example.com", emailVerified: true})
		}, 403, "disabled"},
		{"state does not match", func() *httptest.ResponseRecorder {
			c, code, _ := begin(newcomer)
			return callbackWith(h, c, "code="+code+"&state=forged")
		}, 401, "no longer valid"},
		{"no login cookie", func() *httptest.ResponseRecorder {
			_, code, state := begin(newcomer)
			return callbackWith(h, nil, "code="+code+"&state="+url.QueryEscape(state))
		}, 401, "no longer valid"},
		{"login cookie from another attempt", func() *httptest.ResponseRecorder {
			older, _, _ := begin(newcomer)
			_, code, state := begin(newcomer)
			return callbackWith(h, older, "code="+code+"&state="+url.QueryEscape(state))
		}, 401, "no longer valid"},
		{"forged login cookie", func() *httptest.ResponseRecorder {
			c, code, state := begin(newcomer)
			c.Value = c.Value[:len(c.Value)-4] + "AAAA"
			return callbackWith(h, c, "code="+code+"&state="+url.QueryEscape(state))
		}, 401, "no longer valid"},
		{"code used twice (callback reloaded)", func() *httptest.ResponseRecorder {
			c, code, state := begin(newcomer)
			kc.mu.Lock()
			delete(kc.codes, code)
			kc.mu.Unlock()
			return callbackWith(h, c, "code="+code+"&state="+url.QueryEscape(state))
		}, 401, "no longer valid"},
		{"nonce does not match", tamper(func() { kc.wrongNonce = true }), 401, "no longer valid"},
		{"ID token for another client", tamper(func() { kc.wrongAudience = true }), 500, "on our side"},
		{"no refresh token", tamper(func() { kc.noRefreshToken = true }), 500, "on our side"},
		{"user cancelled at Keycloak", func() *httptest.ResponseRecorder {
			c, _, state := begin(newcomer)
			return callbackWith(h, c, "error=access_denied&error_description=%3Cscript%3E&state="+url.QueryEscape(state))
		}, 401, "access_denied"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := c.do()
			if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.says) {
				t.Fatalf("got %d %s, want %d saying %q", rec.Code, rec.Body, c.status, c.says)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("content type %s", ct)
			}
			if strings.Contains(rec.Body.String(), "<script>") {
				t.Fatal("text from the URL reached the page unescaped")
			}
			for _, ck := range rec.Result().Cookies() {
				if ck.Name == httpapi.SessionCookie {
					t.Fatal("a refused login set a session cookie")
				}
			}
		})
	}
	if n := count(t, db, `SELECT count(*) FROM sessions JOIN users ON users.id = user_id WHERE issuer = $1`, kc.srv.URL); n != 0 {
		t.Fatalf("%d sessions after refused logins", n)
	}
	if n := count(t, db, `SELECT count(*) FROM users WHERE subject = 'kc-1'`); n != 0 {
		t.Fatal("a refused login created the account")
	}
}

// The API starts without Keycloak and finds it once it is up.
func TestLoginWaitsForKeycloak(t *testing.T) {
	kc := newFakeKeycloak(t)
	kc.down = true
	h, _ := newAPIWithIssuer(t, kc.srv.URL)

	rec := send(h, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "on our side") {
		t.Fatalf("Keycloak down: %d %s", rec.Code, rec.Body)
	}
	kc.mu.Lock()
	kc.down = false
	kc.mu.Unlock()
	if rec := kc.login(h, "/", newcomer); rec.Code != http.StatusFound {
		t.Fatalf("Keycloak up: %d %s", rec.Code, rec.Body)
	}
}

// returnTo can only point back into the portal.
func TestLoginReturnTo(t *testing.T) {
	kc := newFakeKeycloak(t)
	h, _ := newAPIWithIssuer(t, kc.srv.URL)
	for returnTo, want := range map[string]string{
		"":                      "/",
		"/orgs/acme":            "/orgs/acme",
		"//evil.example/x":      "/",
		"/\\evil.example":       "/",
		"https://evil.example/": "/",
		"orgs/acme":             "/",
		"/auth/login":           "/",
	} {
		if got := kc.login(h, returnTo, newcomer).Header().Get("Location"); got != want {
			t.Errorf("returnTo %q: redirected to %q, want %q", returnTo, got, want)
		}
	}
}
