//go:build arm64 && !wago_precompiled

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotValidationHintFactory(cfg *RuntimeConfig, workers int) wasm.ValidationHintFactory {
	if workers != 1 || cfg.gcCodeTelemetry || cfg.codeProfile != nil || codegen.SourceChecks {
		return nil
	}
	if on, present := cfg.optimizations["validated-hint-stream"]; present && !on {
		return nil
	}
	return func(m *wasm.Module) wasm.ValidationHintObserver {
		return railshot.NewValidationHintObserver(m, railshot.CompileOptions{Workers: 1, Optimizations: cfg.optimizations, OptimizationSnapshot: cfg.optimizationSnapshot, OptimizationDeltas: cfg.optimizationDeltas})
	}
}
func railshotAttachValidationHintProof(opts *railshotCompileOptions, analysis *wasm.ValidatedModuleAnalysis) {
	if analysis.HintObserver() != nil {
		opts.ValidationAnalysis = analysis
	}
}
