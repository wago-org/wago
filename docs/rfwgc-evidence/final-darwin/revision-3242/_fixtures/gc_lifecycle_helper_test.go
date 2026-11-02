//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

import (
	goruntime "runtime"
	"testing"
	"time"
)

// Unchanged helper extracted from the pinned Linux-gated lifecycle tests.
func awaitGCLifecycle(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("state transition did not complete")
		}
		goruntime.Gosched()
	}
}
