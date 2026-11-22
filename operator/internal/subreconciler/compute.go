package subreconciler

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Reasons of ComputeReady.
const (
	// ReasonPoolHasNoNode: no Ready node carries the shared pool's label.
	ReasonPoolHasNoNode = "PoolHasNoNode"
	// ReasonNodeNotGranted: the Dedicated node is missing or does not carry
	// the Organization's label (PlatformNodeGrant).
	ReasonNodeNotGranted = "NodeNotGranted"
	ReasonNodeNotReady   = "NodeNotReady"
)

// Compute reports ComputeReady and, for a Dedicated Workspace, puts the
// membership label workspaces.tenantry.io/<ws-id>=true on its node
// (sub-reconciler compute, IMPLEMENTATION-PLAN §4.1). The Tenant's
// nodeSelector, which holds the Pods there, is rendered with the Tenant.
func Compute(ctx context.Context, c client.Client, ws *v1.PlatformWorkspace) Result {
	const t = v1.ConditionComputeReady
	if ws.Spec.Compute.Mode != v1.ComputeDedicated {
		var nodes corev1.NodeList
		if err := c.List(ctx, &nodes, client.MatchingLabels{v1.LabelComputePool: ws.Spec.Compute.SharedPoolID}); err != nil {
			return failure(t, err)
		}
		for i := range nodes.Items {
			if nodeIsReady(&nodes.Items[i]) {
				return ok(t)
			}
		}
		return failed(t, ReasonPoolHasNoNode, "no Ready node in shared pool "+ws.Spec.Compute.SharedPoolID)
	}

	node := &corev1.Node{}
	err := c.Get(ctx, client.ObjectKey{Name: ws.Spec.Compute.NodeName}, node)
	if apierrors.IsNotFound(err) {
		return failed(t, ReasonNodeNotGranted, "no node "+ws.Spec.Compute.NodeName)
	}
	if err != nil {
		return failure(t, err)
	}
	if got := node.Labels[v1.LabelDedicatedOrg]; got != ws.Spec.OrganizationID {
		return failed(t, ReasonNodeNotGranted, fmt.Sprintf("node %s is not dedicated to %s (label %s=%q)",
			node.Name, ws.Spec.OrganizationID, v1.LabelDedicatedOrg, got))
	}
	if node.Labels[render.MembershipLabel(ws)] != "true" {
		value := "true"
		if err := SetNodeLabel(ctx, c, node.Name, render.MembershipLabel(ws), &value); err != nil {
			return failure(t, err)
		}
	}
	if !nodeIsReady(node) {
		return failed(t, ReasonNodeNotReady, "node "+node.Name+" is not Ready")
	}
	return ok(t)
}

// SetNodeLabel sets (value != nil) or removes (nil) one label of a node and
// touches nothing else: a JSON merge patch of that key alone.
func SetNodeLabel(ctx context.Context, c client.Client, node, key string, value *string) error {
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{key: value}}})
	if err != nil {
		return err
	}
	n := &corev1.Node{}
	n.Name = node
	return c.Patch(ctx, n, client.RawPatch(types.MergePatchType, patch))
}

func nodeIsReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
