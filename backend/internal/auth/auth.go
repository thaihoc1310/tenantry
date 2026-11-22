// Package auth signs users in through Keycloak and keeps their portal
// sessions in PostgreSQL (architecture/06-security-and-identity.md §3). The
// Platform API is the OIDC client (a backend-for-frontend): tokens never
// reach the browser, which only holds an opaque session value. The package
// knows nothing about HTTP; the HTTP layer puts the values it returns in cookies.
package auth

import (
	"cmp"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// RolePlatformAdmin is the Keycloak realm role of Platform Admins (PRODUCT-PLAN §4.1).
const RolePlatformAdmin = "platform-admin"

const (
	// LoginTTL bounds the time between leaving for Keycloak and coming back.
	LoginTTL = 5 * time.Minute
	// MaxSession is the realm's SSO Session Max: no session outlives it.
	MaxSession = 24 * time.Hour

	// apiAudience is added to the portal client's access tokens by the
	// "audience portal-api" mapper in deploy/keycloak/realm-tenantry.json.
	apiAudience = "portal-api"
	idpTimeout  = 10 * time.Second
	// loginAAD keeps a sealed login state from being accepted as anything else.
	loginAAD     = "tenantry_login"
	statusActive = "Active"
)

var (
	// ErrNoSession answers a missing, unknown, expired or disabled session alike.
	ErrNoSession = apperr.New(apperr.Unauthenticated, "login required")
	// ErrLoginExpired covers a login state that is missing, forged, stale, or
	// from another attempt (a second tab started a newer login).
	ErrLoginExpired = apperr.New(apperr.Unauthenticated,
		"this login attempt is no longer valid (it took more than 5 minutes or was replaced by a newer one); log in again")
	ErrEmailNotVerified = apperr.New(apperr.Forbidden,
		"your email address is not verified yet: open the link in the email Keycloak sent you, then log in again")
	ErrDisabled = apperr.New(apperr.Forbidden, "this account is disabled")
)

type Service struct {
	db     *pgxpool.Pool
	q      *store.Queries
	cfg    config.Auth
	aead   cipher.AEAD
	client *http.Client     // every call to Keycloak
	now    func() time.Time // replaced in tests

	mu  sync.Mutex
	idp *provider // nil until discovery succeeds
}

// provider is what discovery yields: the OAuth2 endpoints and the verifiers
// built on Keycloak's signing keys.
type provider struct {
	oauth       oauth2.Config
	idToken     *oidc.IDTokenVerifier // audience: the portal client
	accessToken *oidc.IDTokenVerifier // audience: portal-api
	endSession  string                // end_session_endpoint; logout
}

func New(db *pgxpool.Pool, cfg config.Auth) (*Service, error) {
	block, err := aes.NewCipher(cfg.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("session key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{
		db: db, q: store.New(db), cfg: cfg, aead: aead,
		client: &http.Client{Timeout: idpTimeout},
		now:    time.Now,
	}, nil
}

// PublicURL is the portal origin browsers use (TENANTRY_PUBLIC_URL), for
// links the API hands out, such as invitation links.
func (s *Service) PublicURL() string { return s.cfg.PublicURL }

// --- login ---

// loginState is everything the callback must check, carried by the browser
// in a sealed cookie so nothing is stored before a login completes.
type loginState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"` // PKCE
	ReturnTo string `json:"r"`
	Started  int64  `json:"t"` // unix seconds
}

// BeginLogin starts an Authorization Code flow with PKCE. It returns the
// Keycloak URL to send the browser to and the sealed state the browser must
// hand back to FinishLogin.
func (s *Service) BeginLogin(ctx context.Context, returnTo string) (redirect, sealed string, err error) {
	p, err := s.provider(ctx)
	if err != nil {
		return "", "", err
	}
	st := loginState{
		State:    randomToken(),
		Nonce:    randomToken(),
		Verifier: oauth2.GenerateVerifier(),
		ReturnTo: SafeReturnTo(returnTo),
		Started:  s.now().Unix(),
	}
	b, err := json.Marshal(st)
	if err != nil {
		return "", "", err
	}
	sealed = base64.RawURLEncoding.EncodeToString(s.seal(b, []byte(loginAAD)))
	return p.oauth.AuthCodeURL(st.State, oauth2.S256ChallengeOption(st.Verifier), oidc.Nonce(st.Nonce)), sealed, nil
}

// SafeReturnTo keeps the post-login destination on the portal: a local path,
// never another host ("//evil.example", "/\evil.example", "https://…"), and
// never /auth itself, which would start a login loop. Anything else becomes "/".
func SafeReturnTo(p string) string {
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(p, "/") ||
		strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n\t") ||
		u.Path == "/auth" || strings.HasPrefix(u.Path, "/auth/") {
		return "/"
	}
	return p
}

// Login is a completed sign-in.
type Login struct {
	Token    string // session value for the browser; only its hash is stored
	UserID   string
	ReturnTo string
}

// FinishLogin checks the callback against the sealed state, redeems the code,
// and creates the user (first login) and a session. A refused login leaves
// nothing behind.
func (s *Service) FinishLogin(ctx context.Context, sealed, state, code string) (Login, error) {
	st, err := s.openLogin(sealed)
	if err != nil || subtle.ConstantTimeCompare([]byte(st.State), []byte(state)) != 1 {
		return Login{}, ErrLoginExpired
	}
	p, err := s.provider(ctx)
	if err != nil {
		return Login{}, err
	}
	ctx = s.clientContext(ctx)

	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	var refused *oauth2.RetrieveError
	if errors.As(err, &refused) && refused.ErrorCode == "invalid_grant" {
		// Code already used or expired, e.g. the callback was reloaded.
		return Login{}, ErrLoginExpired
	}
	if err != nil {
		return Login{}, fmt.Errorf("redeem authorization code: %w", err)
	}

	rawID, _ := tok.Extra("id_token").(string)
	idt, err := p.idToken.Verify(ctx, rawID)
	if err != nil {
		return Login{}, fmt.Errorf("verify ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		return Login{}, ErrLoginExpired
	}
	var profile struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idt.Claims(&profile); err != nil {
		return Login{}, fmt.Errorf("ID token claims: %w", err)
	}
	// Invitations trust the session's email (03-flows §3): an unverified
	// address could be someone else's.
	if !profile.EmailVerified {
		return Login{}, ErrEmailNotVerified
	}

	roles, accessExpiry, err := s.readAccessToken(ctx, p, tok.AccessToken)
	if err != nil {
		return Login{}, err
	}
	if tok.RefreshToken == "" {
		return Login{}, errors.New("token response carries no refresh token")
	}

	token, idHash := newSessionToken()
	var email *string
	if profile.Email != "" {
		email = &profile.Email
	}
	name := cmp.Or(profile.Name, profile.PreferredUsername, profile.Email)

	var userID string
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		u, err := q.UpsertUserByIdentity(ctx, store.UpsertUserByIdentityParams{
			ID: id.New(id.User), Issuer: idt.Issuer, Subject: idt.Subject, Email: email, DisplayName: name,
		})
		if err != nil {
			return err
		}
		if u.Status != statusActive {
			return ErrDisabled
		}
		userID = u.ID
		if err := q.CreateSession(ctx, store.CreateSessionParams{
			IDHash:          idHash,
			UserID:          u.ID,
			RealmRoles:      roles,
			RefreshTokenEnc: s.seal([]byte(tok.RefreshToken), idHash),
			AccessExpiresAt: accessExpiry,
			ExpiresAt:       s.sessionExpiry(tok, s.now()),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{Actor: u.ID, Action: audit.Login, TargetType: "User", TargetID: u.ID})
	})
	if err != nil {
		return Login{}, err
	}
	return Login{Token: token, UserID: userID, ReturnTo: st.ReturnTo}, nil
}

// readAccessToken verifies the access token (signature, issuer, audience
// portal-api, expiry) and returns its realm roles and expiry. The roles are
// only in the access token, not in the ID token.
func (s *Service) readAccessToken(ctx context.Context, p *provider, raw string) ([]string, time.Time, error) {
	at, err := p.accessToken.Verify(ctx, raw)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("verify access token: %w", err)
	}
	var claims struct {
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := at.Claims(&claims); err != nil {
		return nil, time.Time{}, fmt.Errorf("access token claims: %w", err)
	}
	roles := claims.RealmAccess.Roles
	if roles == nil {
		roles = []string{}
	}
	return roles, at.Expiry, nil
}

// sessionExpiry is when the refresh token stops working: Keycloak's
// refresh_expires_in (the SSO idle timeout), never past MaxSession after the
// login at created.
func (s *Service) sessionExpiry(tok *oauth2.Token, created time.Time) time.Time {
	limit := created.Add(MaxSession)
	if sec, ok := tok.Extra("refresh_expires_in").(float64); ok && sec > 0 {
		if t := s.now().Add(time.Duration(sec) * time.Second); t.Before(limit) {
			return t
		}
	}
	return limit
}

// --- sessions ---

// Principal is the caller behind a session.
type Principal struct {
	User  store.User
	Roles []string // Keycloak realm roles
}

func (p Principal) PlatformAdmin() bool { return slices.Contains(p.Roles, RolePlatformAdmin) }

// Session resolves a session value. A disabled user loses access on the next
// request and their session row is deleted. Once the access token has
// expired, the session is refreshed at Keycloak first, which re-reads the
// realm roles and ends the session if Keycloak's SSO session has ended.
func (s *Service) Session(ctx context.Context, token string) (Principal, error) {
	idHash, ok := hashToken(token)
	if !ok {
		return Principal{}, ErrNoSession
	}
	row, err := s.q.GetSession(ctx, idHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNoSession
	}
	if err != nil {
		return Principal{}, err
	}
	if row.User.Status != statusActive {
		if err := s.q.DeleteSession(ctx, idHash); err != nil {
			return Principal{}, err
		}
		return Principal{}, ErrNoSession
	}
	roles := row.Session.RealmRoles
	if !s.now().Before(row.Session.AccessExpiresAt) {
		if roles, err = s.refresh(ctx, idHash); err != nil {
			return Principal{}, err
		}
	}
	return Principal{User: row.User, Roles: roles}, nil
}

// refresh renews the session's expired access token and returns the realm
// roles it carries. The session row stays locked meanwhile: concurrent
// requests of the session wait, then find it renewed and skip Keycloak.
//
// Keycloak refusing the refresh token (invalid_grant: SSO session idle or
// over, logged out, account disabled) ends the session. Keycloak being
// unreachable does not: the request fails and the next one tries again.
// Carrying on with the old roles instead would break the promise that a
// removed platform-admin role stops working within one access token lifetime.
func (s *Service) refresh(ctx context.Context, idHash []byte) ([]string, error) {
	var roles []string
	ended := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		sess, err := q.LockSession(ctx, idHash)
		if errors.Is(err, pgx.ErrNoRows) {
			ended = true // logged out or expired while waiting for the lock
			return nil
		}
		if err != nil {
			return err
		}
		if s.now().Before(sess.AccessExpiresAt) {
			roles = sess.RealmRoles // renewed by the request we waited for
			return nil
		}
		refreshToken, err := s.open(sess.RefreshTokenEnc, idHash)
		if err != nil {
			// Sealed with an older session key: rotating the key logs everyone out.
			ended = true
			return q.DeleteSession(ctx, idHash)
		}

		p, err := s.provider(ctx)
		if err != nil {
			return err
		}
		cctx := s.clientContext(ctx)
		tok, err := p.oauth.TokenSource(cctx, &oauth2.Token{RefreshToken: string(refreshToken)}).Token()
		var refused *oauth2.RetrieveError
		if errors.As(err, &refused) && refused.ErrorCode == "invalid_grant" {
			ended = true
			return q.DeleteSession(ctx, idHash)
		}
		if err != nil {
			return fmt.Errorf("refresh session: %w", err)
		}
		var accessExpiry time.Time
		if roles, accessExpiry, err = s.readAccessToken(cctx, p, tok.AccessToken); err != nil {
			return err
		}
		return q.RenewSession(ctx, store.RenewSessionParams{
			IDHash:          idHash,
			RealmRoles:      roles,
			RefreshTokenEnc: s.seal([]byte(tok.RefreshToken), idHash),
			AccessExpiresAt: accessExpiry,
			ExpiresAt:       s.sessionExpiry(tok, sess.CreatedAt),
		})
	})
	if err != nil {
		return nil, err
	}
	if ended {
		return nil, ErrNoSession
	}
	return roles, nil
}

