//go:build !wago_regalloccheck && (!arm64 || wago_precompiled)

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Inline the original call without carrying validation facts in ordinary code.
func railshotCompileValidatedModuleWith(m *wasm.Module, options railshotCompileOptions, _ *wasm.ValidatedModuleAnalysis, _ wasm.ValidationFeatures) (*railshotCompiledModule, error) {
	return railshotCompileModuleWith(m, options)
}
