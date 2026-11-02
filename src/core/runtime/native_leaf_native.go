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

// The legacy trampoline is deliberately separate from indexed direct entries.
// Distinct registrations must never select one import as a frame-wide callback.
type nativeScalarLeafRegistration struct {
	legacy  nativeScalarLeaf
	imports []nativeScalarLeaf
}

var nativeLeafHosts sync.Map // control-frame address -> immutable registration
var nativeLeafCodeCache struct {
	sync.Mutex
	entries map[nativeScalarLeafKey]*nativeScalarLeafCode
}

// Caller holds the cache lock through acquisition and registration publication.
func acquireNativeScalarLeaf(fn any, params uint32, build func(any, uint32) (nativeScalarLeaf, bool, error)) (nativeScalarLeaf, bool, error) {
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func || value.IsNil() || params < 1 || params > 2 {
		return nativeScalarLeaf{}, false, nil
	}
	key := nativeScalarLeafKey{pc: value.Pointer(), typ: value.Type(), params: params}
	entry := nativeLeafCodeCache.entries[key]
	if entry == nil {
		leaf, ok, err := build(fn, params)
		if err != nil || !ok {
			return nativeScalarLeaf{}, false, err
		}
		entry = &nativeScalarLeafCode{key: key, leaf: leaf}
		if nativeLeafCodeCache.entries == nil {
			nativeLeafCodeCache.entries = make(map[nativeScalarLeafKey]*nativeScalarLeafCode)
		}
		nativeLeafCodeCache.entries[key] = entry
	}
	entry.refs++
	leaf := entry.leaf
	leaf.owner = entry
	return leaf, true, nil
}

func releaseNativeScalarLeaf(leaf nativeScalarLeaf) {
	entry := leaf.owner
	entry.refs--
	if entry.refs == 0 {
		delete(nativeLeafCodeCache.entries, entry.key)
		_ = munmap(entry.leaf.code)
	}
}

func registerSharedNativeScalarLeaf(ctrl []byte, fn any, params uint32, build func(any, uint32) (nativeScalarLeaf, bool, error)) (bool, error) {
	nativeLeafCodeCache.Lock()
	defer nativeLeafCodeCache.Unlock()
	if _, exists := nativeLeafHosts.Load(slicePtr(ctrl)); exists {
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	leaf, ok, err := acquireNativeScalarLeaf(fn, params, build)
	if err != nil || !ok {
		return false, err
	}
	nativeLeafHosts.Store(slicePtr(ctrl), nativeScalarLeafRegistration{legacy: leaf})
	return true, nil
}

// RegisterNativeScalarLeafImports retains independently proven direct wrappers
// for every indexed binding, or retains none. It never changes the trampoline.
// HostScalarCallAccess confines this installation API to the runtime bridge.
func RegisterNativeScalarLeafImports(access runtimebridge.HostScalarCallAccess, ctrl []byte, callbacks []any, params []uint32) (bool, error) {
	if !access.Granted() || len(ctrl) != ctrlFrameSize || len(callbacks) < 2 || len(callbacks) != len(params) {
		return false, nil
	}
	return registerNativeScalarLeafImports(ctrl, callbacks, params, buildNativeScalarLeaf)
}

func registerNativeScalarLeafImports(ctrl []byte, callbacks []any, params []uint32, build func(any, uint32) (nativeScalarLeaf, bool, error)) (bool, error) {
	nativeLeafCodeCache.Lock()
	defer nativeLeafCodeCache.Unlock()
	if _, exists := nativeLeafHosts.Load(slicePtr(ctrl)); exists {
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	leaves := make([]nativeScalarLeaf, 0, len(callbacks))
	committed := false
	defer func() {
		if !committed {
			for _, leaf := range leaves {
				releaseNativeScalarLeaf(leaf)
			}
		}
	}()
	for i, fn := range callbacks {
		leaf, ok, err := acquireNativeScalarLeaf(fn, params[i], build)
		if err != nil || !ok {
			return false, err
		}
		leaves = append(leaves, leaf)
		// arm64 can prove a staged body without proving a direct wrapper.
		if leaf.direct == 0 {
			return false, nil
		}
	}
	nativeLeafHosts.Store(slicePtr(ctrl), nativeScalarLeafRegistration{imports: leaves})
	committed = true
	return true, nil
}

func nativeScalarLeafPtr(ctrl []byte) uintptr {
	if value, ok := nativeLeafHosts.Load(slicePtr(ctrl)); ok {
		return slicePtr(value.(nativeScalarLeafRegistration).legacy.code)
	}
	return 0
}

// Instance quiescence precedes removal. Count every indexed acquisition, even
// aliases sharing one immutable mapping, and unmap only after the last owner.
func unregisterNativeScalarLeaf(ctrl []byte) {
	nativeLeafCodeCache.Lock()
	defer nativeLeafCodeCache.Unlock()
	if value, ok := nativeLeafHosts.LoadAndDelete(slicePtr(ctrl)); ok {
		registration := value.(nativeScalarLeafRegistration)
		if registration.legacy.owner != nil {
			releaseNativeScalarLeaf(registration.legacy)
		}
		for _, leaf := range registration.imports {
			releaseNativeScalarLeaf(leaf)
		}
	}
}

// BindNativeScalarLeafImportAt binds one acquired indexed entry. Validation and
// rollback of the complete instance-owned dispatch array belong to the caller.
func BindNativeScalarLeafImportAt(access runtimebridge.HostScalarCallAccess, ctrl, dispatch []byte, index int) bool {
	if !access.Granted() || len(dispatch) != ImportDispatchEntryBytes {
		return false
	}
	value, ok := nativeLeafHosts.Load(slicePtr(ctrl))
	if !ok {
		return false
	}
	leaves := value.(nativeScalarLeafRegistration).imports
	if index < 0 || index >= len(leaves) {
		return false
	}
	return bindNativeScalarLeafDispatch(dispatch, leaves[index])
}

func bindNativeScalarLeafDispatch(dispatch []byte, leaf nativeScalarLeaf) bool {
	if leaf.direct == 0 {
		return false
	}
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCodePtrOffset:], uint64(leaf.direct))
	caller := binary.LittleEndian.Uint64(dispatch[ImportDispatchCallerContextOffset:])
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCallerContextOffset:], (caller&^ImportDispatchCallerGoHostTag)|ImportDispatchCallerNativeLeafTag)
	return true
}

// BindNativeScalarLeafImport replaces only an instance-owned import dispatch
// cell proven to select the same native leaf as the control frame. Shared compiled host thunks and reference descriptors retain
// their normal staged route. The caller proves this cell belongs to the frame.
func BindNativeScalarLeafImport(access runtimebridge.HostScalarCallAccess, ctrl, dispatch []byte) bool {
	if !access.Granted() || len(dispatch) != ImportDispatchEntryBytes {
		return false
	}
	value, ok := nativeLeafHosts.Load(slicePtr(ctrl))
	if !ok {
		return false
	}
	return bindNativeScalarLeafDispatch(dispatch, value.(nativeScalarLeafRegistration).legacy)
}
