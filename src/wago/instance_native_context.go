package wago

import (
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// beginNativeEntry serializes shared-memory users and rebinds this instance's
// pointer context before native code can observe basedata. Memory size/growth
// fields remain owned by the shared backing; trap and stack fields are refreshed
// by the normal invocation path.
func (in *Instance) beginNativeEntry() *memoryState {
	locked := in.memory.lockExecution()
	ctx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), wruntime.InstanceContextBytes)
	in.jm.BindInstanceContextBytes(ctx)
	return locked
}

func (in *Instance) callNativeAsync(entry uintptr, prepared bool) error {
	locked := in.beginNativeEntry()
	defer locked.unlockExecution()
	if prepared {
		if err := refreshNativeControl(in.nativeControlShared, in.eng, in.jm, in.trap); err != nil {
			return err
		}
		return in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results)
	}
	return callNative(in.c, in.eng, in.jm, in.nativeControlShared, entry, in.serArgs, in.trap, in.results)
}
