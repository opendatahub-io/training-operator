package telemetry

import (
	"os"
	"strings"

	kubeflowv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	logger           = ctrl.Log.WithName("telemetry")
	telemetryEnabled = false
)

func init() {
	// Check environment variable for telemetry enablement
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("TELEMETRY_ENABLED"))); v == "true" || v == "1" {
		telemetryEnabled = true
		logger.Info("Telemetry enabled")
	}
}

func RecordPyTorchJobCreated(job *kubeflowv1.PyTorchJob) {
	if !telemetryEnabled || job == nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			RecordTelemetryError()
			logger.V(1).Info("Telemetry collection failed",
				"operation", "RecordPyTorchJobCreated",
				"error", r,
				"job", job.Name)
		}
	}()

	imageType, runtimeVersion := classifyJob(job)

	trainingJobsTotal.WithLabelValues(imageType, runtimeVersion).Inc()

	logger.V(3).Info("PyTorch job telemetry recorded",
		"job", job.Name,
		"image_type", imageType,
		"runtime_version", runtimeVersion)
}

func classifyJob(job *kubeflowv1.PyTorchJob) (imageType, runtimeVersion string) {
	image := extractPrimaryImage(job)
	if image == "" {
		return "custom", "other"
	}

	imageType = ClassifyImageSource(image)
	runtimeVersion = ClassifyPyTorchVersion(image)

	return imageType, runtimeVersion
}

func extractPrimaryImage(job *kubeflowv1.PyTorchJob) string {
	// Check Master replica first if it exists
	if spec, exists := job.Spec.PyTorchReplicaSpecs[kubeflowv1.PyTorchJobReplicaTypeMaster]; exists && spec != nil {
		if image := getContainerImage(spec); image != "" {
			return image
		}
	}

	// Fall back to Worker replica
	if spec, exists := job.Spec.PyTorchReplicaSpecs[kubeflowv1.PyTorchJobReplicaTypeWorker]; exists && spec != nil {
		if image := getContainerImage(spec); image != "" {
			return image
		}
	}

	return ""
}

func getContainerImage(spec *kubeflowv1.ReplicaSpec) string {
	if spec.Template.Spec.Containers == nil || len(spec.Template.Spec.Containers) == 0 {
		return ""
	}

	// Look for pytorch container first
	for _, container := range spec.Template.Spec.Containers {
		if container.Name == kubeflowv1.PyTorchJobDefaultContainerName || container.Name == "pytorch" {
			return container.Image
		}
	}

	// Fallback to first container
	return spec.Template.Spec.Containers[0].Image
}
