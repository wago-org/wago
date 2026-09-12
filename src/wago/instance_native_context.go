package wago

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
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

const (
	executionFlagIndependent uint32 = 1 << iota
	executionFlagNativeControlShared
	executionFlagImportedGCDomain
	executionFlagDynamicGCDomain
	executionFlagStoreOwnedGCCollector
	executionFlagPreparedActive
)

type invocationID uint64

var nextInvocationID atomic.Uint64

func newInvocationID() invocationID {
	for {
		if id := invocationID(nextInvocationID.Add(1)); id != 0 {
			return id
		}
	}
}

// These counts authorize parked callbacks, never native execution. The inline
// entry covers repeated and recursive callbacks in one chain. Overflow entries
// live only as long as overlapping distinct chains; empty maps are released.
type instanceActivations struct {
	mu            sync.Mutex
	id            invocationID
	count         uint64
	other         map[invocationID]uint64
	reservationID invocationID
	reservation   *pluginOperationReservation
	reservations  map[invocationID]*pluginOperationReservation
}

func markNativeActiveID(in *Instance, id invocationID) {
	markNativeActiveState(in.ensurePluginState(), id)
}

func markNativeActiveState(state *instancePluginState, id invocationID) {
	a := &state.activations
	a.mu.Lock()
	defer a.mu.Unlock()
	if (a.count == 0 && a.other[id] == 0) || (a.count != 0 && a.id == id) {
		if a.count == ^uint64(0) {
			panic(invalidHostReference{err: fmt.Errorf("native activation count exhausted")})
		}
		a.id = id
		a.count++
		return
	}
	if a.other == nil {
		a.other = make(map[invocationID]uint64)
	}
	if a.other[id] == ^uint64(0) {
		panic(invalidHostReference{err: fmt.Errorf("native activation count exhausted")})
	}
	a.other[id]++
}

func unmarkNativeActiveID(in *Instance, id invocationID) {
	unmarkNativeActiveState(in.ensurePluginState(), id)
}

func unmarkNativeActiveState(state *instancePluginState, id invocationID) {
	a := &state.activations
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.count != 0 && a.id == id {
		a.count--
		return
	}
	if a.other[id] <= 1 {
		delete(a.other, id)
		if len(a.other) == 0 {
			a.other = nil
		}
	} else {
		a.other[id]--
	}
}

func isNativeActive(in *Instance, id invocationID) bool {
	if in == nil || id == 0 {
		return false
	}
	state := in.pluginState.Load()
	if state == nil {
		return false
	}
	a := &state.activations
	a.mu.Lock()
	active := (a.id == id && a.count != 0) || a.other[id] != 0
	a.mu.Unlock()
	return active
}

func (in *Instance) swapInvocationReservation(next *pluginOperationReservation) *pluginOperationReservation {
	if in == nil {
		return nil
	}
	id := in.currentInvocationID()
	if id == 0 {
		return nil
	}
	a := &in.ensurePluginState().activations
	a.mu.Lock()
	if (a.reservation != nil && a.reservationID == id) || (a.reservation == nil && a.reservations[id] == nil) {
		previous := a.reservation
		a.reservationID, a.reservation = id, next
		a.mu.Unlock()
		return previous
	}
	previous := a.reservations[id]
	if next == nil {
		delete(a.reservations, id)
		if len(a.reservations) == 0 {
			a.reservations = nil
		}
	} else {
		if a.reservations == nil {
			a.reservations = make(map[invocationID]*pluginOperationReservation)
		}
		a.reservations[id] = next
	}
	a.mu.Unlock()
	return previous
}

func currentInvocationReservation(in *Instance) *pluginOperationReservation {
	if in == nil {
		return nil
	}
	id := in.currentInvocationID()
	if id == 0 {
		return nil
	}
	a := &in.ensurePluginState().activations
	a.mu.Lock()
	var reservation *pluginOperationReservation
	// The inline capability belongs only to this exact invocation. A different
	// active chain must still resolve its own fallback entry.
	if a.reservationID == id && a.reservation != nil {
		reservation = a.reservation
	} else {
		reservation = a.reservations[id]
	}
	a.mu.Unlock()
	return reservation
}

type executionLease struct{ local *sync.Mutex }

