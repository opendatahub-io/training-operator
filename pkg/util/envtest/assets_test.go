package envtest_test

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"

	envtestassets "github.com/kubeflow/training-operator/pkg/util/envtest"
)

func TestBinaryAssetsDirectory(t *testing.T) {
	RegisterTestingT(t)

	t.Run("uses KUBEBUILDER_ASSETS when set", func(t *testing.T) {
		t.Setenv("KUBEBUILDER_ASSETS", "/custom/envtest/assets")

		Expect(envtestassets.BinaryAssetsDirectory()).To(Equal("/custom/envtest/assets"))
	})

	t.Run("falls back to setup-envtest when unset", func(t *testing.T) {
		t.Setenv("KUBEBUILDER_ASSETS", "")

		path := envtestassets.BinaryAssetsDirectory()
		if _, err := os.Stat(path); err != nil {
			t.Skip("setup-envtest assets not available:", err)
		}

		Expect(path).NotTo(BeEmpty())
		Expect(path).To(ContainSubstring("envtest"))
		Expect(os.Getenv("KUBEBUILDER_ASSETS")).To(Equal(path))
	})

	t.Run("treats whitespace-only KUBEBUILDER_ASSETS as unset", func(t *testing.T) {
		t.Setenv("KUBEBUILDER_ASSETS", "   ")

		path := envtestassets.BinaryAssetsDirectory()
		if _, err := os.Stat(path); err != nil {
			t.Skip("setup-envtest assets not available:", err)
		}

		Expect(path).NotTo(BeEmpty())
		Expect(os.Getenv("KUBEBUILDER_ASSETS")).To(Equal(path))
	})
}
