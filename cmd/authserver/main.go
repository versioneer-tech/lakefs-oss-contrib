// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
	"github.com/versioneer-tech/lakefs-oss-contrib/internal/authserver"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(pkgv1beta1.AddToScheme(scheme))
}

func main() {
	var authServerAddr string
	var metricsAddr string
	var probeAddr string
	var defaultUserNamespace string
	flag.StringVar(&authServerAddr, "auth-server-bind-address", ":8080", "The address the lakeFS auth server endpoint binds to. Use 0 to disable.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use 0 to disable.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the health and readiness probe endpoint binds to.")
	flag.StringVar(&defaultUserNamespace, "default-user-namespace", os.Getenv("POD_NAMESPACE"), "Namespace for LakeFSUser objects created through the lakeFS auth API.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		LeaderElection: false,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	indexFields(mgr)

	if err := mgr.Add(authserver.New(
		mgr.GetClient(),
		authServerAddr,
		authserver.WithDefaultUserNamespace(defaultUserNamespace),
	)); err != nil {
		setupLog.Error(err, "Failed to add lakeFS auth server")
		os.Exit(1)
	}

	setupLog.Info("Starting lakeFS auth server")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run lakeFS auth server")
		os.Exit(1)
	}
}

func indexFields(mgr ctrl.Manager) {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &pkgv1beta1.LakeFSCredential{}, pkgv1beta1.CredentialAccessKeyIDField, func(rawObj client.Object) []string {
		credential := rawObj.(*pkgv1beta1.LakeFSCredential)
		if credential.Spec.AccessKeyID == "" {
			return nil
		}
		return []string{credential.Spec.AccessKeyID}
	}); err != nil {
		setupLog.Error(err, "Failed to index LakeFSCredential by access key ID")
		os.Exit(1)
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &pkgv1beta1.LakeFSCredential{}, pkgv1beta1.CredentialUserField, func(rawObj client.Object) []string {
		credential := rawObj.(*pkgv1beta1.LakeFSCredential)
		if credential.Spec.UserRef.Name == "" {
			return nil
		}
		return []string{credential.Spec.UserRef.Name}
	}); err != nil {
		setupLog.Error(err, "Failed to index LakeFSCredential by user")
		os.Exit(1)
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &pkgv1beta1.LakeFSUser{}, pkgv1beta1.UserExternalIDField, func(rawObj client.Object) []string {
		user := rawObj.(*pkgv1beta1.LakeFSUser)
		if user.Spec.ExternalID == "" {
			return nil
		}
		return []string{user.Spec.ExternalID}
	}); err != nil {
		setupLog.Error(err, "Failed to index LakeFSUser by external ID")
		os.Exit(1)
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &pkgv1beta1.LakeFSGroup{}, pkgv1beta1.GroupExternalIDField, func(rawObj client.Object) []string {
		group := rawObj.(*pkgv1beta1.LakeFSGroup)
		if group.Spec.ExternalID == "" {
			return nil
		}
		return []string{group.Spec.ExternalID}
	}); err != nil {
		setupLog.Error(err, "Failed to index LakeFSGroup by external ID")
		os.Exit(1)
	}
}
