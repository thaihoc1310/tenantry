package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/auth"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// Cookie names carry the __Host- prefix: the browser then only accepts them
// from this exact host, Secure and on Path=/. Without it, a page on another
// tenantry.site subdomain (tenant Routes live on *.mvp.tenantry.site, which
// is same-site, so SameSite does not help) could plant its own session or
// CSRF cookie for the portal.
const (
	SessionCookie = "__Host-tenantry_session" // HttpOnly; the session value
	CSRFCookie    = "__Host-tenantry_csrf"    // readable by the SPA, echoed in CSRFHeader
	CSRFHeader    = "X-CSRF-Token"
	loginCookie   = "__Host-tenantry_login" // sealed login state, lives LoginTTL
)

// login sends the browser to Keycloak. ?returnTo=/path is where the browser
// lands after the login; anything but a local path becomes "/".
func login(a *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		redirect, sealed, err := a.BeginLogin(r.Context(), r.URL.Query().Get("returnTo"))
		if err != nil {
			loginFailed(w, r, err)
			return
		}
		setCookie(w, loginCookie, sealed, auth.LoginTTL, true)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, redirect, http.StatusFound)
	}
}

// callback is where Keycloak sends the browser back. On success it sets the
// session and CSRF cookies and redirects to the page the login started from;
// otherwise it shows why, without creating a session.
func callback(a *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCookie(w, loginCookie, "", -1, true) // one try per login state
		w.Header().Set("Cache-Control", "no-store")
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			// e.g. access_denied when the user cancels. error_description is
			// left out: anyone can put text in this URL.
			if len(e) > 64 {
				e = e[:64]
			}
			loginFailed(w, r, apperr.New(apperr.Unauthenticated, "Keycloak did not complete the login ("+e+"); log in again"))
			return
		}
		c, err := r.Cookie(loginCookie)
		if err != nil {
			loginFailed(w, r, auth.ErrLoginExpired)
			return
		}
		l, err := a.FinishLogin(r.Context(), c.Value, q.Get("state"), q.Get("code"))
		if err != nil {
			loginFailed(w, r, err)
			return
		}
		noteUser(r.Context(), l.UserID)
		setCookie(w, SessionCookie, l.Token, auth.MaxSession, true)
		setCookie(w, CSRFCookie, rand.Text(), auth.MaxSession, false)
		http.Redirect(w, r, l.ReturnTo, http.StatusFound)
	}
}

func setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration, httpOnly bool) {
	c := &http.Cookie{
		Name: name, Value: value, Path: "/",
		Secure: true, HttpOnly: httpOnly, SameSite: http.SameSiteLaxMode,
		MaxAge: int(maxAge / time.Second),
	}
	if maxAge < 0 {
		c.MaxAge = -1 // delete now
	}
	http.SetCookie(w, c)
}

var loginFailedPage = template.Must(template.New("").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Login failed · Tenantry</title>
<body style="font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;line-height:1.5">
<h1>Login failed</h1>
<p>{{.Message}}</p>
<p><a href="/auth/login">Log in again</a></p>
<p style="color:#666">Request id: <code>{{.RequestId}}</code> (quote it when asking for help)</p>
</body></html>
`))

// loginFailed is writeError for a browser navigation: a page, not JSON.
func loginFailed(w http.ResponseWriter, r *http.Request, err error) {
	status, body := describe(r, err)
	if body.Code == oapi.INTERNAL {
		body.Message = "Something went wrong on our side. Try again in a moment."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = loginFailedPage.Execute(w, body)
}

// authenticate puts the session's user in the context or answers 401.
func authenticate(a *auth.Service) oapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookie)
			if err != nil {
				writeError(w, r, auth.ErrNoSession)
				return
			}
			p, err := a.Session(r.Context(), c.Value)
			if err != nil {
				writeError(w, r, err)
				return
			}
			noteUser(r.Context(), p.User.ID)
			ctx := context.WithValue(r.Context(), principalKey, principal{user: p.User, platformAdmin: p.PlatformAdmin()})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

var errCSRF = apperr.New(apperr.Forbidden, "missing or wrong "+CSRFHeader+" header: send the value of the "+CSRFCookie+" cookie")

// requireCSRF makes every request that can change something prove it comes
// from the portal (double submit): only a page served from the portal's own
// origin can read the CSRF cookie and copy it into the header.
func requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !csrfOK(r) {
			writeError(w, r, errCSRF)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func csrfOK(r *http.Request) bool {
	c, err := r.Cookie(CSRFCookie)
	return err == nil && c.Value != "" && subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.Header.Get(CSRFHeader))) == 1
}

// logout ends the session here and at Keycloak and deletes the cookies;
// 204 either way, after which the SPA leaves for a page of its choosing.
// Without a session there is nothing to end, so no CSRF check either: the
// cookies are deleted and that is all. With one, CSRF is required, or any
// site could log the user out of everything.
func logout(a *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if c, err := r.Cookie(SessionCookie); err == nil {
			if !csrfOK(r) {
				writeError(w, r, errCSRF)
				return
			}
			if err := a.Logout(r.Context(), c.Value); err != nil {
				// The portal session is gone already; only Keycloak's is left
				// to time out, so the user is not told to retry.
				slog.ErrorContext(r.Context(), "logout left the Keycloak session running", "err", err, "request_id", requestID(r.Context()))
			}
		}
		setCookie(w, SessionCookie, "", -1, true)
		setCookie(w, CSRFCookie, "", -1, false)
		w.WriteHeader(http.StatusNoContent)
	}
}
