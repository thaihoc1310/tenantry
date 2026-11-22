//go:build lab

package isolation

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// holdFinalizer keeps an object as a stuck controller would. In a tenant
// Namespace only a Pod can hold its deletion: Capsule 0.15.2's
// namespace-cleanup controller clears every other object's finalizers once
// the Pods are gone (pkg/tenant/namespace_cleanup.go).
const holdFinalizer = "optest.tenantry.io/hold"

func gone(name string) func() (bool, error) {
	return func() (bool, error) {
		err := admin.Get(ctx, client.ObjectKey{Name: name}, &v1.PlatformWorkspace{})
		return apierrors.IsNotFound(err), err
	}
}

func ready(name string) func() (bool, error) {
	return func() (bool, error) {
		ws := &v1.PlatformWorkspace{}
		err := admin.Get(ctx, client.ObjectKey{Name: name}, ws)
		return ws.Status.Phase == v1.PhaseReady, err
	}
}

// Failure cases of PRODUCT-PLAN §24.5 on the cluster side.
func TestFailure(t *testing.T) {
	t.Run("controller restart in the middle of provisioning", func(t *testing.T) {
		ws := workspace("optest-fail-restart", "01fail", "10.240.252.0/24")
		ws.Spec.Network = nil
		start := time.Now()
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = admin.Delete(ctx, ws); waitFor(t, "delete", 3*time.Minute, gone(ws.Name)) })
		// Kill the operator once it has made the Tenant, before Ready.
		tenantObj := &unstructured.Unstructured{}
		tenantObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta2", Kind: "Tenant"})
		waitFor(t, "Tenant made", time.Minute, func() (bool, error) {
			err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, tenantObj)
			return err == nil, err
		})
		uid := tenantObj.GetUID()
		phaseAtKill := &v1.PlatformWorkspace{}
		_ = admin.Get(ctx, client.ObjectKey{Name: ws.Name}, phaseAtKill)
		var pods corev1.PodList
		if err := admin.List(ctx, &pods, client.InNamespace("tenantry-operator"), client.MatchingLabels{"app.kubernetes.io/component": "operator"}); err != nil || len(pods.Items) == 0 {
			t.Fatalf("operator Pod: %v", err)
		}
		killed := time.Now()
		for i := range pods.Items {
			if err := admin.Delete(ctx, &pods.Items[i], client.GracePeriodSeconds(0)); err != nil {
				t.Fatal(err)
			}
		}
		waitFor(t, "Ready after the restart", 4*time.Minute, ready(ws.Name))
		t.Logf("killed at phase %q; Ready %s after creation, %s after the operator was killed",
			phaseAtKill.Status.Phase, time.Since(start).Round(time.Second), time.Since(killed).Round(time.Second))
		// Idempotent: the same Tenant, not made again, with its owners.
		if err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, tenantObj); err != nil || tenantObj.GetUID() != uid {
			t.Errorf("Tenant made again: uid %s, was %s (%v)", tenantObj.GetUID(), uid, err)
		}
		owners, _, _ := unstructured.NestedSlice(tenantObj.Object, "spec", "owners")
		if len(owners) != 2 {
			t.Errorf("Tenant owners %v, want the owner group and the operator", owners)
		}
		t.Logf("same Tenant uid %s, %d owners, generation %d", uid, len(owners), tenantObj.GetGeneration())
		// Nothing doubled: one Tenant owned once, its Namespaces once.
		var nss corev1.NamespaceList
		if err := admin.List(ctx, &nss, client.MatchingLabels{"capsule.clastix.io/tenant": ws.Name}); err != nil || len(nss.Items) != 2 {
			t.Errorf("Namespaces of the Tenant: %d (%v), want 2", len(nss.Items), err)
		}
	})

	t.Run("delete held by a stuck Pod, then released", func(t *testing.T) {
		ws := workspace("optest-fail-pvc", "01fail", "10.240.252.0/24")
		ws.Spec.Network = nil
		ws.Spec.Namespaces = []string{"data"}
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "Ready", 3*time.Minute, ready(ws.Name))
		pod := heldPod(t, "optest-fail-pvc-data")
		start := time.Now()
		if err := admin.Delete(ctx, ws); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "Deleting names the stuck Namespace", 2*time.Minute, func() (bool, error) {
			got := &v1.PlatformWorkspace{}
			err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, got)
			c := meta.FindStatusCondition(got.Status.Conditions, v1.ConditionDeleting)
			return c != nil && c.Status == metav1.ConditionTrue && strings.Contains(c.Message, "optest-fail-pvc-data"), err
		})
		time.Sleep(10 * time.Second)
		if done, _ := gone(ws.Name)(); done {
			t.Fatal("the Workspace went while its Namespace was still held")
		}
		releasePod(t, pod)
		waitFor(t, "deleted once the Pod is released", 3*time.Minute, gone(ws.Name))
		t.Logf("delete took %s, of which ~10s held on purpose", time.Since(start).Round(time.Second))
	})

	t.Run("force-delete does not wait for a held Namespace", func(t *testing.T) {
		ws := workspace("optest-fail-force", "01fail", "10.240.252.0/24")
		ws.Spec.Network = nil
		ws.Spec.Namespaces = []string{"data"}
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "Ready", 3*time.Minute, ready(ws.Name))
		heldPod(t, "optest-fail-force-data")
		if err := admin.Delete(ctx, ws); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		got := &v1.PlatformWorkspace{}
		if err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, got); err != nil {
			t.Fatal(err)
		}
		got.Annotations = map[string]string{v1.AnnotationForceDelete: "true"}
		if err := admin.Update(ctx, got); err != nil {
			t.Fatal(err)
		}
		forced := time.Now()
		waitFor(t, "force-deleted", 2*time.Minute, gone(ws.Name))
		t.Logf("CR gone %s after the force-delete annotation", time.Since(forced).Round(time.Second))
		var events eventsv1.EventList
		_ = admin.List(ctx, &events)
		for _, reason := range []string{"ForceDelete", "ForceDeleteLeftovers"} {
			found := ""
			for _, e := range events.Items {
				if e.Reason == reason && e.Regarding.Name == ws.Name {
					found = e.Note
				}
			}
			if found == "" {
				t.Errorf("no %s Event on the PlatformWorkspace", reason)
			} else {
				t.Logf("Event %s: %s", reason, found)
			}
		}
		// The Tenant left behind is out of the tenant's reach.
		left := &unstructured.Unstructured{}
		left.SetGroupVersionKind(schema.GroupVersionKind{Group: "capsule.clastix.io", Version: "v1beta2", Kind: "Tenant"})
		if err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, left); err == nil {
			owners, _, _ := unstructured.NestedSlice(left.Object, "spec", "owners")
			bindings, _, _ := unstructured.NestedSlice(left.Object, "spec", "additionalRoleBindings")
			if len(owners) != 1 || len(bindings) != 0 || left.GetDeletionTimestamp() == nil {
				t.Errorf("Tenant left behind: owners %v, bindings %v, deleting %v", owners, bindings, left.GetDeletionTimestamp() != nil)
			}
			t.Logf("Tenant left terminating with only the operator as owner and no RoleBinding")
		}
		ns := &corev1.Namespace{}
		if err := admin.Get(ctx, client.ObjectKey{Name: "optest-fail-force-data"}, ns); err == nil {
			t.Logf("the held Namespace stays Terminating until the Pod is released (phase %s), as :force intends", ns.Status.Phase)
		}
	})

	t.Run("a PVC finalizer does not hold a tenant Namespace", func(t *testing.T) {
		ws := workspace("optest-fail-claim", "01fail", "10.240.252.0/24")
		ws.Spec.Network = nil
		ws.Spec.Namespaces = []string{"data"}
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "Ready", 3*time.Minute, ready(ws.Name))
		heldPVC(t, "optest-fail-claim-data")
		start := time.Now()
		if err := admin.Delete(ctx, ws); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "deleted although the PVC carried a foreign finalizer", 3*time.Minute, gone(ws.Name))
		t.Logf("Capsule's namespace-cleanup cleared the PVC's finalizer: the Workspace went in %s", time.Since(start).Round(time.Second))
	})
}

