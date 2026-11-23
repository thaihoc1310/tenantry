package keycloak

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
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
// (architecture/06 §7): what the worker does still works, what would make it
// or anyone a Platform Admin does not (DENIED), and what Users manage brings
// along on plain users is listed as such (ALLOWED by design). Run by make
// keycloak-live-test once the permissions are applied (KEYCLOAK_TEST_FGAP=1),
// with a plain user (KEYCLOAK_TEST_USER_ID), a Platform Admin
// (KEYCLOAK_TEST_ADMIN_ID), the platform-admin role
// (KEYCLOAK_TEST_ADMIN_ROLE_ID), the group that keeps Platform Admins out of
// reach (KEYCLOAK_TEST_MARKER_ID), tenantry-sync's own service account user
// (KEYCLOAK_TEST_SA_ID), the realm-management client and its role
// manage-users (KEYCLOAK_TEST_RM_CLIENT_ID, KEYCLOAK_TEST_MANAGE_USERS_ROLE_ID).
// The ALLOWED cases run on a user the test creates and deletes, never on a
// person's account.
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
	env := map[string]string{}
	for _, k := range []string{"KEYCLOAK_TEST_USER_ID", "KEYCLOAK_TEST_ADMIN_ID", "KEYCLOAK_TEST_ADMIN_ROLE_ID", "KEYCLOAK_TEST_MARKER_ID",
		"KEYCLOAK_TEST_SA_ID", "KEYCLOAK_TEST_RM_CLIENT_ID", "KEYCLOAK_TEST_MANAGE_USERS_ROLE_ID"} {
		if env[k] = os.Getenv(k); env[k] == "" {
			t.Fatalf("%s is required", k)
		}
	}
	user, admin, role, marker := env["KEYCLOAK_TEST_USER_ID"], env["KEYCLOAK_TEST_ADMIN_ID"], env["KEYCLOAK_TEST_ADMIN_ROLE_ID"], env["KEYCLOAK_TEST_MARKER_ID"]
	sa, rm, manageUsers := env["KEYCLOAK_TEST_SA_ID"], env["KEYCLOAK_TEST_RM_CLIENT_ID"], env["KEYCLOAK_TEST_MANAGE_USERS_ROLE_ID"]

	// What the worker does still works on a plain user.
	if err := c.SetUserEnabled(ctx, user, true); err != nil {
		t.Fatalf("enable a plain user: %v", err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	suffix := hex.EncodeToString(b)
	group, err := c.EnsureGroup(ctx, "tenantry-live-test:"+suffix)
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

	type step struct {
		what, method, path string
		body               any
	}
	expect := func(want int, s step) {
		t.Helper()
		resp, err := c.admin(ctx, s.method, s.path, s.body)
		if err != nil {
			t.Fatalf("%s: %v", s.what, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", s.what, resp.StatusCode, want)
		} else {
			t.Logf("%s: %d", s.what, resp.StatusCode)
		}
	}
	password := func() map[string]any {
		return map[string]any{"type": "password", "value": "Pw-" + suffix, "temporary": true}
	}

	// DENIED: each must be 403.
	for _, s := range []step{
		{"grant platform-admin", "POST", "/users/" + user + "/role-mappings/realm", []map[string]string{{"id": role, "name": "platform-admin"}}},
		{"impersonate a plain user", "POST", "/users/" + user + "/impersonation", nil},
		{"change a Platform Admin's email", "PUT", "/users/" + admin, map[string]any{"email": "taken-over@example.com"}},
		{"disable a Platform Admin", "PUT", "/users/" + admin, map[string]any{"enabled": false}},
		{"put a Platform Admin in a group", "PUT", "/users/" + admin + "/groups/" + group, nil},
		{"reset a Platform Admin's password", "PUT", "/users/" + admin + "/reset-password", password()},
		// The group keeping Platform Admins out of reach cannot go away.
		{"delete the Platform Admins' group", "DELETE", "/groups/" + marker, nil},
		{"rename the Platform Admins' group", "PUT", "/groups/" + marker, map[string]any{"name": "renamed"}},
		{"take a Platform Admin out of the group", "DELETE", "/users/" + admin + "/groups/" + marker, nil},
		{"create a client", "POST", "/clients", map[string]any{"clientId": "tenantry-live-test-" + suffix}},
		{"change the realm", "PUT", "", map[string]any{"displayName": "taken over"}},
		// tenantry-sync cannot give itself a role back.
		{"grant itself platform-admin", "POST", "/users/" + sa + "/role-mappings/realm", []map[string]string{{"id": role, "name": "platform-admin"}}},
		{"grant itself manage-users", "POST", "/users/" + sa + "/role-mappings/clients/" + rm, []map[string]string{{"id": manageUsers, "name": "manage-users"}}},
	} {
		expect(403, s)
	}

	// ALLOWED by design: Users manage, which turning accounts on and off
	// needs, also lets tenantry-sync take over a plain user's account
	// (architecture/06 §7, known limitations of M12). Platform Admins stay out
	// of reach (DENIED above). Run on a user made for the test.
	resp, err := c.admin(ctx, "POST", "/users", map[string]any{"username": "tenantry-live-test-" + suffix, "enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != 201 || !strings.Contains(loc, "/users/") {
		t.Fatalf("create a test user: %d %q", resp.StatusCode, loc)
	}
	plain := loc[strings.LastIndex(loc, "/")+1:]
	t.Cleanup(func() {
		if resp, err := c.admin(context.Background(), "DELETE", "/users/"+plain, nil); err == nil {
			_ = resp.Body.Close()
		}
	})
	for _, s := range []step{
		// Known limitation: a plain user's password can be reset ...
		{"reset a plain user's password", "PUT", "/users/" + plain + "/reset-password", password()},
		// ... or set through a plain update of the user ...
		{"set a plain user's password with an update", "PUT", "/users/" + plain, map[string]any{"credentials": []any{password()}}},
		// ... and the email changed, which opens the forgot-password flow.
		{"change a plain user's email", "PUT", "/users/" + plain, map[string]any{"email": "tenantry-live-test-" + suffix + "@example.com"}},
	} {
		expect(204, s)
	}
}
