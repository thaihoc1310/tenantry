package provisioning

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
)

// fakeCluster keeps CRs in memory the way the API server would for the
// worker: apply creates or replaces, a changed spec bumps the generation.
// Tests play the controller by editing status through setStatus.
type fakeCluster struct {
	mu        sync.Mutex
	objs      map[schema.GroupVersionResource]map[string]*unstructured.Unstructured
	inventory *v1.PlatformNodeInventory
	fail      error // returned by Apply and Delete when set
	pingErr   error
	applies   int
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objs: map[schema.GroupVersionResource]map[string]*unstructured.Unstructured{}}
}

func (f *fakeCluster) Apply(_ context.Context, gvr schema.GroupVersionResource, obj *unstructured.Unstructured) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.applies++
	if f.objs[gvr] == nil {
		f.objs[gvr] = map[string]*unstructured.Unstructured{}
	}
	obj = obj.DeepCopy()
	old := f.objs[gvr][obj.GetName()]
	gen := int64(1)
	if old != nil {
		gen = old.GetGeneration()
		if !equalSpec(old, obj) {
			gen++
		}
		if st, ok := old.Object["status"]; ok {
			obj.Object["status"] = st
		}
	}
	obj.SetGeneration(gen)
	f.objs[gvr][obj.GetName()] = obj
	return nil
}

func equalSpec(a, b *unstructured.Unstructured) bool {
	sa, _, _ := unstructured.NestedFieldCopy(a.Object, "spec")
	sb, _, _ := unstructured.NestedFieldCopy(b.Object, "spec")
	return equality.Semantic.DeepEqual(sa, sb)
}

func (f *fakeCluster) Delete(_ context.Context, gvr schema.GroupVersionResource, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	delete(f.objs[gvr], name)
	return nil
}

func (f *fakeCluster) Ping(context.Context) error { return f.pingErr }

func (f *fakeCluster) MarkForceDelete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o := f.objs[k8s.Workspaces][name]; o != nil {
		a := o.GetAnnotations()
		if a == nil {
			a = map[string]string{}
		}
		a[v1.AnnotationForceDelete] = "true"
		o.SetAnnotations(a)
	}
	return nil
}

func (f *fakeCluster) get(gvr schema.GroupVersionResource, name string) *unstructured.Unstructured {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o := f.objs[gvr][name]; o != nil {
		return o.DeepCopy()
	}
	return nil
}

// setStatus replaces a CR's status as the controller would; status is a pointer.
func (f *fakeCluster) setStatus(gvr schema.GroupVersionResource, name string, status any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(status)
	if err != nil {
		panic(err)
	}
	f.objs[gvr][name].Object["status"] = m
}

func list[T any](f *fakeCluster, gvr schema.GroupVersionResource) ([]T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []T
	for _, o := range f.objs[gvr] {
		var t T
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeCluster) Workspaces() ([]v1.PlatformWorkspace, error) {
	return list[v1.PlatformWorkspace](f, k8s.Workspaces)
}

func (f *fakeCluster) SharedPools() ([]v1.PlatformSharedPool, error) {
	return list[v1.PlatformSharedPool](f, k8s.SharedPools)
}

func (f *fakeCluster) NodeGrants() ([]v1.PlatformNodeGrant, error) {
	return list[v1.PlatformNodeGrant](f, k8s.NodeGrants)
}

func (f *fakeCluster) Inventory() (v1.PlatformNodeInventory, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inventory == nil {
		return v1.PlatformNodeInventory{}, false, nil
	}
	return *f.inventory, true, nil
}

var errForbidden = apierrors.NewForbidden(k8s.Workspaces.GroupResource(), "x", nil)
