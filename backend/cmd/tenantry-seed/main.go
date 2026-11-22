// Command tenantry-seed puts in the data an installation starts from
// (IMPLEMENTATION-PLAN §10): the cluster with the ranges it already uses, its
// nodes, its shared pool, the shared network pool and the onboarding policy
// approvals propose (PRODUCT-PLAN §5.2.1).
//
//	tenantry-seed --proxy-endpoint https://kube-a.example.com \
//	    --console-url https://console-a.example.com --apps-domain mvp.example.com \
//	    --reserved-cidrs 192.168.0.0/16,10.96.0.0/12 \
//	    --node name=wk2,kind=Worker,cpu=3,memory=11776Mi,pods=110,ready=true,schedulable=true,pool=shared-general
//
// Nodes are written only if the cluster does not know them: from M6 the
// operator reports them (PlatformNodeInventory).
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
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

type options struct {
	clusterID, clusterName                string
	proxyEndpoint, consoleURL, appsDomain string
	networkCIDR                           string
	storageClass                          string
	reservedCIDRs                         []netip.Prefix
	nodes                                 []node
}

// node is one node of the cluster as --node gives it.
type node struct {
	name, kind, pool string
	ready, schedule  bool
	cpuMilli, memory int64
	pods             int32
}

// parseNode reads name=…,kind=…,cpu=…,memory=…,pods=…,ready=…,schedulable=…,pool=…
func parseNode(v string) (node, error) {
	n := node{kind: "Worker"}
	for kv := range strings.SplitSeq(v, ",") {
		k, val, ok := strings.Cut(kv, "=")
		if !ok {
			return n, fmt.Errorf("--node %q: want key=value pairs", v)
		}
		var err error
		switch k {
		case "name":
			n.name = val
		case "kind":
			n.kind = val
		case "pool":
			n.pool = val
		case "ready":
			n.ready, err = strconv.ParseBool(val)
		case "schedulable":
			n.schedule, err = strconv.ParseBool(val)
		case "cpu":
			var q resource.Quantity
			if q, err = resource.ParseQuantity(val); err == nil {
				n.cpuMilli = q.MilliValue()
			}
		case "memory":
			var q resource.Quantity
			if q, err = resource.ParseQuantity(val); err == nil {
				n.memory = q.Value()
			}
		case "pods":
			var p int64
			p, err = strconv.ParseInt(val, 10, 32)
			n.pods = int32(p)
		default:
			return n, fmt.Errorf("--node %q: unknown key %q", v, k)
		}
		if err != nil {
			return n, fmt.Errorf("--node %q: %s: %w", v, k, err)
		}
	}
	if n.name == "" {
		return n, fmt.Errorf("--node %q: name is required", v)
	}
	return n, nil
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
	fs.Func("reserved-cidrs", "comma-separated ranges the cluster already uses (Pod, Service CIDRs)", func(v string) error {
		for c := range strings.SplitSeq(v, ",") {
			if c = strings.TrimSpace(c); c == "" {
				continue
			}
			p, err := netip.ParsePrefix(c)
			if err != nil || p != p.Masked() {
				return fmt.Errorf("%q is not a network address", c)
			}
			o.reservedCIDRs = append(o.reservedCIDRs, p)
		}
		return nil
	})
	fs.Func("node", "a node of the cluster (repeatable): name=,kind=,cpu=,memory=,pods=,ready=,schedulable=,pool=", func(v string) error {
		n, err := parseNode(v)
		o.nodes = append(o.nodes, n)
		return err
	})
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

// The per-node system reserve the shared pool starts with: what kubelet,
// the CNI and the node's daemons keep (architecture/05 §4).
var poolReserve = struct {
	cpuMilli, memory int64
	pods             int32
}{500, 1536 << 20, 10}

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
	n, err = q.SeedPoolSystemReserve(ctx, store.SeedPoolSystemReserveParams{
		ID: sharedPoolID, SystemReserveCpuMilli: poolReserve.cpuMilli, SystemReserveMemoryBytes: poolReserve.memory,
		SystemReservePods: poolReserve.pods,
	})
	if err := report("system reserve of "+sharedPoolID, n, err); err != nil {
		return err
	}
	if len(o.reservedCIDRs) > 0 {
		n, err = q.SeedReservedCIDRs(ctx, store.SeedReservedCIDRsParams{ID: o.clusterID, ReservedCidrs: o.reservedCIDRs})
		if err := report("reserved ranges of "+o.clusterID, n, err); err != nil {
			return err
		}
	}
	for _, nd := range o.nodes {
		var pool *string
		if nd.pool != "" {
			pool = &nd.pool
		}
		n, err = q.SeedClusterNode(ctx, store.SeedClusterNodeParams{
			ClusterID: o.clusterID, NodeName: nd.name, Kind: nd.kind, Ready: nd.ready, Schedulable: nd.schedule,
			AllocatableCpuMilli: nd.cpuMilli, AllocatableMemoryBytes: nd.memory, AllocatablePods: nd.pods, SharedPoolID: pool,
		})
		if err := report("node "+nd.name, n, err); err != nil {
			return err
		}
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
