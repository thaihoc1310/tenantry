// Command tenantry-worker runs the background loops (IMPLEMENTATION-PLAN
// §5.3). It ships in the same image as tenantry-api but runs as its own
// Deployments, one per set of credentials, picked with --loops:
//
//	provisioning  delivers outbox events to its cluster and copies status back (TENANTRY_CLUSTER_ID)
//	housekeeping  deletes expired rows (PostgreSQL only)
//	groupsync     mirrors organization groups to Keycloak (TENANTRY_KEYCLOAK_*)
//	all           every loop; for running locally
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/housekeeping"
	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
	"github.com/thaihoc1310/tenantry/backend/internal/keycloak"
	"github.com/thaihoc1310/tenantry/backend/internal/metrics"
	"github.com/thaihoc1310/tenantry/backend/internal/metrics/gauges"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/provisioning"
)

var allLoops = []string{"provisioning", "housekeeping", "groupsync"}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("tenantry-worker stopped", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("tenantry-worker", flag.ContinueOnError)
	loopsFlag := fs.String("loops", "", "loops to run, comma-separated: "+strings.Join(allLoops, ", ")+", or all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	loops, err := parseLoops(*loopsFlag)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := cfg.Logger()
	slog.SetDefault(log)
	// Only the groupsync Deployment holds the Keycloak secret.
	var kc *keycloak.Client
	if slices.Contains(loops, "groupsync") {
		k, err := config.LoadKeycloakSync()
		if err != nil {
			return err
		}
		kc = keycloak.NewClient(k)
	}
	// Only the provisioning Deployment holds the cluster credential.
	var prov config.Provisioning
	var cluster *k8s.Client
	if slices.Contains(loops, "provisioning") {
		if prov, err = config.LoadProvisioning(); err != nil {
			return err
		}
		rc, err := k8s.Config(prov.Kubeconfig)
		if err != nil {
			return fmt.Errorf("cluster credential: %w", err)
		}
		if cluster, err = k8s.New(rc); err != nil {
			return err
		}
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	defer pool.Close()

	// SIGTERM lets each loop finish what it has in hand, then exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// Each loop's numbers, on the port no Ingress reaches.
	if slices.Contains(loops, "provisioning") {
		gauges.Provisioning(pool, prov.ClusterID, log)
	}
	if slices.Contains(loops, "groupsync") {
		gauges.GroupSync(pool, log)
	}
	go metrics.Serve(ctx, cfg.MetricsAddr, log)

	var wg sync.WaitGroup
	for _, l := range loops {
		log := log.With("loop", l)
		wg.Go(func() {
			log.Info("loop started")
			switch l {
			case "provisioning":
				runProvisioning(ctx, pool, prov, cluster, log)
			case "housekeeping":
				housekeeping.Run(ctx, pool, log)
			case "groupsync":
				keycloak.NewWorker(pool, kc, log).Run(ctx)
			}
			log.Info("loop stopped")
		})
	}
	wg.Wait()
	return nil
}

// runProvisioning delivers events, copies the CRs' status back once the
// watch cache has synced, and keeps the health check fresh, until ctx ends.
func runProvisioning(ctx context.Context, pool *pgxpool.Pool, cfg config.Provisioning, cluster *k8s.Client, log *slog.Logger) {
	var wg sync.WaitGroup
	wg.Go(func() { outbox.NewWorker(pool, cfg.ClusterID, provisioning.Handler(cluster), log).Run(ctx) })
	wg.Go(func() { provisioning.RunHealth(ctx, pool, cfg.ClusterID, cluster, log) })
	wg.Go(func() {
		if err := cluster.Start(ctx); err != nil {
			log.Error("watching the cluster failed; status is not copied back", "err", err)
			return
		}
		provisioning.NewSyncer(pool, cfg.ClusterID, cluster, cfg.OperationTimeout, log).Run(ctx)
	})
	wg.Wait()
}

// parseLoops reads --loops. It is required: a Deployment that forgot it
// must not quietly run loops whose credentials it does not hold.
func parseLoops(v string) ([]string, error) {
	switch strings.TrimSpace(v) {
	case "":
		return nil, errors.New("--loops is required")
	case "all":
		return allLoops, nil
	}
	var loops []string
	for l := range strings.SplitSeq(v, ",") {
		l = strings.TrimSpace(l)
		if !slices.Contains(allLoops, l) {
			return nil, fmt.Errorf("--loops: unknown loop %q (want %s, or all)", l, strings.Join(allLoops, ", "))
		}
		if !slices.Contains(loops, l) {
			loops = append(loops, l)
		}
	}
	return loops, nil
}
