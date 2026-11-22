package provisioning

import (
	"context"
	"errors"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

func TestHandler(t *testing.T) {
	ctx := context.Background()
	c := newFakeCluster()
	h := Handler(c)
	apply := store.OutboxEvent{Type: "workspace.apply", Payload: []byte(`{"apiVersion":"tenantry.io/v1alpha1","kind":"PlatformWorkspace",
		"metadata":{"name":"acme-solar"},"spec":{"workspaceId":"ws-1"}}`)}
	if err := h.Handle(ctx, apply); err != nil || c.get(k8s.Workspaces, "acme-solar") == nil {
		t.Fatalf("apply: %v", err)
	}
	// Applying the same CR again is a no-op for the cluster: no new generation.
	if err := h.Handle(ctx, apply); err != nil || c.get(k8s.Workspaces, "acme-solar").GetGeneration() != 1 {
		t.Fatalf("second apply: %v", err)
	}
	if err := h.Handle(ctx, store.OutboxEvent{Type: "workspace.force", Payload: []byte(`{"name":"acme-solar"}`)}); err != nil ||
		c.get(k8s.Workspaces, "acme-solar").GetAnnotations()["tenantry.io/force-delete"] != "true" {
		t.Fatalf("force: %v", err)
	}
	if err := h.Handle(ctx, store.OutboxEvent{Type: "workspace.delete", Payload: []byte(`{"name":"acme-solar"}`)}); err != nil ||
		c.get(k8s.Workspaces, "acme-solar") != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, e := range []store.OutboxEvent{
		{Type: "tenant.apply", Payload: []byte(`{}`)},
		{Type: "workspace.patch", Payload: []byte(`{}`)},
		{Type: "workspace.apply", Payload: []byte(`not json`)},
		{Type: "nodegrant.delete", Payload: []byte(`{}`)},
		{Type: "sharedpool.force", Payload: []byte(`{"name":"x"}`)},
	} {
		if err := h.Handle(ctx, e); !outbox.IsPermanent(err) {
			t.Errorf("%s %s: %v, want a permanent error", e.Type, e.Payload, err)
		}
	}
	// The cluster refusing the credential will refuse it again.
	c.fail = errForbidden
	if err := h.Handle(ctx, apply); !outbox.IsPermanent(err) {
		t.Fatalf("forbidden: %v, want permanent", err)
	}
	c.fail = errors.New("connection refused")
	if err := h.Handle(ctx, apply); err == nil || outbox.IsPermanent(err) {
		t.Fatalf("unreachable: %v, want a temporary error", err)
	}
}
