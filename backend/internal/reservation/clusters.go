package reservation

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// The cluster registry as a Platform Admin keeps it (PRODUCT-PLAN §16.5,
// architecture/04-state-machines §6). In the MVP every status change is the
// Admin's; the worker only records last_health_check.

const (
	ClusterRegistered  = "Registered"
	ClusterMaintenance = "Maintenance"
	ClusterUnavailable = "Unavailable"
)

var (
	ErrNoSuchCluster = apperr.New(apperr.NotFound, "cluster not found")
	ErrClusterTaken  = &apperr.Error{Code: apperr.Conflict, Message: "a cluster with this id exists", Details: map[string]any{"field": "id"}}
	// dnsLabel is the shape of ids that become Kubernetes label values (clusters, pools).
	dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	// dnsName is a domain name of DNS labels.
	dnsName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
)

type Service struct {
	db *pgxpool.Pool
	q  *store.Queries
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db, q: store.New(db)} }

// Cluster is what an Admin says about a cluster.
type Cluster struct {
	ID, DisplayName                       string
	ProxyEndpoint, ConsoleURL, AppsDomain string
	ReservedCIDRs                         []netip.Prefix
}

func (c *Cluster) check() error {
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if c.DisplayName == "" || len(c.DisplayName) > 80 {
		return apperr.Invalid("displayName", "must be 1 to 80 characters")
	}
	for field, v := range map[string]string{"proxyEndpoint": c.ProxyEndpoint, "consoleUrl": c.ConsoleURL} {
		if u, err := url.Parse(v); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return apperr.Invalid(field, "must be an https URL")
		}
	}
	if len(c.AppsDomain) > 253 || !dnsName.MatchString(c.AppsDomain) {
		return apperr.Invalid("appsDomain", "must be a domain name, like apps.example.com")
	}
	for i, p := range c.ReservedCIDRs {
		if p != p.Masked() {
			return apperr.Invalid("reservedCidrs", fmt.Sprintf("%s is not a network address", p))
		}
		for _, q := range c.ReservedCIDRs[:i] {
			if p.Overlaps(q) {
				return apperr.Invalid("reservedCidrs", fmt.Sprintf("%s overlaps %s", p, q))
			}
		}
	}
	return nil
}

func (s *Service) Clusters(ctx context.Context) ([]store.Cluster, error) {
	return s.q.ListClusters(ctx)
}

// Register adds a cluster, Registered until an Admin marks it Ready.
func (s *Service) Register(ctx context.Context, adminID string, c Cluster) (store.Cluster, error) {
	if !dnsLabel.MatchString(c.ID) {
		return store.Cluster{}, apperr.Invalid("id", "must be a DNS label: lowercase letters, digits and hyphens, at most 63")
	}
	if err := c.check(); err != nil {
		return store.Cluster{}, err
	}
	var out store.Cluster
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		if out, err = q.CreateCluster(ctx, store.CreateClusterParams{
			ID: c.ID, DisplayName: c.DisplayName, ProxyEndpoint: c.ProxyEndpoint, ConsoleUrl: c.ConsoleURL,
			AppsDomain: c.AppsDomain, ReservedCidrs: nonNilPrefixes(c.ReservedCIDRs),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, Action: audit.ClusterRegister, TargetType: "Cluster", TargetID: c.ID, Detail: clusterDetail(out),
		})
	})
	if _, ok := store.UniqueViolation(err); ok {
		return store.Cluster{}, ErrClusterTaken
	}
	return out, err
}

// ClusterPatch holds the fields to change; nil keeps the current value.
type ClusterPatch struct {
	DisplayName, ProxyEndpoint, ConsoleURL, AppsDomain, Status *string
	ReservedCIDRs                                              *[]netip.Prefix
}

// transitions are the status changes an Admin may make (04-state-machines §6):
// nothing goes back to Registered.
var transitions = map[string][]string{
	ClusterRegistered:  {ClusterReady},
	ClusterReady:       {ClusterMaintenance, ClusterUnavailable},
	ClusterMaintenance: {ClusterReady, ClusterUnavailable},
	ClusterUnavailable: {ClusterReady, ClusterMaintenance},
}

// UpdateCluster changes a cluster's metadata or status. Reserved ranges may
// not overlap a network pool of the cluster.
func (s *Service) UpdateCluster(ctx context.Context, adminID, clusterID string, p ClusterPatch) (store.Cluster, error) {
	var out store.Cluster
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.LockCluster(ctx, clusterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchCluster
		}
		if err != nil {
			return err
		}
		c := Cluster{ID: cur.ID, DisplayName: cur.DisplayName, ProxyEndpoint: cur.ProxyEndpoint, ConsoleURL: cur.ConsoleUrl,
			AppsDomain: cur.AppsDomain, ReservedCIDRs: cur.ReservedCidrs}
		set := func(dst *string, v *string) {
			if v != nil {
				*dst = *v
			}
		}
		set(&c.DisplayName, p.DisplayName)
		set(&c.ProxyEndpoint, p.ProxyEndpoint)
		set(&c.ConsoleURL, p.ConsoleURL)
		set(&c.AppsDomain, p.AppsDomain)
		if p.ReservedCIDRs != nil {
			c.ReservedCIDRs = *p.ReservedCIDRs
		}
		if err := c.check(); err != nil {
			return err
		}
		status := cur.Status
		if p.Status != nil && *p.Status != cur.Status {
			if !slices.Contains(transitions[cur.Status], *p.Status) {
				return &apperr.Error{Code: apperr.Conflict, Details: map[string]any{"field": "status"},
					Message: fmt.Sprintf("a %s cluster cannot become %s", cur.Status, *p.Status)}
			}
			status = *p.Status
		}
		if p.ReservedCIDRs != nil {
			pools, err := q.NetworkPoolRangesOfCluster(ctx, clusterID)
			if err != nil {
				return err
			}
			for _, r := range c.ReservedCIDRs {
				for _, pool := range pools {
					for _, pc := range pool.Cidrs {
						if r.Overlaps(pc) {
							return overlapWith("reservedCidrs", r, pc, "network pool "+pool.ID)
						}
					}
				}
			}
		}
		if out, err = q.UpdateCluster(ctx, store.UpdateClusterParams{
			ID: c.ID, DisplayName: c.DisplayName, Status: status, ProxyEndpoint: c.ProxyEndpoint, ConsoleUrl: c.ConsoleURL,
			AppsDomain: c.AppsDomain, ReservedCidrs: nonNilPrefixes(c.ReservedCIDRs),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, Action: audit.ClusterUpdate, TargetType: "Cluster", TargetID: c.ID,
			Detail: map[string]any{"before": clusterDetail(cur), "after": clusterDetail(out)},
		})
	})
	return out, err
}

func clusterDetail(c store.Cluster) map[string]any {
	cidrs := make([]string, len(c.ReservedCidrs))
	for i, p := range c.ReservedCidrs {
		cidrs[i] = p.String()
	}
	return map[string]any{"displayName": c.DisplayName, "status": c.Status, "proxyEndpoint": c.ProxyEndpoint,
		"consoleUrl": c.ConsoleUrl, "appsDomain": c.AppsDomain, "reservedCidrs": cidrs}
}

func nonNilPrefixes(p []netip.Prefix) []netip.Prefix {
	if p == nil {
		return []netip.Prefix{}
	}
	return p
}
