//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (in *Instance) gcCollectFrameRoots(public *gcPublicState) gcNativeFrameRoots {
	return gcCollectFrameRoots(in, public, gcNativeFrameLayoutAMD64, false)
}

func (in *Instance) gcHelperRoots(uintptr, *gcPublicState, uint32, *coreruntime.Engine) gc.RootSet {
	return gc.EmptyRoots{}
}
