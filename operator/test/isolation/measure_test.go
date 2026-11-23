//go:build lab

package isolation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Two measurements of the "chưa kiểm" table (docs/report/dod-phase1.md),
// each in an Organization of its own made through the portal, with a
// throwaway optest account; neither touches wk3, a node pool or acme.
//   - TestNamespaceLatency: PRODUCT-PLAN §23 "Namespace create p95 < 15 s".
//   - TestSharedPending: Shared Pods Pending once the pool is full (ADR-0017).

// optestOrg is an Organization an Admin made for a new optest account, with
// its admins group synced. Its cleanups delete it (after the CIDR quarantine
// of its Workspaces) and disable the account, whatever happened.
func optestOrg(t *testing.T, adm *portal, grant map[string]any) (owner *account, slug, orgID, adminsID, adminsMachine string) {
	t.Helper()
	owner = signUp(t)
	t.Cleanup(func() {
		var users struct {
			Items []struct{ ID string } `json:"items"`
		}
		adm.api(t, http.MethodGet, "/admin/users?q="+owner.Email, nil, &users)
		if len(users.Items) == 1 {
			adm.api(t, http.MethodPost, "/admin/users/"+users.Items[0].ID+":disable", nil, nil)
		}
	})
	slug = "optest" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomString(4)))
	var org struct {
		Organization struct{ ID string } `json:"organization"`
	}
	if code := adm.api(t, http.MethodPost, "/admin/organizations", map[string]any{
		"slug": slug, "displayName": "Optest " + slug, "description": "Measurements of docs/report/dod-phase1.md.", "ownerEmail": owner.Email,
		"entitlement":  map[string]any{"maxMembers": 5, "maxGroups": 5, "maxWorkspacesTotal": 4},
		"clusterGrant": grant,
	}, &org); code != http.StatusCreated {
		t.Fatalf("create Organization %s: %d", slug, code)
	}
	orgID = org.Organization.ID
	t.Cleanup(func() {
		for end := time.Now().Add(20 * time.Minute); time.Now().Before(end); time.Sleep(15 * time.Second) {
			if code := owner.api(t, http.MethodDelete, "/organizations/"+orgID, nil, nil); code == http.StatusNoContent || code == http.StatusNotFound {
				t.Logf("Organization %s deleted", slug)
				return
			}
		}
		t.Errorf("Organization %s not deleted after 20 minutes", slug)
	})
	waitFor(t, "group admins synced", 3*time.Minute, func() (bool, error) {
		var groups struct {
			Items []struct{ ID, Name, MachineName, SyncStatus string } `json:"items"`
		}
		owner.api(t, http.MethodGet, "/organizations/"+slug+"/groups", nil, &groups)
		for _, g := range groups.Items {
			if g.Name == "admins" && g.SyncStatus == "Synced" {
				adminsID, adminsMachine = g.ID, g.MachineName
				return true, nil
			}
		}
		return false, nil
	})
	return owner, slug, orgID, adminsID, adminsMachine
}

func sharedGrant(cpu string, namespaces int) map[string]any {
	return map[string]any{
		"clusterId": "cluster-a", "maxWorkspaces": 4, "maxNamespacesTotal": namespaces,
		"resources":     map[string]any{"requestsCpu": cpu, "requestsMemory": "8Gi", "limitsCpu": cpu, "limitsMemory": "8Gi", "pods": 40, "requestsStorage": "2Gi"},
		"sharedPoolIds": []string{"shared-general"}, "networkPoolIds": []string{"onboarding-pod-network"}, "allowedStorageClasses": []string{"standard"},
	}
}

