//go:build amd64 && !wago_sumunroll

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// This wrapper inlines to the existing emitter in normal builds.
func (f *fn) trySelectedLinearSumLatch(loop *ctrlFrame, counter int) bool {
	return f.tryUnrolledLinearSumLatch(loop, counter)
}

// Constant stubs remove the capacity experiment from normal compilation.
func sumUnrollReserveEnabled(deferred bool, workers int, compact bool) bool    { return false }
func sumUnrollCodeHeadroom(m *wasm.Module, hints []funcHints, i, used int) int { return 0 }
