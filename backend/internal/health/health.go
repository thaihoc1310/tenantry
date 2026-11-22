// Package health builds the Platform health page (PRODUCT-PLAN §19,
// /admin/health): the parts the platform needs, judged from what the
// database shows of them, the work in hand, and each cluster's pools.
package health

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Thresholds past which a part shows a warning.
const (
	eventLag   = time.Minute     // an event waiting this long: the provisioning worker is behind or down
	syncLag    = 2 * time.Minute // a group change waiting this long: Keycloak or the sync worker
	checkStale = 2 * time.Minute // four missed health checks (every 30 s)
)

const (
	OK   = "ok"
	Warn = "warn"
)

type Component struct {
	Name, State, Detail string
}

type Cluster struct {
	store.Cluster
	Pools []reservation.PoolView
}

type Report struct {
	Components []Component
	Work       store.HealthCountsRow
	Clusters   []Cluster
}

type Service struct {
	db *pgxpool.Pool
	q  *store.Queries
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db, q: store.New(db)} }

// Check reports on the platform as of now (Platform Admin).
func (s *Service) Check(ctx context.Context, now time.Time) (Report, error) {
	w, err := s.q.HealthCounts(ctx)
	if err != nil {
		return Report{}, err
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return Report{}, err
	}
	r := Report{Work: w, Components: []Component{
		{Name: "Platform API", State: OK, Detail: "Answering: this page came from it."},
		provisioning(w),
		groupSync(w),
		keycloak(w),
	}}
	for _, c := range clusters {
		r.Components = append(r.Components, cluster(c, now))
		pools, err := reservation.PoolViews(ctx, s.q, c.ID)
		if err != nil {
			return Report{}, err
		}
		r.Clusters = append(r.Clusters, Cluster{Cluster: c, Pools: pools})
	}
	return r, nil
}

func seconds(f float64) time.Duration {
	return time.Duration(f * float64(time.Second)).Round(time.Second)
}

func provisioning(w store.HealthCountsRow) Component {
	c := Component{Name: "Provisioning worker", State: OK, Detail: "No event waiting for a cluster."}
	if w.EventsWaiting > 0 {
		lag := seconds(w.OldestEventSeconds)
		c.Detail = fmt.Sprintf("%d events waiting; the oldest for %s.", w.EventsWaiting, lag)
		if lag > eventLag {
			c.State = Warn
		}
	}
	return c
}

func groupSync(w store.HealthCountsRow) Component {
	c := Component{Name: "Group sync worker", State: OK, Detail: "Keycloak has every group, member and account change."}
	if w.SyncWaiting > 0 {
		lag := seconds(w.OldestSyncSeconds)
		c.Detail = fmt.Sprintf("%d changes waiting for Keycloak; the oldest for %s.", w.SyncWaiting, lag)
		if lag > syncLag {
			c.State = Warn
		}
	}
	return c
}

// keycloak is judged by the group sync worker's last failure: the API only
// meets Keycloak at login.
func keycloak(w store.HealthCountsRow) Component {
	if w.LastSyncError != "" {
		return Component{Name: "Keycloak", State: Warn, Detail: "The group sync worker's last call failed: " + w.LastSyncError}
	}
	return Component{Name: "Keycloak", State: OK, Detail: "No failed call from the group sync worker."}
}

func cluster(c store.Cluster, now time.Time) Component {
	out := Component{Name: c.DisplayName, State: OK}
	if c.LastHealthCheck == nil {
		out.State, out.Detail = Warn, "Never checked: no provisioning worker has reached it."
	} else {
		age := now.Sub(*c.LastHealthCheck).Round(time.Second)
		out.Detail = fmt.Sprintf("%s; last check %s ago.", c.Status, age)
		if age > checkStale {
			out.State = Warn
		}
	}
	if c.Status != reservation.ClusterReady {
		out.State = Warn
	}
	return out
}
