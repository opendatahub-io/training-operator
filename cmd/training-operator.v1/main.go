/*
Copyright 2021.

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

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	schedulerpluginsv1alpha1 "sigs.k8s.io/scheduler-plugins/apis/scheduling/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned"

	kubeflowv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	"github.com/kubeflow/training-operator/pkg/cert"
	"github.com/kubeflow/training-operator/pkg/config"
	controllerv1 "github.com/kubeflow/training-operator/pkg/controller.v1"
	"github.com/kubeflow/training-operator/pkg/controller.v1/common"
	"github.com/kubeflow/training-operator/pkg/webhooks"
	//+kubebuilder:scaffold:imports
)

const (
	// EnvKubeflowNamespace is an environment variable for namespace when deployed on kubernetes
	EnvKubeflowNamespace = "KUBEFLOW_NAMESPACE"

	webhookConfigurationName = "kubeflow-validator.training-operator.kubeflow.org"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kubeflowv1.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
	utilruntime.Must(schedulerpluginsv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=apiservers,resourceNames=cluster,verbs=get

func fetchTLSOpts(cfg *restclient.Config) []func(*tls.Config) {
	var tlsOpts []func(*tls.Config)
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bootstrapCancel()
	bootstrapClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Info("Failed to create bootstrap client for TLS profile, using hardened defaults")
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.MinVersion = tls.VersionTLS12
			c.NextProtos = []string{"h2", "http/1.1"}
		})
		return tlsOpts
	}

	apiServer := &unstructured.Unstructured{}
	apiServer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "APIServer",
	})
	if err := bootstrapClient.Get(bootstrapCtx, client.ObjectKey{Name: "cluster"}, apiServer); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			setupLog.Info("TLS profile not available, using hardened defaults (non-OpenShift cluster)")
			tlsOpts = append(tlsOpts, func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS12
				c.CipherSuites = intermediateCiphers
			})
		} else {
			setupLog.Error(err, "Failed to read APIServer TLS profile, operator cannot start without TLS policy")
			os.Exit(1)
		}
	} else {
		minVersion, ciphers := parseTLSProfile(apiServer)
		if ciphers != nil && len(ciphers) == 0 {
			setupLog.Error(nil, "Custom TLS profile specified ciphers but none are supported by Go, "+
				"refusing to start with unrestricted ciphers")
			os.Exit(1)
		}
		setupLog.Info("Applying cluster TLS profile", "minVersion", minVersion, "ciphers", len(ciphers))
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.MinVersion = minVersion
			if len(ciphers) > 0 {
				c.CipherSuites = ciphers
			}
		})
	}
	tlsOpts = append(tlsOpts, func(c *tls.Config) {
		c.NextProtos = []string{"h2", "http/1.1"}
	})
	return tlsOpts
}

var tlsVersionMap = map[string]uint16{
	"VersionTLS12": tls.VersionTLS12,
	"VersionTLS13": tls.VersionTLS13,
}

var openSSLToGoCipher = map[string]uint16{
	"ECDHE-ECDSA-AES128-GCM-SHA256": tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	"ECDHE-RSA-AES128-GCM-SHA256":   tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	"ECDHE-ECDSA-AES256-GCM-SHA384": tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	"ECDHE-RSA-AES256-GCM-SHA384":   tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	"ECDHE-ECDSA-CHACHA20-POLY1305": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	"ECDHE-RSA-CHACHA20-POLY1305":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	"ECDHE-ECDSA-AES128-SHA256":     tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
	"ECDHE-RSA-AES128-SHA256":       tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
	"AES128-GCM-SHA256":             tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	"AES256-GCM-SHA384":             tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	"AES128-SHA256":                 tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
}

// Intermediate profile defaults (used when profile is nil, empty, or Intermediate)
var intermediateMinVersion uint16 = tls.VersionTLS12
var intermediateCiphers = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

func parseTLSProfile(apiServer *unstructured.Unstructured) (uint16, []uint16) {
	profile, found, err := unstructured.NestedMap(apiServer.Object, "spec", "tlsSecurityProfile")
	if err != nil {
		setupLog.Error(err, "Failed to read tlsSecurityProfile from APIServer, using Intermediate defaults")
		return intermediateMinVersion, intermediateCiphers
	}
	if !found || profile == nil {
		return intermediateMinVersion, intermediateCiphers
	}

	profileType, _ := profile["type"].(string)
	switch profileType {
	case "Intermediate", "":
		return intermediateMinVersion, intermediateCiphers
	case "Custom":
		custom, _, err := unstructured.NestedMap(profile, "custom")
		if err != nil {
			setupLog.Error(err, "Failed to read custom TLS profile, using Intermediate defaults")
			return intermediateMinVersion, intermediateCiphers
		}
		if custom == nil {
			setupLog.Info("Custom TLS profile type set but no custom block provided, using Intermediate defaults")
			return intermediateMinVersion, intermediateCiphers
		}
		minVer, _ := custom["minTLSVersion"].(string)
		minVersion := tlsVersionMap[minVer]
		if minVersion == 0 {
			minVersion = tls.VersionTLS12
		}
		cipherNames, _, err := unstructured.NestedStringSlice(custom, "ciphers")
		if err != nil {
			setupLog.Error(err, "Failed to read ciphers from custom TLS profile, proceeding without cipher restrictions")
		}
		ciphers := make([]uint16, 0, len(cipherNames))
		for _, name := range cipherNames {
			if id, ok := openSSLToGoCipher[name]; ok {
				ciphers = append(ciphers, id)
			} else {
				setupLog.Info("Cipher from TLS profile not supported by Go, skipping", "cipher", name)
			}
		}
		return minVersion, ciphers
	case "Modern":
		return tls.VersionTLS13, nil
	case "Old":
		return tls.VersionTLS12, nil
	default:
		setupLog.Info("Unrecognized TLS profile type, using Intermediate defaults", "profileType", profileType)
		return intermediateMinVersion, intermediateCiphers
	}
}

// newCacheOptions builds cache options with label selectors to restrict informer
// cache to operator-managed resources only. Without filtering, the cache loads all
// Pods, Services, and ConfigMaps cluster-wide, causing unbounded memory growth.
func newCacheOptions(namespace string) cache.Options {
	// Use "exists" requirement on the operator-name label that is already set
	// on all Pods, Services, and ConfigMaps created by the training-operator.
	operatorLabelReq, err := labels.NewRequirement(
		kubeflowv1.OperatorNameLabel,
		selection.Exists,
		nil,
	)
	if err != nil {
		panic("failed to create label requirement: " + err.Error())
	}
	operatorSelector := labels.NewSelector().Add(*operatorLabelReq)
	operatorFilter := cache.ByObject{Label: operatorSelector}

	opts := cache.Options{
		DefaultTransform: cache.TransformStripManagedFields(),
		ByObject: map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}:      operatorFilter,
			&corev1.Pod{}:            operatorFilter,
			&corev1.Service{}:        operatorFilter,
			&corev1.ServiceAccount{}: operatorFilter,
			&corev1.Secret{}:         operatorFilter,
			&rbacv1.Role{}:           operatorFilter,
			&rbacv1.RoleBinding{}:    operatorFilter,
		},
	}

	if namespace != "" {
		opts.DefaultNamespaces = map[string]cache.Config{
			namespace: {},
		}
	}

	return opts
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var leaderElectionID string
	var probeAddr string
	var enabledSchemes controllerv1.EnabledSchemes
	var gangSchedulerName string
	var namespace string
	var controllerThreads int
	var webhookServerPort int
	var webhookServiceName string
	var webhookSecretName string
	var clientQps int
	var clientBurst int

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "1ca428e5.training-operator.kubeflow.org", "The ID for leader election.")
	flag.Var(&enabledSchemes, "enable-scheme", "Enable scheme(s) as --enable-scheme=tfjob --enable-scheme=pytorchjob, case insensitive."+
		" Now supporting TFJob, PyTorchJob, XGBoostJob, PaddleJob, JAXJob. By default, all supported schemes will be enabled.")
	flag.StringVar(&gangSchedulerName, "gang-scheduler-name", "", "Now Supporting volcano and scheduler-plugins."+
		" Note: If you set another scheduler name, the training-operator assumes it's the scheduler-plugins.")
	flag.StringVar(&namespace, "namespace", os.Getenv(EnvKubeflowNamespace), "The namespace to monitor kubeflow jobs. If unset, it monitors all namespaces cluster-wide."+
		"If set, it only monitors kubeflow jobs in the given namespace.")
	flag.IntVar(&controllerThreads, "controller-threads", 1, "Number of worker threads used by the controller.")
	flag.IntVar(&clientQps, "kube-api-qps", 20, "QPS indicates the maximum QPS to the master from this client.")
	flag.IntVar(&clientBurst, "kube-api-burst", 30, "Maximum burst for throttle.")
	// PyTorch related flags
	flag.StringVar(&config.Config.PyTorchInitContainerImage, "pytorch-init-container-image",
		config.PyTorchInitContainerImageDefault, "The image for pytorch init container")
	flag.StringVar(&config.Config.PyTorchInitContainerTemplateFile, "pytorch-init-container-template-file",
		config.PyTorchInitContainerTemplateFileDefault, "The template file for pytorch init container")
	flag.IntVar(&config.Config.PyTorchInitContainerMaxTries, "pytorch-init-container-max-tries",
		config.PyTorchInitContainerMaxTriesDefault, "The number of tries for the pytorch init container")

	// MPI related flags
	flag.StringVar(&config.Config.MPIKubectlDeliveryImage, "mpi-kubectl-delivery-image",
		config.MPIKubectlDeliveryImageDefault, "The image for mpi launcher init container")

	// Cert generation flags
	flag.IntVar(&webhookServerPort, "webhook-server-port", 9443, "Endpoint port for the webhook server.")
	flag.StringVar(&webhookServiceName, "webhook-service-name", "training-operator", "Name of the Service used as part of the DNSName")
	flag.StringVar(&webhookSecretName, "webhook-secret-name", "training-operator-webhook-cert", "Name of the Secret to store CA  and server certs")

	opts := zap.Options{
		Development:     true,
		StacktraceLevel: zapcore.DPanicLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cacheOpts := newCacheOptions(namespace)

	cfg := ctrl.GetConfigOrDie()
	cfg.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(clientQps), clientBurst)

	tlsOpts := fetchTLSOpts(cfg)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
			TLSOpts:     tlsOpts,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookServerPort,
			TLSOpts: tlsOpts,
		}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
		Cache:                  cacheOpts,
		// ConfigMap appears in both ByObject (label-filtered cache) and DisableFor.
		// ByObject ensures the informer only watches operator-labeled ConfigMaps,
		// while DisableFor makes client.Get/List bypass the cache entirely for
		// direct API reads, avoiding stale data for configuration lookups.
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.ConfigMap{},
					&corev1.Secret{},
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	certsReady := make(chan struct{})
	defer close(certsReady)
	certGenerationConfig := cert.Config{
		WebhookSecretName:        webhookSecretName,
		WebhookServiceName:       webhookServiceName,
		WebhookConfigurationName: webhookConfigurationName,
	}
	if err = cert.ManageCerts(mgr, certGenerationConfig, certsReady); err != nil {
		setupLog.Error(err, "Unable to set up cert rotation")
		os.Exit(1)
	}

	setupProbeEndpoints(mgr, certsReady)
	// Set up controllers using goroutines to start the manager quickly.
	go setupControllers(mgr, enabledSchemes, gangSchedulerName, controllerThreads, certsReady)

	//+kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setupControllers(mgr ctrl.Manager, enabledSchemes controllerv1.EnabledSchemes, gangSchedulerName string, controllerThreads int, certsReady <-chan struct{}) {
	setupLog.Info("Waiting for certificate generation to complete")
	<-certsReady
	setupLog.Info("Certs ready")

	setupLog.Info("registering controllers...")
	// Prepare GangSchedulingSetupFunc
	gangSchedulingSetupFunc := common.GenNonGangSchedulerSetupFunc()
	if strings.EqualFold(gangSchedulerName, string(common.GangSchedulerVolcano)) {
		cfg := mgr.GetConfig()
		volcanoClientSet := volcanoclient.NewForConfigOrDie(cfg)
		gangSchedulingSetupFunc = common.GenVolcanoSetupFunc(volcanoClientSet)
		gvk := v1beta1.SchemeGroupVersion.WithKind("PodGroup")
		validateCRD(mgr, gvk)
	} else if gangSchedulerName != "" {
		gangSchedulingSetupFunc = common.GenSchedulerPluginsSetupFunc(mgr.GetClient(), gangSchedulerName)
		gvk := schedulerpluginsv1alpha1.SchemeGroupVersion.WithKind("PodGroup")
		validateCRD(mgr, gvk)
	}

	// TODO: We need a general manager. all rest reconciler addsToManager
	// Based on the user configuration, we start different controllers
	if enabledSchemes.Empty() {
		enabledSchemes.FillAll()
	}
	errMsg := "failed to set up controllers"
	for _, s := range enabledSchemes {
		setupReconcilerFunc, supportedReconciler := controllerv1.SupportedSchemeReconciler[s]
		if !supportedReconciler {
			setupLog.Error(errors.New(errMsg), "scheme is not supported", "scheme", s)
			os.Exit(1)
		}
		if err := setupReconcilerFunc(mgr, gangSchedulingSetupFunc, controllerThreads); err != nil {
			setupLog.Error(errors.New(errMsg), "unable to create controller", "scheme", s)
			os.Exit(1)
		}
		setupWebhookFunc, supportedWebhook := webhooks.SupportedSchemeWebhook[s]
		if !supportedWebhook {
			setupLog.Error(errors.New(errMsg), "scheme is not supported", "scheme", s)
			os.Exit(1)
		}
		if err := setupWebhookFunc(mgr); err != nil {
			setupLog.Error(errors.New(errMsg), "unable to start webhook server", "scheme", s)
			os.Exit(1)
		}
	}
}

func setupProbeEndpoints(mgr ctrl.Manager, certsReady <-chan struct{}) {
	defer setupLog.Info("Probe endpoints are configured on healthz and readyz")

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Wait for the webhook server to be listening before advertising the
	// training-operator replica as ready. This allows users to wait with sending the first
	// requests, requiring webhooks, until the training-operator deployment is available, so
	// that the early requests are not rejected during the traininig-operator's startup.
	// We wrap the call to GetWebhookServer in a closure to delay calling
	// the function, otherwise a not fully-initialized webhook server (without
	// ready certs) fails the start of the manager.
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		select {
		case <-certsReady:
			return mgr.GetWebhookServer().StartedChecker()(req)
		default:
			return errors.New("certificates are not ready")
		}
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
}

func validateCRD(mgr ctrl.Manager, gvk schema.GroupVersionKind) {
	_, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			setupLog.Error(err, "crd might be missing, please install crd", "apiVersion", gvk.GroupVersion().String(), "kind", gvk.Kind)
			os.Exit(1)
		}
		setupLog.Error(err, "unable to get crd", "apiVersion", gvk.GroupVersion().String(), "kind", gvk.Kind)
		os.Exit(1)
	}
}
