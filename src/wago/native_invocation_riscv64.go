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

// The prototype word is saved/restored by Go scratch reentry. Native code uses
// only the basedata view slot, serialized by the execution lease: independent
// prototype restores cannot overwrite an active foreign activation. Neither
// word owns a lifetime root. The gate retains the Engine/buffers/attachments.
func railshotBindNativeInvocation(in *Instance) {
	pointer := in.eng.RV64InvocationViewPointer()
	binary.LittleEndian.PutUint64(rv64NativeInvocationContext(in)[abi.RV64InvocationContextOffset:], uint64(pointer))
	in.jm.SetRV64InvocationView(pointer)
}

// A foreign callback can reenter its producer on a different Engine. Resume
// must select its exact parked activation view, which can borrow transport
// within an ordinary foreign bridge, rather than the producer's own Engine.
func railshotRestoreNativeInvocation(active, root *Instance, pointer uintptr) {
	if active != nil && root != nil {
		if active != root {
			active.lifeMu.Lock()
			defer active.lifeMu.Unlock()
		}
		active.jm.SetRV64InvocationView(pointer)
	}
}

func railshotSaveNativeInvocation(in *Instance) uintptr {
	return uintptr(binary.LittleEndian.Uint64(rv64NativeInvocationContext(in)[abi.RV64InvocationContextOffset:]))
}
func railshotRestoreSavedNativeInvocation(in *Instance, pointer uintptr) {
	binary.LittleEndian.PutUint64(rv64NativeInvocationContext(in)[abi.RV64InvocationContextOffset:], uint64(pointer))
}

// Called before releasing the native lease, after active-owner authentication.
func railshotParkedNativeInvocation(active *Instance) uintptr {
	return active.jm.RV64InvocationView()
}

// A native foreign hop can replace this shared home without changing the Go
// prototype/version. Resume compares the exact parked control and live view
// under the reacquired native lease before trusting a reusable context.
func railshotParkedNativeContextChanged(in *Instance, ctrl, view uintptr) bool {
	return in.jm.CaptureInstanceContext().CustomCtx != ctrl || in.jm.RV64InvocationView() != view
}
