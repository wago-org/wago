//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"unsafe"
)

func nativeInstanceContextView(in *Instance) []byte {
	return unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), wruntime.InstanceContextBytes)
}
