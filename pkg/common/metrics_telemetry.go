// Copyright 2021 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License

package common

import (
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// TELEMETRY METRICS (P2 - Go to Observatorium via telemeter-client)
// RHOAISTRAT-575 compliant: 3 metrics, 10 timeseries TOTAL
// ============================================================================
var (
	telemetryJobsByRuntime *prometheus.CounterVec

	telemetryImagePreference *prometheus.CounterVec

	telemetryFrameworkUsage *prometheus.CounterVec

	telemetryEnabled bool
)

// InitializeTelemetryMetrics initializes telemetry metrics if enabled
// This function should be called after environment variables are loaded
// Uses TELEMETRY_ENABLED environment variable (set by RHOAI overlay)
func InitializeTelemetryMetrics() {
	telemetryEnabled = os.Getenv("TELEMETRY_ENABLED") == "true"

	if telemetryEnabled {
		telemetryJobsByRuntime = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "training_operator_jobs_created_by_runtime_total",
				Help: "Total training jobs by runtime version and image source",
			},
			[]string{"runtime"},
		)

		telemetryImagePreference = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "training_operator_image_preference_total",
				Help: "Total jobs by image source (rhoai vs external)",
			},
			[]string{"source"},
		)

		telemetryFrameworkUsage = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "training_operator_framework_usage_total",
				Help: "Total jobs by ML framework",
			},
			[]string{"framework"},
		)

		// Register with controller-runtime metrics registry
		metrics.Registry.MustRegister(
			telemetryJobsByRuntime,
			telemetryImagePreference,
			telemetryFrameworkUsage,
		)

		// Runtime: 5 timeseries
		telemetryJobsByRuntime.WithLabelValues("pytorch-2.4").Add(0)
		telemetryJobsByRuntime.WithLabelValues("pytorch-2.5").Add(0)
		telemetryJobsByRuntime.WithLabelValues("pytorch-other").Add(0)
		telemetryJobsByRuntime.WithLabelValues("tensorflow").Add(0)
		telemetryJobsByRuntime.WithLabelValues("other").Add(0)

		// Image source: 2 timeseries
		telemetryImagePreference.WithLabelValues("rhoai").Add(0)
		telemetryImagePreference.WithLabelValues("external").Add(0)

		// Framework: 3 timeseries (simplified from 6 frameworks)
		telemetryFrameworkUsage.WithLabelValues("pytorch").Add(0)
		telemetryFrameworkUsage.WithLabelValues("tensorflow").Add(0)
		telemetryFrameworkUsage.WithLabelValues("other").Add(0)

		klog.Info("Telemetry metrics initialized (RHOAISTRAT-575 compliant)")
		klog.Infof("Telemetry cardinality: 5 (runtime) + 2 (image) + 3 (framework) = 10 timeseries TOTAL")
	} else {
		klog.Info("Telemetry metrics DISABLED (set TELEMETRY_ENABLED=true to enable)")
	}
}

// UpdateTelemetryMetricsForJob records telemetry metrics for a training job, this function is called by all 6 controllers
func UpdateTelemetryMetricsForJob(framework string, jobNamespace string, jobName string, imageName string, isActive bool) {
	if !telemetryEnabled || !isActive {
		return
	}

	// Classify runtime based on framework and image
	runtime := classifyRuntime(framework, imageName)
	telemetryJobsByRuntime.WithLabelValues(runtime).Inc()

	// Classify image source
	source := "external"
	if isRHOAIImage(imageName) {
		source = "rhoai"
	}
	telemetryImagePreference.WithLabelValues(source).Inc()

	// Normalize framework for telemetry
	frameworkLabel := normalizeFramework(framework)
	telemetryFrameworkUsage.WithLabelValues(frameworkLabel).Inc()

	klog.V(4).Infof("Telemetry recorded for %s/%s: runtime=%s, source=%s, framework=%s",
		jobNamespace, jobName, runtime, source, frameworkLabel)
}

// classifyRuntime determines the runtime label for telemetry
// Simplified to meet Red Hat Handbook limit: max 10 timeseries
// PyTorch is checked first as it represents most workloads
func classifyRuntime(framework, image string) string {
	imageLower := strings.ToLower(image)
	frameworkLower := strings.ToLower(framework)

	if frameworkLower == "pytorch" || frameworkLower == "pytorchjob" ||
		strings.Contains(imageLower, "pytorch") || strings.Contains(imageLower, "torch") {

		if strings.Contains(imageLower, "2.4") || strings.Contains(imageLower, "2-4") ||
			strings.Contains(imageLower, "24") {
			return "pytorch-2.4"
		}

		if strings.Contains(imageLower, "2.5") || strings.Contains(imageLower, "2-5") ||
			strings.Contains(imageLower, "25") {
			return "pytorch-2.5"
		}
		return "pytorch-other"
	}

	// Check if it's TensorFlow (all versions combined to reduce cardinality)
	if frameworkLower == "tensorflow" || frameworkLower == "tfjob" ||
		strings.Contains(imageLower, "tensorflow") || strings.Contains(imageLower, "tf-") {
		return "tensorflow"
	}
	return "other"
}

// isRHOAIImage checks if the image is from Red Hat OpenShift AI
func isRHOAIImage(image string) bool {
	imageLower := strings.ToLower(image)
	return strings.Contains(imageLower, "registry.redhat.io") ||
		strings.Contains(imageLower, "quay.io/modh") ||
		strings.Contains(imageLower, "quay.io/opendatahub") ||
		strings.Contains(imageLower, "image-registry.openshift-image-registry")
}

func normalizeFramework(framework string) string {
	frameworkLower := strings.ToLower(framework)

	switch {
	case strings.Contains(frameworkLower, "pytorch"):
		return "pytorch"
	case strings.Contains(frameworkLower, "tensorflow") || frameworkLower == "tfjob":
		return "tensorflow"
	default:
		return "other"
	}
}
