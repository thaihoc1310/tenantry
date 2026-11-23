// Package e2e runs the persona scenario of PRODUCT-PLAN §24.3 against the
// lab: throwaway accounts (optest-alice-*, optest-joe-*, optest-bob-*) go
// through the portal's API and kubectl as people would. Run it with
// make e2e-personas. Headlamp's SSO needs a browser: the web e2e suite
// (Playwright) checks it, not this one.
package e2e

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

const dedicatedNode = "fdp-k8s-wk3"

// cleanups run in reverse order at the end, each bounded, whatever failed:
// registered right after what they undo is made.
type cleanups struct {
	t     *testing.T
	steps []func()
}

func (c *cleanups) add(what string, f func()) { c.addWithin(what, 6*time.Minute, f) }

func (c *cleanups) addWithin(what string, limit time.Duration, f func()) {
	c.steps = append(c.steps, func() {
		done := make(chan struct{})
		go func() { defer close(done); f() }()
		select {
		case <-done:
			c.t.Logf("cleanup: %s", what)
		case <-time.After(limit):
			c.t.Errorf("cleanup: %s timed out", what)
		}
	})
}

func (c *cleanups) run() {
	for i := len(c.steps) - 1; i >= 0; i-- {
		c.steps[i]()
	}
}

func step(t *testing.T, format string, args ...any) { t.Logf("STEP "+format, args...) }

type group struct{ ID, Name, MachineName, SyncStatus string }

func groups(t *testing.T, s *session, org string) map[string]group {
	t.Helper()
	var out struct{ Items []group }
	s.api(t, http.MethodGet, "/organizations/"+org+"/groups", nil, &out, 200)
	m := map[string]group{}
	for _, g := range out.Items {
		m[g.Name] = g
	}
	return m
}

func waitSynced(t *testing.T, s *session, org string, names ...string) map[string]group {
	t.Helper()
	var gs map[string]group
	until(t, "groups "+strings.Join(names, ", ")+" in Keycloak", 4*time.Minute, func() bool {
		gs = groups(t, s, org)
		for _, n := range names {
			if gs[n].SyncStatus != "Synced" {
				return false
			}
		}
		return true
	})
	return gs
}

var invitationToken = regexp.MustCompile(`invitations/([A-Za-z0-9_-]+)`)

type workspace struct {
	Workspace struct {
		ID, K8sName string
		Network     *struct{ CIDR string }
	}
	Operation struct{ ID string }
}

