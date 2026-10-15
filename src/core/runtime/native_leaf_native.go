//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"sync"

	"github.com/wago-org/wago/internal/runtimebridge"
)

// NativeScalarLeafSupported reports whether this build can bind native callbacks.
const NativeScalarLeafSupported = true

type nativeScalarLeaf struct {
	code   []byte
	direct uintptr
}

var nativeLeafHosts sync.Map // control-frame address -> immutable nativeScalarLeaf

func nativeScalarLeafPtr(ctrl []byte) uintptr {
	if mem, ok := nativeLeafHosts.Load(slicePtr(ctrl)); ok {
		return slicePtr(mem.(nativeScalarLeaf).code)
	}
	return 0
}
func unregisterNativeScalarLeaf(ctrl []byte) {
	if mem, ok := nativeLeafHosts.LoadAndDelete(slicePtr(ctrl)); ok {
		_ = munmap(mem.(nativeScalarLeaf).code)
	}
}

// BindNativeScalarLeafImport replaces only an instance-owned import dispatch
// cell proven to select the same native leaf as the control frame. Shared compiled host thunks and reference descriptors retain
// their normal staged route. The caller proves this cell belongs to the frame.
func BindNativeScalarLeafImport(access runtimebridge.HostScalarCallAccess, ctrl, dispatch []byte) bool {
	if !access.Granted() || len(dispatch) != ImportDispatchEntryBytes {
		return false
	}
	value, ok := nativeLeafHosts.Load(slicePtr(ctrl))
	if !ok || value.(nativeScalarLeaf).direct == 0 {
		return false
	}
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCodePtrOffset:], uint64(value.(nativeScalarLeaf).direct))
	// Preserve the wrapper ABI for tail calls and fallback paths. The native
	// leaf tag additionally allows verified callers to omit context publication.
	caller := binary.LittleEndian.Uint64(dispatch[ImportDispatchCallerContextOffset:])
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCallerContextOffset:], (caller&^ImportDispatchCallerGoHostTag)|ImportDispatchCallerNativeLeafTag)
	return true
}
