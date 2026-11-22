package controller

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// labelControlPlane is the role label kubeadm puts on control-plane nodes.
const labelControlPlane = "node-role.kubernetes.io/control-plane"

// nodeKind classifies a node like cluster_nodes.kind.
func nodeKind(n *corev1.Node) v1.NodeKind {
	if _, cp := n.Labels[labelControlPlane]; cp {
		return v1.NodeControlPlane
	}
	if n.Labels[v1.LabelComputePool] == v1.SystemPool {
		return v1.NodeSystem
	}
	return v1.NodeWorker
}

func nodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// allocatable keeps what capacity math uses: cpu, memory, pods.
func allocatable(n *corev1.Node) v1.ResourceList {
	out := v1.ResourceList{}
	for _, k := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourcePods} {
		if q, ok := n.Status.Allocatable[k]; ok {
			out[string(k)] = q.DeepCopy()
		}
	}
	return out
}

func addTo(sum v1.ResourceList, l v1.ResourceList) {
	for k, q := range l {
		total := sum[k]
		if total.IsZero() {
			total = resource.Quantity{Format: q.Format}
		}
		total.Add(q)
		sum[k] = total
	}
}

// setNodeLabel sets (value != nil) or removes (nil) one label of a node and
// touches nothing else: a JSON merge patch of that key alone.
func setNodeLabel(ctx context.Context, c client.Client, node, key string, value *string) error {
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{key: value}}})
	if err != nil {
		return err
	}
	n := &corev1.Node{}
	n.Name = node
	return c.Patch(ctx, n, client.RawPatch(types.MergePatchType, patch))
}
