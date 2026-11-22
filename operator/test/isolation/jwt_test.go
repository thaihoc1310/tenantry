//go:build lab

package isolation

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestTokenSize (IMPLEMENTATION-PLAN M11 item 3): a user in about 20 groups
// gets a kubectl token with all of them, and every hop takes it: kube-apiserver
// directly, Capsule Proxy behind Traefik, Headlamp. A much bigger header shows
// how much room is left.
func TestTokenSize(t *testing.T) {
	adm := adminPortal(t)
	owner := signUp(t)
	t.Cleanup(func() {
		var users struct {
			Items []struct{ ID string } `json:"items"`
		}
		adm.api(t, http.MethodGet, "/admin/users?q="+owner.Email, nil, &users)
		if len(users.Items) == 1 {
			adm.api(t, http.MethodPost, "/admin/users/"+users.Items[0].ID+":disable", nil, nil)
		}
	})
	slug := "optest" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomString(4)))
	var org struct {
		Organization struct{ ID string } `json:"organization"`
	}
	if code := adm.api(t, http.MethodPost, "/admin/organizations", map[string]any{
		"slug": slug, "displayName": "Optest " + slug, "description": "Token size test.", "ownerEmail": owner.Email,
		"entitlement": map[string]any{"maxMembers": 5, "maxGroups": 25, "maxWorkspacesTotal": 1},
		"clusterGrant": map[string]any{
			"clusterId": "cluster-a", "maxWorkspaces": 1, "maxNamespacesTotal": 1,
			"resources":     map[string]any{"requestsCpu": "1", "requestsMemory": "1Gi", "limitsCpu": "1", "limitsMemory": "1Gi", "pods": 2, "requestsStorage": "1Gi"},
			"sharedPoolIds": []string{"shared-general"}, "networkPoolIds": []string{"onboarding-pod-network"}, "allowedStorageClasses": []string{"standard"},
		},
	}, &org); code != http.StatusCreated {
		t.Fatalf("create Organization: %d", code)
	}
	t.Cleanup(func() { owner.api(t, http.MethodDelete, "/organizations/"+org.Organization.ID, nil, nil) })

	var me struct{ ID string }
	owner.api(t, http.MethodGet, "/me", nil, &me)
	const extra = 20
	for i := 1; i <= extra; i++ {
		var g struct{ ID string }
		if code := owner.api(t, http.MethodPost, "/organizations/"+slug+"/groups", map[string]any{"name": fmt.Sprintf("team-%02d", i)}, &g); code != http.StatusCreated {
			t.Fatalf("create group %d: %d", i, code)
		}
		if code := owner.api(t, http.MethodPut, "/groups/"+g.ID+"/members/"+me.ID, nil, nil); code >= 300 {
			t.Fatalf("join group %d: %d", i, code)
		}
	}
	waitFor(t, "groups and memberships synced to Keycloak", 5*time.Minute, func() (bool, error) {
		var groups struct {
			Items []struct {
				Name, SyncStatus string
				Members          []struct{ SyncStatus string } `json:"members"`
			} `json:"items"`
		}
		owner.api(t, http.MethodGet, "/organizations/"+slug+"/groups", nil, &groups)
		synced := 0
		for _, g := range groups.Items {
			ok := g.SyncStatus == "Synced"
			for _, m := range g.Members {
				ok = ok && (m.SyncStatus == "" || m.SyncStatus == "Synced")
			}
			if ok && strings.HasPrefix(g.Name, "team-") {
				synced++
			}
		}
		return synced == extra, nil
	})

	token := owner.kubectlToken(t)
	groups, _ := claims(t, token)["groups"].([]any)
	t.Logf("TOKEN groups=%d jwt_bytes=%d authorization_header_bytes=%d", len(groups), len(token), len("Bearer ")+len(token))
	if len(groups) < extra {
		t.Fatalf("the token has %d groups, want at least %d", len(groups), extra)
	}

	review := `{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview"}`
	call := func(name, base, path string, c *http.Client, padding int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, base+path, strings.NewReader(review))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if padding > 0 {
			req.Header.Set("X-Optest-Padding", strings.Repeat("a", padding))
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		seen := bytes.Count(body, []byte(`"oidc:`))
		t.Logf("HOP %s padding=%d status=%d oidc_names_in_answer=%d", name, padding, resp.StatusCode, seen)
		if padding == 0 && (resp.StatusCode != http.StatusCreated || seen < extra) {
			t.Errorf("%s: status %d, %d oidc: names in %s", name, resp.StatusCode, seen, firstLine(string(body)))
		}
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(cfg.CAData)
	direct := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	public := &http.Client{Timeout: 15 * time.Second}
	const reviewPath = "/apis/authentication.k8s.io/v1/selfsubjectreviews"
	for _, padding := range []int{0, 16 << 10, 64 << 10} {
		call("kube-apiserver", cfg.Host, reviewPath, direct, padding)
		call("Traefik → Capsule Proxy", "https://"+envOr("KUBE_HOST", "kube-a.tenantry.site"), reviewPath, public, padding)
		call("Headlamp", "https://"+envOr("CONSOLE_HOST", "console-a.tenantry.site"), "/clusters/cluster-a"+reviewPath, public, padding)
	}
}
