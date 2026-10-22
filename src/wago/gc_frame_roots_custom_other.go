//go:build !linux || !riscv64 || tinygo || wago_precompiled

package wago

import (
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlanWithInstructions(m *wasm.Module, exact, generic bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis, _ map[uint32]railcore.CustomInstruction) *shared.GCModuleFrameRootPlan {
	return newGCFrameRootPlan(m, exact, generic, diagnostic, analysis)
}

func railshotGCRootedScalarCustomModule(*wasm.Module, map[uint32]railcore.CustomInstruction) bool {
	return false
}
