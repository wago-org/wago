package wago

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

var errAtomicWaitInstanceClosed = errors.New("wago: instance closed while waiting")

type atomicWaitHelperError struct{ err error }

type atomicWaitInvocationContext struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

var atomicWaitInvocationContexts sync.Map // *Instance -> *atomicWaitInvocationContext

func (in *Instance) publishAtomicWaitContext(parent context.Context) func() {
	if in == nil || in.c == nil || !in.c.usesAtomicWaitHelpers() {
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
		atomicWaitInvocationContexts.CompareAndDelete(in, state)
		cancel(context.Canceled)
	}
}

func cancelAtomicWaitContext(in *Instance) {
	if state, ok := atomicWaitInvocationContexts.Load(in); ok {
		state.(*atomicWaitInvocationContext).cancel(errAtomicWaitInstanceClosed)
	}
}

func (in *Instance) dispatchAtomicWaitHelper(helper uint32, args, results []uint64) {
	if in == nil || in.memory == nil {
		panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper has no shared memory")})
	}
	if len(results) < 1 {
		panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper %d has no result slot", helper)})
	}
	ctx := context.Background()
	if state, ok := atomicWaitInvocationContexts.Load(in); ok {
		ctx = state.(*atomicWaitInvocationContext).ctx
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
		result, err = in.memory.notify(uint64(uint32(args[0]))+args[2], uint32(args[1]))
	case shared.AtomicHelperWait32:
		if len(args) != 4 {
			err = fmt.Errorf("atomic wait32 helper arity = %d, want 4", len(args))
			break
		}
		result, err = in.memory.wait32(ctx, uint64(uint32(args[0]))+args[3], uint32(args[1]), int64(args[2]))
	case shared.AtomicHelperWait64:
		if len(args) != 4 {
			err = fmt.Errorf("atomic wait64 helper arity = %d, want 4", len(args))
			break
		}
		result, err = in.memory.wait64(ctx, uint64(uint32(args[0]))+args[3], args[1], int64(args[2]))
	default:
		err = fmt.Errorf("unknown atomic wait helper %d", helper)
	}
	if err != nil {
		panic(atomicWaitHelperError{err: err})
	}
	results[0] = uint64(result)
}
