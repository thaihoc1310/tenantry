//go:build lab

package isolation

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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The portal and Keycloak of the lab, as a browser reaches them.
var (
	portalURL = envOr("PORTAL_URL", "https://portal.tenantry.site")
	issuer    = envOr("OIDC_ISSUER", "https://auth.tenantry.site/realms/tenantry")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// portal is a logged-in portal session: the BFF's cookies, its CSRF token,
// and Keycloak's session in the same jar (so a second client logs in by SSO).
type portal struct {
	http *http.Client
}

var loginForm = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)

// logIn goes through the portal's login as a browser would: the portal sends
// to Keycloak, the login form is posted, Keycloak sends back to the portal.
func logIn(t *testing.T, username, password string) *portal {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp, err := c.Get(portalURL + "/auth/login")
	if err != nil {
		t.Fatalf("portal login: %v", err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := loginForm.FindSubmatch(page)
	if m == nil {
		t.Fatalf("no Keycloak login form (status %d at %s)", resp.StatusCode, resp.Request.URL)
	}
	resp, err = c.PostForm(html.UnescapeString(string(m[1])), url.Values{"username": {username}, "password": {password}, "credentialId": {""}})
	if err != nil {
		t.Fatalf("Keycloak login: %v", err)
	}
	resp.Body.Close()
	if !strings.HasPrefix(resp.Request.URL.String(), portalURL) {
		t.Fatalf("login of %s ended at %s (status %d)", username, resp.Request.URL, resp.StatusCode)
	}
	return &portal{http: c}
}

func (p *portal) csrf() string {
	u, _ := url.Parse(portalURL)
	for _, c := range p.http.Jar.Cookies(u) {
		if c.Name == "__Host-tenantry_csrf" {
			v, _ := url.QueryUnescape(c.Value)
			return v
		}
	}
	return ""
}

// api calls /api/v1 and decodes the JSON answer into out (when not nil).
func (p *portal) api(t *testing.T, method, path string, body, out any) int {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, portalURL+"/api/v1"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", p.csrf())
	if method == http.MethodPost || method == http.MethodPut {
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
		if resp, err = p.http.Do(req); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
		}
	}
	if resp.StatusCode >= 400 {
		t.Logf("%s %s: %d %s", method, path, resp.StatusCode, firstLine(string(raw)))
	}
	return resp.StatusCode
}

// kubectlToken is the ID token the kubectl client gets (audience
// kubernetes, groups claim), by authorization code with PKCE on the
// session's Keycloak SSO; the loopback redirect is read, never served.
func (p *portal) kubectlToken(t *testing.T) string {
	t.Helper()
	const redirect = "http://localhost:8000"
	verifier := randomString(32)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {"kubectl"}, "response_type": {"code"}, "scope": {"openid"}, "redirect_uri": {redirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"state": {randomString(8)},
	}
	c := *p.http
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
		t.Fatalf("kubectl authorization: %v", err)
	}
	resp.Body.Close()
	if code == "" {
		t.Fatalf("no authorization code (status %d at %s)", resp.StatusCode, resp.Request.URL)
	}
	resp, err = http.PostForm(issuer+"/protocol/openid-connect/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"kubectl"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	if err != nil {
		t.Fatalf("kubectl token: %v", err)
	}
	defer resp.Body.Close()
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.IDToken == "" {
		t.Fatalf("kubectl token: %v (status %d)", err, resp.StatusCode)
	}
	return tok.IDToken
}

