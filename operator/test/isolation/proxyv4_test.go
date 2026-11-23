//go:build lab

package isolation

import (
	"net/http"
	"os"
	"testing"
	"time"
)

// TestProxyRequestAsDemoUser sends one request through Capsule Proxy as the
// demo account alice, with the ID token the kubectl client gets (as
// kubelogin would), so the proxy's log at level 4 shows how it calls the API
// server for a real tenant (docs/adr/0013). It runs only with PROXY_V4=1:
// it logs in as a demo account. The password is read from the Secret and
// kept in memory; neither it nor the token is logged.
func TestProxyRequestAsDemoUser(t *testing.T) {
	if os.Getenv("PROXY_V4") != "1" {
		t.Skip("set PROXY_V4=1: logs in as the demo account alice")
	}
	alice := logIn(t, "alice", secret(t, "keycloak", "keycloak-realm", "DEMO_USER_PASSWORD"))
	token := alice.kubectlToken(t)
	t.Logf("alice's sub: %v, groups in the token: %v", claims(t, token)["sub"], claims(t, token)["groups"])
	req, _ := http.NewRequest(http.MethodGet, "https://"+envOr("KUBE_HOST", "kube-a.tenantry.site")+"/api/v1/namespaces/default/pods", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("through Capsule Proxy: %v", err)
	}
	resp.Body.Close()
	// alice has no right in default: a 403 from the API server is the answer
	// wanted, it shows the request was forwarded as her.
	t.Logf("GET pods in default through Capsule Proxy as alice: %s", resp.Status)
}
