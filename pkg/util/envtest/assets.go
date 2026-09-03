package envtest

import (
	"os"
	"os/exec"
	"strings"
)

const defaultK8sVersion = "1.31.0"

// BinaryAssetsDirectory returns kubebuilder/envtest binary assets.
// It honors a non-empty KUBEBUILDER_ASSETS value, otherwise falls back to setup-envtest.
// An empty but set KUBEBUILDER_ASSETS is cleared so controller-runtime envtest does not
// treat it as an override and fall back to PATH binaries.
func BinaryAssetsDirectory() string {
	if dir := strings.TrimSpace(os.Getenv("KUBEBUILDER_ASSETS")); dir != "" {
		return dir
	}

	os.Unsetenv("KUBEBUILDER_ASSETS")

	out, err := exec.Command("setup-envtest", "use", defaultK8sVersion, "-p", "path").Output()
	if err != nil {
		return ""
	}

	dir := strings.TrimSpace(string(out))
	if dir != "" {
		_ = os.Setenv("KUBEBUILDER_ASSETS", dir)
	}

	return dir
}