// claims decodes a JWT's payload.
func claims(t *testing.T, jwt string) map[string]any {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatal("not a JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// secret reads one key of a Secret on the lab; the value is never logged.
func secret(t *testing.T, ns, name, key string) string {
	t.Helper()
	sec := &corev1.Secret{}
	if err := admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, sec); err != nil {
		t.Fatalf("Secret %s/%s: %v", ns, name, err)
	}
	return string(sec.Data[key])
}

func adminPortal(t *testing.T) *portal {
	t.Helper()
	return logIn(t, "admin", secret(t, "keycloak", "keycloak-realm", "PLATFORM_ADMIN_PASSWORD"))
}

var (
	registrationLink = regexp.MustCompile(`href="([^"]*login-actions/registration[^"]*)"`)
	registerForm     = regexp.MustCompile(`<form[^>]*id="kc-register-form"[^>]*action="([^"]+)"`)
	passwordForm     = regexp.MustCompile(`<form[^>]*id="kc-passwd-update-form"[^>]*action="([^"]+)"`)
	actionTokenLink  = regexp.MustCompile(`https://\S+/login-actions/action-token\?\S+`)
)

// account is a throwaway portal user, optest-<random>.
type account struct {
	Username, Email, password string
	*portal
}

// signUp registers an account through Keycloak's pages as a person would:
// the registration form, the verification mail read from Mailpit, a
// password set from its link. It ends logged in to the portal.
func signUp(t *testing.T) *account {
	t.Helper()
	name := "optest-" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomString(6)))
	a := &account{Username: name, Email: name + "@tenantry.site", password: "Pw-" + randomString(18)}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	page := func(resp *http.Response, err error) (string, *url.URL) {
		t.Helper()
		if err != nil {
			t.Fatalf("sign up %s: %v", name, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.Request.URL
	}
	body, at := page(c.Get(portalURL + "/auth/login"))
	m := registrationLink.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no registration link at %s", at)
	}
	reg, _ := at.Parse(html.UnescapeString(m[1]))
	body, at = page(c.Get(reg.String()))
	if m = registerForm.FindStringSubmatch(body); m == nil {
		t.Fatalf("no registration form at %s", at)
	}
	post, _ := at.Parse(html.UnescapeString(m[1]))
	body, at = page(c.PostForm(post.String(), url.Values{
		"username": {name}, "email": {a.Email}, "firstName": {"Optest"}, "lastName": {name},
	}))
	if !strings.Contains(strings.ToLower(body), "verify your email") {
		t.Fatalf("registration of %s did not ask to verify the address (at %s)", name, at)
	}
	link := verificationLink(t, a.Email)
	body, at = page(c.Get(link))
	if m = passwordForm.FindStringSubmatch(body); m == nil {
		t.Fatalf("no password form behind the verification link (at %s)", at)
	}
	post, _ = at.Parse(html.UnescapeString(m[1]))
	_, at = page(c.PostForm(post.String(), url.Values{"password-new": {a.password}, "password-confirm": {a.password}}))
	if !strings.HasPrefix(at.String(), portalURL) {
		t.Fatalf("sign-up of %s ended at %s", name, at)
	}
	a.portal = &portal{http: c}
	t.Logf("signed up %s", name)
	return a
}

// verificationLink reads Keycloak's mail to email from Mailpit, through a
// port-forward this test opens.
func verificationLink(t *testing.T, email string) string {
	t.Helper()
	mailpit := mailpitURL(t)
	for range 30 {
		var found struct {
			Messages []struct{ ID string } `json:"messages"`
		}
		if r, err := http.Get(mailpit + "/api/v1/search?query=" + url.QueryEscape(`to:"`+email+`"`)); err == nil {
			_ = json.NewDecoder(r.Body).Decode(&found)
			r.Body.Close()
		}
		if len(found.Messages) > 0 {
			var msg struct{ Text string }
			if r, err := http.Get(mailpit + "/api/v1/message/" + found.Messages[0].ID); err == nil {
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
	url string
}

func mailpitURL(t *testing.T) string {
	t.Helper()
	if mailpitOnce.url != "" {
		return mailpitOnce.url
	}
	cmd := exec.Command("kubectl", "--context", envOr("KUBE_CONTEXT", "hocbt@kubernetes"), "-n", "keycloak",
		"port-forward", "svc/mailpit", "18025:8025")
	if err := cmd.Start(); err != nil {
		t.Fatalf("port-forward to Mailpit: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	time.Sleep(2 * time.Second)
	mailpitOnce.url = "http://127.0.0.1:18025"
	return mailpitOnce.url
}

// operation waits for an operation to succeed.
func (p *portal) operation(t *testing.T, id string) {
	t.Helper()
	waitFor(t, "operation "+id, 5*time.Minute, func() (bool, error) {
		var op struct {
			State string `json:"state"`
		}
		p.api(t, http.MethodGet, "/operations/"+id, nil, &op)
		if op.State == "Failed" || op.State == "Blocked" {
			t.Fatalf("operation %s %s", id, op.State)
		}
		return op.State == "Succeeded", nil
	})
}

// uuid is a random UUID (version 4), the form the API wants for Idempotency-Key.
func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
