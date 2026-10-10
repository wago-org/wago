package wago

import (
	"context"
	"errors"
	"fmt"

	"github.com/wago-org/wago/src/core/runtime"
)

var completedInstanceClose = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

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
		if state.completed.Load() {
			err = joinPrimary(state.result, railshotRetryClosedDependencyComponent(in))
			err = joinPrimary(err, railshotRetryClosedNativeTableRetirement(in))
			err = joinPrimary(err, railshotRetryClosedTableDescriptorRetirement(in))
			return joinPrimary(err, railshotRetryClosedGlobalDescriptorRetirement(in))
		}
		// A callback may reenter Close while the lifecycle owner is still
		// active. Returning promptly avoids self-deadlock; external callers
		// that need completion use WaitClosed.
		return nil
	}
	defer func() {
		if recover() != nil {
			err = joinPrimary(err, fmt.Errorf("wago: instance close: %w", ErrCallbackPanic))
		}
		err = joinPrimary(err, railshotRetryClosedNativeTableRetirement(in))
		err = joinPrimary(err, railshotRetryClosedTableDescriptorRetirement(in))
		err = joinPrimary(err, railshotRetryClosedGlobalDescriptorRetirement(in))
		collectionEnabled := railshotTerminalClosedDependencyCollectionEnabled(in, state)
		var collect bool
		err, collect = state.finishLogicalCloseCollectionHandoff(err, collectionEnabled, func() error {
			return railshotClosedContainerAdoptionResult(in, state)
		})
		if collect {
			err = joinPrimary(err, callShutdownSafely("collect closed dependency component", func() {
				railshotCollectClosingDependencyComponent(in, state)
			}))
			// The terminal-first handoff leaves ordinary finalization to this
			// result owner. Keep its scoped permit and error journal through the
			// scan, then retry after the transient finalizing claim is clear.
			err = joinPrimary(err, callShutdownSafely("finalize closing dependency owner", func() {
				in.referenceLifetime().finalize()
			}))
			err = joinPrimary(err, callShutdownSafely("retry closing dependency component", func() {
				railshotCollectClosingDependencyComponent(in, state)
			}))
			state.completeLogicalCloseCollection(err)
		}
		railshotCompleteClosingDependencyCollection(in, state)
		// Managed terminal work must not detach ownership before the logical
		// result is available to drain and WaitClosed. Retry after publication.
		if in.hasManagedOwner() {
			in.tryFinalize()
		}
	}()
	if codeProfileEnabled && in.lifecycleProfile() != nil {
		span := in.beginProfileLifecycle("logical-close")
		defer finishProfileBoundary(span, &err)
	}
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
	state := in.closeState.Load()
	if state == nil {
		return fmt.Errorf("wago: instance close has not started")
	}
	select {
	case <-state.doneChannel():
		return state.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (in *Instance) beginClose() (*instanceCloseState, bool) {
	if active := in.closeState.Load(); active != nil {
		return active, false
	}
	candidate := &instanceCloseState{}
	if in.rt != nil || in.hasManagedOwner() {
		candidate.quiesced = make(chan struct{})
		candidate.terminalDone = make(chan struct{})
	}
	if in.closeState.CompareAndSwap(nil, candidate) {
		return candidate, true
	}
	return in.closeState.Load(), false
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
		in.closeState.Load().interruptStop = runtime.RequestInterruptAsync(in.trap)
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
		closeState := in.closeState.Load()
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
	in.closeState.Load().prepared.Store(true)
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
	_ = in.Close()
	return in.waitTerminalClose()
}

// waitTerminalClose joins logical preparation and terminal hooks, but does not
// wait for resources retained by other instances or public reference tokens.
func (in *Instance) waitTerminalClose() error {
	state := in.closeState.Load()
	if state != nil {
		<-state.doneChannel()
		if state.terminalDone != nil {
			<-state.terminalDone
		}
		result := joinPrimary(state.result, state.terminalResult)
		if adoptionErr := railshotClosedContainerAdoptionResult(in, state); adoptionErr != nil && !errors.Is(result, adoptionErr) {
			result = joinPrimary(result, adoptionErr)
		}
		return result
	}
	return nil
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

func (in *Instance) beginDirectInvocation() error {
	if in.tryBeginDirectInvocation() {
		return nil
	}
	return in.beginInvocation()
}

// tryBeginDirectInvocation keeps the uncontended admission small enough to
// inline. A false result acquired no lease; the caller must use beginInvocation
// for Runtime accounting, contention, and the original error handling.
func (in *Instance) tryBeginDirectInvocation() bool {
	return in.rt == nil && !in.guestStorageBorrowed() && in.invocationState.word.CompareAndSwap(0, 1)
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
			if closeState := in.closeState.Load(); closeState != nil {
				closeState.signalQuiesced()
			}
			var cleanup resourceCleanup
			cleanup.run(in.tryFinalize)
			cleanup.run(in.endRuntimeInvocation)
			cleanup.finish()
			return
		}
		in.endRuntimeInvocation()
		return
	}
}

