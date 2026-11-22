// Package metrics is what tenantry-api and tenantry-worker tell Prometheus
// (PRODUCT-PLAN §21.2), on a port of their own that no Ingress routes to.
// Counters and histograms are updated where things happen; numbers that live
// in the database (backlogs, free address blocks) are read when Prometheus
// scrapes, each with a short timeout.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds every metric of the process, with the Go runtime's and the
// process's own.
var Registry = prometheus.NewRegistry()

func init() {
	Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

var (
	// API.
	Requests = promauto(prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tenantry_api_requests_total", Help: "Requests to the API, by route pattern, method and status code.",
	}, []string{"route", "method", "code"}))
	RequestDuration = promauto(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "tenantry_api_request_duration_seconds", Help: "Time to answer a request, by route pattern and method; event streams excluded.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"route", "method"}))
	Logins = promauto(prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tenantry_api_logins_total", Help: "Portal logins through Keycloak, by result (ok, refused, error).",
	}, []string{"result"}))

	// Operations, in the process that ends them.
	OperationDuration = promauto(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "tenantry_operation_duration_seconds", Help: "From an operation's creation to its end, by type and final state.",
		Buckets: []float64{1, 5, 10, 20, 30, 60, 120, 300, 600, 1200},
	}, []string{"type", "state"}))

	// Worker.
	Deliveries = promauto(prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tenantry_outbox_deliveries_total", Help: "Outbox events handed to the cluster, by event type and result (delivered, failed).",
	}, []string{"type", "result"}))
	SyncFailures = promauto(prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tenantry_groupsync_failures_total", Help: "Keycloak calls of the group sync that failed, by kind (group, member, user).",
	}, []string{"kind"}))
)

func promauto[C prometheus.Collector](c C) C {
	Registry.MustRegister(c)
	return c
}

// ObserveOperation records an operation that just ended.
func ObserveOperation(opType, state string, created, finished time.Time) {
	OperationDuration.WithLabelValues(opType, state).Observe(finished.Sub(created).Seconds())
}

// scrapeTimeout bounds each database read made for a scrape.
const scrapeTimeout = 3 * time.Second

// Gauge reads one number from the database at each scrape; a failed read
// reports nothing rather than a wrong number.
type Gauge struct {
	Desc *prometheus.Desc
	Read func(ctx context.Context) ([]Sample, error)
	Log  *slog.Logger
}

// Sample is one value of a Gauge with its label values, in Desc's order.
type Sample struct {
	Value  float64
	Labels []string
}

func (g Gauge) Describe(ch chan<- *prometheus.Desc) { ch <- g.Desc }

func (g Gauge) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	samples, err := g.Read(ctx)
	if err != nil {
		g.Log.Warn("metrics: read failed", "metric", g.Desc.String(), "err", err)
		return
	}
	for _, s := range samples {
		ch <- prometheus.MustNewConstMetric(g.Desc, prometheus.GaugeValue, s.Value, s.Labels...)
	}
}

// Serve answers /metrics on addr until ctx ends. An empty addr serves nothing.
func Serve(ctx context.Context, addr string, log *slog.Logger) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("metrics listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("metrics server stopped", "err", err)
	}
}
