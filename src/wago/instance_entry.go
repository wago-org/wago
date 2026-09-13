package wago

import (
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
)

// invocationGate has a zero-value, allocation-free uncontended path. Waiters
// share a notification channel, not an invocation slot. Cancellation never
// changes the owner or interrupts the active invocation.
type invocationGate struct {
	state   atomic.Uint32
	mu      sync.Mutex
	changed chan struct{}
}

const (
	invocationGateHeld    = uint32(1)
	invocationGateWaiters = uint32(2)
	invocationGateFast    = uint32(4)
	invocationGateRevoked = uint32(8)
)

func (g *invocationGate) Lock() { _ = g.lockContext(nil) }

func (g *invocationGate) lockContext(ctx context.Context) error {
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if g.state.CompareAndSwap(0, invocationGateHeld) || g.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) {
			// Cancellation observed after acquisition wins; return the slot before
			// the caller publishes an identity or arms an interrupt watcher.
			if ctx != nil {
				if err := ctx.Err(); err != nil {
					g.Unlock()
					return err
				}
			}
			return nil
		}
		g.mu.Lock()
		registered := false
		for {
			state := g.state.Load()
			if state&invocationGateHeld == 0 {
				break
			}
			// Registration and release use the same atomic word. If release wins,
			// retry admission; otherwise Unlock must take mu and notify this waiter.
			if g.state.CompareAndSwap(state, state|invocationGateWaiters) {
				registered = true
				break
			}
		}
		if !registered {
			g.mu.Unlock()
			continue
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
	}
}

// Unlock preserves revocation and releases either kind of owner with one CAS.
// Sharing and waiter registration can force a retry and notification.
func (g *invocationGate) Unlock() {
	for {
		previous := g.state.Load()
		if previous&invocationGateHeld == 0 {
			panic("unlock of unlocked invocation gate")
		}
		if !g.state.CompareAndSwap(previous, previous&invocationGateRevoked) {
			continue
		}
		if previous&invocationGateWaiters != 0 {
			g.notify()
		}
		return
	}
}

func (g *invocationGate) notify() {
	g.mu.Lock()
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
	g.mu.Unlock()
}

// revokeFast orders resource publication against direct entry on the same word
// that owns ordinary invocation admission. Revocation is permanent. Ordinary
// invocations may still acquire the gate and use their native/context guards.
func (g *invocationGate) revokeFast() {
	g.mu.Lock()
	for {
		state := g.state.Load()
		next := state | invocationGateRevoked
		if state&invocationGateFast != 0 {
			next |= invocationGateWaiters
		}
		if !g.state.CompareAndSwap(state, next) {
			continue
		}
		if state&invocationGateFast == 0 {
			g.mu.Unlock()
			return
		}
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		changed := g.changed
		g.mu.Unlock()
		<-changed
		g.mu.Lock()
	}
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