func TestPersonas(t *testing.T) {
	c := &cleanups{t: t}
	defer c.run()
	adm := logIn(t, "admin", secretValue(t, "keycloak", "keycloak-realm", "PLATFORM_ADMIN_PASSWORD"))

	// Alice, Joe and Bob sign up; the accounts are disabled at the end.
	alice, joe, bob := signUp(t, "alice"), signUp(t, "joe"), signUp(t, "bob")
	for _, p := range []*person{alice, joe, bob} {
		c.add("disable "+p.Username, func() { adm.call(http.MethodPost, "/admin/users/"+p.ID+":disable", nil) })
	}

	step(t, "Alice asks for an Organization; the Platform Admin approves it with the default grant")
	slug := "optest" + alnum(5)
	var org struct{ ID string }
	alice.api(t, http.MethodPost, "/organizations", map[string]any{"slug": slug, "displayName": "Optest Acme " + slug, "description": "Persona e2e."}, &org, 201)
	// A deleted Workspace keeps its row while its CIDR is in quarantine (10
	// minutes), and the Organization cannot go before it: try again.
	c.addWithin("delete Organization "+slug, 21*time.Minute, func() {
		for end := time.Now().Add(20 * time.Minute); time.Now().Before(end); time.Sleep(15 * time.Second) {
			if code, _ := alice.call(http.MethodDelete, "/organizations/"+org.ID, nil); code == 204 || code == 404 {
				return
			}
		}
	})
	var policy struct {
		Entitlement  map[string]any `json:"entitlement"`
		ClusterGrant map[string]any `json:"clusterGrant"`
	}
	adm.api(t, http.MethodGet, "/admin/onboarding-policy", nil, &policy, 200)
	policy.ClusterGrant["clusterId"] = "cluster-a"
	adm.api(t, http.MethodPost, "/admin/organizations/"+org.ID+":approve", map[string]any{"entitlement": policy.Entitlement, "clusterGrant": policy.ClusterGrant}, nil, 200)
	gs := waitSynced(t, alice.session, slug, "admins", "developers", "viewers")

	step(t, "Alice invites Joe and Bob; they accept from their own accounts")
	for _, p := range []*person{joe, bob} {
		var inv struct{ Link string }
		alice.api(t, http.MethodPost, "/organizations/"+org.ID+"/invitations", map[string]any{"email": p.Email, "role": "OrgMember"}, &inv, 201)
		m := invitationToken.FindStringSubmatch(inv.Link)
		if m == nil {
			t.Fatalf("no token in the invitation link %q", inv.Link)
		}
		p.api(t, http.MethodPost, "/invitations/"+m[1]+":accept", nil, nil, 200)
	}

	step(t, "Alice puts Joe in developers, Bob in viewers, and makes groups backend and qa")
	alice.api(t, http.MethodPut, "/groups/"+gs["developers"].ID+"/members/"+joe.ID, nil, nil, 204)
	alice.api(t, http.MethodPut, "/groups/"+gs["viewers"].ID+"/members/"+bob.ID, nil, nil, 204)
	for _, n := range []string{"backend", "qa"} {
		alice.api(t, http.MethodPost, "/organizations/"+org.ID+"/groups", map[string]any{"name": n}, nil, 201)
	}
	gs = waitSynced(t, alice.session, slug, "admins", "developers", "viewers", "backend", "qa")

	step(t, "Alice makes the Shared Workspace Solar, 2 CPU / 4Gi requests, 4 CPU / 8Gi limits, network auto-allocated")
	bindings := []any{
		map[string]any{"groupId": gs["admins"].ID, "role": "TenantOwner"},
		map[string]any{"groupId": gs["developers"].ID, "role": "Developer"},
		map[string]any{"groupId": gs["viewers"].ID, "role": "Viewer"},
	}
	var solar workspace
	alice.api(t, http.MethodPost, "/organizations/"+slug+"/workspaces", map[string]any{
		"slug": "solar", "displayName": "Solar", "clusterId": "cluster-a",
		"compute":   map[string]any{"mode": "Shared", "sharedPoolId": "shared-general"},
		"resources": map[string]any{"requestsCpu": "2", "requestsMemory": "4Gi", "limitsCpu": "4", "limitsMemory": "8Gi", "pods": 10, "requestsStorage": "5Gi", "namespaceQuota": 3},
		"bindings":  bindings,
	}, &solar, 202)
	c.add("delete Workspace solar", func() { deleteWorkspace(alice.session, solar.Workspace.ID, "solar") })
	alice.operation(t, solar.Operation.ID)

	step(t, "Alice makes Namespace frontend on the portal")
	var nsChange struct{ Operation struct{ ID string } }
	alice.api(t, http.MethodPost, "/workspaces/"+solar.Workspace.ID+"/namespaces", map[string]any{"name": "frontend"}, &nsChange, 202)
	alice.operation(t, nsChange.Operation.ID)
	frontend := solar.Workspace.K8sName + "-frontend"

	step(t, "Joe downloads his kubeconfig; kubectl goes through kube-a")
	kubeconfig := download(t, joe.session, "/workspaces/"+solar.Workspace.ID+"/kubeconfig")
	for _, want := range []string{"server: https://kube-a.", "oidc-login", "client-id=kubectl"} {
		if !strings.Contains(kubeconfig, want) {
			t.Errorf("Joe's kubeconfig lacks %q", want)
		}
	}
	joeToken := joe.kubectlToken(t)
	joeKube := kubeFor(t, "Joe", kubeconfig, joeToken)
	if out := joeKube.may(t, "list the Namespaces he sees", "", "get", "namespaces", "-o", "name"); !strings.Contains(out, frontend) {
		t.Errorf("Joe does not see %s through the proxy: %s", frontend, out)
	}

	step(t, "Joe installs deploy/demo-app (Deployment, PVC on standard, Service) in %s", frontend)
	if out, err := joeKube.helm("install", "web", "../../deploy/demo-app", "-n", frontend, "--wait", "--timeout", "4m"); err != nil {
		t.Fatalf("Joe's helm install: %s", out)
	}
	joeKube.may(t, "see the PVC bound", "", "-n", frontend, "wait", "pvc/web-data", "--for=jsonpath={.status.phase}=Bound", "--timeout=2m")

	step(t, "Alice makes a Route for the app; its https URL answers")
	var route struct {
		ID, URL, State, Message string
	}
	var change struct {
		Route struct {
			ID, URL, State, Message string
		}
		Operation struct{ ID string }
	}
	alice.api(t, http.MethodPost, "/workspaces/"+solar.Workspace.ID+"/routes", map[string]any{"namespace": frontend, "name": "web", "service": "web", "port": 80}, &change, 202)
	route.ID, route.URL = change.Route.ID, change.Route.URL
	alice.operation(t, change.Operation.ID)
	defer func() {
		if t.Failed() && route.State != "Active" {
			cr, _ := admin(t, "", "get", "platformworkspace", solar.Workspace.K8sName, "-o",
				"jsonpath=generation={.metadata.generation} observed={.status.observedGeneration} phase={.status.phase} routes={.status.routes} conditions={.status.conditions}")
			t.Logf("Route web: state %q message %q; the CR: %s", route.State, route.Message, cr)
		}
	}()
	until(t, "Route web Active", 3*time.Minute, func() bool {
		var list struct {
			Items []struct{ ID, URL, State, Message string }
		}
		alice.api(t, http.MethodGet, "/workspaces/"+solar.Workspace.ID+"/routes", nil, &list, 200)
		for _, r := range list.Items {
			if r.ID == route.ID {
				route.URL, route.State, route.Message = r.URL, r.State, r.Message
			}
		}
		return route.State == "Active"
	})
	until(t, route.URL+" answers", 2*time.Minute, func() bool {
		resp, err := http.Get(route.URL)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200
	})
	t.Logf("%s answers 200", route.URL)

	step(t, "Bob only reads")
	bobKube := kubeFor(t, "Bob", download(t, bob.session, "/workspaces/"+solar.Workspace.ID+"/kubeconfig"), bob.kubectlToken(t))
	bobKube.may(t, "list Pods in "+frontend, "", "-n", frontend, "get", "pods")
	bobKube.mayNot(t, "delete the Deployment", "", "-n", frontend, "delete", "deployment", "web", "--dry-run=server")
	bobKube.mayNot(t, "read Secrets", "", "-n", frontend, "get", "secrets")
	if code := bob.api(t, http.MethodPost, "/workspaces/"+solar.Workspace.ID+"/namespaces", map[string]any{"name": "bobs"}, nil); code != 403 {
		t.Errorf("Bob makes a Namespace on the portal: %d, want 403", code)
	}

	step(t, "The Platform Admin raises the grant, takes wk3 out of the shared pool and gives it to the Organization")
	raiseGrant(t, adm, org.ID)
	c.add("give wk3 back to shared-general", func() { setPoolNodes(adm, func(n []string) []string { return appendMissing(n, dedicatedNode) }) })
	c.add("take wk3 back from the Organization", func() {
		code, raw := adm.call(http.MethodPut, "/admin/organizations/"+org.ID+"/cluster-grants/cluster-a/dedicated-nodes", map[string]any{"nodes": []any{}})
		waitOperations(adm, code, raw)
	})
	if err := setPoolNodes(adm, func(n []string) []string { return without(n, dedicatedNode) }); err != nil {
		t.Fatal(err)
	}
	// Out of the pool, wk3 still runs the Pods it had (Solar's app among
	// them): the Platform Admin drains it, then uncordons it, before the
	// grant, which wants a clean node (PRODUCT-PLAN §24.3).
	c.add("uncordon wk3", func() { _, _ = admin(t, "", "uncordon", dedicatedNode) })
	if out, err := admin(t, "", "drain", dedicatedNode, "--ignore-daemonsets", "--delete-emptydir-data", "--timeout=4m"); err != nil {
		t.Fatalf("drain %s: %s", dedicatedNode, out)
	}
	mustAdmin(t, "uncordon", dedicatedNode)
	t.Logf("%s drained and uncordoned", dedicatedNode)
	var give struct {
		Items      []struct{ ID string }
		Operations []struct{ ID string }
	}
	adm.api(t, http.MethodPut, "/admin/organizations/"+org.ID+"/cluster-grants/cluster-a/dedicated-nodes", map[string]any{
		"nodes": []any{map[string]any{"name": dedicatedNode, "systemReserve": map[string]any{"cpu": "500m", "memory": "1536Mi", "pods": 10}}},
	}, &give, 200)
	for _, op := range give.Operations {
		adm.operation(t, op.ID)
	}
	grant := give.Items[0].ID
	until(t, "wk3 Ready for the Organization", 3*time.Minute, func() bool {
		var dn struct{ Items []struct{ NodeState string } }
		alice.api(t, http.MethodGet, "/organizations/"+slug+"/clusters/cluster-a/dedicated-nodes", nil, &dn, 200)
		return len(dn.Items) == 1 && dn.Items[0].NodeState == "Ready"
	})

	step(t, "Alice makes the Dedicated Workspaces Water and Green, 1 CPU / 4Gi each; a third is refused")
	dedicated := func(ws string) (workspace, int, map[string]any) {
		var w workspace
		var raw map[string]any
		code, body := alice.call(http.MethodPost, "/organizations/"+slug+"/workspaces", map[string]any{
			"slug": ws, "displayName": ws, "clusterId": "cluster-a",
			"compute":    map[string]any{"mode": "Dedicated", "dedicatedNodeGrantId": grant},
			"resources":  map[string]any{"requestsCpu": "1", "requestsMemory": "4Gi", "pods": 10, "requestsStorage": "1Gi", "namespaceQuota": 1},
			"namespaces": []string{"app"},
			"bindings":   []any{bindings[0]},
		})
		_ = jsonUnmarshal(body, &w)
		_ = jsonUnmarshal(body, &raw)
		return w, code, raw
	}
	water, code, raw := dedicated("water")
	if code != 202 {
		t.Fatalf("Water: %d %v", code, raw)
	}
	c.add("delete Workspace water", func() { deleteWorkspace(alice.session, water.Workspace.ID, "water") })
	alice.operation(t, water.Operation.ID)
	green, code, raw := dedicated("green")
	if code != 202 {
		t.Fatalf("Green: %d %v", code, raw)
	}
	c.add("delete Workspace green", func() { deleteWorkspace(alice.session, green.Workspace.ID, "green") })
	alice.operation(t, green.Operation.ID)
	if _, code, raw := dedicated("earth"); code != 409 || raw["code"] != "DEDICATED_NODE_INSUFFICIENT_CAPACITY" {
		t.Errorf("a third Dedicated Workspace: %d %v, want 409 DEDICATED_NODE_INSUFFICIENT_CAPACITY", code, raw["code"])
	} else {
		t.Logf("third Dedicated Workspace refused: %v", raw["message"])
	}

	step(t, "Pods get their Workspace's CIDR and the right nodes; Solar and Water do not reach each other")
	aliceKube := kubeFor(t, "Alice", download(t, alice.session, "/workspaces/"+solar.Workspace.ID+"/kubeconfig"), alice.kubectlToken(t))
	waterNS := water.Workspace.K8sName + "-app"
	aliceKube.may(t, "start a server in Solar", podJSON(frontend, "srv", []string{"netexec", "--http-port=8080"}, false), "apply", "-f", "-")
	aliceKube.may(t, "start a server in Water", podJSON(waterNS, "srv", []string{"netexec", "--http-port=8080"}, true), "apply", "-f", "-")
	for _, ns := range []string{frontend, waterNS} {
		aliceKube.may(t, "see the server in "+ns+" Ready", "", "-n", ns, "wait", "pod/srv", "--for=condition=Ready", "--timeout=3m")
	}
	cidrs := map[string]string{frontend: workspaceCIDR(t, alice.session, solar.Workspace.ID), waterNS: workspaceCIDR(t, alice.session, water.Workspace.ID)}
	for ns, cidr := range cidrs {
		ip, node := jsonpath(t, "pod", ns, "srv", "{.status.podIP}"), jsonpath(t, "pod", ns, "srv", "{.spec.nodeName}")
		if !inCIDR(ip, cidr) {
			t.Errorf("Pod in %s has %s, outside its CIDR %s", ns, ip, cidr)
		}
		pool := strings.TrimSpace(mustAdmin(t, "get", "node", node, "-o", "jsonpath={.metadata.labels.tenantry\\.io/compute-pool}"))
		switch {
		case ns == waterNS && node != dedicatedNode:
			t.Errorf("Water's Pod runs on %s, not on its node %s", node, dedicatedNode)
		case ns == frontend && pool != "shared-general":
			t.Errorf("Solar's Pod runs on %s (pool %q), not in shared-general", node, pool)
		}
		t.Logf("Pod in %s: %s in %s, on %s", ns, ip, cidr, node)
	}
	solarIP, waterIP := jsonpath(t, "pod", frontend, "srv", "{.status.podIP}"), jsonpath(t, "pod", waterNS, "srv", "{.status.podIP}")
	if connects(t, aliceKube, frontend, "to-water", waterIP+":8080", false) {
		t.Error("Solar reaches Water")
	}
	if connects(t, aliceKube, waterNS, "to-solar", solarIP+":8080", true) {
		t.Error("Water reaches Solar")
	}

	step(t, "Joe does not reach a Workspace he is not bound to")
	joeKube.mayNot(t, "list Pods in Water", "", "-n", waterNS, "get", "pods")

	step(t, "Alice removes Joe from the Organization; a new token has no right")
	alice.api(t, http.MethodDelete, "/organizations/"+org.ID+"/members/"+joe.ID, nil, nil, 204)
	until(t, "Joe out of developers in Keycloak", 3*time.Minute, func() bool {
		joe.relogIn(t)
		return !strings.Contains(strings.Join(groupsOf(t, joe.kubectlToken(t)), ","), gs["developers"].MachineName)
	})
	fresh := kubeFor(t, "Joe (new token)", kubeconfig, joe.kubectlToken(t))
	fresh.mayNot(t, "list Pods in "+frontend+" with a new token", "", "-n", frontend, "get", "pods")
	if _, err := joeKube.kubectl("", "-n", frontend, "get", "pods"); err == nil {
		t.Log("Joe's old token still works until it expires (the contract of architecture/06 §5)")
	} else {
		t.Log("Joe's old token no longer works")
	}

	step(t, "Alice deletes Water; Green and its node keep working")
	deleteWorkspace(alice.session, water.Workspace.ID, "water")
	var g struct{ State string }
	alice.api(t, http.MethodGet, "/workspaces/"+green.Workspace.ID, nil, &g, 200)
	if g.State != "Ready" {
		t.Errorf("Green is %s after Water's delete", g.State)
	}
	var dn struct{ Items []map[string]any }
	alice.api(t, http.MethodGet, "/organizations/"+slug+"/clusters/cluster-a/dedicated-nodes", nil, &dn, 200)
	t.Logf("wk3 after Water's delete: %v", dn.Items)

	step(t, "Headlamp SSO: not here, the web e2e suite (Playwright) checks it")
}

