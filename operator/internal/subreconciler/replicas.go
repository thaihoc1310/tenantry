package subreconciler

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// replicaSoon is how often a Workspace waiting for Capsule to copy its
// LimitRange and NetworkPolicies into a new Namespace looks again: the copies
// came 0 to 11 s after the Namespace on the lab (docs/report/namespace-latency.md).
const replicaSoon = 2 * time.Second

// Replicas checks that Capsule has copied every object of a Workspace's
// GlobalTenantResource into each of its Namespaces. The replication's Ready
// condition alone is not enough: on the lab it was True while a new
// Namespace still had no NetworkPolicy, so the Workspace said Ready while
// the tenantry-tenant-network-ready policy still refused its Pods.
//
// It reads with Reader, straight from the API server, so the operator needs
// get on NetworkPolicies and LimitRanges but no cache of every one of them
// in the cluster. A Namespace found complete is remembered and not read
// again; Capsule puts back a copy someone deletes.
//
// ponytail: the memory holds one entry per Namespace the process has seen
// complete and is never pruned; prune on Namespace delete if that grows.
type Replicas struct {
	Reader client.Reader

	mu       sync.Mutex
	complete map[string]bool // namespace UID | object list → all present
}

// replicaItem is one object the replication copies into every Namespace.
type replicaItem struct {
	gvk  schema.GroupVersionKind
	name string
}

// items lists the objects of a GlobalTenantResource's rawItems.
func items(rep *unstructured.Unstructured) []replicaItem {
	var out []replicaItem
	resources, _, _ := unstructured.NestedSlice(rep.Object, "spec", "resources")
	for _, r := range resources {
		raw, _ := r.(map[string]any)["rawItems"].([]any)
		for _, it := range raw {
			obj, _ := it.(map[string]any)
			u := unstructured.Unstructured{Object: obj}
			if u.GetKind() == "" || u.GetName() == "" {
				continue
			}
			out = append(out, replicaItem{gvk: u.GroupVersionKind(), name: u.GetName()})
		}
	}
	return out
}

// Missing names the copies not yet in the Workspace's live Namespaces, as
// "<namespace>: <Kind>/<name>, …"; empty when every Namespace has them all.
func (r *Replicas) Missing(ctx context.Context, c client.Client, ws *v1.PlatformWorkspace, rep *unstructured.Unstructured) (string, error) {
	want := items(rep)
	if len(want) == 0 {
		return "", nil
	}
	names := make([]string, len(want))
	for i, it := range want {
		names[i] = it.gvk.Kind + "/" + it.name
	}
	slices.Sort(names)
	set := strings.Join(names, ",")

	namespaces, err := TenantNamespaces(ctx, c, ws)
	if err != nil {
		return "", err
	}
	var missing []string
	for _, ns := range namespaces {
		if ns.DeletionTimestamp != nil {
			continue
		}
		key := string(ns.UID) + "|" + set
		r.mu.Lock()
		done := r.complete[key]
		r.mu.Unlock()
		if done {
			continue
		}
		var absent []string
		for _, it := range want {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(it.gvk)
			switch err := r.Reader.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: it.name}, obj); {
			case apierrors.IsNotFound(err):
				absent = append(absent, it.gvk.Kind+"/"+it.name)
			case err != nil:
				return "", err
			}
		}
		if len(absent) > 0 {
			missing = append(missing, fmt.Sprintf("%s: %s", ns.Name, strings.Join(absent, ", ")))
			continue
		}
		r.mu.Lock()
		if r.complete == nil {
			r.complete = map[string]bool{}
		}
		r.complete[key] = true
		r.mu.Unlock()
	}
	slices.Sort(missing)
	return strings.Join(missing, "; "), nil
}