// Keep the operation admitted through finalization, including panicking physical
// callbacks. Managed terminal hooks have a separate terminalDone drain.
func (in *Instance) endRuntimeInvocation() {
	if in.rt == nil {
		return
	}
	in.rt.mu.Lock()
	if in.rt.activeOperations == 0 {
		in.rt.mu.Unlock()
		panic("wago: runtime invocation operation underflow")
	}
	in.rt.activeOperations--
	in.rt.stateCond.Broadcast()
	in.rt.mu.Unlock()
}

// endDirectInvocation releases the uncontended direct-instance lease with one
// atomic operation. A concurrent Close changes the state and takes the full
// endInvocation finalization path; Runtime-owned instances retain operation accounting.
func (in *Instance) endDirectInvocation() {
	if in.rt == nil && in.invocationState.word.CompareAndSwap(1, 0) {
		return
	}
	in.endInvocation()
}

// tryFinalize delegates the reference-lifetime transition. Keeping this small
// call point lets resource-root and invocation paths remain direct.
func (in *Instance) tryFinalize() {
	if in == nil || in.constructionIsActive() || in.invocationState.Load()&instanceInvocationClosed == 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
		return
	}
	if closeState := in.closeState.Load(); closeState != nil {
		if !closeState.prepared.Load() {
			return
		}
		managed := in.hasManagedOwner()
		if managed {
			if !closeState.completed.Load() {
				return
			}
		}
		closeState.signalQuiesced()
		if closeState.terminalStarted.CompareAndSwap(false, true) {
			if managed && closeState.hooks != nil && len(closeState.hooks.afterClose) != 0 {
				// Managed Close must return without waiting for terminal hooks.
				// One short-lived worker owns hooks, detachment, and finalization;
				// there is no per-instance background waiter for ownership cleanup.
				go in.finishTerminalClose(closeState)
			} else {
				in.finishTerminalClose(closeState)
			}
			return
		} else {
			// A hook can release a reference and reenter tryFinalize. Do not
			// wait for that hook here, or release its resources underneath it.
			if !closeState.terminalComplete.Load() {
				return
			}
		}
	}
	in.referenceLifetime().finalize()
}

func (in *Instance) hasManagedOwner() bool {
	in.lifeMu.Lock()
	managed := in.finalizers != nil && in.finalizers.managed != nil
	in.lifeMu.Unlock()
	return managed
}

