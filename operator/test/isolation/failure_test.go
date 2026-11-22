//go:build lab

package isolation

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// holdFinalizer keeps a PVC as a stuck CSI driver would.
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
		// Kill the operator as soon as it has started on the Workspace.
		waitFor(t, "provisioning started", time.Minute, func() (bool, error) {
			got := &v1.PlatformWorkspace{}
			err := admin.Get(ctx, client.ObjectKey{Name: ws.Name}, got)
			return len(got.Finalizers) > 0, err
		})
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
		t.Logf("Ready %s after creation, %s after the operator was killed", time.Since(start).Round(time.Second), time.Since(killed).Round(time.Second))
		// Nothing doubled: one Tenant owned once, its Namespaces once.
		var nss corev1.NamespaceList
		if err := admin.List(ctx, &nss, client.MatchingLabels{"capsule.clastix.io/tenant": ws.Name}); err != nil || len(nss.Items) != 2 {
			t.Errorf("Namespaces of the Tenant: %d (%v), want 2", len(nss.Items), err)
		}
	})

	t.Run("delete held by a PVC finalizer, then released", func(t *testing.T) {
		ws := workspace("optest-fail-pvc", "01fail", "10.240.252.0/24")
		ws.Spec.Network = nil
		ws.Spec.Namespaces = []string{"data"}
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "Ready", 3*time.Minute, ready(ws.Name))
		pvc := heldPVC(t, "optest-fail-pvc-data")
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
		release(t, pvc)
		waitFor(t, "deleted once the PVC is released", 3*time.Minute, gone(ws.Name))
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
		pvc := heldPVC(t, "optest-fail-force-data")
		t.Cleanup(func() { release(t, pvc) })
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
		waitFor(t, "force-deleted", 2*time.Minute, gone(ws.Name))
		ns := &corev1.Namespace{}
		if err := admin.Get(ctx, client.ObjectKey{Name: "optest-fail-force-data"}, ns); err == nil {
			t.Logf("the held Namespace stays Terminating until the PVC is released (phase %s), as :force intends", ns.Status.Phase)
		}
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