// heldPVC makes a bound-less PVC in ns that only goes once release drops
// its extra finalizer.
func heldPVC(t *testing.T, ns string) *corev1.PersistentVolumeClaim {
	t.Helper()
	class := "standard"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "held", Namespace: ns, Finalizers: []string{holdFinalizer}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: &class,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("100Mi")}},
		},
	}
	if err := admin.Create(ctx, pvc); err != nil {
		t.Fatal(err)
	}
	// Even when the test fails: let the PVC go, and its NFS volume with it.
	t.Cleanup(func() {
		release(t, pvc)
		waitFor(t, "PersistentVolume of "+ns+" deleted", 3*time.Minute, func() (bool, error) {
			var pvs corev1.PersistentVolumeList
			err := admin.List(ctx, &pvs)
			for _, pv := range pvs.Items {
				if pv.Spec.ClaimRef != nil && pv.Spec.ClaimRef.Namespace == ns {
					return false, err
				}
			}
			return err == nil, err
		})
	})
	return pvc
}

func release(t *testing.T, pvc *corev1.PersistentVolumeClaim) {
	t.Helper()
	got := &corev1.PersistentVolumeClaim{}
	if err := admin.Get(ctx, client.ObjectKeyFromObject(pvc), got); err != nil {
		return
	}
	var keep []string
	for _, f := range got.Finalizers {
		if f != holdFinalizer {
			keep = append(keep, f)
		}
	}
	got.Finalizers = keep
	if err := admin.Update(ctx, got); err != nil {
		t.Errorf("release PVC: %v", err)
	}
}

// heldPod makes a Pod in ns that terminates but only goes once releasePod
// drops its extra finalizer: a stuck Pod.
func heldPod(t *testing.T, ns string) *corev1.Pod {
	t.Helper()
	p := pod(ns, "held", func(p *corev1.Pod) { p.Finalizers = []string{holdFinalizer} })
	if err := admin.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releasePod(t, p) })
	return p
}

func releasePod(t *testing.T, p *corev1.Pod) {
	t.Helper()
	got := &corev1.Pod{}
	if err := admin.Get(ctx, client.ObjectKeyFromObject(p), got); err != nil {
		return
	}
	got.Finalizers = nil
	if err := admin.Update(ctx, got); err != nil {
		t.Errorf("release Pod: %v", err)
	}
}