func (in *Instance) finishTerminalClose(state *instanceCloseState) {
	var errs []error
	if state.hooks != nil && state.event != nil {
		for i := len(state.hooks.afterClose) - 1; i >= 0; i-- {
			fn := state.hooks.afterClose[i]
			if err := callShutdownSafely("AfterClose", func() { fn(*state.event) }); err != nil {
				errs = append(errs, err)
			}
		}
	}
	adoptionErr := callShutdownSafely("adopt closed container dependencies", func() {
		railshotTerminalClosedContainerDependencyAdoption(in, state)
	})
	railshotPublishClosedContainerAdoptionResult(in, state, adoptionErr)
	in.lifeMu.Lock()
	if in.finalizers != nil && in.finalizers.managed != nil {
		state.terminalResult = errors.Join(errs...)
		managed := in.finalizers.managed
		in.finalizers.managed = nil
		managed.finishTerminalClose(state)
		in.lifeMu.Unlock()
		in.referenceLifetime().finalize()
		return
	}
	in.lifeMu.Unlock()

	// If logical Close already published while an invocation was active, this
	// terminal owner is the final production trigger for an ordinary closed
	// component. Publish terminal eligibility before discovery, but keep the
	// terminal waiter blocked until callback failures have joined its result.
	// Synchronous Close still uses its narrower pre-publication permit later in
	// the Close owner defer. Managed terminal ownership remains a separate phase.
	collectionEnabled := railshotTerminalClosedDependencyCollectionEnabled(in, state)
	terminalOwnsCollection := collectionEnabled && state.publishTerminalCollectionReady()
	if terminalOwnsCollection {
		if err := callShutdownSafely("collect terminal closed dependency component", func() {
			railshotCollectTerminalClosedDependencyComponent(in, state)
		}); err != nil {
			errs = append(errs, err)
		}
		// A peer can become terminal-ready while this owner is in its ordinary
		// finalization scan. Finalizer exit requests one durable ordinary retry;
		// keep terminalDone unpublished until that retry and its callbacks finish.
		if err := callShutdownSafely("finalize terminal dependency owner", func() {
			in.referenceLifetime().finalize()
		}); err != nil {
			errs = append(errs, err)
		}
		// The finalization scan clears its transient finalizing claim before it
		// returns. Retry only from this delayed-unmanaged terminal authority: a
		// peer may have consumed its first attempt while that claim was visible.
		// Keeping the retry here prevents managed and publication-side adoption
		// schedulers from inheriting callback/error ownership.
		if err := callShutdownSafely("retry terminal closed dependency component", func() {
			railshotCollectTerminalClosedDependencyComponent(in, state)
		}); err != nil {
			errs = append(errs, err)
		}
		state.terminalResult = errors.Join(errs...)
		state.signalTerminalDone()
		return
	}
	state.terminalResult = errors.Join(errs...)
	state.signalTerminalDone()
	if collectionEnabled {
		// Terminal readiness won the crossing. The logical Close owner now owns
		// collection, this ordinary finalization scan, its bounded retry, and any
		// callback error. Starting a second scan here would make its own scoped
		// attempt observe finalizing and strand the component.
		return
	}
	in.referenceLifetime().finalize()
}

// finishLogicalCloseCollectionHandoff atomically chooses which side of the
// logical-result/terminal-readiness crossing owns the one collection attempt.
// If terminal work is not ready, publishing completed under the same lock
// guarantees that the terminal owner will observe it. If terminal work is
// ready, this Close owner claims collection and delays result publication until
// its callbacks have been accounted for.
func (state *instanceCloseState) finishLogicalCloseCollectionHandoff(result error, enabled bool, terminalResult func() error) (error, bool) {
	state.terminalCollectionMu.Lock()
	defer state.terminalCollectionMu.Unlock()
	ready := state.terminalComplete.Load()
	if ready && terminalResult != nil {
		result = joinPrimary(result, terminalResult())
	}
	required := enabled || state.terminalCollectionDeferred
	if required && ready && !state.terminalCollectionClaimed {
		state.terminalCollectionClaimed = true
		state.terminalCollectionDeferred = false
		return result, true
	}
	state.complete(result)
	return result, false
}

func (state *instanceCloseState) completeLogicalCloseCollection(result error) {
	state.terminalCollectionMu.Lock()
	state.complete(result)
	state.terminalCollectionMu.Unlock()
}

// publishTerminalCollectionReady is the other half of the handoff. Publishing
// readiness and observing completed share one mutex with the Close owner, so
// the crossing cannot leave both sides believing the other will collect.
func (state *instanceCloseState) publishTerminalCollectionReady() bool {
	state.terminalCollectionMu.Lock()
	defer state.terminalCollectionMu.Unlock()
	state.terminalComplete.Store(true)
	if !state.completed.Load() || state.terminalCollectionClaimed {
		if !state.completed.Load() {
			state.terminalCollectionDeferred = true
		}
		return false
	}
	state.terminalCollectionClaimed = true
	return true
}

func (state *instanceCloseState) signalQuiesced() {
	if state.quiesced != nil {
		state.quiescedOnce.Do(func() { close(state.quiesced) })
	}
}

func (state *instanceCloseState) complete(result error) {
	state.result = result
	state.completed.Store(true)
	state.doneMu.Lock()
	if state.done != nil {
		close(state.done)
	}
	state.doneMu.Unlock()
}

