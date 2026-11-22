// Package gauges reads, at each scrape, the numbers of internal/metrics that
// live in the database. Apart from metrics so that the packages metrics
// counts in (operation, outbox) need not import the ones read here.
package gauges

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/thaihoc1310/tenantry/backend/internal/metrics"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Provisioning adds what the provisioning worker of clusterID
// watches: its outbox backlog and the address blocks left in its pools.
func Provisioning(db *pgxpool.Pool, clusterID string, log *slog.Logger) {
	q := store.New(db)
	pending := prometheus.NewDesc("tenantry_outbox_pending_events", "Outbox events of the cluster not delivered yet.", []string{"cluster"}, nil)
	lag := prometheus.NewDesc("tenantry_outbox_oldest_pending_seconds", "Age of the oldest undelivered outbox event of the cluster: the queue lag.", []string{"cluster"}, nil)
	backlog := func(ctx context.Context) (store.OutboxBacklogRow, error) { return q.OutboxBacklog(ctx, clusterID) }
	metrics.Registry.MustRegister(
		metrics.Gauge{Desc: pending, Log: log, Read: func(ctx context.Context) ([]metrics.Sample, error) {
			b, err := backlog(ctx)
			return []metrics.Sample{{Value: float64(b.Pending), Labels: []string{clusterID}}}, err
		}},
		metrics.Gauge{Desc: lag, Log: log, Read: func(ctx context.Context) ([]metrics.Sample, error) {
			b, err := backlog(ctx)
			return []metrics.Sample{{Value: b.OldestSeconds, Labels: []string{clusterID}}}, err
		}},
		metrics.Gauge{
			Desc: prometheus.NewDesc("tenantry_network_pool_free_blocks",
				"Address blocks still free in a network pool, at each prefix length it allows (PRODUCT-PLAN §7.2).", []string{"pool", "prefix_length"}, nil),
			Log:  log,
			Read: func(ctx context.Context) ([]metrics.Sample, error) { return freeBlocks(ctx, q, clusterID) },
		},
	)
}

func freeBlocks(ctx context.Context, q *store.Queries, clusterID string) ([]metrics.Sample, error) {
	pools, err := q.ListNetworkPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	cluster, err := q.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	taken, err := q.ClusterAllocatedCIDRs(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	var out []metrics.Sample
	for _, p := range pools {
		for _, l := range p.AllowedPrefixLengths {
			out = append(out, metrics.Sample{
				Value:  float64(reservation.FreeBlocks(p.Cidrs, int(l), taken, cluster.ReservedCidrs)),
				Labels: []string{p.ID, strconv.Itoa(int(l))},
			})
		}
	}
	return out, nil
}

// GroupSync adds what the group sync watches: rows Keycloak has not
// caught up with and how long the oldest has waited, by kind.
func GroupSync(db *pgxpool.Pool, log *slog.Logger) {
	q := store.New(db)
	read := func(value func(store.GroupSyncBacklogRow) float64) func(ctx context.Context) ([]metrics.Sample, error) {
		return func(ctx context.Context) ([]metrics.Sample, error) {
			rows, err := q.GroupSyncBacklog(ctx)
			out := make([]metrics.Sample, 0, len(rows))
			for _, r := range rows {
				out = append(out, metrics.Sample{Value: value(r), Labels: []string{r.Kind}})
			}
			return out, err
		}
	}
	metrics.Registry.MustRegister(
		metrics.Gauge{
			Desc: prometheus.NewDesc("tenantry_groupsync_pending", "Groups, memberships and accounts Keycloak has not caught up with, by kind.", []string{"kind"}, nil),
			Log:  log, Read: read(func(r store.GroupSyncBacklogRow) float64 { return float64(r.Pending) }),
		},
		metrics.Gauge{
			Desc: prometheus.NewDesc("tenantry_groupsync_oldest_pending_seconds", "How long the oldest change not in Keycloak yet has waited, by kind: the sync lag.", []string{"kind"}, nil),
			Log:  log, Read: read(func(r store.GroupSyncBacklogRow) float64 { return r.OldestSeconds }),
		},
	)
}
