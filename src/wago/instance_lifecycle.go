package wago

import (
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
)

const (
	instanceInvocationClosed = uint32(1 << 31)
	instanceInvocationCount  = instanceInvocationClosed - 1
)

// Close logically closes the instance and releases its mapped code, engine, and
// owned memory as soon as no invocation or retained reference can still reach
// them. An activation parked in host code may finish after Close returns; its
// invocation lease defers physical release until native execution has unwound.
// Imported memory is left for the host to Close. Close is idempotent. Concurrent
// callers wait for the active close operation and receive its same result.
func (in *Instance) Close() (err error) {
	if in == nil {
		return nil
	}
	state, owner := in.beginClose()
	if !owner {
		<-state.done
		return state.result
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if panicErr, ok := recovered.(error); ok {
				err = joinPrimary(err, fmt.Errorf("wago: instance close panicked: %w", panicErr))
			} else {
				err = joinPrimary(err, fmt.Errorf("wago: instance close panicked: %v", recovered))
			}
		}
		state.result = err
		close(state.done)
	}()
	return in.closeOnce()
}

func (in *Instance) beginClose() (*instanceCloseState, bool) {
	state := in.ensurePluginState()
	if active := state.close.Load(); active != nil {
		return active, false
	}
	candidate := &instanceCloseState{done: make(chan struct{})}
	if state.close.CompareAndSwap(nil, candidate) {
		return candidate, true
	}
	return state.close.Load(), false
}

func (in *Instance) closeOnce() error {
	var errs []error
	appendStep := func(phase string, fn func()) {
		if err := callHookSafely(phase, fn); err != nil {
			errs = append(errs, err)
		}
	}

	in.lifeMu.Lock()
	alreadyClosed := in.closed
	in.lifeMu.Unlock()
	if alreadyClosed {
		return nil
	}

	// Publish the invocation gate before lifecycle hooks. Existing activations may
	// finish, but hooks and concurrent callers observe a logically closed instance.
	// Physical finalization remains disabled until all BeforeClose hooks finish.
	previousInvocations := in.closeInvocationEntry()
	activeInvocations := previousInvocations & instanceInvocationCount
	in.lifeMu.Lock()
	if activeInvocations != 0 && len(in.trap) >= 4 {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&in.trap[0])), uint32(runtime.TrapInterrupted))
	}
	in.lifeMu.Unlock()

	if in.rt != nil {
		for i := len(in.rt.hooks.internalBeforeClose) - 1; i >= 0; i-- {
			fn := in.rt.hooks.internalBeforeClose[i]
			appendStep("internal BeforeClose", func() { fn(in) })
		}
	}

	var hctx *InstanceContext
	if in.rt != nil && (len(in.rt.hooks.beforeClose) != 0 || len(in.rt.hooks.afterClose) != 0) {
		hctx = &InstanceContext{
			Runtime: in.rt, Compiled: in.c, Instance: in, Origin: in.instantiateOrigin(), Metadata: map[string]any{},
		}
		for i := len(in.rt.hooks.beforeClose) - 1; i >= 0; i-- {
			fn := in.rt.hooks.beforeClose[i]
			appendStep("BeforeClose", func() { fn(hctx) })
		}
	}

	// An activation parked in host code may have resumed and mutated an imported
	// table/global while hooks ran. Root transfer is therefore deferred to
	// tryFinalize after the active count reaches zero. closed enables that final
	// transition only after BeforeClose is completely finished.
	in.lifeMu.Lock()
	in.closed = true
	store := in.refStore
	in.lifeMu.Unlock()

	if store != nil {
		appendStep("close reference store instance", func() { store.instanceClosed(in) })
	}
	appendStep("finalize instance resources", in.tryFinalize)
	if hctx != nil {
		for i := len(in.rt.hooks.afterClose) - 1; i >= 0; i-- {
			fn := in.rt.hooks.afterClose[i]
			appendStep("AfterClose", func() { fn(hctx) })
		}
	}
	return errors.Join(errs...)
}

