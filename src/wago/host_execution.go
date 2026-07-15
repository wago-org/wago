package wago

import (
	"fmt"
	"sync"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// hostControlInstances maps stable off-heap control-frame addresses to the
// physically live instance that owns the frame. Native code publishes the exact
// active frame when it parks, so cross-instance host dispatch uses the callee's
// import namespace and HostModule rather than the public root's.
var hostControlInstances sync.Map // map[uintptr]*Instance

func registerHostControl(in *Instance) error {
	if in == nil || len(in.ctrl) < coreruntime.HostCtrlFrameBytes {
		return fmt.Errorf("invalid synchronous host control frame")
	}
	ptr := offHeapSlicePtr(in.ctrl)
	if _, loaded := hostControlInstances.LoadOrStore(ptr, in); loaded {
		return fmt.Errorf("duplicate synchronous host control frame %x", ptr)
	}
	return nil
}

func unregisterHostControl(in *Instance) {
	if in == nil || len(in.ctrl) == 0 {
		return
	}
	ptr := offHeapSlicePtr(in.ctrl)
	if current, ok := hostControlInstances.Load(ptr); ok && current == in {
		hostControlInstances.Delete(ptr)
	}
}

func offHeapSlicePtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// dispatchSynchronousHostCall routes the common root-instance host call without
// touching the process-wide registry. A cross-instance callee falls back to the
// active control-frame lookup published by its native host stub.
func (root *Instance) dispatchSynchronousHostCall(ctrl uintptr, importIdx uint32, args, results []uint64) {
	active := root
	if root == nil || ctrl != offHeapSlicePtr(root.ctrl) {
		value, ok := hostControlInstances.Load(ctrl)
		if !ok {
			panic(invalidHostReference{err: fmt.Errorf("host control frame %x has no live instance", ctrl)})
		}
		active, ok = value.(*Instance)
		if !ok || active == nil {
			panic(invalidHostReference{err: fmt.Errorf("host control frame %x has no live instance", ctrl)})
		}
	}
	if active.hostCall == nil {
		panic(invalidHostReference{err: fmt.Errorf("host control frame %x has no dispatcher", ctrl)})
	}

	// Run arbitrary Go host code without the non-reentrant native execution
	// lease. The deferred reacquire covers normal return, HostExit, validation
	// panics, and arbitrary host panics. Rebind the exact parked callee because a
	// nested wasm entry may have replaced its shared basedata context.
	epoch := nativeExecutionEpoch
	nativeExecutionMu.Unlock()
	defer func() {
		nativeExecutionMu.Lock()
		// If no nested or competing public entry ran while host code owned the Go
		// stack, the parked callee's basedata is still installed. Avoid rewriting
		// all eight context words on this overwhelmingly common return path.
		if nativeExecutionEpoch != epoch {
			if err := active.bindNativeContext(); err != nil {
				panic(invalidHostReference{err: err})
			}
		}
	}()
	active.hostCall(ctrl, importIdx, args, results)
}
