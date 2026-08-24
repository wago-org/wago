//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlan(_ *wasm.Module, exactRoots bool) *shared.GCModuleFrameRootPlan {
	if !exactRoots {
		return nil
	}
	return &shared.GCModuleFrameRootPlan{Diagnostic: "exact native GC root maps are unavailable on this build target"}
}
