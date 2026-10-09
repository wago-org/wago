package wago

import (
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
)

// invocationGate has a zero-value, allocation-free uncontended path. Contended
// callers queue for wake-one notification; cancellation removes its waiter in
// constant time and never changes the active owner's lifetime.
type invocationGate struct {
	state atomic.Uint32
	mu    sync.Mutex
	slow  *invocationGateSlowState
}

type invocationGateSlowState struct {
	head              *invocationGateWaiter
	tail              *invocationGateWaiter
	changed           chan struct{} // revocation observers only
	revocationWaiters bool
}

type invocationGateWaiter struct {
	ready    chan struct{}
	previous *invocationGateWaiter
	next     *invocationGateWaiter
	queued   bool
	granted  bool
}

const (
	invocationGateHeld    = uint32(1)
	invocationGateWaiters = uint32(2)
	invocationGateFast    = uint32(4)
	invocationGateRevoked = uint32(8)
	invocationGateHandoff = uint32(16)
)

func (g *invocationGate) Lock() {
	if g.state.CompareAndSwap(0, invocationGateHeld) {
		return
	}
	g.lockAfterFastAttempt()
}

// Keep the ordinary CAS path inline without repeating it for a revoked gate.
//
//go:noinline
func (g *invocationGate) lockAfterFastAttempt() {
	if g.state.Load() == invocationGateRevoked && g.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) {
		return
	}
	_ = g.lockContext(nil)
}

func (g *invocationGate) lockContext(ctx context.Context) error {
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if g.state.CompareAndSwap(0, invocationGateHeld) || g.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) || g.tryAcquireHandoff() {
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
		state := g.state.Load()
		slow := g.slow
		if state&invocationGateHeld == 0 && (slow == nil || slow.head == nil) {
			// The owner released before this caller joined the queue. Claim the
			// slot under mu so a later waiter cannot overtake it.
			if !g.state.CompareAndSwap(state, (state&invocationGateRevoked)|invocationGateHeld) {
				g.mu.Unlock()
				continue
			}
			g.mu.Unlock()
			if ctx != nil {
				if err := ctx.Err(); err != nil {
					g.Unlock()
					return err
				}
			}
			return nil
		}
		waiter := g.enqueueWaiterLocked(state)
		if waiter == nil {
			g.mu.Unlock()
			continue
		}
		g.mu.Unlock()

		if ctx == nil {
			<-waiter.ready
		} else {
			select {
			case <-waiter.ready:
			case <-ctx.Done():
				g.mu.Lock()
				if waiter.queued {
					g.removeWaiterLocked(waiter)
					g.updateWaiterBitLocked()
					g.mu.Unlock()
					return ctx.Err()
				}
				granted := waiter.granted
				g.mu.Unlock()
				if granted && g.tryAcquireHandoff() {
					g.Unlock()
				}
				return ctx.Err()
			}
		}
		// Notification transfers the obligation to acquire or pass on the gate.
		// Another caller may claim this handoff before the notified waiter runs.
		if !g.tryAcquireHandoff() {
			continue
		}
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				g.Unlock()
				return err
			}
		}
		return nil
	}
}

// Handoff is a reserved ownership obligation carried by a notified caller.
// A new caller may claim it before that caller runs, avoiding a scheduler round
// trip for every short critical section. Claiming clears Handoff atomically;
// the successful caller alone may enter, and must eventually Unlock. A notified
// caller that loses the claim retries admission or, when canceled, returns.
func (g *invocationGate) tryAcquireHandoff() bool {
	for {
		state := g.state.Load()
		if state&invocationGateHandoff == 0 {
			return false
		}
		if g.state.CompareAndSwap(state, state&^invocationGateHandoff) {
			return true
		}
	}
}

