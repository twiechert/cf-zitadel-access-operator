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

	accessv1alpha1 "github.com/twiechert/zitadel-access-operator/api/v1alpha1"
	cfclient "github.com/twiechert/zitadel-access-operator/internal/cloudflare"
	"github.com/twiechert/zitadel-access-operator/internal/controller"
	"github.com/twiechert/zitadel-access-operator/internal/zitadel"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(accessv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		zitadelURL           string
		cfAccountID          string
		cfIdPID              string
		sessionDuration      string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election.")
	flag.StringVar(&zitadelURL, "zitadel-url", os.Getenv("ZITADEL_URL"), "Base URL of the Zitadel instance.")
	flag.StringVar(&cfAccountID, "cloudflare-account-id", os.Getenv("CLOUDFLARE_ACCOUNT_ID"), "Cloudflare account ID.")
	flag.StringVar(&cfIdPID, "cloudflare-idp-id", os.Getenv("CLOUDFLARE_IDP_ID"), "Cloudflare Access Identity Provider ID for Zitadel.")
	flag.StringVar(&sessionDuration, "session-duration", "24h", "Cloudflare Access session duration.")

	// Sensitive values — env-only, never exposed as CLI flags.
	zitadelToken := os.Getenv("ZITADEL_TOKEN")
	cfAPIToken := os.Getenv("CLOUDFLARE_API_TOKEN")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	if zitadelURL == "" || zitadelToken == "" {
		setupLog.Error(nil, "ZITADEL_URL and ZITADEL_TOKEN env vars are required")
		os.Exit(1)
	}
	if cfAPIToken == "" || cfAccountID == "" || cfIdPID == "" {
		setupLog.Error(nil, "CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, and CLOUDFLARE_IDP_ID are required")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "zitadel-access-operator.access.zitadel.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	reconciler := &controller.SecuredApplicationReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Zitadel:    zitadel.NewClient(zitadelURL, zitadelToken),
		Cloudflare: cfclient.NewClient(cfAPIToken, cfAccountID),
		Config: controller.Config{
			CloudflareIdPID: cfIdPID,
			SessionDuration: sessionDuration,
		},
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SecuredApplication")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
