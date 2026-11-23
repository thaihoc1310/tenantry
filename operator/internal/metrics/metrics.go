// Package metrics holds the operator's Prometheus metrics (PRODUCT-PLAN
// §21.2), next to controller-runtime's own (reconcile count, errors and
// duration per controller). Gauges are read from the cache at scrape time,
// so they cannot drift from the objects.
package metrics

import (
	"context"
	"net/netip"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

var (
	// ProvisionSeconds: from a Workspace's creation to its first Ready.
	ProvisionSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "tenantry_workspace_provision_seconds",
		Help:    "Time from a PlatformWorkspace's creation to its first Ready phase.",
		Buckets: []float64{5, 10, 20, 30, 60, 120, 300, 600},
	})
	// ProxyProbes counts the proxyaccess probes by result (a condition reason).
	ProxyProbes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tenantry_proxy_probes_total",
		Help: "Synthetic checks through Capsule Proxy, by result.",
	}, []string{"result"})
)

var (
	workspacesDesc = prometheus.NewDesc("tenantry_workspaces", "PlatformWorkspaces by phase.", []string{"phase"}, nil)
	routesDesc     = prometheus.NewDesc("tenantry_routes", "Routes by readiness.", []string{"ready"}, nil)
	poolNodesDesc  = prometheus.NewDesc("tenantry_shared_pool_ready_nodes", "Ready nodes of a shared pool.", []string{"pool"}, nil)
	poolAllocDesc  = prometheus.NewDesc("tenantry_shared_pool_allocatable", "Allocatable of a shared pool.", []string{"pool", "resource"}, nil)
	ipUsedDesc     = prometheus.NewDesc("tenantry_workspace_ip_used", "Addresses Calico gave out in a Workspace's CIDR.", []string{"workspace"}, nil)
	ipSizeDesc     = prometheus.NewDesc("tenantry_workspace_ip_capacity", "Addresses in a Workspace's CIDR.", []string{"workspace"}, nil)
)

// Register adds the operator's metrics to controller-runtime's registry.
func Register(reader client.Reader) {
	crmetrics.Registry.MustRegister(ProvisionSeconds, ProxyProbes, &collector{reader: reader})
}

type collector struct{ reader client.Reader }

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{workspacesDesc, routesDesc, poolNodesDesc, poolAllocDesc, ipUsedDesc, ipSizeDesc} {
		ch <- d
	}
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var workspaces v1.PlatformWorkspaceList
	if err := c.reader.List(ctx, &workspaces); err == nil {
		// Every phase, so a cluster without Workspaces shows zeros, not no data.
		phases := map[string]float64{}
		for _, p := range []v1.WorkspacePhase{v1.PhaseProvisioning, v1.PhaseReady, v1.PhaseUpdating, v1.PhaseDegraded, v1.PhaseFailed, v1.PhaseDeleting} {
			phases[string(p)] = 0
		}
		routes := map[string]float64{"true": 0, "false": 0}
		for _, ws := range workspaces.Items {
			phase := ws.Status.Phase
			if phase == "" {
				// Created, not reconciled once yet: what the controller will
				// report first, not a phase of its own.
				phase = v1.PhaseProvisioning
			}
			phases[string(phase)]++
			for _, r := range ws.Status.Routes {
				if r.Ready {
					routes["true"]++
				} else {
					routes["false"]++
				}
			}
		}
		for phase, n := range phases {
			ch <- prometheus.MustNewConstMetric(workspacesDesc, prometheus.GaugeValue, n, phase)
		}
		for ready, n := range routes {
			ch <- prometheus.MustNewConstMetric(routesDesc, prometheus.GaugeValue, n, ready)
		}
		c.collectIPs(ctx, ch, workspaces.Items)
	}
	var pools v1.PlatformSharedPoolList
	if err := c.reader.List(ctx, &pools); err == nil {
		for _, p := range pools.Items {
			ch <- prometheus.MustNewConstMetric(poolNodesDesc, prometheus.GaugeValue, float64(p.Status.ReadyNodes), p.Name)
			for res, q := range p.Status.Allocatable {
				ch <- prometheus.MustNewConstMetric(poolAllocDesc, prometheus.GaugeValue, q.AsApproximateFloat64(), p.Name, res)
			}
		}
	}
}

// collectIPs: how full each Workspace's CIDR is, from Calico's blocks.
func (c *collector) collectIPs(ctx context.Context, ch chan<- prometheus.Metric, workspaces []v1.PlatformWorkspace) {
	blocks := &unstructured.UnstructuredList{}
	blocks.SetGroupVersionKind(render.IPAMBlockGVK.GroupVersion().WithKind(render.IPAMBlockGVK.Kind + "List"))
	if err := c.reader.List(ctx, blocks); err != nil {
		return
	}
	for _, ws := range workspaces {
		if ws.Spec.Network == nil {
			continue
		}
		cidr, err := netip.ParsePrefix(ws.Spec.Network.CIDR)
		if err != nil {
			continue
		}
		used := 0
		for _, b := range blocks.Items {
			s, _, _ := unstructured.NestedString(b.Object, "spec", "cidr")
			if p, err := netip.ParsePrefix(s); err != nil || !cidr.Overlaps(p) {
				continue
			}
			allocations, _, _ := unstructured.NestedSlice(b.Object, "spec", "allocations")
			for _, a := range allocations {
				if a != nil {
					used++
				}
			}
		}
		ch <- prometheus.MustNewConstMetric(ipUsedDesc, prometheus.GaugeValue, float64(used), ws.Name)
		ch <- prometheus.MustNewConstMetric(ipSizeDesc, prometheus.GaugeValue, float64(uint64(1)<<(32-cidr.Bits())), ws.Name)
	}
}
