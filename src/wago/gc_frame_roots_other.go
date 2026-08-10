//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlan(_ *wasm.Module, genericGC bool) *shared.GCModuleFrameRootPlan {
	if !genericGC {
		return nil
	}
	return &shared.GCModuleFrameRootPlan{Diagnostic: "exact native GC root maps are unavailable on this build target"}
}
