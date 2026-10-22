//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlanWithInstructions(m *wasm.Module, exact, generic bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis, instructions map[uint32]railcore.CustomInstruction) *shared.GCModuleFrameRootPlan {
	if !exact || len(instructions) == 0 {
		return newGCFrameRootPlan(m, exact, generic, diagnostic, analysis)
	}
	if err := railshot.ValidateCustomInstructions(m, instructions); err != nil {
		if diagnostic != nil {
			*diagnostic = err.Error()
		}
		return nil
	}
	return newGCFrameRootPlanOwned(m, exact, diagnostic, analysis, railshotManagedExceptionReferences(m), instructions)
}

func railshotGCRootedScalarCustomModule(m *wasm.Module, instructions map[uint32]railcore.CustomInstruction) bool {
	return railshot.AdmitGCRootedScalarCustomModule(m, instructions)
}
