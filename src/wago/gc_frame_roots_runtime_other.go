//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

import "github.com/wago-org/wago/src/core/runtime/gc"

func (in *Instance) gcHelperRoots(uintptr, *gcPublicState, uint32) gc.RootSet { return gc.EmptyRoots{} }
