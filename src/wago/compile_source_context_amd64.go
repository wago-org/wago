//go:build amd64 && !wago_standalone

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The AMD64 entry borrows validation independently of the checked source proof.
func railshotCompileValidatedModuleWith(m *wasm.Module, options railshotCompileOptions, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) (*railshotCompiledModule, error) {
	if codegen.SourceChecks {
		options.Codegen = codegen.SourceOptions(options.Codegen, m, analysis, features)
		defer codegen.CloseSourceContext(options.Codegen, m)
	}
	return railshotCompileModuleWithAnalysis(m, options, analysis)
}
