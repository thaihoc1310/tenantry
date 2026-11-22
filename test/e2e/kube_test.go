package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// admin runs kubectl as the lab's admin (kubeconfig context), for checks a
// person cannot make: where a Pod runs, what a node carries.
func admin(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("kubectl", append([]string{"--context", kubeContext}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func secretValue(t *testing.T, ns, name, key string) string {
	t.Helper()
	out, err := admin(t, "", "-n", ns, "get", "secret", name, "-o", "jsonpath={.data."+key+"}")
	if err != nil {
		t.Fatalf("Secret %s/%s: %s", ns, name, out)
	}
	cmd := exec.Command("base64", "-d")
	cmd.Stdin = strings.NewReader(out)
	b, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// kube is kubectl and helm as one person, the way their kubeconfig from the
// portal works: its server (Capsule Proxy behind Traefik) and the person's
// own token, which kubelogin would fetch.
type kube struct {
	who, server, token, dir string
}

var serverLine = regexp.MustCompile(`(?m)^\s*server:\s*(\S+)`)

func kubeFor(t *testing.T, who, kubeconfig, token string) *kube {
	t.Helper()
	m := serverLine.FindStringSubmatch(kubeconfig)
	if m == nil {
		t.Fatalf("no server in the kubeconfig of %s", who)
	}
	dir := t.TempDir()
	// An empty kubeconfig: nothing of the admin's leaks into the person's calls.
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &kube{who: who, server: m[1], token: token, dir: dir}
}

func (k *kube) flags() []string {
	return []string{"--kubeconfig", filepath.Join(k.dir, "config"), "--server", k.server, "--token", k.token}
}

func (k *kube) kubectl(stdin string, args ...string) (string, error) {
	cmd := exec.Command("kubectl", append(k.flags(), args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (k *kube) helm(args ...string) (string, error) {
	f := k.flags()
	cmd := exec.Command("helm", append([]string{"--kubeconfig", f[1], "--kube-apiserver", k.server, "--kube-token", k.token}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// may runs a command the person must be allowed.
func (k *kube) may(t *testing.T, what string, stdin string, args ...string) string {
	t.Helper()
	out, err := k.kubectl(stdin, args...)
	if err != nil {
		t.Fatalf("%s: %s may %s, but: %s", k.who, k.who, what, out)
	}
	t.Logf("%s may %s", k.who, what)
	return out
}

// mayNot runs a command the person must be refused.
func (k *kube) mayNot(t *testing.T, what string, stdin string, args ...string) {
	t.Helper()
	out, err := k.kubectl(stdin, args...)
	if err == nil {
		t.Fatalf("%s may not %s, but it worked: %s", k.who, what, out)
	}
	t.Logf("%s may not %s: %s", k.who, what, firstLine(out))
}

// podJSON is a small Pod; guaranteed makes requests equal limits (Dedicated).
func podJSON(ns, name string, args []string, guaranteed bool) string {
	limits := map[string]string{"cpu": "100m", "memory": "64Mi"}
	if !guaranteed {
		limits = map[string]string{"cpu": "200m", "memory": "64Mi"}
	}
	p := map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": name, "namespace": ns, "labels": map[string]string{"app": name}},
		"spec": map[string]any{
			"restartPolicy":                "Never",
			"automountServiceAccountToken": false,
			"securityContext":              map[string]any{"runAsNonRoot": true, "runAsUser": 65532, "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
			"containers": []any{map[string]any{
				"name": "c", "image": "registry.k8s.io/e2e-test-images/agnhost:2.53", "args": args,
				"resources":       map[string]any{"requests": map[string]string{"cpu": "100m", "memory": "64Mi"}, "limits": limits},
				"securityContext": map[string]any{"allowPrivilegeEscalation": false, "capabilities": map[string]any{"drop": []string{"ALL"}}},
			}},
		},
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func jsonpath(t *testing.T, kind, ns, name, path string) string {
	t.Helper()
	out, _ := admin(t, "", "-n", ns, "get", kind, name, "-o", "jsonpath="+path)
	return out
}

func fmtList(s []string) string { return fmt.Sprintf("%v", s) }
