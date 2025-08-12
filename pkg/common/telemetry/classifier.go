package telemetry

import (
	"strings"
)

const (
	ImageTypeRHOAI  = "rhoai"
	ImageTypeCustom = "custom"
)

func ClassifyImageSource(image string) string {
	if image == "" {
		return ImageTypeCustom
	}

	imageLower := strings.ToLower(image)

	rhoaiRegistries := []string{
		"quay.io/opendatahub/",
		"quay.io/rhoai/",
		"quay.io/modh/",
		"registry.redhat.io/rhoai/",
		"registry.redhat.io/ubi8/",
		"registry.redhat.io/ubi9/",
	}

	for _, registry := range rhoaiRegistries {
		if strings.Contains(imageLower, registry) {
			return ImageTypeRHOAI
		}
	}

	// RHOAI-specific image patterns
	rhoaiPatterns := []string{
		"pytorch-notebook",
		"tensorflow-notebook",
		"minimal-notebook",
		"datascience-notebook",
		"cuda-notebook",
	}

	for _, pattern := range rhoaiPatterns {
		if strings.Contains(imageLower, pattern) &&
			!strings.Contains(imageLower, "docker.io") &&
			!strings.Contains(imageLower, "gcr.io") {
			return ImageTypeRHOAI
		}
	}

	return ImageTypeCustom
}

func ClassifyPyTorchVersion(image string) string {
	if image == "" {
		return "other"
	}

	imageLower := strings.ToLower(image)

	// Check for supported versions in order of preference
	versions := []string{"2.5", "2.4", "2.3", "2.2"}

	for _, version := range versions {
		patterns := []string{
			"pytorch-" + version,
			"pytorch:" + version,
			"pytorch_" + version,
			"-py" + strings.Replace(version, ".", "", -1),
		}

		for _, pattern := range patterns {
			if strings.Contains(imageLower, pattern) {
				return version
			}
		}
	}

	return "other"
}