// Logout ends the session and the Keycloak SSO session behind it, so
// Headlamp and kubelogin need a new login too ("log out" means everywhere).
// The row goes first: the portal session is over even when Keycloak cannot
// be told, and the error returned then is only worth logging.
func (s *Service) Logout(ctx context.Context, token string) error {
	idHash, ok := hashToken(token)
	if !ok {
		return nil
	}
	var ended store.EndSessionRow
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		if ended, err = q.EndSession(ctx, idHash); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{Actor: ended.UserID, Action: audit.Logout, TargetType: "User", TargetID: ended.UserID})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already gone
	}
	if err != nil {
		return err
	}
	refreshToken, err := s.open(ended.RefreshTokenEnc, idHash)
	if err != nil {
		return fmt.Errorf("end SSO session: refresh token sealed with another session key: %w", err)
	}
	p, err := s.provider(ctx)
	if err != nil {
		return err
	}
	return s.endSSO(ctx, p, string(refreshToken))
}

// endSSO calls Keycloak's logout endpoint the way a confidential client
// does from its backend: a form POST with the refresh token, the client
// authenticated like on the token endpoint.
func (s *Service) endSSO(ctx context.Context, p *provider, refreshToken string) error {
	if p.endSession == "" {
		return errors.New("end SSO session: the issuer advertises no end_session_endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endSession,
		strings.NewReader(url.Values{"refresh_token": {refreshToken}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// RFC 6749 §2.3.1: both parts form-encoded, as oauth2 does for the token endpoint.
	req.SetBasicAuth(url.QueryEscape(s.cfg.ClientID), url.QueryEscape(s.cfg.ClientSecret))
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("end SSO session: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&e)
	if resp.StatusCode == http.StatusBadRequest && e.Error == "invalid_grant" {
		return nil // the SSO session had already ended
	}
	return fmt.Errorf("end SSO session: Keycloak answered %d %s", resp.StatusCode, e.Error)
}

// newSessionToken returns 32 random bytes for the browser and their sha256 for the table.
func newSessionToken() (token string, idHash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(b), h[:]
}

func hashToken(token string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 {
		return nil, false
	}
	h := sha256.Sum256(b)
	return h[:], true
}

// --- Keycloak discovery ---

// provider discovers Keycloak on first use rather than at start-up, so the
// API comes up, and serves existing sessions, while Keycloak is still
// starting or briefly down. A failed discovery is retried on the next login.
func (s *Service) provider(ctx context.Context) (*provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idp != nil {
		return s.idp, nil
	}
	p, err := oidc.NewProvider(s.clientContext(ctx), s.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer %s: %w", s.cfg.Issuer, err)
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := p.Claims(&extra); err != nil {
		return nil, fmt.Errorf("discover OIDC issuer %s: %w", s.cfg.Issuer, err)
	}
	s.idp = &provider{
		endSession: extra.EndSession,
		oauth: oauth2.Config{
			ClientID:     s.cfg.ClientID,
			ClientSecret: s.cfg.ClientSecret,
			Endpoint:     p.Endpoint(),
			RedirectURL:  s.cfg.PublicURL + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		idToken:     p.Verifier(&oidc.Config{ClientID: s.cfg.ClientID, Now: s.now}),
		accessToken: p.Verifier(&oidc.Config{ClientID: apiAudience, Now: s.now}),
	}
	// Keycloak accepts the secret in the Authorization header; saying so
	// skips the library's probing, which would send the code twice.
	s.idp.oauth.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
	return s.idp, nil
}

// clientContext makes go-oidc and oauth2 use s.client (with its timeout).
func (s *Service) clientContext(ctx context.Context) context.Context {
	return context.WithValue(oidc.ClientContext(ctx, s.client), oauth2.HTTPClient, s.client)
}

// --- encryption ---

// seal encrypts with AES-256-GCM. aad binds the ciphertext to its use: a
// refresh token to its session row, a login state to the login cookie.
func (s *Service) seal(plain, aad []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return s.aead.Seal(nonce, nonce, plain, aad)
}

func (s *Service) open(sealed, aad []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("ciphertext too short")
	}
	return s.aead.Open(nil, sealed[:n], sealed[n:], aad)
}

func (s *Service) openLogin(sealed string) (loginState, error) {
	var st loginState
	b, err := base64.RawURLEncoding.DecodeString(sealed)
	if err == nil {
		b, err = s.open(b, []byte(loginAAD))
	}
	if err == nil {
		err = json.Unmarshal(b, &st)
	}
	if err != nil {
		return st, err
	}
	age := s.now().Sub(time.Unix(st.Started, 0))
	if age < 0 || age > LoginTTL {
		return st, errors.New("login state expired")
	}
	return st, nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
