package wago

import (
	"context"
	"errors"
	"fmt"

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
// Imported memory is left for the host to Close. Close is idempotent. A caller
// that joins an active close returns promptly, which permits callback reentry.
// Call WaitClosed to wait for the active close operation and receive its result.
func (in *Instance) Close() (err error) {
	if in == nil {
		return nil
	}
	state, owner := in.beginClose()
	if !owner {
		select {
		case <-state.done:
			return state.result
		default:
			// A callback may reenter Close while the lifecycle owner is still
			// active. Returning promptly avoids self-deadlock; external callers
			// that need completion use WaitClosed.
			return nil
		}
	}
	defer func() {
		if recover() != nil {
			err = joinPrimary(err, fmt.Errorf("wago: instance close: %w", ErrCallbackPanic))
		}
		state.result = err
		close(state.done)
	}()
	return in.closeOnce()
}

// WaitClosed waits for an already-started Close operation and returns its result.
// It does not start closure or wait for physical release held by active guest
// calls or retained references. Close callbacks must not wait for themselves.
func (in *Instance) WaitClosed(ctx context.Context) error {
	if in == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	state := in.ensurePluginState().close.Load()
	if state == nil {
		return fmt.Errorf("wago: instance close has not started")
	}
	select {
	case <-state.done:
		return state.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (in *Instance) beginClose() (*instanceCloseState, bool) {
	state := in.ensurePluginState()
	if active := state.close.Load(); active != nil {
		return active, false
	}
	candidate := &instanceCloseState{done: make(chan struct{}), quiesced: make(chan struct{})}
	if state.close.CompareAndSwap(nil, candidate) {
		return candidate, true
	}
	return state.close.Load(), false
}

func (in *Instance) closeOnce() error {
	var errs []error
	appendStep := func(phase string, fn func()) {
		if err := callShutdownSafely(phase, fn); err != nil {
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
	cancelAtomicWaitContext(in)
	activeInvocations := previousInvocations & instanceInvocationCount
	in.lifeMu.Lock()
	if activeInvocations != 0 && len(in.trap) >= 4 {
		// Host re-entry swaps the trap slice under lifeMu, so Close observes one
		// complete active slice header before requesting interruption.
		in.ensurePluginState().close.Load().interruptStop = runtime.RequestInterruptAsync(in.trap)
	}
	in.lifeMu.Unlock()

	var hooks *hookRegistry
	if in.rt != nil {
		hooks = in.rt.loadHooks()
		for i := len(hooks.internalBeforeClose) - 1; i >= 0; i-- {
			fn := hooks.internalBeforeClose[i]
			appendStep("internal BeforeClose", func() { fn(in) })
		}
	}

	var closeEvent *InstanceCloseEvent
	if hooks != nil && (len(hooks.beforeClose) != 0 || len(hooks.afterClose) != 0) {
		event := InstanceCloseEvent{Module: ModuleView{compiled: in.c, identity: in.moduleIdentity}, Instance: InstanceIdentity{value: in}, Origin: in.instantiateOrigin()}
		closeEvent = &event
		closeState := in.ensurePluginState().close.Load()
		closeState.hooks, closeState.event = hooks, closeEvent
		for i := len(hooks.beforeClose) - 1; i >= 0; i-- {
			fn := hooks.beforeClose[i]
			appendStep("BeforeClose", func() { fn(*closeEvent) })
		}
	}

	// An activation parked in host code may have resumed and mutated an imported
	// table/global while hooks ran. Mark logical closure now, but defer terminal
	// disposal observation and physical release until every admitted invocation
	// and the construction lifecycle have quiesced.
	in.lifeMu.Lock()
	in.closed = true
	store := in.refStore
	in.lifeMu.Unlock()

	appendStep("close reference store instance", func() { in.referenceLifetime().notifyStore(store, referenceLifetimeClosed) })
	appendStep("finalize instance resources", in.tryFinalize)
	return errors.Join(errs...)
}

// closeAndWait closes the public invocation gate and waits until no guest
// activation or synchronous host callback can still execute plugin code. It is
// used by Runtime and manager shutdown; ordinary Instance.Close intentionally
// remains a prompt logical close when retained references defer physical release.
func (in *Instance) closeAndWait() error {
	if in == nil {
		return nil
	}
	closeErr := in.Close()
	state := in.ensurePluginState().close.Load()
	if state != nil {
		<-state.done
		<-state.quiesced
		return joinPrimary(state.result, state.terminalResult)
	}
	return closeErr
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
	if in.guestStorageBorrowed() {
		return fmt.Errorf("instance access is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	if in.rt == nil {
		return in.beginInstanceInvocation()
	}
	in.rt.mu.Lock()
	if in.rt.state == runtimeClosed || in.rt.state == runtimeClosing && in.instantiateOrigin() != InstantiateManaged {
		in.rt.mu.Unlock()
		return fmt.Errorf("instance runtime is closed")
	}
	in.rt.activeOperations++
	in.rt.mu.Unlock()
	admitted := false
	defer func() {
		if admitted {
			return
		}
		in.rt.mu.Lock()
		in.rt.activeOperations--
		in.rt.stateCond.Broadcast()
		in.rt.mu.Unlock()
	}()
	if err := in.beginInstanceInvocation(); err != nil {
		return err
	}
	admitted = true
	return nil
}

func (in *Instance) beginInstanceInvocation() error {
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
			if closeState := in.ensurePluginState().close.Load(); closeState != nil {
				closeState.quiescedOnce.Do(func() { close(closeState.quiesced) })
			}
			in.tryFinalize()
		}
		// Keep the Runtime operation admitted through terminal instance
		// finalization. Runtime shutdown uses this count as its barrier, so
		// publishing it earlier could let WaitClosed return before reference
		// tokens and store membership were released.
		if in.rt != nil {
			in.rt.mu.Lock()
			if in.rt.activeOperations == 0 {
				in.rt.mu.Unlock()
				panic("wago: runtime invocation operation underflow")
			}
			in.rt.activeOperations--
			in.rt.stateCond.Broadcast()
			in.rt.mu.Unlock()
		}
		return
	}
}

// tryFinalize delegates the reference-lifetime transition. Keeping this small
// call point lets resource-root and invocation paths remain direct.
func (in *Instance) tryFinalize() {
	if in == nil || in.constructionIsActive() || in.invocationState.Load()&instanceInvocationClosed == 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
		return
	}
	if closeState := in.ensurePluginState().close.Load(); closeState != nil {
		closeState.terminalOnce.Do(func() {
			if closeState.hooks != nil && closeState.event != nil {
				var errs []error
				for i := len(closeState.hooks.afterClose) - 1; i >= 0; i-- {
					fn := closeState.hooks.afterClose[i]
					if err := callShutdownSafely("AfterClose", func() { fn(*closeState.event) }); err != nil {
						errs = append(errs, err)
					}
				}
				closeState.terminalResult = errors.Join(errs...)
			}
			closeState.quiescedOnce.Do(func() { close(closeState.quiesced) })
		})
	}
	in.referenceLifetime().finalize()
}

func (in *Instance) constructionIsActive() bool {
	if in == nil {
		return false
	}
	in.lifeMu.Lock()
	active := in.constructionActive
	in.lifeMu.Unlock()
	return active
}

func (in *Instance) beginConstruction(reservation *pluginOperationReservation) {
	if in == nil {
		return
	}
	in.lifeMu.Lock()
	if in.constructionActive {
		in.lifeMu.Unlock()
		panic("wago: instance construction lifetime already started")
	}
	in.constructionActive = true
	in.constructionReservation = reservation
	in.lifeMu.Unlock()
}

func (in *Instance) endConstruction() {
	if in == nil {
		return
	}
	in.lifeMu.Lock()
	if !in.constructionActive {
		in.lifeMu.Unlock()
		panic("wago: instance construction lifetime is not active")
	}
	in.constructionActive = false
	in.constructionReservation = nil
	in.lifeMu.Unlock()
	in.tryFinalize()
}

func (in *Instance) constructionReservationSnapshot() *pluginOperationReservation {
	if in == nil {
		return nil
	}
	in.lifeMu.Lock()
	reservation := in.constructionReservation
	in.lifeMu.Unlock()
	return reservation
}

// releaseResources performs the physical teardown after tryFinalize has claimed
// it by setting resourcesClosed under lifeMu.
func (in *Instance) releaseResources() {
	if state := in.ensurePluginState().close.Load(); state != nil && state.interruptStop != nil {
		state.interruptStop()
		state.interruptStop = nil
	}
	// Every imported raw-pointer dependency remains attached until physical
	// release. A table/global/token or downstream function importer may keep this
	// instance's native code and context callable after logical Close.
	detachImportedFunctions(in)
	detachImportedHostFuncRefs(in)
	detachImportedGlobals(in)
	detachImportedTables(in)
	detachImportedTags(in)
	if in.gc != nil {
		closeCollector := func() {
			if table := in.existingGCRefTestTableState(); table != nil {
				table.drop(in.gc)
			}
			if in.executionFlags.Load()&executionFlagStoreOwnedGCCollector == 0 {
				in.gc.Close()
			}
		}
		if state := in.existingPublicGCState(); state != nil {
			state.mu.Lock()
			closeCollector()
			state.mu.Unlock()
		} else {
			closeCollector()
		}
	}
	for table := in.table; table != nil; table = table.next {
		table.releaseRetainedInstances()
	}
	for _, global := range in.globalCells {
		global.instanceOwnerClosed(in)
	}
	unregisterHostControl(in)
	if in.thunkMem != nil {
		runtime.Unmap(in.thunkMem)
		in.thunkMem = nil
	}
	in.c.releaseCode()
	runtime.ReleaseArena(in.ar)
	var detachedMemories importDedup[*Memory]
	if in.memoryDir != nil {
		for i := len(in.memoryDir.memories) - 1; i >= 1; i-- {
			memory := in.memoryDir.memories[i]
			if memory == nil {
				continue
			}
			if i < len(in.memoryDir.owns) && in.memoryDir.owns[i] {
				memoryJM := memory.jobMemory()
				memory.ownerClosed()
				runtime.ReleaseJobMemory(memoryJM)
			} else if detachedMemories.add(memory) && !in.ownsTransferredMemoryAttachment(memory) {
				memory.detachImporter()
			}
		}
	}
	if in.c.threadedMemory0() {
		runtime.ReleaseJobMemory(in.jm)
		if in.memory != nil && detachedMemories.add(in.memory) && !in.ownsTransferredMemoryAttachment(in.memory) {
			in.memory.detachImporter()
		}
	} else if in.ownsMem {
		if in.memory != nil {
			in.memory.ownerClosed()
		}
		runtime.ReleaseJobMemory(in.jm)
	} else if in.memory != nil && detachedMemories.add(in.memory) && !in.ownsTransferredMemoryAttachment(in.memory) {
		in.memory.detachImporter()
	}
	transferredImportAttachments.Delete(in)
	runtime.ReleaseEngine(in.eng)
	if in.rt != nil {
		in.rt.unregisterInstance(in)
	}

	// Reusable arenas and job-memory objects may immediately back a later
	// instance. Keep the closed instance from retaining any stale view into that
	// storage after every teardown step above has completed.
	in.jm = nil
	in.ar = nil
	in.eng = nil
	in.base = 0
	in.nativeContext = 0
	in.tableDescPtr = 0
	in.tableDescLen = 0
	in.globalCells = nil
	in.globals = nil
	in.funcRefDescs = nil
	in.passiveDataDesc = nil
	in.serArgs = nil
	in.results = nil
	in.trap = nil
	in.resultVals = nil
}

// Memory returns the instance's linear-memory object (instance-owned or the
// host-imported one). Use Memory().UnsafeBytes() for an explicitly unsafe
// zero-copy byte view. A close that wins the acquisition race returns nil
// instead of a dangling object.
func (in *Instance) Memory() *Memory {
	if in == nil || in.c == nil || in.c.memoryCount() == 0 || in.memory == nil || in.beginInvocation() != nil {
		return nil
	}
	defer in.endInvocation()
	if in.ownsMem && in.memory.observeOwner(in) != nil {
		return nil
	}
	return in.memory
}