// connects runs agnhost connect from a Pod in ns as the person and says
// whether it got through.
func connects(t *testing.T, k *kube, ns, name, target string, guaranteed bool) bool {
	t.Helper()
	k.may(t, "start a probe in "+ns, podJSON(ns, name, []string{"connect", target, "--timeout=3s"}, guaranteed), "apply", "-f", "-")
	defer k.kubectl("", "-n", ns, "delete", "pod", name, "--wait=false")
	var phase string
	until(t, "probe "+name, 2*time.Minute, func() bool {
		phase = jsonpath(t, "pod", ns, name, "{.status.phase}")
		return phase == "Succeeded" || phase == "Failed"
	})
	t.Logf("probe %s in %s to %s: %s", name, ns, target, phase)
	return phase == "Succeeded"
}

func download(t *testing.T, s *session, path string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, portalURL+"/api/v1"+path, nil)
	resp, err := s.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := new(strings.Builder)
	_, _ = fmt.Fprint(b, readAll(resp))
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d %s", path, resp.StatusCode, firstLine(b.String()))
	}
	return b.String()
}

func workspaceCIDR(t *testing.T, s *session, id string) string {
	t.Helper()
	var w struct{ Network *struct{ CIDR string } }
	s.api(t, http.MethodGet, "/workspaces/"+id, nil, &w, 200)
	if w.Network == nil || w.Network.CIDR == "" {
		t.Fatalf("Workspace %s has no CIDR", id)
	}
	return w.Network.CIDR
}

// deleteWorkspace deletes a Workspace and waits for its operation; already
// gone is fine.
func deleteWorkspace(s *session, id, slug string) {
	code, raw := s.call(http.MethodDelete, "/workspaces/"+id+"?confirmName="+slug, nil)
	if code != 202 {
		return
	}
	var out struct{ Operation struct{ ID string } }
	_ = jsonUnmarshal(raw, &out)
	waitOperation(s, out.Operation.ID)
}
