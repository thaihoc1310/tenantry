package e2e

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// The lab as a person reaches it. Passwords are read from the cluster at
// run time and never logged.
var (
	portalURL   = envOr("E2E_PORTAL", "https://portal.tenantry.site")
	issuer      = envOr("E2E_ISSUER", "https://auth.tenantry.site/realms/tenantry")
	kubeContext = envOr("E2E_KUBE_CONTEXT", "hocbt@kubernetes")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// session is a logged-in portal session: the BFF's cookies and Keycloak's
// SSO session in one jar.
type session struct {
	name string
	http *http.Client
}

var (
	loginForm        = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)
	registrationLink = regexp.MustCompile(`href="([^"]*login-actions/registration[^"]*)"`)
	registerForm     = regexp.MustCompile(`<form[^>]*id="kc-register-form"[^>]*action="([^"]+)"`)
	passwordForm     = regexp.MustCompile(`<form[^>]*id="kc-passwd-update-form"[^>]*action="([^"]+)"`)
	actionTokenLink  = regexp.MustCompile(`https://\S+/login-actions/action-token\?\S+`)
)

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 60 * time.Second, Transport: retrying{http.DefaultTransport}}
}

// retrying sends a request again, twice, when it gets no answer at all: the
// lab's ingress drops a connection now and then. Bodies are replayed with
// GetBody, which http.NewRequest sets for the bodies used here.
type retrying struct{ next http.RoundTripper }

func (r retrying) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
			if req.GetBody != nil {
				if req.Body, err = req.GetBody(); err != nil {
					return nil, err
				}
			}
		}
		if resp, err = r.next.RoundTrip(req); err == nil {
			return resp, nil
		}
	}
	return resp, err
}

// pageOf returns a reader of a response: its body and final URL.
func pageOf(t *testing.T) func(*http.Response, error) (string, *url.URL) {
	return func(resp *http.Response, err error) (string, *url.URL) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.Request.URL
	}
}

func formAction(t *testing.T, re *regexp.Regexp, body string, at *url.URL, what string) string {
	t.Helper()
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s at %s", what, at)
	}
	u, _ := at.Parse(html.UnescapeString(m[1]))
	return u.String()
}

// logIn goes through the portal's login: portal → Keycloak form → portal.
func logIn(t *testing.T, username, password string) *session {
	t.Helper()
	c := newClient()
	pg := pageOf(t)
	body, at := pg(c.Get(portalURL + "/auth/login"))
	_, at = pg(c.PostForm(formAction(t, loginForm, body, at, "login form"),
		url.Values{"username": {username}, "password": {password}, "credentialId": {""}}))
	if !strings.HasPrefix(at.String(), portalURL) {
		t.Fatalf("login of %s ended at %s", username, at)
	}
	return &session{name: username, http: c}
}

// person is a throwaway account, optest-<role>-<random>.
type person struct {
	Username, Email, password string
	ID                        string
	*session
}

// signUp registers an account through Keycloak's pages: the registration
// form, the verification mail from Mailpit, a password set from its link.
func signUp(t *testing.T, role string) *person {
	t.Helper()
	name := "optest-" + role + "-" + strings.ToLower(alnum(6))
	p := &person{Username: name, Email: name + "@tenantry.site", password: "Pw-" + alnum(20)}
	c := newClient()
	pg := pageOf(t)
	body, at := pg(c.Get(portalURL + "/auth/login"))
	reg := formAction(t, registrationLink, body, at, "registration link")
	body, at = pg(c.Get(reg))
	body, at = pg(c.PostForm(formAction(t, registerForm, body, at, "registration form"), url.Values{
		"username": {name}, "email": {p.Email}, "firstName": {"Optest"}, "lastName": {role},
	}))
	if !strings.Contains(strings.ToLower(body), "verify your email") {
		t.Fatalf("registration of %s did not ask to verify the address (at %s)", name, at)
	}
	body, at = pg(c.Get(verificationLink(t, p.Email)))
	_, at = pg(c.PostForm(formAction(t, passwordForm, body, at, "password form"),
		url.Values{"password-new": {p.password}, "password-confirm": {p.password}}))
	if !strings.HasPrefix(at.String(), portalURL) {
		t.Fatalf("sign-up of %s ended at %s", name, at)
	}
	p.session = &session{name: name, http: c}
	var me struct{ ID string }
	p.api(t, http.MethodGet, "/me", nil, &me, 200)
	p.ID = me.ID
	t.Logf("signed up %s", name)
	return p
}

// relogIn starts a new session, as after a browser restart.
func (p *person) relogIn(t *testing.T) { p.session = logIn(t, p.Username, p.password) }

