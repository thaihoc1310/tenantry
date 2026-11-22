package keycloak

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
)

// TestLiveKeycloak runs the client against a real Keycloak, as the service
// account of tenantry-sync, to check the fake answers the way Keycloak does
// (make keycloak-live-test, against the lab). Without KEYCLOAK_TEST_URL it is
// skipped. KEYCLOAK_TEST_USER_ID, a Keycloak user id, adds a membership check.
func TestLiveKeycloak(t *testing.T) {
	if os.Getenv("KEYCLOAK_TEST_URL") == "" {
		t.Skip("KEYCLOAK_TEST_URL not set")
	}
	t.Setenv("TENANTRY_KEYCLOAK_URL", os.Getenv("KEYCLOAK_TEST_URL"))
	t.Setenv("TENANTRY_KEYCLOAK_CLIENT_SECRET", os.Getenv("KEYCLOAK_TEST_CLIENT_SECRET"))
	cfg, err := config.LoadKeycloakSync()
	if err != nil {
		t.Fatal(err)
	}
	c, ctx := NewClient(cfg), context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "tenantry-live-test:" + hex.EncodeToString(b)

	id, err := c.EnsureGroup(ctx, name)
	if err != nil || id == "" {
		t.Fatalf("create: %q %v", id, err)
	}
	t.Cleanup(func() { _ = c.DeleteGroup(context.Background(), id) })
	if again, err := c.EnsureGroup(ctx, name); err != nil || again != id {
		t.Fatalf("ensure again: %q %v, want %q", again, err, id)
	}
	// exact=true: a prefix of the name is another group.
	if _, found, err := c.FindGroup(ctx, name[:len(name)-1]); err != nil || found {
		t.Fatalf("prefix found: %v %v", found, err)
	}

	nobody := "00000000-0000-4000-8000-" + hex.EncodeToString(b)
	if err := c.AddMember(ctx, nobody, id); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("add an unknown user: %v", err)
	}
	if err := c.RemoveMember(ctx, nobody, id); err != nil {
		t.Fatalf("remove an unknown user: %v", err)
	}
	if user := os.Getenv("KEYCLOAK_TEST_USER_ID"); user != "" {
		for range 2 {
			if err := c.AddMember(ctx, user, id); err != nil {
				t.Fatalf("add: %v", err)
			}
		}
		for range 2 {
			if err := c.RemoveMember(ctx, user, id); err != nil {
				t.Fatalf("remove: %v", err)
			}
		}
	}

	for range 2 {
		if err := c.DeleteGroup(ctx, id); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if _, found, err := c.FindGroup(ctx, name); err != nil || found {
		t.Fatalf("deleted group found: %v %v", found, err)
	}
}
