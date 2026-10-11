//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (in *Instance) gcCollectFrameRoots(public *gcPublicState) gcNativeFrameRoots {
	return gcCollectFrameRoots(in, public, gcNativeFrameLayoutAMD64, false)
}

func (in *Instance) gcHelperRoots(_ uintptr, state *gcPublicState, _ uint32, _ *coreruntime.Engine) gc.RootSet {
	// The exact RV64 gc/ref_eq checkpoint has no collector references live in
	// native frames across a helper call: init stores each fresh handle into its
	// rooted eqref table before the next allocation. It still must publish the
	// instance/domain persistent-root universe so a stress collection cannot
	// recycle an earlier table-resident handle.
	if in != nil && state != nil && rv64OfficialGCRefEq(in.c) {
		state.frameRoots = gcCollectFrameRoots(in, state, gcNativeFrameLayoutAMD64, false)
		return &state.frameRoots
	}
	return gc.EmptyRoots{}
}
