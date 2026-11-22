package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
)

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "TENANTRY_ADDR", "TENANTRY_LOG_LEVEL", "TENANTRY_LOG_FORMAT", "TENANTRY_TRUSTED_PROXIES", "TENANTRY_METRICS_ADDR"} {
		t.Setenv(k, env[k])
	}
}

func TestDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x"})
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{DatabaseURL: "postgres://x", Addr: ":8080", MetricsAddr: ":9090", LogLevel: slog.LevelInfo, LogFormat: "json"}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v, want %+v", c, want)
	}
}

func TestExplicitValues(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": "postgres://x", "TENANTRY_ADDR": "127.0.0.1:9000",
		"TENANTRY_LOG_LEVEL": "debug", "TENANTRY_LOG_FORMAT": "text",
		"TENANTRY_TRUSTED_PROXIES": " 192.168.0.0/16, 10.1.2.3 ,::ffff:10.9.9.9,192.168.7.7/16,",
	})
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:9000" || c.LogLevel != slog.LevelDebug || c.LogFormat != "text" {
		t.Fatalf("got %+v", c)
	}
	// Bare addresses become single-address prefixes; host bits are dropped.
	if got := fmt.Sprint(c.TrustedProxies); got != "[192.168.0.0/16 10.1.2.3/32 10.9.9.9/32 192.168.0.0/16]" {
		t.Fatalf("trusted proxies %s", got)
	}
}

// Every bad variable is reported, not only the first, and a typo never falls
// back to a default (e.g. "verbose" must not quietly mean info).
func TestInvalidValuesAreAllReported(t *testing.T) {
	setEnv(t, map[string]string{"TENANTRY_LOG_LEVEL": "verbose", "TENANTRY_LOG_FORMAT": "xml", "TENANTRY_TRUSTED_PROXIES": "traefik", "TENANTRY_METRICS_ADDR": "9090"})
	_, err := config.Load()
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"DATABASE_URL", "TENANTRY_LOG_LEVEL", "TENANTRY_LOG_FORMAT", "TENANTRY_TRUSTED_PROXIES", "TENANTRY_METRICS_ADDR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

// 32 bytes of 0x01, base64.
const testKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

func setAuthEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"TENANTRY_OIDC_ISSUER", "TENANTRY_OIDC_CLIENT_ID", "TENANTRY_OIDC_CLIENT_SECRET", "TENANTRY_PUBLIC_URL", "TENANTRY_SESSION_KEY"} {
		t.Setenv(k, env[k])
	}
}

func TestLoadAuth(t *testing.T) {
	setAuthEnv(t, map[string]string{
		"TENANTRY_OIDC_ISSUER":        "https://auth.tenantry.site/realms/tenantry",
		"TENANTRY_OIDC_CLIENT_SECRET": "s3cret",
		"TENANTRY_PUBLIC_URL":         "https://portal.tenantry.site/",
		"TENANTRY_SESSION_KEY":        testKey,
	})
	a, err := config.LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if a.ClientID != "portal" || a.PublicURL != "https://portal.tenantry.site" || !bytes.Equal(a.SessionKey, bytes.Repeat([]byte{1}, 32)) {
		t.Fatalf("got %+v", a)
	}

	// The Vite dev server.
	t.Setenv("TENANTRY_PUBLIC_URL", "http://localhost:5173")
	if _, err := config.LoadAuth(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAuthRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"nothing set": {},
		"plain http to a remote host": {
			"TENANTRY_OIDC_ISSUER": "http://auth.tenantry.site/realms/tenantry", "TENANTRY_PUBLIC_URL": "http://portal.tenantry.site",
		},
		"public URL with a path": {"TENANTRY_PUBLIC_URL": "https://portal.tenantry.site/app"},
		"relative issuer":        {"TENANTRY_OIDC_ISSUER": "/realms/tenantry"},
		"short key":              {"TENANTRY_SESSION_KEY": "AQEB"},
		"key not base64":         {"TENANTRY_SESSION_KEY": "not base64!"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			full := map[string]string{
				"TENANTRY_OIDC_ISSUER": "https://auth.tenantry.site/realms/tenantry", "TENANTRY_OIDC_CLIENT_SECRET": "s3cret",
				"TENANTRY_PUBLIC_URL": "https://portal.tenantry.site", "TENANTRY_SESSION_KEY": testKey,
			}
			if len(env) == 0 {
				full = env
			}
			for k, v := range env {
				full[k] = v
			}
			setAuthEnv(t, full)
			_, err := config.LoadAuth()
			if err == nil {
				t.Fatal("no error")
			}
			if strings.Contains(err.Error(), "not base64!") {
				t.Fatalf("the session key leaked into the error: %v", err)
			}
		})
	}
}

func TestLoadKeycloakSync(t *testing.T) {
	t.Setenv("TENANTRY_KEYCLOAK_URL", "http://keycloak-http.keycloak.svc/")
	t.Setenv("TENANTRY_KEYCLOAK_REALM", "")
	t.Setenv("TENANTRY_KEYCLOAK_CLIENT_ID", "")
	t.Setenv("TENANTRY_KEYCLOAK_CLIENT_SECRET", "s3cret")
	k, err := config.LoadKeycloakSync()
	if err != nil || k.URL != "http://keycloak-http.keycloak.svc" || k.Realm != "tenantry" || k.ClientID != "tenantry-sync" {
		t.Fatalf("got %+v, %v", k, err)
	}
	for _, bad := range []string{"", "keycloak-http", "ftp://keycloak", "https://auth.tenantry.site/realms/tenantry"} {
		t.Setenv("TENANTRY_KEYCLOAK_URL", bad)
		if _, err := config.LoadKeycloakSync(); err == nil {
			t.Errorf("URL %q accepted", bad)
		}
	}
	t.Setenv("TENANTRY_KEYCLOAK_URL", "http://keycloak")
	t.Setenv("TENANTRY_KEYCLOAK_CLIENT_SECRET", "")
	if _, err := config.LoadKeycloakSync(); err == nil {
		t.Error("missing client secret accepted")
	}
}

func TestMetricsOff(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x", "TENANTRY_METRICS_ADDR": "off"})
	if c, err := config.Load(); err != nil || c.MetricsAddr != "" {
		t.Fatalf("off: %q %v", c.MetricsAddr, err)
	}
}