func (state *instanceCloseState) doneChannel() <-chan struct{} {
	if state.completed.Load() {
		return completedInstanceClose
	}
	state.doneMu.Lock()
	defer state.doneMu.Unlock()
	if state.completed.Load() {
		return completedInstanceClose
	}
	if state.done == nil {
		state.done = make(chan struct{})
	}
	return state.done
}

func (state *instanceCloseState) signalTerminalDone() {
	state.terminalComplete.Store(true)
	if state.terminalDone != nil {
		close(state.terminalDone)
	}
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
func (in *Instance) releaseResources(cleanup *resourceCleanup) {
	if codeProfileEnabled && in.lifecycleProfile() != nil {
		span := in.beginProfileLifecycle("physical-release")
		defer finishProfileBoundary(span, nil)
	}
	if state := in.closeState.Load(); state != nil && state.interruptStop != nil {
		state.interruptStop()
		state.interruptStop = nil
	}
	// Every imported raw-pointer dependency remains attached until physical
	// release. A table/global/token or downstream function importer may keep this
	// instance's native code and context callable after logical Close.
	cleanup.run(func() { railshotReleaseNativeTail(in) })
	cleanup.run(func() { detachImportedFunctions(in) })
	cleanup.run(func() { detachImportedHostFuncRefs(in) })
	cleanup.run(func() { detachImportedGlobals(in) })
	cleanup.run(func() { railshotReleaseInstanceGlobalReferences(in) })
	cleanup.run(func() { detachImportedTables(in) })
	cleanup.run(func() { detachImportedTags(in) })
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
	// These containers consume their root units before producer callbacks run.
	// A nested physical callback must not interrupt our remaining native teardown.
	for table := in.table; table != nil; table = table.next {
		cleanup.run(table.releaseRetainedInstances)
	}
	// The architecture holder survives native descriptor teardown. Its sweep
	// retries retired/Publishing rows no longer present in a live retained map.
	cleanup.run(func() { railshotReleaseOwnedTableDescriptorDependencies(in) })
	for _, global := range in.globalCells {
		cleanup.run(func() { global.instanceOwnerClosed(in) })
	}
	// The architecture holder survives globalCells being cleared below. Its
	// sweep retries retired/Publishing rows that are no longer in a live map.
	cleanup.run(func() { railshotReleaseOwnedGlobalDescriptorDependencies(in) })
	unregisterHostControl(in)
	if in.thunkMem != nil {
		_ = in.c.releaseOwnedCodeMapping(in.thunkMem)
		in.thunkMem = nil
	}
	in.c.releaseCode()
	runtime.ReleaseArena(in.ar)
	managedMemories := railshotHasInstanceMemoryDependencies(in)
	var detachedMemories importDedup[*Memory]
	if in.memoryDir != nil {
		for i := len(in.memoryDir.memories) - 1; i >= 1; i-- {
			memory := in.memoryDir.memories[i]
			if memory == nil {
				continue
			}
			if i < len(in.memoryDir.owns) && in.memoryDir.owns[i] {
				cleanup.run(func() {
					memoryJM := memory.jobMemory()
					memory.ownerClosed()
					runtime.ReleaseJobMemory(memoryJM)
				})
			} else if !managedMemories && detachedMemories.add(memory) && !in.ownsTransferredMemoryAttachment(memory) {
				cleanup.run(memory.detachImporter)
			}
		}
	}
	if in.c.threadedMemory0() {
		cleanup.run(func() { runtime.ReleaseJobMemory(in.jm) })
		if !managedMemories && in.memory != nil && detachedMemories.add(in.memory) && !in.ownsTransferredMemoryAttachment(in.memory) {
			cleanup.run(in.memory.detachImporter)
		}
	} else if in.ownsMem {
		cleanup.run(func() {
			if in.memory != nil {
				in.memory.ownerClosed()
			}
			runtime.ReleaseJobMemory(in.jm)
		})
	} else if !managedMemories && in.memory != nil && detachedMemories.add(in.memory) && !in.ownsTransferredMemoryAttachment(in.memory) {
		cleanup.run(in.memory.detachImporter)
	}
	if managedMemories {
		cleanup.run(func() { railshotDrainInstanceMemoryDependencies(in) })
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
	if state := in.importState.Load(); state != nil {
		state.importRefEntries = nil
		state.globalRefEntries = nil
	}
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
