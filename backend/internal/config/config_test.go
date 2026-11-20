package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
)

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "TENANTRY_ADDR", "TENANTRY_AUTH_DEV_HEADER", "TENANTRY_LOG_LEVEL", "TENANTRY_LOG_FORMAT"} {
		t.Setenv(k, env[k])
	}
}

func TestDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x"})
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{DatabaseURL: "postgres://x", Addr: ":8080", LogLevel: slog.LevelInfo, LogFormat: "json"}
	if c != want {
		t.Fatalf("got %+v, want %+v", c, want)
	}
}

func TestExplicitValues(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": "postgres://x", "TENANTRY_ADDR": "127.0.0.1:9000", "TENANTRY_AUTH_DEV_HEADER": "true",
		"TENANTRY_LOG_LEVEL": "debug", "TENANTRY_LOG_FORMAT": "text",
	})
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:9000" || !c.DevAuth || c.LogLevel != slog.LevelDebug || c.LogFormat != "text" {
		t.Fatalf("got %+v", c)
	}
}

// Every bad variable is reported, not only the first, and a typo never falls
// back to a default (e.g. "yes" must not quietly mean dev auth off).
func TestInvalidValuesAreAllReported(t *testing.T) {
	setEnv(t, map[string]string{"TENANTRY_AUTH_DEV_HEADER": "yes", "TENANTRY_LOG_LEVEL": "verbose", "TENANTRY_LOG_FORMAT": "xml"})
	_, err := config.Load()
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"DATABASE_URL", "TENANTRY_AUTH_DEV_HEADER", "TENANTRY_LOG_LEVEL", "TENANTRY_LOG_FORMAT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}
