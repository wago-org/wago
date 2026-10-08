//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"unsafe"
)

const railshotNativeInvocationContextBytes = abi.RV64InstanceContextBytes

func rv64NativeInvocationContext(in *Instance) []byte {
	return unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), abi.RV64InstanceContextBytes)
}

// Root entry and every foreign selector overwrite this borrowed pointer before
// guest code can read it. It is never a lifetime root or artifact address. The
// invocation gate retains the root's Engine/buffers and physical attachments.
func railshotBindNativeInvocation(in *Instance) {
	binary.LittleEndian.PutUint64(rv64NativeInvocationContext(in)[abi.RV64InvocationContextOffset:], uint64(in.eng.RV64InvocationViewPointer()))
}

// A foreign callback can reenter its producer on a different Engine. Resume
// must select the parked root's view, rather than the producer's own Engine.
func railshotRestoreNativeInvocation(active, root *Instance) {
	if active != nil && root != nil {
		if active != root {
			active.lifeMu.Lock()
			defer active.lifeMu.Unlock()
		}
		binary.LittleEndian.PutUint64(rv64NativeInvocationContext(active)[abi.RV64InvocationContextOffset:], uint64(root.eng.RV64InvocationViewPointer()))
	}
}

func railshotSaveNativeInvocation(in *Instance) uintptr {
	return uintptr(binary.LittleEndian.Uint64(rv64NativeInvocationContext(in)[abi.RV64InvocationContextOffset:]))
}
func railshotRestoreSavedNativeInvocation(in *Instance, pointer uintptr) {
	binary.LittleEndian.PutUint64(rv64NativeInvocationContext(in)[abi.RV64InvocationContextOffset:], uint64(pointer))
}
