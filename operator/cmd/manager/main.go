// Command manager runs the Workspace Operator of one cluster.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	tenantryv1alpha1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

func main() {
	var metricsAddr, probeAddr string
	// Metrics stay off until M12 scrapes them (plain HTTP, inside the cluster).
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "address of the metrics endpoint, 0 to disable")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "address of /healthz and /readyz")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tenantryv1alpha1.AddToScheme(scheme))

	// One replica, no leader election: every write is a server-side apply
	// or an idempotent patch, so the overlap of a rollout is harmless and
	// the operator needs no Lease rights.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		log.Error(err, "creating the manager")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "adding the health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "adding the ready check")
		os.Exit(1)
	}
	log.Info("starting")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "running the manager")
		os.Exit(1)
	}
}
