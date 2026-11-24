//go:build lab

package isolation

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// TestSchedulingStatus checks status.scheduling on the lab (operator rev 18):
// a Pod asking 4 CPU, more than any node of shared-general holds (3 CPU
// each), within the Workspace's quota, shows in status.scheduling and in its
// Namespace's count while the Workspace stays Ready; deleted, it leaves
// both. It records how long the operator took each way, and what the
// portal shows and when.
func TestSchedulingStatus(t *testing.T) {
	adm := adminPortal(t)
	owner, slug, _, adminsID, adminsMachine := optestOrg(t, adm, sharedGrant("4", 1))
	quota := map[string]any{"requestsCpu": "4", "requestsMemory": "1Gi", "limitsCpu": "4", "limitsMemory": "1Gi",
		"pods": 5, "requestsStorage": "1Gi", "namespaceQuota": 1}
	id, _ := sharedWorkspace(t, owner, slug, "sched", adminsID, quota, []string{"app"})
	name, ns := slug+"-sched", slug+"-sched-app"
	tenant := tenantAs(t, "oidc:optest-sched", "oidc:"+adminsMachine)

	tmpl := pauseDeployment(ns, 1).Spec.Template
	cpu := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Mi")}
	tmpl.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: cpu, Limits: cpu.DeepCopy()}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "too-big", Namespace: ns}, Spec: tmpl.Spec}
	waitFor(t, "the Namespace's NetworkPolicies", 2*time.Minute, func() (bool, error) {
		err := tenant.Create(ctx, pod.DeepCopy(), client.DryRunAll)
		return err == nil, err
	})
	if err := tenant.Create(ctx, pod.DeepCopy()); err != nil {
		t.Fatalf("create the 4 CPU Pod: %v", err)
	}
	created := time.Now()
	t.Cleanup(func() { _ = admin.Delete(ctx, pod.DeepCopy()) })

	cr := func() *v1.PlatformWorkspace {
		got := &v1.PlatformWorkspace{}
		if err := admin.Get(ctx, client.ObjectKey{Name: name}, got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	nsCount := func(ws *v1.PlatformWorkspace) int32 {
		for _, n := range ws.Status.Namespaces {
			if n.Name == ns {
				return n.UnschedulablePods
			}
		}
		return -1
	}
	waitFor(t, "status.scheduling with the Pod", 3*time.Minute, func() (bool, error) {
		s := cr().Status.Scheduling
		return s != nil && s.UnschedulablePods == 1, nil
	})
	shown := time.Since(created)
	ws := cr()
	s := ws.Status.Scheduling
	t.Logf("SCHEDULING after %s: %d Pod, since %v, message %q", shown.Round(time.Second), s.UnschedulablePods, s.Since, s.Message)
	if !strings.Contains(s.Message, "Insufficient cpu") {
		t.Errorf("message %q, want the scheduler's Insufficient cpu", s.Message)
	}
	if got := nsCount(ws); got != 1 {
		t.Errorf("%s counts %d, want 1", ns, got)
	}
	if ws.Status.Phase != v1.PhaseReady {
		t.Errorf("phase %s with a Pod unschedulable, want Ready", ws.Status.Phase)
	}
	// The portal's view: the worker copies the CR's status into the database.
	var view json.RawMessage
	portalScheduling := func() *struct{ UnschedulablePods int } {
		var got struct {
			Scheduling *struct{ UnschedulablePods int } `json:"scheduling"`
		}
		owner.api(t, http.MethodGet, "/workspaces/"+id, nil, &view)
		_ = json.Unmarshal(view, &got)
		return got.Scheduling
	}
	waitFor(t, "the portal showing the Pod", 3*time.Minute, func() (bool, error) {
		s := portalScheduling()
		return s != nil && s.UnschedulablePods == 1, nil
	})
	t.Logf("PORTAL shows it %s after the Pod was created: %s", time.Since(created).Round(time.Second), view)

	if err := tenant.Delete(ctx, pod.DeepCopy()); err != nil {
		t.Fatalf("delete the Pod: %v", err)
	}
	deleted := time.Now()
	waitFor(t, "status.scheduling gone", 3*time.Minute, func() (bool, error) {
		return cr().Status.Scheduling == nil, nil
	})
	t.Logf("SCHEDULING gone %s after the delete", time.Since(deleted).Round(time.Second))
	waitFor(t, "the portal no longer showing it", 3*time.Minute, func() (bool, error) {
		return portalScheduling() == nil, nil
	})
	t.Logf("PORTAL clear %s after the delete: %s", time.Since(deleted).Round(time.Second), view)
	ws = cr()
	if got := nsCount(ws); got != 0 {
		t.Errorf("%s still counts %d", ns, got)
	}
	if ws.Status.Phase != v1.PhaseReady {
		t.Errorf("phase %s after the delete, want Ready", ws.Status.Phase)
	}
}