// beginNativeEntry acquires the serialized execution lease and rebinds this
// instance's pointer context before native code can observe basedata. Memory
// size/growth fields remain backing-owned; invocation control is refreshed by
// the engine entry/resume paths.
func (in *Instance) beginNativeEntry() (executionLease, error) {
	if in.usesIndependentExecution() {
		mu := in.independentNativeExecutionMu()
		mu.Lock()
		if err := in.bindAndValidateNativeContext(); err != nil {
			mu.Unlock()

			return executionLease{}, err
		}

		return executionLease{local: mu}, nil
	}
	if in.c.threadedMemory0() {
		mu := &in.memoryDir.nativeMu
		mu.Lock()
		if err := in.bindAndValidateNativeContext(); err != nil {
			mu.Unlock()
			return executionLease{}, err
		}
		return executionLease{local: mu}, nil
	}
	nativeExecutionMu.Lock()
	nativeExecutionEpoch++
	if err := in.bindAndValidateNativeContext(); err != nil {
		nativeExecutionMu.Unlock()
		return executionLease{}, err
	}
	return executionLease{}, nil
}

func (in *Instance) bindAndValidateNativeContext() error {
	if err := in.bindNativeContext(); err != nil {
		return err
	}
	return validateNativeGCEntry(in)
}

func (in *Instance) bindNativeContext() error {
	in.invalidateNativeContext()
	ctx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), wruntime.InstanceContextBytes)
	in.jm.BindInstanceContextBytes(ctx)
	primary := in.jm.LinMemBase()
	in.jm.SetGuardOwner(primary)
	return in.refreshMemoryDirectory()
}

// A saturated version permanently disables reuse. Nested entries and guarded
// host access must invalidate before changing any pointer/control state.
func (in *Instance) invalidateNativeContext() {
	v := &in.ensurePluginState().nativeContextVersion
	for {
		previous := v.Load()
		if previous == ^uint64(0) || v.CompareAndSwap(previous, previous+1) {
			return
		}
	}
}

func (in *Instance) canReuseParkedNativeContext(version uint64) bool {
	return in.canReuseParkedNativeContextWithState(version, in.ensurePluginState())
}

func (in *Instance) canReuseParkedNativeContextWithState(version uint64, state *instancePluginState) bool {
	// Independent admission excludes imported resources. Exporting a resource
	// or native function revokes it. GC and threaded memory remain conservative:
	// their shared owners can change native state outside this instance's entry.
	return version != ^uint64(0) && in.usesIndependentExecution() &&
		in.gc == nil && !in.c.threadedMemory0() &&
		state.nativeContextVersion.Load() == version
}

// refreshMemoryDirectory rebinds the instance-owned indexed-memory directory
// and synchronizes every entry while the process-wide native execution lease is
// held. This makes shared memory tenants safe without copying the whole basedata.
func (in *Instance) refreshMemoryDirectory() error {
	dir := in.memoryDir
	if dir == nil {
		return nil
	}
	if len(dir.native) < len(dir.memories)*abi.MemoryDirEntryBytes {
		return fmt.Errorf("indexed memory directory is truncated")
	}
	for i, memory := range dir.memories {
		if memory == nil {
			return fmt.Errorf("indexed memory %d is unavailable", i)
		}
		jm := memory.jobMemory()
		if jm == nil {
			return fmt.Errorf("indexed memory %d owner is closed", i)
		}
		entry := dir.native[i*abi.MemoryDirEntryBytes:]
		if !in.c.threadedMemory0() {
			jm.SetGuardOwner(in.jm.LinMemBase())
		}
		pages := jm.CurrentPages()
		binary.LittleEndian.PutUint64(entry[abi.MemoryDirBaseOffset:], uint64(jm.LinMemBase()))
		binary.LittleEndian.PutUint64(entry[abi.MemoryDirCurrentBytesOffset:], uint64(pages)<<16)
		binary.LittleEndian.PutUint32(entry[abi.MemoryDirCurrentPagesOffset:], pages)
	}
	in.jm.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir.native[0])))
	return nil
}

func (l executionLease) unlockExecution() {
	if l.local != nil {
		l.local.Unlock()
		return
	}
	nativeExecutionMu.Unlock()
}

func (in *Instance) independentNativeExecutionMu() *sync.Mutex {
	if in.memoryDir != nil {
		return &in.memoryDir.nativeMu
	}

	return &in.ensurePluginState().nativeExecutionMu
}

func (in *Instance) usesIndependentExecution() bool {
	if in == nil {
		return false
	}
	flags := in.executionFlags.Load()

	return flags&executionFlagIndependent != 0 && flags&executionFlagNativeControlShared == 0
}

