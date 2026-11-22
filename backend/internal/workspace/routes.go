package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Routes publish a Service of a Workspace at https://<name>-<k8s_name>.<apps
// domain> (PRODUCT-PLAN §7.6). Tenants never write an Ingress: a Route goes
// into spec.routes and the controller builds the Ingress, with the platform's
// wildcard certificate. Who may change them is who may change Namespaces.

// MaxRoutes per Workspace (PRODUCT-PLAN §7.6).
const MaxRoutes = 20

const (
	routeDesired  = "Desired"
	routeActive   = "Active"
	routeDeleting = "Deleting"
)

var (
	ErrNoRoute = apperr.New(apperr.NotFound, "route not found")
	// A DNS label for the name; a DNS-1035 label for the Service, as Kubernetes names Services.
	routeName   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	serviceName = regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)
)

// NewRoute is what a person asks for: a Namespace of the Workspace by its
// full name, a name, and the Service and port to send requests to.
type NewRoute struct {
	Namespace, Name, Service string
	Port                     int32
}

type RouteChange struct {
	Route     store.WorkspaceRoute
	Operation store.Operation
}

// Hostname is where a Route answers: its first label must fit 63 characters.
func Hostname(name, k8sName, appsDomain string) (string, error) {
	label := name + "-" + k8sName
	if len(label) > 63 {
		return "", apperr.Invalid("name", fmt.Sprintf("at most %d characters here: %s-%s must fit 63", 63-len(k8sName)-1, name, k8sName))
	}
	return label + "." + appsDomain, nil
}

// Routes lists a Workspace's Routes for anyone who sees it.
func (s *Service) Routes(ctx context.Context, userID, wsID string) ([]store.WorkspaceRoute, error) {
	if _, _, err := s.access(ctx, userID, wsID); err != nil {
		return nil, err
	}
	return s.q.ListWorkspaceRoutes(ctx, wsID)
}

// CreateRoute adds a Route to a Ready Workspace, in one of its Active Namespaces.
func (s *Service) CreateRoute(ctx context.Context, userID, wsID string, r NewRoute, correlationID string) (RouteChange, error) {
	if _, err := s.tenantOwner(ctx, userID, wsID); err != nil {
		return RouteChange{}, err
	}
	switch {
	case !routeName.MatchString(r.Name):
		return RouteChange{}, apperr.Invalid("name", "lowercase letters, digits and hyphens")
	case !serviceName.MatchString(r.Service) || len(r.Service) > 63:
		return RouteChange{}, apperr.Invalid("service", "a Service name: lowercase letters, digits and hyphens, starting with a letter")
	case r.Port < 1 || r.Port > 65535:
		return RouteChange{}, apperr.Invalid("port", "from 1 to 65535")
	}
	var out RouteChange
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockForChange(ctx, q, wsID)
		if err != nil {
			return err
		}
		ns, err := q.GetWorkspaceNamespace(ctx, store.GetWorkspaceNamespaceParams{WorkspaceID: ws.ID, Name: r.Namespace})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Invalid("namespace", "is not a Namespace of the Workspace")
		}
		if err != nil {
			return err
		}
		if ns.State != nsActive {
			return &apperr.Error{Code: apperr.WorkspaceNotReady, Message: fmt.Sprintf("Namespace %s is %s, not Active yet", ns.Name, ns.State),
				Details: map[string]any{"field": "namespace"}}
		}
		if n, err := q.CountWorkspaceRoutes(ctx, ws.ID); err != nil {
			return err
		} else if n >= MaxRoutes {
			return &apperr.Error{Code: apperr.RouteQuotaExceeded, Message: fmt.Sprintf("the Workspace has %d Routes, as many as allowed", n),
				Details: map[string]any{"limit": MaxRoutes}}
		}
		cluster, err := q.GetCluster(ctx, ws.ClusterID)
		if err != nil {
			return err
		}
		host, err := Hostname(r.Name, ws.K8sName, cluster.AppsDomain)
		if err != nil {
			return err
		}
		uid := userID
		route, err := q.CreateWorkspaceRoute(ctx, store.CreateWorkspaceRouteParams{
			ID: id.New(id.Route), WorkspaceID: ws.ID, Namespace: ns.Name, Name: r.Name, Hostname: host,
			ServiceName: r.Service, ServicePort: r.Port, CreatedBy: &uid,
		})
		if err != nil {
			return err
		}
		op, err := beginUpdate(ctx, q, ws, "route.create", userID, correlationID)
		if err != nil {
			return err
		}
		out = RouteChange{Route: route, Operation: op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: audit.RouteCreate, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{"name": r.Name, "hostname": host, "namespace": ns.Name, "service": r.Service, "port": r.Port, "operationId": op.ID},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == "workspace_routes_hostname_uq" {
		return RouteChange{}, &apperr.Error{Code: apperr.RouteHostnameTaken, Message: "another Route holds this hostname", Details: map[string]any{"field": "name"}}
	}
	return out, err
}

// DeleteRoute takes a Route out of spec.routes; the controller deletes its Ingress.
func (s *Service) DeleteRoute(ctx context.Context, userID, wsID, routeID, correlationID string) (RouteChange, error) {
	if _, err := s.tenantOwner(ctx, userID, wsID); err != nil {
		return RouteChange{}, err
	}
	var out RouteChange
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockForChange(ctx, q, wsID)
		if err != nil {
			return err
		}
		route, err := q.GetWorkspaceRoute(ctx, store.GetWorkspaceRouteParams{ID: routeID, WorkspaceID: ws.ID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && route.State == routeDeleting) {
			return ErrNoRoute
		}
		if err != nil {
			return err
		}
		if route, err = q.SetRouteState(ctx, store.SetRouteStateParams{ID: route.ID, State: routeDeleting}); err != nil {
			return err
		}
		op, err := beginUpdate(ctx, q, ws, "route.delete", userID, correlationID)
		if err != nil {
			return err
		}
		out = RouteChange{Route: route, Operation: op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: audit.RouteDelete, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{"name": route.Name, "hostname": route.Hostname, "operationId": op.ID},
		})
	})
	return out, err
}
