// Package subreconciler holds the steps PlatformWorkspaceReconciler runs in
// order (IMPLEMENTATION-PLAN §4.1). Each step applies what render produced
// and reports one Condition; Wait asks the caller to look again soon.
package subreconciler

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FieldManager of every server-side apply of the controller (IMPLEMENTATION-PLAN §4.2).
const FieldManager = "tenantry-workspace-controller"

// Reasons of the conditions the steps report. A False condition with
// ReasonProgressing is a step still under way; any other False reason is a
// failure (architecture/04 §1.4 tells them apart for the phase).
const (
	ReasonReconciled  = "Reconciled"
	ReasonProgressing = "Progressing"
	// ReasonRejected: the API server or a webhook refused an object, so
	// trying again with the same spec cannot help.
	ReasonRejected    = "Rejected"
	ReasonApplyFailed = "ApplyFailed"
	// ReasonCapsuleNotReady: Capsule reports one of its objects not Ready.
	ReasonCapsuleNotReady = "CapsuleNotReady"
	// ReasonNamespaceFailed: a Namespace could not be created or deleted.
	ReasonNamespaceFailed = "NamespaceFailed"
	// ReasonCordoned: the Workspace is cordoned, so Capsule refuses the
	// Namespace changes still pending.
	ReasonCordoned = "Cordoned"
)

type Result struct {
	Condition metav1.Condition
	Wait      bool
	// After asks for a slower look again than Wait, for a state that
	// changes on its own time and that no watch reports.
	After time.Duration
}

func ok(t string) Result {
	return Result{Condition: metav1.Condition{Type: t, Status: metav1.ConditionTrue, Reason: ReasonReconciled}}
}

func waiting(t, msg string) Result {
	return Result{Wait: true, Condition: metav1.Condition{Type: t, Status: metav1.ConditionFalse, Reason: ReasonProgressing, Message: msg}}
}

func failed(t, reason, msg string) Result {
	return Result{Wait: true, Condition: metav1.Condition{Type: t, Status: metav1.ConditionFalse, Reason: reason, Message: msg}}
}

// failure turns an API error into the condition it deserves.
func failure(t string, err error) Result {
	if apierrors.IsInvalid(err) || apierrors.IsForbidden(err) || apierrors.IsBadRequest(err) {
		return failed(t, ReasonRejected, err.Error())
	}
	return failed(t, ReasonApplyFailed, err.Error())
}

// Apply server-side applies obj, taking over fields someone else changed
// (drift is restored, PRODUCT-PLAN §14.4), and reads the result back into obj.
func Apply(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	return c.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj), client.ForceOwnership, client.FieldOwner(FieldManager))
}

// IsTransient tells a step's error worth returning to controller-runtime (it
// retries with backoff) from one that only fails the condition.
func IsTransient(err error) bool {
	var s apierrors.APIStatus
	if !errors.As(err, &s) {
		return true
	}
	return apierrors.IsConflict(err) || apierrors.IsServerTimeout(err) || apierrors.IsTimeout(err) ||
		apierrors.IsTooManyRequests(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err)
}
