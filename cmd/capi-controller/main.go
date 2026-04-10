/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// capi-controller runs a controller-runtime manager that hosts the
// CAPI-to-ClusterProfile reconciler, which watches CAPI Cluster objects
// and maintains corresponding ClusterProfile status via SSA patches.
//
// Rollback / Escape Hatch:
//   - Disable reconciliation without code changes:
//     --enable-capi-cluster-to-clusterprofile-controller=false
//   - Scale the Deployment to 0 replicas:
//     kubectl scale deploy/<name> --replicas=0 -n <ns>
//
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	convertctrl "sigs.k8s.io/cluster-inventory-api/controllers/capiclustertoclusterprofile"
	labelctrl "sigs.k8s.io/cluster-inventory-api/controllers/clusterprofiletolabels"
	cpSDK "sigs.k8s.io/cluster-inventory-api/pkg/clusterprofile"
)

func main() {
	var (
		metricsAddr              string
		healthAddr               string
		clusterManagerName       string
		enableConvertController  bool
		enablePropertiesToLabels bool
		enableWebhooks           bool
		leaderElect              bool
		requeueAfterDuration     time.Duration
		maxConcurrentReconciles  int
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address the metrics endpoint binds to.")
	flag.StringVar(&healthAddr, "health-probe-bind-address", ":8081", "Address the health/ready probes bind to.")
	flag.StringVar(&clusterManagerName, "cluster-manager-name", "capi",
		"Name of the cluster manager to set on created ClusterProfiles.")
	flag.BoolVar(
		&enableConvertController,
		"enable-capi-cluster-to-clusterprofile-controller",
		true,
		"Enable the CAPI-to-ClusterProfile convert controller reconciler.",
	)
	flag.BoolVar(
		&enablePropertiesToLabels,
		"enable-clusterprofile-properties-to-labels-controller",
		true,
		"Enable the ClusterProfile properties-to-labels sync controller.",
	)
	flag.BoolVar(&enableWebhooks, "enable-webhooks", true, "Enable admission webhooks.")
	flag.BoolVar(&leaderElect, "leader-elect", false,
		"Enable leader election for controller manager. Ensuring only one active controller manager.")
	flag.DurationVar(&requeueAfterDuration, "requeue-after-duration", 15*time.Second,
		"Duration to requeue when access providers are not ready")
	flag.IntVar(&maxConcurrentReconciles, "max-concurrent-reconciles", 3,
		"Maximum number of concurrent reconciles")

	opts := zap.Options{Development: os.Getenv("LOG_DEVELOPMENT") != "false"}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Error(err, "unable to add client-go scheme")
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		log.Error(err, "unable to add cluster-inventory-api scheme")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: healthAddr,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		LeaderElection:         leaderElect,
		LeaderElectionID:       "capi-controller.cluster-inventory-api.x-k8s.io",
	})
	if err != nil {
		log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if enableConvertController {
		statusApplier := &cpSDK.RuntimeClient{Inner: mgr.GetClient()}
		reconciler := &convertctrl.Reconciler{
			Client:                  mgr.GetClient(),
			Scheme:                  mgr.GetScheme(),
			Log:                     ctrl.Log.WithName("controllers").WithName("CAPIClusterToClusterProfile"),
			StatusApplier:           statusApplier,
			Recorder:                mgr.GetEventRecorderFor("capi-cluster-to-clusterprofile-controller"), //nolint:staticcheck // SA1019: new events API returns incompatible type
			ClusterManagerName:      clusterManagerName,
			RequeueAfterDuration:    requeueAfterDuration,
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}
		if err := reconciler.SetupWithManager(mgr); err != nil {
			log.Error(err, "unable to setup capi-cluster-to-clusterprofile-controller")
			os.Exit(1)
		}
		log.Info("capi-cluster-to-clusterprofile-controller registered")
	} else {
		log.Info("capi-cluster-to-clusterprofile-controller disabled via flag")
	}

	if enablePropertiesToLabels {
		propsToLabels := &labelctrl.PropertiesToLabelsReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("PropertiesToLabels"),
		}
		if err := propsToLabels.SetupWithManager(mgr); err != nil {
			log.Error(err, "unable to setup clusterprofile-properties-to-labels controller")
			os.Exit(1)
		}
		log.Info("clusterprofile-properties-to-labels controller registered")
	} else {
		log.Info("clusterprofile-properties-to-labels controller disabled via flag")
	}

	if enableWebhooks {
		webhookServer := mgr.GetWebhookServer()
		webhookServer.Register("/validate-cluster", &admission.Webhook{
			Handler: convertctrl.NewClusterNameValidatorWithLogger(
				mgr.GetClient(), scheme,
				ctrl.Log.WithName("webhooks").WithName("ClusterNameValidator"),
			),
		})
		log.Info("CAPI Cluster name uniqueness webhook registered")
	} else {
		log.Info("Webhooks disabled via flag")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	log.Info("starting capi-controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "capi-controller exited with error")
		os.Exit(1)
	}
}
