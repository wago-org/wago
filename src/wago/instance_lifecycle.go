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

	// Before marking the instance closed, transfer producer roots to imported
	// funcref tables/globals that still hold any descriptor reachable through this
	// instance: local functions, canonical InstanceExport identities, bare-producer
	// proxies, or HostFuncRef proxies. Retaining the writer preserves its existing
	// transitive attachments. retainResourceRoot refuses a closed instance, so this
	// runs first; the container prunes the root after overwrite or on close.
	appendStep("retain imported table roots", func() { retainProducerRootsInImportedTables(in) })
	appendStep("retain imported global roots", func() { retainProducerRootsInImportedGlobals(in) })

	previousInvocations := in.closeInvocationEntry()
	activeInvocations := previousInvocations & instanceInvocationCount
	in.lifeMu.Lock()
	in.closed = true
	shouldRelease := in.resourceRefs == 0 && activeInvocations == 0
	store := in.refStore
	if activeInvocations != 0 && len(in.trap) >= 4 {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&in.trap[0])), uint32(runtime.TrapInterrupted))
	}
	in.lifeMu.Unlock()

	if store != nil {
		appendStep("close reference store instance", func() { store.instanceClosed(in) })
	}
	if shouldRelease {
		appendStep("release instance resources", in.releaseResources)
	}
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
	state := in.invocationState.Add(1)
	if state&instanceInvocationClosed == 0 {
		return nil
	}
	in.invocationState.Add(^uint32(0))
	return fmt.Errorf("instance is closed")
}

func (in *Instance) endInvocation() {
	if in == nil {
		return
	}
	state := in.invocationState.Add(^uint32(0))
	if state != instanceInvocationClosed {
		return
	}
	in.lifeMu.Lock()
	shouldRelease := in.closed && in.resourceRefs == 0 && !in.resourcesClosed
	store := in.refStore
	in.lifeMu.Unlock()
	if shouldRelease {
		if store != nil {
			store.resourceOwnerReleased(in)
		}
		in.releaseResources()
	}
}

func (in *Instance) releaseResources() {
	in.lifeMu.Lock()
	if in.resourcesClosed {
		in.lifeMu.Unlock()
		return
	}
	in.resourcesClosed = true
	in.lifeMu.Unlock()

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
