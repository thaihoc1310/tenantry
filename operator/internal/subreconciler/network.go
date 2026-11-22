package subreconciler

import (
	"context"
	"fmt"
	"net/netip"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Network applies the Workspace's Calico IPPool and reports NetworkReady
// (sub-reconciler network, IMPLEMENTATION-PLAN §4.1). The Namespace
// annotation that picks the pool is part of the Tenant, the baseline
// NetworkPolicies part of the replication. A Workspace without a network
// (made before M9) has no pool and no NetworkReady: applicable is false.
func Network(ctx context.Context, c client.Client, scheme *runtime.Scheme, ws *v1.PlatformWorkspace) (res Result, applicable bool) {
	const t = v1.ConditionNetworkReady
	if ws.Spec.Network == nil {
		return Result{}, false
	}
	pool := render.IPPool(ws)
	if err := controllerutil.SetControllerReference(ws, pool, scheme); err != nil {
		return failure(t, err), true
	}
	if err := Apply(ctx, c, pool); err != nil {
		return failure(t, err), true
	}
	return ok(t), true
}

// IPAMBlocks are Calico's blocks inside cidr, by name; inUse names those
// that still give out addresses, with how many.
func IPAMBlocks(ctx context.Context, c client.Client, cidr string) (blocks, inUse []string, err error) {
	within, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, nil, err
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(render.IPAMBlockGVK.GroupVersion().WithKind(render.IPAMBlockGVK.Kind + "List"))
	if err := c.List(ctx, list); err != nil {
		return nil, nil, err
	}
	for _, b := range list.Items {
		s, _, _ := unstructured.NestedString(b.Object, "spec", "cidr")
		p, err := netip.ParsePrefix(s)
		if err != nil || !within.Overlaps(p) {
			continue
		}
		blocks = append(blocks, b.GetName())
		allocations, _, _ := unstructured.NestedSlice(b.Object, "spec", "allocations")
		if n := len(allocations) - countNil(allocations); n > 0 {
			inUse = append(inUse, fmt.Sprintf("%s (%d addresses)", s, n))
		}
	}
	slices.Sort(blocks)
	slices.Sort(inUse)
	return blocks, inUse, nil
}

func countNil(s []any) int {
	n := 0
	for _, v := range s {
		if v == nil {
			n++
		}
	}
	return n
}
