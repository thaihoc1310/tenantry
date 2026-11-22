// Command manager runs the Workspace Operator of one cluster.
package main

import (
	"flag"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	tenantryv1alpha1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/controller"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

func main() {
	var metricsAddr, probeAddr, serviceAccount, registries, kubeContext string
	// Outside the cluster (make run-operator): which kubeconfig context.
	flag.StringVar(&kubeContext, "context", "", "kubeconfig context, when running outside the cluster")
	var blockedAfter, resync time.Duration
	// Every object is reconciled again this often, so drift that no watch
	// reports is restored too (IMPLEMENTATION-PLAN M7, decision 8).
	flag.DurationVar(&resync, "resync", 5*time.Minute, "how often every object is reconciled again")
	// Metrics stay off until M12 scrapes them (plain HTTP, inside the cluster).
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "address of the metrics endpoint, 0 to disable")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "address of /healthz and /readyz")
	flag.StringVar(&serviceAccount, "service-account", "",
		"the operator's own username, system:serviceaccount:<namespace>:<name>: made an owner of every Tenant")
	flag.StringVar(&registries, "allowed-registries", "docker.io,ghcr.io,quay.io,registry.k8s.io",
		"comma-separated registries tenants may pull images from")
	flag.DurationVar(&blockedAfter, "blocked-after", 10*time.Minute,
		"how long a delete waits for Namespaces before it reports BlockedByFinalizer")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("setup")
	if !strings.HasPrefix(serviceAccount, "system:serviceaccount:") {
		log.Info("--service-account must be system:serviceaccount:<namespace>:<name>")
		os.Exit(2)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tenantryv1alpha1.AddToScheme(scheme))

	// One replica, no leader election: every write is a server-side apply
	// or an idempotent patch, so the overlap of a rollout is harmless and
	// the operator needs no Lease rights.
	cfg, err := config.GetConfigWithContext(kubeContext)
	if err != nil {
		log.Error(err, "loading the kubeconfig")
		os.Exit(1)
	}
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		Cache:                  cache.Options{SyncPeriod: &resync},
	})
	if err != nil {
		log.Error(err, "creating the manager")
		os.Exit(1)
	}
	must := func(what string, err error) {
		if err != nil {
			log.Error(err, what)
			os.Exit(1)
		}
	}
	must("setting up platformworkspace", (&controller.PlatformWorkspaceReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorder("tenantry-operator"),
		Render: render.Options{
			OperatorServiceAccount: serviceAccount,
			AllowedRegistries:      strings.Split(registries, ","),
		},
		BlockedAfter: blockedAfter,
	}).SetupWithManager(mgr))
	must("setting up platformsharedpool", (&controller.PlatformSharedPoolReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr))
	must("setting up platformnodegrant", (&controller.PlatformNodeGrantReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr))
	must("setting up platformnodeinventory", (&controller.PlatformNodeInventoryReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr))
	must("adding the health check", mgr.AddHealthzCheck("healthz", healthz.Ping))
	must("adding the ready check", mgr.AddReadyzCheck("readyz", healthz.Ping))

	log.Info("starting")
	must("running the manager", mgr.Start(ctrl.SetupSignalHandler()))
}
