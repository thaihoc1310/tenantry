package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := New(nil, config.Auth{SessionKey: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A login state is good for LoginTTL and only as a login state.
func TestLoginStateLifetime(t *testing.T) {
	s := newTestService(t)
	start := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	b, _ := json.Marshal(loginState{State: "st", Started: start.Unix()})
	sealed := base64.RawURLEncoding.EncodeToString(s.seal(b, []byte(loginAAD)))

	for _, c := range []struct {
		after time.Duration
		ok    bool
	}{{0, true}, {LoginTTL, true}, {LoginTTL + time.Second, false}, {-time.Minute, false}} {
		s.now = func() time.Time { return start.Add(c.after) }
		if _, err := s.openLogin(sealed); (err == nil) != c.ok {
			t.Errorf("after %s: err %v, want ok=%v", c.after, err, c.ok)
		}
	}

	// The same bytes sealed for another purpose (a refresh token) are not a login state.
	s.now = func() time.Time { return start }
	other := base64.RawURLEncoding.EncodeToString(s.seal(b, []byte("refresh")))
	if _, err := s.openLogin(other); err == nil {
		t.Fatal("accepted a ciphertext sealed for another purpose")
	}
}

// A refresh token sealed for one session row does not open for another, so
// copying ciphertext between rows gains nothing.
func TestSealBindsToItsUse(t *testing.T) {
	s := newTestService(t)
	sealed := s.seal([]byte("refresh-token"), []byte("session-a"))
	if got, err := s.open(sealed, []byte("session-a")); err != nil || string(got) != "refresh-token" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := s.open(sealed, []byte("session-b")); err == nil {
		t.Fatal("opened with another session's binding")
	}
	otherKey, err := New(nil, config.Auth{SessionKey: bytes.Repeat([]byte{8}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherKey.open(sealed, []byte("session-a")); err == nil {
		t.Fatal("opened with another key")
	}
	if _, err := s.open([]byte("short"), nil); err == nil {
		t.Fatal("opened garbage")
	}
}

func TestSafeReturnTo(t *testing.T) {
	for in, want := range map[string]string{
		"/":                  "/",
		"/orgs/acme?tab=x#y": "/orgs/acme?tab=x#y",
		"/authors":           "/authors",
		"/auth":              "/",
		"/auth/callback?x=1": "/",
		"//evil.example":     "/",
		"/\\evil.example":    "/",
		"/x\r\nSet-Cookie:":  "/",
		"javascript:alert()": "/",
		"https://evil/":      "/",
		"":                   "/",
	} {
		if got := SafeReturnTo(in); got != want {
			t.Errorf("SafeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
