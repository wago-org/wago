package wago

import (
	"sync"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// nativeExecutionMu is the initial correctness execution lease: exactly one
// native activation runs process-wide. Cross-instance calls therefore own every
// target basedata region they may rebind without recursive per-memory lock
// ordering. Synchronous host dispatch releases the lease while arbitrary Go code
// runs, then reacquires it and rebinds the exact parked callee before resume.
var (
	nativeExecutionMu    sync.Mutex
	nativeExecutionEpoch uint64 // guarded by nativeExecutionMu; advances on every public native entry
)

type executionLease struct{}

// beginNativeEntry acquires the serialized execution lease and rebinds this
// instance's pointer context before native code can observe basedata. Memory
// size/growth fields remain backing-owned; invocation control is refreshed by
// the engine entry/resume paths.
func (in *Instance) beginNativeEntry() *executionLease {
	nativeExecutionMu.Lock()
	nativeExecutionEpoch++
	in.bindNativeContext()
	return &executionLease{}
}

func (in *Instance) bindNativeContext() {
	ctx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), wruntime.InstanceContextBytes)
	in.jm.BindInstanceContextBytes(ctx)
}

func (*executionLease) unlockExecution() { nativeExecutionMu.Unlock() }

func (in *Instance) callNativeAsync(entry uintptr, prepared bool) error {
	locked := in.beginNativeEntry()
	defer locked.unlockExecution()
	if prepared {
		if err := refreshNativeControl(true, in.eng, in.jm, in.trap); err != nil {
			return err
		}
		return in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results)
	}
	return callNative(in.c, in.eng, in.jm, true, entry, in.serArgs, in.trap, in.results)
}
