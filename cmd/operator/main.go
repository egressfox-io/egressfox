package main

import (
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/controller"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
	"github.com/egressfox-io/egressfox/internal/state"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(egressv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddress, healthAddress, statePath, mihomoBinary, singBoxBinary string
	var leaderElect bool
	flag.StringVar(&metricsAddress, "metrics-bind-address", ":8443", "HTTPS metrics bind address, or 0 to disable")
	flag.StringVar(&healthAddress, "health-probe-bind-address", ":8081", "health and readiness bind address")
	flag.StringVar(&statePath, "state-path", "/var/lib/egressfox/private/state.db", "protected SQLite state path")
	flag.StringVar(&mihomoBinary, "mihomo-binary", "/usr/local/bin/mihomo", "absolute Mihomo v1.19.31 binary path")
	flag.StringVar(&singBoxBinary, "sing-box-binary", "/usr/local/bin/sing-box", "absolute sing-box v1.14.1 binary path")
	flag.BoolVar(&leaderElect, "leader-elect", true, "use Kubernetes Lease leader election")
	logOptions := zap.Options{Development: false}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions)))

	namespace := strings.TrimSpace(os.Getenv("WATCH_NAMESPACE"))
	if namespace == "" || strings.Contains(namespace, ",") {
		setupLog.Error(nil, "WATCH_NAMESPACE must contain exactly one namespace")
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		setupLog.Error(err, "unable to prepare protected state directory")
		os.Exit(1)
	}
	store, err := state.Open(statePath, state.DefaultRetention())
	if err != nil {
		setupLog.Error(err, "unable to open protected state")
		os.Exit(1)
	}
	defer store.Close()

	disableHTTP2 := func(config *tls.Config) { config.NextProtos = []string{"http/1.1"} }
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		Cache:                   cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		Metrics:                 metricsserver.Options{BindAddress: metricsAddress, SecureServing: true, TLSOpts: []func(*tls.Config){disableHTTP2}, FilterProvider: filters.WithAuthenticationAndAuthorization},
		WebhookServer:           webhook.NewServer(webhook.Options{Port: -1}),
		HealthProbeBindAddress:  healthAddress,
		LeaderElection:          leaderElect,
		LeaderElectionID:        "egressfox-operator.egressfox.io",
		LeaderElectionNamespace: namespace,
		LeaseDuration:           new(15 * time.Second), RenewDeadline: new(10 * time.Second), RetryPeriod: new(2 * time.Second),
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}
	pipeline, err := operatoradapter.NewPipeline(operatoradapter.PipelineConfig{
		Client: manager.GetClient(), Reader: manager.GetAPIReader(), Scheme: manager.GetScheme(), Store: store,
		MihomoBinary: mihomoBinary, SingBoxBinary: singBoxBinary,
	})
	if err != nil {
		setupLog.Error(err, "unable to create pipeline")
		os.Exit(1)
	}
	if err := (&controller.ProxyPoolReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme()}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to register ProxyPool controller")
		os.Exit(1)
	}
	if err := (&controller.EgressGatewayReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme(), Pipeline: pipeline}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to register EgressGateway controller")
		os.Exit(1)
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to register health check")
		os.Exit(1)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to register readiness check")
		os.Exit(1)
	}
	setupLog.Info("starting namespace-scoped EgressFox operator", "namespace", namespace)
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager stopped")
		os.Exit(1)
	}
}
