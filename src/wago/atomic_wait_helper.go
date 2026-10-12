package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var errAtomicWaitInstanceClosed = errors.New("wago: instance closed while waiting")

type atomicWaitHelperError struct{ err error }

type atomicWaitInvocationContext struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

var atomicWaitInvocationContexts sync.Map // *Instance -> *atomicWaitInvocationContext

func (in *Instance) needsAtomicWaitContext() bool {
	return in != nil && in.c != nil && (in.c.usesAtomicWaitHelpers() || in.executionFlags.Load()&executionFlagForeignNativeCalls != 0)
}

func (in *Instance) publishAtomicWaitContext(parent context.Context) func() {
	if in == nil || in.c == nil || !in.needsAtomicWaitContext() {
		return noOpCancellationWatch
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	state := &atomicWaitInvocationContext{ctx: ctx, cancel: cancel}
	atomicWaitInvocationContexts.Store(in, state)
	// Close can publish the invocation gate immediately before this entry is
	// installed. Checking after Store closes that missed-cancellation window;
	// the opposite ordering is caught by cancelAtomicWaitContext's map lookup.
	if in.isLogicallyClosed() {
		cancel(errAtomicWaitInstanceClosed)
	}
	return func() {
		// Threaded Invoke owns the instance invocation mutex until this cleanup,
		// so no later context for the same instance can be published concurrently.
		// Delete keeps this path compatible with TinyGo's sync.Map subset.
		atomicWaitInvocationContexts.Delete(in)
		cancel(context.Canceled)
	}
}

func cancelAtomicWaitContext(in *Instance) {
	if state, ok := atomicWaitInvocationContexts.Load(in); ok {
		state.(*atomicWaitInvocationContext).cancel(errAtomicWaitInstanceClosed)
	}
}

func (in *Instance) dispatchAtomicWaitHelper(helper uint32, args, results []uint64) {
	in.dispatchAtomicWaitHelperContext(helper, args, results, nil)
}

func (in *Instance) dispatchAtomicWaitHelperContext(helper uint32, args, results []uint64, ctx context.Context) {
	if in == nil || in.memory == nil {
		panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper has no shared memory")})
	}
	if len(results) < 1 {
		panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper %d has no result slot", helper)})
	}
	if helper == shared.AtomicHelperMemoryGrow {
		if len(args) != 1 || in.memoryDir == nil || len(in.memoryDir.native) < abi.MemoryDirEntryBytes {
			panic(atomicWaitHelperError{err: fmt.Errorf("shared memory.grow helper has invalid arguments or directory")})
		}
		maximum := binary.LittleEndian.Uint32(in.memoryDir.native[abi.MemoryDirPolicyMaxPagesOffset:])
		address64 := in.c.memoryDef(0).Addr64
		if address64 && args[0] > uint64(^uint32(0)) {
			results[0] = ^uint64(0)
			return
		}
		previous := in.memory.growShared(uint32(args[0]), maximum)
		results[0] = uint64(previous)
		if address64 && previous == ^uint32(0) {
			results[0] = ^uint64(0)
		}
		return
	}
	if ctx == nil {
		ctx = context.Background()
		if state, ok := atomicWaitInvocationContexts.Load(in); ok {
			ctx = state.(*atomicWaitInvocationContext).ctx
		}
	}
	var (
		result uint32
		err    error
	)
	switch helper {
	case shared.AtomicHelperNotify:
		if len(args) != 3 {
			err = fmt.Errorf("atomic notify helper arity = %d, want 3", len(args))
			break
		}
		result, err = in.memory.notify(in.atomicHelperAddress(args[0], args[2], 4), uint32(args[1]))
	case shared.AtomicHelperWait32:
		if len(args) != 4 {
			err = fmt.Errorf("atomic wait32 helper arity = %d, want 4", len(args))
			break
		}
		result, err = in.memory.wait32(ctx, in.atomicHelperAddress(args[0], args[3], 4), uint32(args[1]), int64(args[2]))
	case shared.AtomicHelperWait64:
		if len(args) != 4 {
			err = fmt.Errorf("atomic wait64 helper arity = %d, want 4", len(args))
			break
		}
		result, err = in.memory.wait64(ctx, in.atomicHelperAddress(args[0], args[3], 8), args[1], int64(args[2]))
	default:
		err = fmt.Errorf("unknown atomic wait helper %d", helper)
	}
	if err != nil {
		panic(atomicWaitHelperError{err: err})
	}
	results[0] = uint64(result)
}

func (in *Instance) atomicHelperAddress(address, offset, width uint64) uint64 {
	if in.c == nil || !in.c.memoryDef(0).Addr64 {
		address = uint64(uint32(address))
	}
	effective := address + offset
	if effective < address {
		// Preserve the alignment-before-bounds trap order even when addition
		// exceeds 64 bits. An aligned impossible address cannot wrap into backing.
		return (^uint64(0) &^ (width - 1)) | (effective & (width - 1))
	}
	return effective
}
