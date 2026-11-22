// Command tenantry-seed puts in the data an installation starts from
// (IMPLEMENTATION-PLAN §10): the cluster, its shared pool, the shared network
// pool and the onboarding policy approvals propose (PRODUCT-PLAN §5.2.1).
//
//	tenantry-seed --proxy-endpoint https://kube-a.example.com \
//	    --console-url https://console-a.example.com --apps-domain mvp.example.com
//
// Running it again changes nothing: existing rows are kept as they are, so
// it never undoes what an Admin has changed since.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

type options struct {
	clusterID, clusterName                string
	proxyEndpoint, consoleURL, appsDomain string
	networkCIDR                           string
	storageClass                          string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var o options
	fs := flag.NewFlagSet("tenantry-seed", flag.ContinueOnError)
	fs.StringVar(&o.clusterID, "cluster-id", "cluster-a", "id of the cluster")
	fs.StringVar(&o.clusterName, "cluster-name", "Cluster A", "display name of the cluster")
	fs.StringVar(&o.proxyEndpoint, "proxy-endpoint", "", "Capsule Proxy URL, the server in tenant kubeconfigs (required)")
	fs.StringVar(&o.consoleURL, "console-url", "", "Headlamp URL (required)")
	fs.StringVar(&o.appsDomain, "apps-domain", "", "domain of tenant Routes (required)")
	fs.StringVar(&o.networkCIDR, "network-cidr", "10.240.0.0/18", "address space of the shared network pool: the first quarter of the tenant supernet 10.240.0.0/16")
	fs.StringVar(&o.storageClass, "storage-class", "standard", "StorageClass the onboarding grant allows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cidr, err := o.check()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := cfg.Logger()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return seed(ctx, store.New(tx), o, cidr, log) })
}

func (o options) check() (netip.Prefix, error) {
	var errs []error
	for name, v := range map[string]string{"--proxy-endpoint": o.proxyEndpoint, "--console-url": o.consoleURL} {
		if u, err := url.Parse(v); err != nil || u.Scheme != "https" || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s: want an https URL, got %q", name, v))
		}
	}
	if o.appsDomain == "" {
		errs = append(errs, errors.New("--apps-domain is required"))
	}
	cidr, err := netip.ParsePrefix(o.networkCIDR)
	if err != nil || cidr != cidr.Masked() {
		errs = append(errs, fmt.Errorf("--network-cidr: %q is not a network address", o.networkCIDR))
	}
	return cidr, errors.Join(errs...)
}

const (
	sharedPoolID    = "shared-general"
	networkPoolID   = "onboarding-pod-network"
	policyID        = "default"
	overcommit      = "3.0"
	workspacePrefix = 24 // a /24 per Workspace
)

func seed(ctx context.Context, q *store.Queries, o options, cidr netip.Prefix, log *slog.Logger) error {
	report := func(what string, created int64, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if created == 1 {
			log.Info("created", "row", what)
		} else {
			log.Info("kept as it is", "row", what)
		}
		return nil
	}

	// No bootstrap check promotes a cluster yet (M5), so the seeded one starts Ready.
	n, err := q.SeedCluster(ctx, store.SeedClusterParams{
		ID: o.clusterID, DisplayName: o.clusterName, Status: "Ready",
		ProxyEndpoint: o.proxyEndpoint, ConsoleUrl: o.consoleURL, AppsDomain: o.appsDomain,
	})
	if err := report("cluster "+o.clusterID, n, err); err != nil {
		return err
	}

	var ratio pgtype.Numeric
	if err := ratio.Scan(overcommit); err != nil {
		return err
	}
	n, err = q.SeedSharedPool(ctx, store.SeedSharedPoolParams{
		ID: sharedPoolID, ClusterID: o.clusterID, Name: "Shared general", AllocationOvercommitRatio: ratio,
	})
	if err := report("shared pool "+sharedPoolID, n, err); err != nil {
		return err
	}

	n, err = q.SeedNetworkPool(ctx, store.SeedNetworkPoolParams{
		ID: networkPoolID, ClusterID: o.clusterID, Cidrs: []netip.Prefix{cidr},
		DefaultPrefixLength: workspacePrefix, AllowedPrefixLengths: []int32{workspacePrefix},
	})
	if err := report("network pool "+networkPoolID, n, err); err != nil {
		return err
	}

	entitlement, grant, err := policyTemplates(o.storageClass)
	if err != nil {
		return err
	}
	n, err = q.SeedOnboardingPolicy(ctx, store.SeedOnboardingPolicyParams{
		ID: policyID, DefaultClusterID: o.clusterID, EntitlementTemplate: entitlement, GrantTemplate: grant,
	})
	return report("onboarding policy "+policyID, n, err)
}

// policyTemplates are the onboarding defaults of PRODUCT-PLAN §5.1 and
// §5.2.1, built from the API's own types so they are what the approval form
// takes. The grant leaves out clusterId: the policy's default cluster fills it.
func policyTemplates(storageClass string) (entitlement, grant []byte, err error) {
	entitlement, err = json.Marshal(oapi.Entitlement{MaxMembers: 20, MaxGroups: 10, MaxWorkspacesTotal: 8})
	if err != nil {
		return nil, nil, err
	}
	b, err := json.Marshal(oapi.ClusterGrantInput{
		MaxWorkspaces: 2, MaxNamespacesTotal: 6,
		Resources: oapi.GrantResources{
			RequestsCpu: "2", RequestsMemory: "4Gi", LimitsCpu: "4", LimitsMemory: "8Gi", Pods: 20, RequestsStorage: "20Gi",
		},
		SharedPoolIds:         []oapi.PoolId{sharedPoolID},
		NetworkPoolIds:        []oapi.PoolId{networkPoolID},
		AllowedStorageClasses: []string{storageClass},
	})
	if err != nil {
		return nil, nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, nil, err
	}
	delete(doc, "clusterId")
	grant, err = json.Marshal(doc)
	return entitlement, grant, err
}