func (s *session) csrf() string {
	u, _ := url.Parse(portalURL)
	for _, c := range s.http.Jar.Cookies(u) {
		if c.Name == "__Host-tenantry_csrf" {
			v, _ := url.QueryUnescape(c.Value)
			return v
		}
	}
	return ""
}

// api calls /api/v1, decodes the answer into out and checks the status
// against want (any status when want is empty); it returns the status.
func (s *session) api(t *testing.T, method, path string, body, out any, want ...int) int {
	t.Helper()
	code, raw := s.call(method, path, body)
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	if len(want) > 0 && !contains(want, code) {
		t.Fatalf("%s %s %s: %d %s", s.name, method, path, code, firstLine(string(raw)))
	}
	return code
}

// call is api without testing.T, for cleanups.
func (s *session) call(method, path string, body any) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, portalURL+"/api/v1"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", s.csrf())
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		req.Header.Set("Idempotency-Key", uuid())
	}
	// A slow answer or a dropped connection is tried again, twice.
	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
			if body != nil {
				b, _ := json.Marshal(body)
				req.Body = io.NopCloser(bytes.NewReader(b))
			}
		}
		if resp, err = s.http.Do(req); err == nil {
			break
		}
	}
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// operation waits for an operation to succeed.
func (s *session) operation(t *testing.T, id string) {
	t.Helper()
	until(t, "operation "+id, 6*time.Minute, func() bool {
		var op struct {
			State, ErrorCode, ErrorMessage string
		}
		s.api(t, http.MethodGet, "/operations/"+id, nil, &op, 200)
		if op.State == "Failed" || op.State == "Blocked" {
			t.Fatalf("operation %s %s: %s %s", id, op.State, op.ErrorCode, op.ErrorMessage)
		}
		return op.State == "Succeeded"
	})
}

// kubectlToken is the ID token the kubectl client gets by authorization code
// with PKCE on the session's SSO, what kubelogin would get.
func (s *session) kubectlToken(t *testing.T) string {
	t.Helper()
	const redirect = "http://localhost:8000"
	verifier := alnum(43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {"kubectl"}, "response_type": {"code"}, "scope": {"openid"}, "redirect_uri": {redirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "state": {alnum(8)},
	}
	c := *s.http
	var code string
	c.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if strings.HasPrefix(req.URL.String(), redirect) {
			code = req.URL.Query().Get("code")
			return http.ErrUseLastResponse
		}
		return nil
	}
	resp, err := c.Get(issuer + "/protocol/openid-connect/auth?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if code == "" {
		t.Fatalf("no authorization code for %s", s.name)
	}
	resp, err = http.PostForm(issuer+"/protocol/openid-connect/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"kubectl"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.IDToken == "" {
		t.Fatalf("kubectl token of %s: %v (status %d)", s.name, err, resp.StatusCode)
	}
	return tok.IDToken
}

// groupsOf reads the groups claim of a token.
func groupsOf(t *testing.T, jwt string) []string {
	t.Helper()
	parts := strings.Split(jwt, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Groups []string `json:"groups"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.Groups
}

// verificationLink reads Keycloak's mail to email from Mailpit, through a
// port-forward opened once.
func verificationLink(t *testing.T, email string) string {
	t.Helper()
	base := mailpit(t)
	for range 40 {
		var found struct {
			Messages []struct{ ID string } `json:"messages"`
		}
		if r, err := http.Get(base + "/api/v1/search?query=" + url.QueryEscape(`to:"`+email+`"`)); err == nil {
			_ = json.NewDecoder(r.Body).Decode(&found)
			r.Body.Close()
		}
		for _, m := range found.Messages {
			var msg struct{ Text string }
			if r, err := http.Get(base + "/api/v1/message/" + m.ID); err == nil {
				_ = json.NewDecoder(r.Body).Decode(&msg)
				r.Body.Close()
			}
			if l := actionTokenLink.FindString(msg.Text); l != "" {
				return l
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("no verification mail for %s in Mailpit", email)
	return ""
}

var mailpitOnce struct {
	sync.Once
	url string
	cmd *exec.Cmd
}

func mailpit(t *testing.T) string {
	mailpitOnce.Do(func() {
		mailpitOnce.cmd = exec.Command("kubectl", "--context", kubeContext, "-n", "keycloak", "port-forward", "svc/mailpit", "18026:8025")
		if err := mailpitOnce.cmd.Start(); err != nil {
			t.Fatalf("port-forward to Mailpit: %v", err)
		}
		time.Sleep(2 * time.Second)
		mailpitOnce.url = "http://127.0.0.1:18026"
	})
	return mailpitOnce.url
}

func until(t *testing.T, what string, timeout time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Second)
	}
}

func alnum(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if len(s) > 300 {
		s = s[:300]
	}
	return strings.SplitN(s, "\n", 2)[0]
}

// uuid is a random UUID (version 4), the form the API wants for Idempotency-Key.
func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
