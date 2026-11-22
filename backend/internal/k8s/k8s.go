// Package k8s is the provisioning worker's only door to a cluster
// (IMPLEMENTATION-PLAN M6 step 6): server-side apply and delete of the four
// tenantry.io CRs, and a watch cache of them. The API never calls
// Kubernetes. The credential behind it may touch those CRDs and nothing else
// (architecture/06 §7), so this package asks for nothing more.
package k8s

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// FieldManager owns the fields the worker applies (PRODUCT-PLAN §14.4).
const FieldManager = "tenantry-provisioning-worker"

// The four CRDs, cluster-scoped.
var (
	Workspaces    = v1.GroupVersion.WithResource("platformworkspaces")
	SharedPools   = v1.GroupVersion.WithResource("platformsharedpools")
	NodeGrants    = v1.GroupVersion.WithResource("platformnodegrants")
	NodeInventory = v1.GroupVersion.WithResource("platformnodeinventories")
)

// resyncEvery re-lists the watched CRs, a safety net under the watch.
const resyncEvery = 5 * time.Minute

// Client applies, deletes and watches the tenantry.io CRs of one cluster.
type Client struct {
	dyn       dynamic.Interface
	discovery discovery.DiscoveryInterface
	informers dynamicinformer.DynamicSharedInformerFactory
	listers   map[schema.GroupVersionResource]cache.GenericLister
	synced    []cache.InformerSynced
}

// Config is the in-cluster ServiceAccount, or the given kubeconfig when
// running outside the cluster (dev only, IMPLEMENTATION-PLAN §10).
func Config(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		return rest.InClusterConfig()
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func New(cfg *rest.Config) (*Client, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewForClients(dyn, disc), nil
}

// NewForClients builds a Client on given clients (tests pass fakes).
func NewForClients(dyn dynamic.Interface, disc discovery.DiscoveryInterface) *Client {
	f := dynamicinformer.NewDynamicSharedInformerFactory(dyn, resyncEvery)
	c := &Client{dyn: dyn, discovery: disc, informers: f, listers: map[schema.GroupVersionResource]cache.GenericLister{}}
	for _, gvr := range []schema.GroupVersionResource{Workspaces, SharedPools, NodeGrants, NodeInventory} {
		inf := f.ForResource(gvr)
		c.listers[gvr] = inf.Lister()
		c.synced = append(c.synced, inf.Informer().HasSynced)
	}
	return c
}

// Start runs the watches until ctx ends and waits for their first list.
func (c *Client) Start(ctx context.Context) error {
	c.informers.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), c.synced...) {
		return fmt.Errorf("k8s: watch caches did not sync: %w", ctx.Err())
	}
	return nil
}

// Apply server-side applies obj with the worker's field manager, taking over
// fields another manager set (the worker's desired state wins). Apply is
// idempotent: the same object twice changes nothing, not even the generation.
func (c *Client) Apply(ctx context.Context, gvr schema.GroupVersionResource, obj *unstructured.Unstructured) error {
	_, err := c.dyn.Resource(gvr).Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: FieldManager, Force: true})
	return err
}

// Delete deletes a CR in the background (its finalizer runs); one already
// gone is deleted.
func (c *Client) Delete(ctx context.Context, gvr schema.GroupVersionResource, name string) error {
	bg := metav1.DeletePropagationBackground
	err := c.dyn.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &bg})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// MarkForceDelete sets tenantry.io/force-delete on a PlatformWorkspace, as
// a field manager of its own so the spec the worker applies stays as it is.
func (c *Client) MarkForceDelete(ctx context.Context, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(v1.GroupVersion.String())
	obj.SetKind("PlatformWorkspace")
	obj.SetName(name)
	obj.SetAnnotations(map[string]string{v1.AnnotationForceDelete: "true"})
	_, err := c.dyn.Resource(Workspaces).Apply(ctx, name, obj, metav1.ApplyOptions{FieldManager: FieldManager + "-force", Force: true})
	if apierrors.IsNotFound(err) {
		return nil // gone already
	}
	return err
}

// Ping asks the cluster for its version (the health check, IMPLEMENTATION-PLAN §5.1).
func (c *Client) Ping(context.Context) error {
	_, err := c.discovery.ServerVersion()
	return err
}

// Workspaces, SharedPools, NodeGrants and Inventory read the watch cache.

func (c *Client) Workspaces() ([]v1.PlatformWorkspace, error) {
	return list[v1.PlatformWorkspace](c.listers[Workspaces])
}

func (c *Client) SharedPools() ([]v1.PlatformSharedPool, error) {
	return list[v1.PlatformSharedPool](c.listers[SharedPools])
}

func (c *Client) NodeGrants() ([]v1.PlatformNodeGrant, error) {
	return list[v1.PlatformNodeGrant](c.listers[NodeGrants])
}

// Inventory is the cluster's PlatformNodeInventory; found is false until the
// operator's chart has created it.
func (c *Client) Inventory() (inv v1.PlatformNodeInventory, found bool, err error) {
	obj, err := c.listers[NodeInventory].Get(v1.NodeInventoryName)
	if apierrors.IsNotFound(err) {
		return inv, false, nil
	}
	if err != nil {
		return inv, false, err
	}
	return inv, true, fromUnstructured(obj, &inv)
}

func list[T any](l cache.GenericLister) ([]T, error) {
	objs, err := l.List(labelsEverything)
	if err != nil {
		return nil, err
	}
	out := make([]T, len(objs))
	for i, o := range objs {
		if err := fromUnstructured(o, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func fromUnstructured(o runtime.Object, into any) error {
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("k8s: cache holds %T, not unstructured", o)
	}
	return runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, into)
}

// ToUnstructured turns a typed CR into what Apply takes.
func ToUnstructured(obj any) (*unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: m}, nil
}

var labelsEverything = labels.Everything()
