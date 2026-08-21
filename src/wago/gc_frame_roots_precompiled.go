//go:build wago_precompiled && ((linux && amd64) || ((linux || darwin) && arm64))

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlan(_ *wasm.Module, exactRoots bool) *shared.GCModuleFrameRootPlan {
	if !exactRoots {
		return nil
	}
	return &shared.GCModuleFrameRootPlan{Diagnostic: "source compilation is unavailable in a precompiled runtime"}
}
