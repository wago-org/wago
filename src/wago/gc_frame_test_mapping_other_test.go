//go:build !linux && !darwin

package wago

import (
	"runtime"
	"testing"
)

func newGCNativeTestFrame(t testing.TB, size int) ([]byte, func()) {
	t.Helper()
	frame := make([]byte, size)
	return frame, func() { runtime.KeepAlive(frame) }
}
