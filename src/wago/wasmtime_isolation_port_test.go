//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"testing"
	"time"

	"github.com/wago-org/wago/internal/wasmtimetest"
)

func runWasmtimeIsolatedPortTest(t *testing.T) bool {
	t.Helper()
	return wasmtimetest.RunIsolated(t, wasmtimetest.Timeout(t, 30*time.Second))
}
