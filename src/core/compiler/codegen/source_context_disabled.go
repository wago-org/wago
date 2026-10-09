//go:build !wago_regalloccheck

package codegen

import "github.com/wago-org/wago/src/core/compiler/wasm"

const SourceChecks = false

// Options are shared code-generation dependencies selected by the caller after
// frontend validation and runtime configuration normalization.
type Options struct {
	Runtime RuntimeABI
	Heap    HeapABI
	Module  ModuleInfo
}

type SourceContext struct{}

func SourceOptions(opts Options, _ *wasm.Module, _ *wasm.ValidatedModuleAnalysis, _ wasm.ValidationFeatures) Options {
	return opts
}

func AttachSourceContext(*Options, *wasm.Module, *wasm.ValidatedModuleAnalysis, wasm.ValidationFeatures) bool {
	return false
}
func SourceContextFor(Options, *wasm.Module) *SourceContext { return nil }
func ValidatedSourceContext(*SourceContext, *wasm.Module) (*wasm.ValidatedModuleAnalysis, wasm.ValidationFeatures, bool) {
	return nil, wasm.ValidationFeatures{}, false
}
func CloseSourceContext(Options, *wasm.Module) {}
