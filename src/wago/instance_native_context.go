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
	nativeExecutionMu       sync.Mutex
	nativeExecutionEpoch    uint64 // guarded by nativeExecutionMu; advances on every public native entry
	nativeActiveMu          sync.Mutex
	nativeActive            = map[nativeActivation]uint32{}
	invocationReservationMu sync.Mutex
	invocationReservations  = map[nativeActivation]*pluginOperationReservation{}
)

const (
	executionFlagIndependent uint32 = 1 << iota
	executionFlagNativeControlShared
	executionFlagImportedGCDomain
	executionFlagDynamicGCDomain
	executionFlagStoreOwnedGCCollector
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

type nativeActivation struct {
	in *Instance
	id invocationID
}

func markNativeActiveID(in *Instance, id invocationID) {
	activation := nativeActivation{in: in, id: id}
	nativeActiveMu.Lock()
	nativeActive[activation]++
	nativeActiveMu.Unlock()
}

func unmarkNativeActiveID(in *Instance, id invocationID) {
	activation := nativeActivation{in: in, id: id}
	nativeActiveMu.Lock()
	if nativeActive[activation] <= 1 {
		delete(nativeActive, activation)
	} else {
		nativeActive[activation]--
	}
	nativeActiveMu.Unlock()
}

func isNativeActive(in *Instance, id invocationID) bool {
	nativeActiveMu.Lock()
	active := id != 0 && nativeActive[nativeActivation{in: in, id: id}] != 0
	nativeActiveMu.Unlock()
	return active
}

func (in *Instance) swapInvocationReservation(next *pluginOperationReservation) *pluginOperationReservation {
	if in == nil {
		return nil
	}
	activation := nativeActivation{in: in, id: in.currentInvocationID()}
	if activation.id == 0 {
		return nil
	}
	invocationReservationMu.Lock()
	previous := invocationReservations[activation]
	if next == nil {
		delete(invocationReservations, activation)
	} else {
		invocationReservations[activation] = next
	}
	invocationReservationMu.Unlock()
	return previous
}

func currentInvocationReservation(in *Instance) *pluginOperationReservation {
	if in == nil {
		return nil
	}
	activation := nativeActivation{in: in, id: in.currentInvocationID()}
	if activation.id == 0 {
		return nil
	}
	invocationReservationMu.Lock()
	reservation := invocationReservations[activation]
	invocationReservationMu.Unlock()
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
	ctx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), wruntime.InstanceContextBytes)
	in.jm.BindInstanceContextBytes(ctx)
	primary := in.jm.LinMemBase()
	in.jm.SetGuardOwner(primary)
	return in.refreshMemoryDirectory()
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
	for {
		flags := in.executionFlags.Load()
		if flags&executionFlagNativeControlShared != 0 ||
			in.executionFlags.CompareAndSwap(flags, flags|executionFlagNativeControlShared) {
			return
		}
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

		return mu.Unlock
	}
	if in != nil && in.c != nil && in.c.threadedMemory0() {
		in.memoryDir.nativeMu.Lock()
		return in.memoryDir.nativeMu.Unlock
	}
	return lockNativeExecutionForHostAccess()
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
	if in == nil || in.c == nil || in.c.boundsMode == BoundsChecksSignalsBased ||
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

func (in *Instance) callPreparedPrivate(entry uintptr, activeTrap []byte) error {
	nativeExecutionMu.Lock()
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
	if err := refreshNativeControl(true, in.eng, in.jm, activeTrap); err != nil {
		return err
	}
	return in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results))
}
