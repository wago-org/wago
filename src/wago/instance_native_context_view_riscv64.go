//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import wruntime "github.com/wago-org/wago/src/core/runtime"

func nativeInstanceContextView(in *Instance) []byte {
	return in.ar.AllocatedRange(in.nativeContext, wruntime.InstanceContextBytes)
}
