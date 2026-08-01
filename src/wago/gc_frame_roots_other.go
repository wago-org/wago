//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlan(*wasm.Module, bool) *shared.GCModuleFrameRootPlan { return nil }
