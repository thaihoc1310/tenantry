package workspace

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// How people reach a Workspace's cluster (PRODUCT-PLAN §11, §12): kubectl
// through the cluster's Capsule Proxy, and its Headlamp. Neither hands out a
// credential: the token is always the user's own, from Keycloak.

// Connection is what a kubeconfig or a console link is made of.
type Connection struct {
	Workspace store.Workspace
	Cluster   store.Cluster
	Namespace string // the first Namespace, "" when it has none yet
}

func (s *Service) connection(ctx context.Context, userID, wsID string) (Connection, error) {
	ws, _, err := s.access(ctx, userID, wsID)
	if err != nil {
		return Connection{}, err
	}
	c, err := s.q.GetCluster(ctx, ws.ClusterID)
	if err != nil {
		return Connection{}, err
	}
	nss, err := s.q.ListWorkspaceNamespaces(ctx, ws.ID)
	if err != nil {
		return Connection{}, err
	}
	out := Connection{Workspace: ws, Cluster: c}
	for _, n := range nss {
		if n.State == nsActive {
			out.Namespace = n.Name
			break
		}
	}
	return out, nil
}

// Kubeconfig is what a kubeconfig of the Workspace needs; the download is audited.
func (s *Service) Kubeconfig(ctx context.Context, userID, wsID string) (Connection, error) {
	c, err := s.connection(ctx, userID, wsID)
	if err != nil {
		return c, err
	}
	return c, audit.Record(ctx, s.q, audit.Event{
		Actor: userID, OrgID: c.Workspace.OrgID, Action: audit.KubeconfigDownload,
		TargetType: "Workspace", TargetID: c.Workspace.ID, Detail: map[string]any{"slug": c.Workspace.Slug},
	})
}

// Console is the Workspace's cluster for its Headlamp, refused while the
// cluster is Unavailable.
func (s *Service) Console(ctx context.Context, userID, wsID string) (Connection, error) {
	c, err := s.connection(ctx, userID, wsID)
	if err == nil && c.Cluster.Status == reservation.ClusterUnavailable {
		err = apperr.New(apperr.ClusterNotReady, "the cluster is Unavailable; its web console cannot be reached")
	}
	return c, err
}
