//go:build arm64 && !wago_regalloccheck && !wago_precompiled

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Call the same backend directly to avoid two nested forwarding wrappers
// exceeding the ordinary Go inlining budget.
func railshotCompileValidatedModuleWith(m *wasm.Module, options railshotCompileOptions, _ *wasm.ValidatedModuleAnalysis, _ wasm.ValidationFeatures) (*railshotCompiledModule, error) {
	return railshot.CompileModuleWith(m, options)
}
