//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"sync"

	"github.com/wago-org/wago/internal/runtimebridge"
)

// NativeScalarLeafSupported reports whether this build can bind native callbacks.
const NativeScalarLeafSupported = true

type nativeScalarLeaf struct {
	code   []byte
	direct uintptr
	owner  *nativeScalarLeafCode
}

// A process has one architecture and Go/native wrapper ABI. Function identity,
// the exact Go type, and parameter count distinguish the admitted wrapper shape.
type nativeScalarLeafKey struct {
	pc     uintptr
	typ    reflect.Type
	params uint32
}

type nativeScalarLeafCode struct {
	key  nativeScalarLeafKey
	leaf nativeScalarLeaf
	refs uint64
}

var nativeLeafHosts sync.Map // control-frame address -> retained nativeScalarLeaf
var nativeLeafCodeCache struct {
	sync.Mutex
	entries map[nativeScalarLeafKey]*nativeScalarLeafCode
}

// Admission, construction, and reference acquisition share this lock so a cache
// hit neither revalidates Go instructions nor assembles/maps another wrapper.
func registerSharedNativeScalarLeaf(ctrl []byte, fn any, params uint32, build func(any, uint32) (nativeScalarLeaf, bool, error)) (bool, error) {
	nativeLeafCodeCache.Lock()
	defer nativeLeafCodeCache.Unlock()
	if _, exists := nativeLeafHosts.Load(slicePtr(ctrl)); exists {
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func || value.IsNil() || params < 1 || params > 2 {
		return false, nil
	}
	key := nativeScalarLeafKey{pc: value.Pointer(), typ: value.Type(), params: params}
	entry := nativeLeafCodeCache.entries[key]
	fresh := entry == nil
	if fresh {
		leaf, ok, err := build(fn, params)
		if err != nil || !ok {
			return false, err
		}
		entry = &nativeScalarLeafCode{key: key, leaf: leaf}
	}
	leaf := entry.leaf
	leaf.owner = entry
	if _, loaded := nativeLeafHosts.LoadOrStore(slicePtr(ctrl), leaf); loaded {
		if fresh {
			_ = munmap(leaf.code)
		}
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	entry.refs++
	if fresh {
		if nativeLeafCodeCache.entries == nil {
			nativeLeafCodeCache.entries = make(map[nativeScalarLeafKey]*nativeScalarLeafCode)
		}
		nativeLeafCodeCache.entries[key] = entry
	}
	return true, nil
}

func nativeScalarLeafPtr(ctrl []byte) uintptr {
	if mem, ok := nativeLeafHosts.Load(slicePtr(ctrl)); ok {
		return slicePtr(mem.(nativeScalarLeaf).code)
	}
	return 0
}

// Instance quiescence precedes removal. Other registrations retain the immutable
// mapping; the last registration evicts it and releases executable memory.
func unregisterNativeScalarLeaf(ctrl []byte) {
	if value, ok := nativeLeafHosts.LoadAndDelete(slicePtr(ctrl)); ok {
		entry := value.(nativeScalarLeaf).owner
		nativeLeafCodeCache.Lock()
		defer nativeLeafCodeCache.Unlock()
		entry.refs--
		if entry.refs == 0 {
			delete(nativeLeafCodeCache.entries, entry.key)
			_ = munmap(entry.leaf.code)
		}
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
