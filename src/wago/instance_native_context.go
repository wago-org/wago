package wago

import (
	"encoding/binary"
	"fmt"
	"sync"
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

type executionLease struct{}

// beginNativeEntry acquires the serialized execution lease and rebinds this
// instance's pointer context before native code can observe basedata. Memory
// size/growth fields remain backing-owned; invocation control is refreshed by
// the engine entry/resume paths.
func (in *Instance) beginNativeEntry() (*executionLease, error) {
	nativeExecutionMu.Lock()
	nativeExecutionEpoch++
	if err := in.bindNativeContext(); err != nil {
		nativeExecutionMu.Unlock()
		return nil, err
	}
	return &executionLease{}, nil
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
		jm.SetGuardOwner(in.jm.LinMemBase())
		pages := jm.CurrentPages()
		binary.LittleEndian.PutUint64(entry[abi.MemoryDirBaseOffset:], uint64(jm.LinMemBase()))
		binary.LittleEndian.PutUint64(entry[abi.MemoryDirCurrentBytesOffset:], uint64(pages)<<16)
		binary.LittleEndian.PutUint32(entry[abi.MemoryDirCurrentPagesOffset:], pages)
	}
	in.jm.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir.native[0])))
	return nil
}

func (*executionLease) unlockExecution() { nativeExecutionMu.Unlock() }

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

func (in *Instance) preparedPrivateEligible() bool {
	if in == nil || in.memoryDir != nil || in.nativeControlShared || in.syncMode {
		return false
	}
	if in.memory == nil {
		return true
	}
	if !in.ownsMem {
		return false
	}
	_, shared := in.memory.importShape()
	return !shared
}

func (in *Instance) callPreparedPrivate(entry uintptr, activeTrap []byte) error {
	nativeExecutionMu.Lock()
	nativeExecutionEpoch++
	defer nativeExecutionMu.Unlock()
	if err := refreshNativeControl(true, in.eng, in.jm, activeTrap); err != nil {
		return err
	}
	return in.decorateTrap(in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results))
}
