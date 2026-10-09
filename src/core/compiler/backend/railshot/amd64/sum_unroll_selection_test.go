//go:build linux && amd64 && !wago_sumunroll

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// The first experiment commit measures the unchanged compiler.
func selectSumUnroll(t testing.TB) { t.Helper() }

func sumUnrollBaseline(t testing.TB, m *wasm.Module) *sumNative {
	return sumUnrollNative(t, m, CompileOptions{})
}
