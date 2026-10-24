//go:build arm64 && !wago_precompiled

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotOptKnobSnapshot() ([]railshotKnobInfo, railshotOptimizationSnapshot) {
	return railshot.OptKnobSnapshot()
}

func railshotCurrentOptKnobSnapshot() railshotOptimizationSnapshot {
	return railshot.CurrentOptKnobSnapshot()
}

func railshotCompileModuleWith(m *wasm.Module, opts railshotCompileOptions) (*railshotCompiledModule, error) {
	return railshot.CompileModuleWith(m, opts)
}
