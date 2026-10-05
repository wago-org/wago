//go:build wago_regalloccheck

package codegen

import "github.com/wago-org/wago/src/core/compiler/wasm"

const SourceChecks = true

// Options are shared code-generation dependencies selected by the caller after
// frontend validation and runtime configuration normalization.
type Options struct {
	Runtime RuntimeABI
	Heap    HeapABI
	Module  ModuleInfo
	source  *SourceContext
}

// SourceContext borrows immutable validation facts for one compilation. Workers
// must never mutate it or its module/analysis. Options copies must not reuse it
// for another compilation or share it between concurrent compilations. The owner
// closes it only after all workers finish. It is not heap-policy metadata.
type SourceContext struct {
	module   *wasm.Module
	analysis *wasm.ValidatedModuleAnalysis
	features wasm.ValidationFeatures
}

// SourceOptions attaches original frontend validation facts to compile options.
func SourceOptions(opts Options, m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) Options {
	AttachSourceContext(&opts, m, analysis, features)
	return opts
}

// AttachSourceContext must receive the exact profile used by frontend validation,
// before execution features are narrowed. ValidFor authenticates module identity,
// not the profile's provenance; callers must keep all borrowed storage immutable.
func AttachSourceContext(opts *Options, m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) bool {
	if opts == nil {
		return false
	}
	opts.source = nil
	if m == nil || !analysis.ValidFor(m) {
		return false
	}
	opts.source = &SourceContext{module: m, analysis: analysis, features: features}
	return true
}

// SourceContextFor returns only a matching, still valid borrowed context.
func SourceContextFor(opts Options, m *wasm.Module) *SourceContext {
	if _, _, ok := ValidatedSourceContext(opts.source, m); !ok {
		return nil
	}
	return opts.source
}

// ValidatedSourceContext returns read-only analysis and the original profile.
// Missing or mismatched context is unavailable, never evidence of verification.
func ValidatedSourceContext(ctx *SourceContext, m *wasm.Module) (*wasm.ValidatedModuleAnalysis, wasm.ValidationFeatures, bool) {
	if ctx == nil || m == nil || ctx.module != m || !ctx.analysis.ValidFor(m) {
		return nil, wasm.ValidationFeatures{}, false
	}
	return ctx.analysis, ctx.features, true
}

// CloseSourceContext releases the matching compilation's borrowed facts, even
// if its analysis became stale. All options copies then become unavailable.
// Backends close after compilation; callers also close after early option errors.
// Call only after all users finish. Borrowed module/analysis storage is unchanged.
func CloseSourceContext(opts Options, m *wasm.Module) {
	if opts.source != nil && opts.source.module == m {
		*opts.source = SourceContext{}
	}
}
