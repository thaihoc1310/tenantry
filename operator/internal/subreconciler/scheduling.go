package subreconciler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// Unschedulable is the scheduler's message on a Pod it cannot place, or ""
// for any other Pod: one being deleted, not Pending, scheduling-gated, or
// placed and only starting its containers.
func Unschedulable(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodPending {
		return ""
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
			if c.Message == "" {
				return c.Reason
			}
			return c.Message
		}
	}
	return ""
}

// Scheduling counts the Pods the scheduler cannot place in the Workspace's
// live Namespaces: in total, with the message of the one waiting the longest
// (nil when there is none), and per Namespace. It does not touch a
// condition: a Workspace whose Pods do not fit is still Ready, its pool is
// what is full (ADR-0017). The operator caches Pending Pods only.
func Scheduling(ctx context.Context, c client.Reader, namespaces []corev1.Namespace) (*v1.SchedulingStatus, map[string]int32, error) {
	var out *v1.SchedulingStatus
	var oldest string // namespace/name of the Pod out.Message is from
	perNamespace := map[string]int32{}
	for _, ns := range namespaces {
		if ns.DeletionTimestamp != nil {
			continue
		}
		var pods corev1.PodList
		if err := c.List(ctx, &pods, client.InNamespace(ns.Name)); err != nil {
			return nil, nil, err
		}
		for i := range pods.Items {
			p := &pods.Items[i]
			msg := Unschedulable(p)
			if msg == "" {
				continue
			}
			perNamespace[ns.Name]++
			since := scheduledFalseSince(p)
			if out == nil {
				out = &v1.SchedulingStatus{}
			}
			out.UnschedulablePods++
			// Times are to the second: ties go to the first name, so the
			// message does not flip between passes.
			key := p.Namespace + "/" + p.Name
			if out.Since == nil || since.Before(out.Since) || (since.Equal(out.Since) && key < oldest) {
				out.Message, out.Since, oldest = msg, since, key
			}
		}
	}
	return out, perNamespace, nil
}

// scheduledFalseSince is when the Pod's PodScheduled condition last changed.
func scheduledFalseSince(p *corev1.Pod) *metav1.Time {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled {
			t := c.LastTransitionTime
			return &t
		}
	}
	t := p.CreationTimestamp
	return &t
}
