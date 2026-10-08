//go:build wago_regalloccheck

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This handoff preserves the original validation profile, before frontend
// narrowing. Options stays compile-scoped; workers borrow immutable facts.
func railshotCompileValidatedModuleWith(m *wasm.Module, options railshotCompileOptions, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) (*railshotCompiledModule, error) {
	options.Codegen = codegen.SourceOptions(options.Codegen, m, analysis, features)
	defer codegen.CloseSourceContext(options.Codegen, m)
	return railshotCompileModuleWith(m, options)
}
