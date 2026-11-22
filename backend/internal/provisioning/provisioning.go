// Package provisioning is the provisioning worker (IMPLEMENTATION-PLAN §5.1):
// it delivers outbox events to its cluster as tenantry.io CRs, copies what
// the controller reports back into the database, and keeps the cluster's
// health check fresh.
package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/jackc/pgx/v5/pgxpool"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Cluster is what the worker needs of a cluster; *k8s.Client is the real one.
type Cluster interface {
	Apply(ctx context.Context, gvr schema.GroupVersionResource, obj *unstructured.Unstructured) error
	Delete(ctx context.Context, gvr schema.GroupVersionResource, name string) error
	Ping(ctx context.Context) error
	Workspaces() ([]v1.PlatformWorkspace, error)
	SharedPools() ([]v1.PlatformSharedPool, error)
	NodeGrants() ([]v1.PlatformNodeGrant, error)
	Inventory() (v1.PlatformNodeInventory, bool, error)
}

// Event types are <kind>.<verb>: apply carries the whole CR, delete
// {"name": ...}. The kinds are the CRs the worker may write.
var kinds = map[string]schema.GroupVersionResource{
	"workspace":  k8s.Workspaces,
	"sharedpool": k8s.SharedPools,
	"nodegrant":  k8s.NodeGrants,
}

// Handler delivers outbox events to c.
func Handler(c Cluster) outbox.Handler {
	return outbox.HandlerFunc(func(ctx context.Context, e store.OutboxEvent) error {
		kind, verb, _ := strings.Cut(e.Type, ".")
		gvr, ok := kinds[kind]
		if !ok || (verb != "apply" && verb != "delete") {
			return outbox.Permanent(fmt.Errorf("unknown event type %q", e.Type))
		}
		var err error
		if verb == "apply" {
			obj := &unstructured.Unstructured{}
			if err := json.Unmarshal(e.Payload, &obj.Object); err != nil {
				return outbox.Permanent(fmt.Errorf("%s payload: %w", e.Type, err))
			}
			err = c.Apply(ctx, gvr, obj)
		} else {
			var p struct{ Name string }
			if err := json.Unmarshal(e.Payload, &p); err != nil || p.Name == "" {
				return outbox.Permanent(fmt.Errorf("%s payload names no object", e.Type))
			}
			err = c.Delete(ctx, gvr, p.Name)
		}
		// The cluster refusing the credential or the object will refuse it
		// again: no use waiting 10 attempts to tell the user.
		if apierrors.IsForbidden(err) || apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) || apierrors.IsUnauthorized(err) {
			return outbox.Permanent(err)
		}
		return err
	})
}

// HealthEvery is how often the cluster is pinged (IMPLEMENTATION-PLAN §5.1).
const HealthEvery = 30 * time.Second

// RunHealth pings the cluster and records last_health_check on success. It
// never changes the cluster's status: an Admin does (04-state-machines §6).
func RunHealth(ctx context.Context, db *pgxpool.Pool, clusterID string, c Cluster, log *slog.Logger) {
	q := store.New(db)
	for {
		if err := c.Ping(ctx); err != nil {
			log.Warn("cluster health check failed", "cluster_id", clusterID, "err", err)
		} else if err := q.TouchClusterHealth(ctx, clusterID); err != nil {
			log.Error("record cluster health check", "cluster_id", clusterID, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(HealthEvery):
		}
	}
}