func (in *Instance) markNativeControlShared() {
	in.ensurePluginState().invokeMu.revokeFast()
	for {
		flags := in.executionFlags.Load()
		if flags&executionFlagNativeControlShared != 0 ||
			in.executionFlags.CompareAndSwap(flags, flags|executionFlagNativeControlShared) {
			break
		}
	}
	// Publishing the shared bit prevents new specialized entries. Do not return
	// a shareable resource until the previous fast activation has left native
	// code. The gate notification closes the check/wait race without spinning.
	if in.executionFlags.Load()&executionFlagPreparedActive != 0 {
		gate := &in.ensurePluginState().invokeMu
		gate.mu.Lock()
		for in.executionFlags.Load()&executionFlagPreparedActive != 0 {
			if gate.changed == nil {
				gate.changed = make(chan struct{})
			}
			changed := gate.changed
			gate.mu.Unlock()
			<-changed
			gate.mu.Lock()
		}
		gate.mu.Unlock()
	}
}

func (in *Instance) nativeControlIsShared() bool {
	return in != nil && in.executionFlags.Load()&executionFlagNativeControlShared != 0
}

func (in *Instance) lockThreadedInstanceState() *sync.Mutex {
	if in == nil || in.c == nil || !in.c.threadedMemory0() {
		return nil
	}
	in.memoryDir.invokeMu.Lock()
	return &in.memoryDir.invokeMu
}

func (in *Instance) lockInstanceNativeStateForHostAccess() func() {
	if in.usesIndependentExecution() {
		mu := in.independentNativeExecutionMu()
		mu.Lock()
		in.invalidateNativeContext()

		return mu.Unlock
	}
	if in != nil && in.c != nil && in.c.threadedMemory0() {
		in.memoryDir.nativeMu.Lock()
		in.invalidateNativeContext()
		return in.memoryDir.nativeMu.Unlock
	}
	unlock := lockNativeExecutionForHostAccess()
	if in != nil {
		in.invalidateNativeContext()
	}
	return unlock
}

// lockNativeExecutionForHostAccess serializes direct host access to native-visible
// global cells with guest execution without rebinding any instance context. Host
// callbacks may call this safely because synchronous dispatch releases the native
// execution lease before arbitrary Go code runs. Lock order while this guard is
// held is nativeExecutionMu -> globalOwner.mu -> referenceStore.mu -> Instance.lifeMu;
// no container lock may be held while acquiring the guard.
func lockNativeExecutionForHostAccess() func() {
	nativeExecutionMu.Lock()
	return nativeExecutionMu.Unlock
}

func (in *Instance) callNativeAsync(entry uintptr, prepared bool) error {
	return in.callNativeAsyncWithTrap(entry, prepared, in.trap)
}

// callNativeAsyncWithTrap enters this instance while preserving an outer
// caller's trap cell across a Go-level re-export delegation.
func (in *Instance) callNativeAsyncWithTrap(entry uintptr, prepared bool, activeTrap []byte) error {
	locked, err := in.beginNativeEntry()
	if err != nil {
		return err
	}
	defer locked.unlockExecution()
	if prepared {
		if err := refreshNativeControl(true, in.eng, in.jm, activeTrap); err != nil {
			return err
		}
		return in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results))
	}
	return in.decorateTrap(callNative(in.c, in.eng, in.jm, true, entry, in.serArgs, activeTrap, in.results))
}

type preparedEntryMode uint8

const (
	preparedEntryGeneral preparedEntryMode = iota
	preparedEntryPrivate
	preparedEntryIsolated
)

func (in *Instance) preparedEntryMode() preparedEntryMode {
	return in.preparedEntryModeFor(false)
}

// preparedMemoryFreeEntryMode is reserved for entries whose compiler metadata
// proves they cannot access linear memory. Signal-backed bounds checks do not
// participate in such an entry, so they need not force the general guarded
// adapter. All ownership, table, GC, import, and shared-control exclusions stay
// identical to ordinary prepared entry.
func (in *Instance) preparedMemoryFreeEntryMode() preparedEntryMode {
	return in.preparedEntryModeFor(true)
}

func (in *Instance) preparedEntryModeFor(memoryFree bool) preparedEntryMode {
	if in == nil || in.c == nil || (!memoryFree && in.c.boundsMode == BoundsChecksSignalsBased) ||
		in.memoryDir != nil || in.nativeControlIsShared() || in.syncMode {
		return preparedEntryGeneral
	}
	if in.memory != nil {
		if !in.ownsMem {
			return preparedEntryGeneral
		}
		_, shared := in.memory.importShape()
		if shared {
			return preparedEntryGeneral
		}
	}
	privateRefStore := in.refStore == nil || in.refStore.private
	isolatedTables := privateRefStore && in.tableDescPtr != 0 && in.c.preparedIsolatedTables &&
		!in.c.needsFuncRefContextHeader
	if len(in.globalCells) == 0 && (in.tableDescPtr == 0 || isolatedTables) && in.gc == nil &&
		in.c.NumImports == 0 && (!in.c.needsFuncRefContext() || isolatedTables) {
		return preparedEntryIsolated
	}
	return preparedEntryPrivate
}

