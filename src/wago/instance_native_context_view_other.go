//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

func nativeInstanceContextView(in *Instance) []byte {
	return unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), wruntime.InstanceContextBytes)
}