func (in *Instance) isLogicallyClosed() bool {
	return in == nil || in.invocationState.Load()&instanceInvocationClosed != 0
}

func (in *Instance) closeInvocationEntry() uint32 {
	for {
		state := in.invocationState.Load()
		if state&instanceInvocationClosed != 0 {
			return state
		}
		if in.invocationState.CompareAndSwap(state, state|instanceInvocationClosed) {
			return state
		}
	}
}

func (in *Instance) beginInvocation() error {
	if in == nil {
		return fmt.Errorf("instance is nil")
	}
	for {
		state := in.invocationState.Load()
		if state&instanceInvocationClosed != 0 {
			return fmt.Errorf("instance is closed")
		}
		if state&instanceInvocationCount == instanceInvocationCount {
			return fmt.Errorf("instance has too many active invocations")
		}
		if in.invocationState.CompareAndSwap(state, state+1) {
			return nil
		}
	}
}

func (in *Instance) endInvocation() {
	if in == nil {
		return
	}
	for {
		state := in.invocationState.Load()
		if state&instanceInvocationCount == 0 {
			panic("wago: invocation lease underflow")
		}
		next := state - 1 // the count cannot borrow through the separately checked close bit
		if !in.invocationState.CompareAndSwap(state, next) {
			continue
		}
		if next == instanceInvocationClosed {
			in.tryFinalize()
		}
		return
	}
}

// tryFinalize owns the exactly-once transition from logically closed to
// physically released. The final imported-table/global scan runs only after the
// invocation gate is closed and the active count is zero, so it observes every
// write made by a resumed host-parked activation. Resource-root release races
// are resolved by the final locked recheck before resourcesClosed is committed.
func (in *Instance) tryFinalize() {
	if in == nil {
		return
	}
	in.lifeMu.Lock()
	if !in.closed || in.finalizing || in.resourcesClosed || in.invocationState.Load()&instanceInvocationCount != 0 {
		in.lifeMu.Unlock()
		return
	}
	in.finalizing = true
	in.lifeMu.Unlock()

	retainProducerRootsInImportedTablesForFinalization(in)
	retainProducerRootsInImportedGlobalsForFinalization(in)

	in.lifeMu.Lock()
	in.finalizing = false
	if in.resourcesClosed || !in.closed || in.resourceRefs != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
		in.lifeMu.Unlock()
		return
	}
	in.resourcesClosed = true // commit the one physical-release owner
	store := in.refStore
	in.lifeMu.Unlock()

	if store != nil {
		store.resourceOwnerReleased(in)
	}
	in.releaseResources()
}

// releaseResources performs the physical teardown after tryFinalize has claimed
// it by setting resourcesClosed under lifeMu.
func (in *Instance) releaseResources() {
	// Every imported raw-pointer dependency remains attached until physical
	// release. A table/global/token or downstream function importer may keep this
	// instance's native code and context callable after logical Close.
	detachImportedFunctions(in)
	detachImportedHostFuncRefs(in)
	detachImportedGlobals(in)
	detachImportedTables(in)
	transferredImportAttachments.Delete(in)
	if in.gc != nil {
		in.gc.Close()
	}
	for table := in.table; table != nil; table = table.next {
		table.releaseRetainedInstances()
	}
	unregisterHostControl(in)
	if in.thunkMem != nil {
		runtime.Unmap(in.thunkMem)
		in.thunkMem = nil
	}
	in.c.releaseCode()
	runtime.ReleaseArena(in.ar)
	if in.ownsMem {
		if in.memory != nil {
			in.memory.ownerClosed()
		}
		runtime.ReleaseJobMemory(in.jm)
	} else if in.memory != nil {
		in.memory.detachImporter()
	}
	runtime.ReleaseEngine(in.eng)
}

// Memory returns the instance's linear-memory object (instance-owned or the
// host-imported one). Use Memory().Bytes() for the zero-copy byte view.
func (in *Instance) Memory() *Memory { return in.memory }
