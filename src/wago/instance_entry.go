package wago

import (
	"context"
	"encoding/binary"
	"sync"
)

// invocationGate has a zero-value, allocation-free uncontended path. Waiters
// share a notification channel, not an invocation slot. Cancellation never
// changes the owner or interrupts the active invocation.
type invocationGate struct {
	mu      sync.Mutex
	held    bool
	changed chan struct{}
}

func (g *invocationGate) Lock() { _ = g.lockContext(nil) }

func (g *invocationGate) lockContext(ctx context.Context) error {
	g.mu.Lock()
	for {
		// Cancellation wins if it is visible at the admission decision, including
		// when both the notification and Done channels became ready.
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				g.mu.Unlock()
				return err
			}
		}
		if !g.held {
			g.held = true
			g.mu.Unlock()
			return nil
		}
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		changed := g.changed
		g.mu.Unlock()
		if ctx == nil {
			<-changed
		} else {
			select {
			case <-changed:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		g.mu.Lock()
	}
}

func (g *invocationGate) Unlock() {
	g.mu.Lock()
	if !g.held {
		g.mu.Unlock()
		panic("unlock of unlocked invocation gate")
	}
	g.held = false
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
	g.mu.Unlock()
}

func (in *Instance) lockInvocationContext(ctx context.Context, id invocationID) (*instancePluginState, error) {
	state := in.ensurePluginState()
	if err := state.invokeMu.lockContext(ctx); err != nil {
		return nil, err
	}
	if id == 0 {
		id = newInvocationID()
	}
	state.invocationID = id
	return state, nil
}

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