// sharedWorkspace creates a Shared Workspace on shared-general, waits for it,
// and deletes it at cleanup. It answers its id.
func sharedWorkspace(t *testing.T, owner *account, slug, ws, adminsID string, resources map[string]any, namespaces []string) string {
	t.Helper()
	var created struct {
		Workspace struct{ ID string } `json:"workspace"`
		Operation struct{ ID string } `json:"operation"`
	}
	if code := owner.api(t, http.MethodPost, "/organizations/"+slug+"/workspaces", map[string]any{
		"slug": ws, "displayName": ws, "clusterId": "cluster-a",
		"compute":    map[string]any{"mode": "Shared", "sharedPoolId": "shared-general"},
		"resources":  resources,
		"namespaces": namespaces,
		"bindings":   []any{map[string]any{"groupId": adminsID, "role": "TenantOwner"}},
	}, &created); code != http.StatusAccepted {
		t.Fatalf("create Workspace %s: %d", ws, code)
	}
	id := created.Workspace.ID
	t.Cleanup(func() {
		var del struct {
			Operation struct{ ID string } `json:"operation"`
		}
		if owner.api(t, http.MethodDelete, "/workspaces/"+id+"?confirmName="+ws, nil, &del) == http.StatusAccepted {
			owner.operation(t, del.Operation.ID)
		}
	})
	owner.operation(t, created.Operation.ID)
	return id
}

type nsSample struct {
	name             string
	cluster, forUser time.Duration
}

