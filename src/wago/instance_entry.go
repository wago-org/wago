package wago

import (
	"context"
	"encoding/binary"
)

// lockInvocation serializes a complete public call, including parked callbacks.
// A zero identity requests a fresh chain; host reentry supplies its existing ID.
// Before native entry, the caller must also hold an invocation or construction
// lifetime lease.
func (in *Instance) lockInvocation(id invocationID) *instancePluginState {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	if id == 0 {
		id = newInvocationID()
	}
	state.invocationID = id
	return state
}

func (state *instancePluginState) unlockInvocation() {
	state.invocationID = 0
	state.invokeMu.Unlock()
}

// invokeVoidEntry runs a table dispatcher or local start under the same native,
// collector, callback, and cancellation machinery as ordinary invocation. The
// caller owns serialization, identity, lifetime, and any argument slots.
func (in *Instance) invokeVoidEntry(ctx context.Context, entry uintptr, reservation *pluginOperationReservation) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	gcLease := in.lockGCInvocation(in.currentInvocationID())
	defer func() {
		gcLease.unlock()
		if in.importsFuncrefStorage() || in.table != nil {
			in.reconcileFuncrefRoots()
		}
	}()
	if reservation != nil {
		previous := in.swapInvocationReservation(reservation)
		defer in.swapInvocationReservation(previous)
	}
	if mu := in.lockThreadedInstanceState(); mu != nil {
		defer mu.Unlock()
	}
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return err
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if ctx == nil || ctx.Done() == nil {
		// Keep non-cancelable entries free of context watcher allocations.
		if err := in.callVoidNative(entry); err != nil {
			return err
		}
		return in.reconcileGCGlobalRoots()
	}
	stopCancel, err := in.startCancellationWatch(ctx, in.trap)
	if err != nil {
		return err
	}
	defer stopCancel()
	if in.syncMode {
		err = in.callNativeSyncWithTrapContext(entry, in.trap, ctx)
	} else {
		err = in.callVoidNative(entry)
	}
	if err != nil {
		return contextInterruptError(ctx, err)
	}
	return in.reconcileGCGlobalRoots()
}

func (in *Instance) callVoidNative(entry uintptr) error {
	if in.syncMode {
		return in.callNativeSync(entry)
	}
	if err := in.callNativeAsync(entry, false); err != nil {
		return err
	}
	return in.replayHostLog()
}
