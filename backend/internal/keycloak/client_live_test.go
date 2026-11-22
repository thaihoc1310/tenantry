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

// TestLiveKeycloakNarrowed checks, as tenantry-sync, what the fine-grained
// admin permissions of deploy/cluster/keycloak-fgap.py leave it
// (architecture/06 §7): enabling a person's account still works, and none of
// what would make it a Platform Admin does. Run by make keycloak-live-test
// once the permissions are applied (KEYCLOAK_TEST_FGAP=1), with a plain user
// (KEYCLOAK_TEST_USER_ID), a Platform Admin (KEYCLOAK_TEST_ADMIN_ID), the id
// of the platform-admin role (KEYCLOAK_TEST_ADMIN_ROLE_ID) and of the group
// that keeps Platform Admins out of reach (KEYCLOAK_TEST_MARKER_ID).
func TestLiveKeycloakNarrowed(t *testing.T) {
	if os.Getenv("KEYCLOAK_TEST_URL") == "" || os.Getenv("KEYCLOAK_TEST_FGAP") == "" {
		t.Skip("KEYCLOAK_TEST_URL or KEYCLOAK_TEST_FGAP not set")
	}
	t.Setenv("TENANTRY_KEYCLOAK_URL", os.Getenv("KEYCLOAK_TEST_URL"))
	t.Setenv("TENANTRY_KEYCLOAK_CLIENT_SECRET", os.Getenv("KEYCLOAK_TEST_CLIENT_SECRET"))
	cfg, err := config.LoadKeycloakSync()
	if err != nil {
		t.Fatal(err)
	}
	c, ctx := NewClient(cfg), context.Background()
	user, admin, role := os.Getenv("KEYCLOAK_TEST_USER_ID"), os.Getenv("KEYCLOAK_TEST_ADMIN_ID"), os.Getenv("KEYCLOAK_TEST_ADMIN_ROLE_ID")
	marker := os.Getenv("KEYCLOAK_TEST_MARKER_ID")
	if user == "" || admin == "" || role == "" || marker == "" {
		t.Fatal("KEYCLOAK_TEST_USER_ID, KEYCLOAK_TEST_ADMIN_ID, KEYCLOAK_TEST_ADMIN_ROLE_ID and KEYCLOAK_TEST_MARKER_ID are required")
	}

	// What the worker does still works on a plain user.
	if err := c.SetUserEnabled(ctx, user, true); err != nil {
		t.Fatalf("enable a plain user: %v", err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	group, err := c.EnsureGroup(ctx, "tenantry-live-test:"+hex.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.DeleteGroup(context.Background(), group) })
	if err := c.AddMember(ctx, user, group); err != nil {
		t.Fatalf("add a plain user: %v", err)
	}
	if err := c.RemoveMember(ctx, user, group); err != nil {
		t.Fatalf("remove a plain user: %v", err)
	}

	// None of this: each must be 403.
	for _, step := range []struct {
		what, method, path string
		body               any
	}{
		{"reset a plain user's password", "PUT", "/users/" + user + "/reset-password", map[string]any{"type": "password", "value": "Pw-" + hex.EncodeToString(b), "temporary": true}},
		{"grant platform-admin", "POST", "/users/" + user + "/role-mappings/realm", []map[string]string{{"id": role, "name": "platform-admin"}}},
		{"impersonate a plain user", "POST", "/users/" + user + "/impersonation", nil},
		{"change a Platform Admin's email", "PUT", "/users/" + admin, map[string]any{"email": "taken-over@example.com"}},
		{"disable a Platform Admin", "PUT", "/users/" + admin, map[string]any{"enabled": false}},
		{"put a Platform Admin in a group", "PUT", "/users/" + admin + "/groups/" + group, nil},
		{"reset a Platform Admin's password", "PUT", "/users/" + admin + "/reset-password", map[string]any{"type": "password", "value": "Pw-" + hex.EncodeToString(b), "temporary": true}},
		// The group keeping Platform Admins out of reach cannot go away.
		{"delete the Platform Admins' group", "DELETE", "/groups/" + marker, nil},
		{"rename the Platform Admins' group", "PUT", "/groups/" + marker, map[string]any{"name": "renamed"}},
		{"take a Platform Admin out of the group", "DELETE", "/users/" + admin + "/groups/" + marker, nil},
		{"create a client", "POST", "/clients", map[string]any{"clientId": "tenantry-live-test-" + hex.EncodeToString(b)}},
		{"change the realm", "PUT", "", map[string]any{"displayName": "taken over"}},
	} {
		resp, err := c.admin(ctx, step.method, step.path, step.body)
		if err != nil {
			t.Fatalf("%s: %v", step.what, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("%s: %d, want 403", step.what, resp.StatusCode)
		} else {
			t.Logf("%s: 403", step.what)
		}
	}
}