// TestNamespaceLatency adds NS_BENCH_N Namespaces (default 20) through the
// portal to two Shared Workspaces, one at a time in each (a Workspace runs
// one operation at a time), the two Workspaces in parallel. For each it
// measures:
//   - cluster: from the CR first showing the Namespace in spec.namespaces to
//     the Namespace Active with the baseline NetworkPolicy, the LimitRange,
//     and the Workspace's GlobalResourceQuota counting it;
//   - forUser: from the POST to the operation Succeeded (outbox, worker,
//     controller, status back).
func TestNamespaceLatency(t *testing.T) {
	n := 20
	if v, err := strconv.Atoi(os.Getenv("NS_BENCH_N")); err == nil && v > 0 {
		n = v
	}
	perWorkspace := (n + 1) / 2
	adm := adminPortal(t)
	owner, slug, _, adminsID, _ := optestOrg(t, adm, sharedGrant("1", 2*(perWorkspace+1)))
	small := map[string]any{"requestsCpu": "200m", "requestsMemory": "256Mi", "limitsCpu": "400m", "limitsMemory": "512Mi",
		"pods": 10, "requestsStorage": "1Gi", "namespaceQuota": perWorkspace + 1}
	ids := map[string]string{}
	for _, ws := range []string{"nsa", "nsb"} {
		ids[ws] = sharedWorkspace(t, owner, slug, ws, adminsID, small, []string{"base"})
	}

	var mu sync.Mutex
	var samples []nsSample
	var wg sync.WaitGroup
	for _, ws := range []string{"nsa", "nsb"} {
		wg.Go(func() {
			for i := range perWorkspace {
				s := addNamespace(t, owner, slug, ws, ids[ws], fmt.Sprintf("n%02d", i))
				mu.Lock()
				samples = append(samples, s)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	cluster, forUser := make([]time.Duration, len(samples)), make([]time.Duration, len(samples))
	for i, s := range samples {
		cluster[i], forUser[i] = s.cluster, s.forUser
		t.Logf("NSSAMPLE %s cluster=%s user=%s", s.name, s.cluster.Round(time.Millisecond), s.forUser.Round(time.Millisecond))
	}
	t.Logf("NSBENCH n=%d cluster_p50=%s cluster_p95=%s cluster_max=%s user_p50=%s user_p95=%s user_max=%s",
		len(samples), pct(cluster, 50), pct(cluster, 95), pct(cluster, 100), pct(forUser, 50), pct(forUser, 95), pct(forUser, 100))
}

func addNamespace(t *testing.T, owner *account, slug, ws, wsID, suffix string) nsSample {
	k8sName := slug + "-" + ws
	full := k8sName + "-" + suffix
	start := time.Now()
	var change struct {
		Operation struct{ ID string } `json:"operation"`
	}
	if code := owner.api(t, http.MethodPost, "/workspaces/"+wsID+"/namespaces", map[string]any{"name": suffix}, &change); code != http.StatusAccepted {
		t.Errorf("add Namespace %s: %d", full, code)
		return nsSample{name: full}
	}
	var inSpec, ready, succeeded time.Time
	deadline := start.Add(5 * time.Minute)
	for (ready.IsZero() || succeeded.IsZero()) && time.Now().Before(deadline) {
		now := time.Now()
		if inSpec.IsZero() {
			cr := &v1.PlatformWorkspace{}
			if admin.Get(ctx, client.ObjectKey{Name: k8sName}, cr) == nil && contains(cr.Spec.Namespaces, suffix) {
				inSpec = now
			}
		}
		if !inSpec.IsZero() && ready.IsZero() && namespaceReady(k8sName, full) {
			ready = now
		}
		if succeeded.IsZero() {
			var op struct{ State string }
			owner.api(t, http.MethodGet, "/operations/"+change.Operation.ID, nil, &op)
			switch op.State {
			case "Succeeded":
				succeeded = now
			case "Failed", "Blocked":
				t.Errorf("operation for %s %s", full, op.State)
				return nsSample{name: full}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if ready.IsZero() || succeeded.IsZero() {
		t.Errorf("%s not ready (%v) or its operation not Succeeded (%v) in 5 minutes", full, !ready.IsZero(), !succeeded.IsZero())
		return nsSample{name: full}
	}
	return nsSample{name: full, cluster: ready.Sub(inSpec), forUser: succeeded.Sub(start)}
}

// namespaceReady: Active, with the baseline NetworkPolicy and the LimitRange
// replicated into it, and counted by the Workspace's GlobalResourceQuota.
func namespaceReady(tenant, ns string) bool {
	nsObj := &corev1.Namespace{}
	if admin.Get(ctx, client.ObjectKey{Name: ns}, nsObj) != nil || nsObj.Status.Phase != corev1.NamespaceActive {
		return false
	}
	if admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: "tenant-default-deny"}, &networkingv1.NetworkPolicy{}) != nil {
		return false
	}
	if admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: render.LimitRangeName}, &corev1.LimitRange{}) != nil {
		return false
	}
	grq := &unstructured.Unstructured{}
	grq.SetGroupVersionKind(render.QuotaGVK)
	if admin.Get(ctx, client.ObjectKey{Name: tenant + "-" + render.QuotaName}, grq) != nil {
		return false
	}
	status, _ := json.Marshal(grq.Object["status"])
	return strings.Contains(string(status), `"`+ns+`"`)
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// pct is the p-th percentile (nearest rank) of d; 100 is the maximum.
func pct(d []time.Duration, p int) time.Duration {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := (p*len(s) + 99) / 100
	if i < 1 {
		i = 1
	}
	return s[i-1].Round(time.Millisecond)
}

// TestSharedPending fills shared-general past what its nodes can schedule,
// within every quota the platform checks: two Shared Workspaces, each with a
// quota of PENDING_REPLICAS (default 4) CPU and as many pause Pods of 1 CPU.
// With wk2 and wk3 in the pool (6 CPU) the default asks 8, so some Pods stay
// Pending; with wk2 alone (3 CPU) 2 replicas each are enough. The total must
// stay under the pool's ceiling (ratio × reservable, less what others hold)
// or the API refuses the Workspaces: read the pool before choosing. It
// records what the user sees, then scales to zero at once.
func TestSharedPending(t *testing.T) {
	replicas := int32(4)
	if v, err := strconv.Atoi(os.Getenv("PENDING_REPLICAS")); err == nil && v > 0 {
		replicas = int32(v)
	}
	adm := adminPortal(t)
	cpu := strconv.Itoa(int(replicas))
	owner, slug, _, adminsID, adminsMachine := optestOrg(t, adm, sharedGrant(strconv.Itoa(2*int(replicas)), 4))
	quota := map[string]any{"requestsCpu": cpu, "requestsMemory": "2Gi", "limitsCpu": cpu, "limitsMemory": "2Gi",
		"pods": 10, "requestsStorage": "1Gi", "namespaceQuota": 1}
	tenant := tenantAs(t, "oidc:optest-pending", "oidc:"+adminsMachine)
	type ws struct{ name, id, ns string }
	var all []ws
	for _, name := range []string{"fill", "over"} {
		w := ws{name: name, ns: slug + "-" + name + "-app"}
		w.id = sharedWorkspace(t, owner, slug, name, adminsID, quota, []string{"app"})
		all = append(all, w)
	}
	for _, w := range all {
		d := pauseDeployment(w.ns, replicas)
		waitFor(t, "the Namespace's NetworkPolicies", 2*time.Minute, func() (bool, error) {
			err := tenant.Create(ctx, d.DeepCopy(), client.DryRunAll)
			return err == nil, err
		})
		// Calls are tried again: a connection to the lab can drop (EOF).
		waitFor(t, "Deployment in "+w.ns, time.Minute, func() (bool, error) {
			err := tenant.Create(ctx, d.DeepCopy())
			return err == nil || apierrors.IsAlreadyExists(err), err
		})
		// Scale to zero before anything else is undone: the nodes are full.
		t.Cleanup(func() {
			waitFor(t, "scale "+w.ns+" to zero", 2*time.Minute, func() (bool, error) {
				err := admin.Patch(ctx, d.DeepCopy(), client.RawPatch(types.MergePatchType, []byte(`{"spec":{"replicas":0}}`)))
				return err == nil, err
			})
		})
	}

	var running, pending int
	var reason string
	waitFor(t, "every Pod placed or found unschedulable", 3*time.Minute, func() (bool, error) {
		running, pending, reason = 0, 0, ""
		for _, w := range all {
			pods := &corev1.PodList{}
			if err := admin.List(ctx, pods, client.InNamespace(w.ns)); err != nil {
				return false, err
			}
			for _, p := range pods.Items {
				switch {
				case p.Status.Phase == corev1.PodRunning:
					running++
				case unschedulable(&p) != "":
					pending++
					reason = unschedulable(&p)
				}
			}
		}
		return running+pending == 2*int(replicas), nil
	})
	t.Logf("PENDING running=%d pending=%d reason=%q", running, pending, reason)
	if pending == 0 {
		t.Errorf("no Pod Pending: the pool held %d CPU of Pods", running)
	}
	for _, w := range all {
		var usage json.RawMessage
		owner.api(t, http.MethodGet, "/workspaces/"+w.id+"/usage", nil, &usage)
		t.Logf("PORTAL usage of %s: %s", w.name, usage)
		var wsView json.RawMessage
		owner.api(t, http.MethodGet, "/workspaces/"+w.id, nil, &wsView)
		t.Logf("PORTAL Workspace %s: %s", w.name, wsView)
	}
	events := &corev1.EventList{}
	if err := admin.List(ctx, events, client.InNamespace(all[1].ns)); err == nil {
		for _, e := range events.Items {
			if e.Reason == "FailedScheduling" {
				t.Logf("EVENT %s: %s", e.InvolvedObject.Name, e.Message)
				break
			}
		}
	}
}

func unschedulable(p *corev1.Pod) string {
	if p.Status.Phase != corev1.PodPending {
		return ""
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
			return c.Message
		}
	}
	return ""
}

func pauseDeployment(ns string, replicas int32) *appsv1.Deployment {
	cpu := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("16Mi")}
	labels := map[string]string{"app": "fill"}
	no, yes := false, true
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "fill", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
					Containers: []corev1.Container{{
						Name: "pause", Image: "registry.k8s.io/pause:3.10",
						Resources: corev1.ResourceRequirements{Requests: cpu, Limits: cpu.DeepCopy()},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}