func (in *Instance) preparedPrivateEligible() bool {
	return in.preparedEntryMode() != preparedEntryGeneral
}

// preparedIsolatedEligible identifies instances whose native execution has no
// process-visible state that direct host access or another instance can observe.
// PreparedFunction already forbids concurrent calls on one Instance; each such
// instance owns its Engine, stack, trap cell, argument/result buffers, and memory.
func (in *Instance) preparedIsolatedEligible() bool {
	return in.preparedEntryMode() == preparedEntryIsolated
}

// Shared control requires native locking/rebinding. Imported, dynamic, and
// store-owned GC domains require general GC admission, even for numeric exports.
// Non-private domains are registered before public entry. Late boundary stores
// are private; an isolated module has no collector or imports to attach there.
// Resource sharing revokes direct gate admission before setting the shared bit.
const preparedFastBlocked = executionFlagNativeControlShared | executionFlagImportedGCDomain | executionFlagDynamicGCDomain | executionFlagStoreOwnedGCCollector

func (in *Instance) preparedFastStateValid() bool {
	return in.executionFlags.Load()&preparedFastBlocked == 0
}

// Reservation and ownership revocation use the same atomic word. Thus a
// revoker either prevents entry or waits for its reservation to finish. The
// invocation gate permits only one fast activation per instance. Private paths
// acquire the global lease first, since a revoker can already own that lease.
func (in *Instance) lockPreparedFastState() bool {
	const blocked = preparedFastBlocked | executionFlagPreparedActive
	for {
		flags := in.executionFlags.Load()
		if flags&blocked != 0 {
			return false
		}
		if in.executionFlags.CompareAndSwap(flags, flags|executionFlagPreparedActive) {
			return true
		}
	}
}

func (in *Instance) unlockPreparedFastState() {
	for {
		flags := in.executionFlags.Load()
		if !in.executionFlags.CompareAndSwap(flags, flags&^executionFlagPreparedActive) {
			continue
		}
		if flags&executionFlagNativeControlShared != 0 {
			gate := &in.ensurePluginState().invokeMu
			gate.mu.Lock()
			if gate.changed != nil {
				close(gate.changed)
				gate.changed = nil
			}
			gate.mu.Unlock()
		}
		return
	}
}

func (in *Instance) callPreparedPrivate(entry uintptr, activeTrap []byte) error {
	nativeExecutionMu.Lock()
	if !in.lockPreparedFastState() {
		nativeExecutionMu.Unlock()
		return in.callNativeAsyncWithTrap(entry, true, activeTrap)
	}
	defer in.unlockPreparedFastState()
	nativeExecutionEpoch++
	defer nativeExecutionMu.Unlock()
	if err := validateNativeGCEntry(in); err != nil {
		return err
	}
	if err := refreshNativeControl(true, in.eng, in.jm, activeTrap); err != nil {
		return err
	}
	return in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results))
}

func (in *Instance) callPreparedIsolated(entry uintptr, activeTrap []byte) error {
	if !in.lockPreparedFastState() {
		return in.callNativeAsyncWithTrap(entry, true, activeTrap)
	}
	defer in.unlockPreparedFastState()
	if err := refreshNativeControl(true, in.eng, in.jm, activeTrap); err != nil {
		return err
	}
	return in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results))
}

// tryPreparedDirect reserves both invocation ownership and private entry. The
// caller holds a lifetime lease and has the compiler's direct-entry proof plus
// the immutable isolated module shape: no imports/host calls, collector, global
// cells, external memory, or externally reachable function context. A scalar
// signature alone is not sufficient. Non-private GC domains are registered
// during construction; late private stores cannot add GC to this module shape.
// Resource export revokes this gate before publishing shared ownership. Direct
// memory-free code uses its own engine and cannot need native context rebinding.
func (in *Instance) tryPreparedDirect() bool {
	gate := &in.ensurePluginState().invokeMu
	if !gate.state.CompareAndSwap(0, invocationGateHeld|invocationGateFast) {
		return false
	}
	if !in.preparedFastStateValid() {
		gate.Unlock()
		return false
	}
	return true
}