// enqueueWaiterLocked commits registration only while the observed owner still
// owns the gate. If its atomic release wins, the caller must retry acquisition.
// Once Waiters is published, release must take mu and will see the queued node.
// The caller holds mu throughout publication and insertion.
func (g *invocationGate) enqueueWaiterLocked(observed uint32) *invocationGateWaiter {
	if observed&invocationGateHeld == 0 || !g.state.CompareAndSwap(observed, observed|invocationGateWaiters) {
		return nil
	}
	slow := g.slowStateLocked()
	waiter := &invocationGateWaiter{ready: make(chan struct{}), queued: true, previous: slow.tail}
	if slow.tail == nil {
		slow.head = waiter
	} else {
		slow.tail.next = waiter
	}
	slow.tail = waiter
	return waiter
}

func (g *invocationGate) slowStateLocked() *invocationGateSlowState {
	if g.slow == nil {
		g.slow = new(invocationGateSlowState)
	}
	return g.slow
}

func (g *invocationGate) updateWaiterBitLocked() {
	slow := g.slow
	for {
		state := g.state.Load()
		next := state &^ invocationGateWaiters
		if slow != nil && (slow.head != nil || slow.revocationWaiters) {
			next |= invocationGateWaiters
		}
		if g.state.CompareAndSwap(state, next) {
			return
		}
	}
}

func (g *invocationGate) removeWaiterLocked(waiter *invocationGateWaiter) {
	slow := g.slow
	if slow == nil || !waiter.queued {
		return
	}
	if waiter.previous == nil {
		slow.head = waiter.next
	} else {
		waiter.previous.next = waiter.next
	}
	if waiter.next == nil {
		slow.tail = waiter.previous
	} else {
		waiter.next.previous = waiter.previous
	}
	waiter.previous, waiter.next = nil, nil
	waiter.queued = false
}

// Unlock notifies the oldest waiter and reserves a handoff obligation. The
// notified caller or a new caller atomically claims it before entering. Keeping
// Held set until the claim prevents an ownerless sleeping queue, while allowing
// barging avoids forcing a scheduler round trip for every contended call.
func (g *invocationGate) Unlock() {
	// Both ordinary and direct owners release atomically when no queue or
	// revocation observer needs a handoff. Registration validates this same word.
	state := g.state.Load()
	if state&invocationGateHeld != 0 && state&invocationGateWaiters == 0 &&
		g.state.CompareAndSwap(state, state&invocationGateRevoked) {
		return
	}
	g.mu.Lock()
	previous := g.state.Load()
	if previous&invocationGateHeld == 0 {
		g.mu.Unlock()
		panic("unlock of unlocked invocation gate")
	}
	wasFast := previous&invocationGateFast != 0
	slow := g.slow
	notifyRevocation := wasFast && slow != nil && slow.revocationWaiters
	var revocationChanged chan struct{}
	if notifyRevocation {
		slow.revocationWaiters = false
		revocationChanged = slow.changed
		slow.changed = nil
	}
	var waiter *invocationGateWaiter
	if slow != nil {
		waiter = slow.head
	}
	if waiter != nil {
		g.removeWaiterLocked(waiter)
		waiter.granted = true
		next := previous&invocationGateRevoked | invocationGateHeld | invocationGateHandoff
		if slow.head != nil || slow.revocationWaiters {
			next |= invocationGateWaiters
		}
		g.state.Store(next)
		close(waiter.ready)
	} else {
		g.state.Store(previous & invocationGateRevoked)
	}
	g.updateWaiterBitLocked()
	if revocationChanged != nil {
		close(revocationChanged)
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
			g.updateWaiterBitLocked()
			g.mu.Unlock()
			return
		}
		slow := g.slowStateLocked()
		slow.revocationWaiters = true
		if slow.changed == nil {
			slow.changed = make(chan struct{})
		}
		changed := slow.changed
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
	gcLease, err := in.lockGCInvocationContext(ctx, in.currentInvocationID())
	if err != nil {
		return err
	}
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
		var err error
		if codeProfileEnabled && in.lifecycleProfile() != nil && in.syncMode {
			err = in.callNativeSyncWithTrapContext(entry, in.trap, ctx)
		} else {
			err = in.callVoidNative(entry)
		}
		if err != nil {
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
	return contextInterruptError(ctx, in.reconcileGCGlobalRoots())
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
