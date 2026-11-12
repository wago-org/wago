//go:build amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// Artifact-only tests may model instructions the real host cannot execute.
// Keep the compiler's capability cache consistent with the mocked admission
// flags; callers must never invoke the resulting synthetic native images.
func mockAMD64ArtifactCPU(t *testing.T, features shared.AMD64Features) {
	t.Helper()
	cachedAMD64CPUFeatures()
	savedFeatures, savedOK := amd64CPUCache.features, amd64CPUCache.ok
	t.Cleanup(func() { amd64CPUCache.features, amd64CPUCache.ok = savedFeatures, savedOK })
	amd64CPUCache.features, amd64CPUCache.ok = features, true
}
