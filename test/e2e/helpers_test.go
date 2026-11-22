package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func readAll(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func inCIDR(ip, cidr string) bool {
	a, err1 := netip.ParseAddr(ip)
	p, err2 := netip.ParsePrefix(cidr)
	return err1 == nil && err2 == nil && p.Contains(a)
}

func mustAdmin(t *testing.T, args ...string) string {
	t.Helper()
	out, err := admin(t, "", args...)
	if err != nil {
		t.Fatalf("kubectl %v: %s", args, out)
	}
	return out
}

// waitOperation waits for an operation to end, for cleanups: no testing.T.
func waitOperation(s *session, id string) {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		_, raw := s.call(http.MethodGet, "/operations/"+id, nil)
		var op struct{ State string }
		_ = json.Unmarshal(raw, &op)
		if op.State == "Succeeded" || op.State == "Failed" || op.State == "Blocked" {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// waitOperations waits for the operations an answer lists.
func waitOperations(s *session, code int, raw []byte) {
	if code >= 300 {
		return
	}
	var out struct {
		Operation  *struct{ ID string }  `json:"operation"`
		Operations []struct{ ID string } `json:"operations"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Operation != nil {
		waitOperation(s, out.Operation.ID)
	}
	for _, op := range out.Operations {
		waitOperation(s, op.ID)
	}
}

// setPoolNodes gives shared-general the nodes nodes() returns, through the
// admin API, and waits for it; for the scenario and its cleanup alike.
func setPoolNodes(adm *session, nodes func([]string) []string) error {
	_, raw := adm.call(http.MethodGet, "/admin/clusters/cluster-a/shared-pools", nil)
	var pools struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &pools); err != nil {
		return err
	}
	for _, p := range pools.Items {
		if p["id"] != "shared-general" {
			continue
		}
		var current []string
		for _, n := range p["nodes"].([]any) {
			current = append(current, n.(string))
		}
		want := nodes(slices.Clone(current))
		slices.Sort(want)
		slices.Sort(current)
		if slices.Equal(want, current) {
			return nil
		}
		code, raw := adm.call(http.MethodPut, "/admin/clusters/cluster-a/shared-pools/shared-general", map[string]any{
			"name": p["name"], "allocationOvercommitRatio": p["allocationOvercommitRatio"], "systemReserve": p["systemReserve"], "nodes": want,
		})
		if code != 200 {
			return fmt.Errorf("set the nodes of shared-general: %d %s", code, firstLine(string(raw)))
		}
		waitOperations(adm, code, raw)
		return nil
	}
	return fmt.Errorf("no shared pool shared-general")
}

// raiseGrant: maxWorkspaces 4, maxNamespacesTotal 10, requestsStorage 50Gi;
// CPU, memory and Pods stay, Dedicated does not count against them.
func raiseGrant(t *testing.T, adm *session, orgID string) {
	t.Helper()
	var detail struct {
		ClusterGrants []map[string]any `json:"clusterGrants"`
	}
	adm.api(t, http.MethodGet, "/admin/organizations/"+orgID, nil, &detail, 200)
	if len(detail.ClusterGrants) != 1 {
		t.Fatalf("the Organization has %d cluster grants", len(detail.ClusterGrants))
	}
	g := detail.ClusterGrants[0]
	resources, _ := g["resources"].(map[string]any)
	resources["requestsStorage"] = "50Gi"
	in := map[string]any{
		"clusterId": "cluster-a", "maxWorkspaces": 4, "maxNamespacesTotal": 10, "resources": resources,
		"sharedPoolIds": g["sharedPoolIds"], "networkPoolIds": g["networkPoolIds"], "allowedStorageClasses": g["allowedStorageClasses"],
	}
	adm.api(t, http.MethodPut, "/admin/organizations/"+orgID+"/cluster-grants/cluster-a", in, nil, 200, 201)
}

func without(s []string, x string) []string {
	var out []string
	for _, v := range s {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func appendMissing(s []string, x string) []string {
	if slices.Contains(s, x) {
		return s
	}
	return append(s, x)
}

var _ = strings.TrimSpace
