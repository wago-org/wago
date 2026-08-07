//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"
	"time"

	"github.com/wago-org/wago/tests/regressiontest"
)

func runRegressionIsolatedPortTest(t *testing.T) bool {
	t.Helper()
	return regressiontest.RunIsolated(t, regressiontest.Timeout(t, 30*time.Second))
}
