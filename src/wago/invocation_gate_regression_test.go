package wago

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestInvocationGateReleaseBeforeWaiterPublication(t *testing.T) {
	for _, owner := range []uint32{invocationGateHeld, invocationGateHeld | invocationGateFast, invocationGateHeld | invocationGateRevoked} {
		t.Run(fmt.Sprint(owner), func(t *testing.T) {
			var gate invocationGate
			gate.state.Store(owner)
			gate.mu.Lock()
			observed := gate.state.Load()
			// Force the owner to release after the waiter observes Held but before
			// registration. This release must work even while the waiter holds mu.
			released := make(chan struct{})
			go func() { gate.Unlock(); close(released) }()
			select {
			case <-released:
			case <-time.After(time.Second):
				gate.mu.Unlock()
				<-released
				t.Fatal("uncontended release entered the queue mutex")
			}
			waiter := gate.enqueueWaiterLocked(observed)
			gate.mu.Unlock()
			if waiter != nil {
				t.Fatal("registered against an owner that already released")
			}
			// The failed registration takes the same acquisition retry as lockContext.
			done := make(chan struct{})
			go func() {
				gate.Lock()
				//lint:ignore SA2001 Acquiring and releasing proves the gate still makes progress.
				gate.Unlock()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("retry did not acquire ownerless gate")
			}
		})
	}
}

func TestInvocationGateQueuedCancellationRacesRelease(t *testing.T) {
	for n := 0; n < 100; n++ {
		var gate invocationGate
		gate.Lock()
		base, cancel := context.WithCancel(context.Background())
		ctx := &admissionContext{Context: base, waiting: make(chan struct{})}
		done := make(chan error, 1)
		go func() {
			err := gate.lockContext(ctx)
			if err == nil {
				gate.Unlock()
			}
			done <- err
		}()
		<-ctx.waiting
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); <-start; cancel() }()
		go func() { defer workers.Done(); <-start; gate.Unlock() }()
		close(start)
		workers.Wait()
		select {
		case err := <-done:
			if err != nil && err != context.Canceled {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("handoff/cancellation lost ownership")
		}
		gate.Lock()
		//lint:ignore SA2001 Acquiring and releasing proves the gate still makes progress.
		gate.Unlock()
		if got := gate.state.Load(); got != 0 {
			t.Fatalf("residual state: %d", got)
		}
	}
}

func TestInvocationGateFIFORepeatedHandoff(t *testing.T) {
	var gate invocationGate
	gate.Lock()
	const count = 32
	done := make(chan int, count)
	release := make(chan struct{})
	for i := 0; i < count; i++ {
		ctx := &admissionContext{Context: context.Background(), waiting: make(chan struct{})}
		go func(i int) {
			if err := gate.lockContext(ctx); err != nil {
				panic(err)
			}
			done <- i
			<-release
			gate.Unlock()
		}(i)
		<-ctx.waiting
	}
	gate.Unlock()
	for i := 0; i < count; i++ {
		select {
		case got := <-done:
			if got != i {
				t.Fatalf("handoff %d reached waiter %d", i, got)
			}
		case <-time.After(time.Second):
			t.Fatal("queued waiter made no progress")
		}
		release <- struct{}{}
	}
	gate.Lock()
	//lint:ignore SA2001 Acquiring and releasing proves the final handoff completes.
	gate.Unlock()
}

func TestInvocationGateBargingPreservesNotifiedWaiterProgress(t *testing.T) {
	var gate invocationGate
	gate.Lock()
	gate.mu.Lock()
	first := gate.enqueueWaiterLocked(gate.state.Load())
	second := gate.enqueueWaiterLocked(gate.state.Load())
	gate.mu.Unlock()
	gate.Unlock()
	<-first.ready
	// The first notified caller has not run yet. A new caller may claim its
	// reserved handoff, but cannot remove the second caller's notification.
	if !gate.tryAcquireHandoff() {
		t.Fatal("new caller failed to claim handoff")
	}
	if gate.tryAcquireHandoff() {
		t.Fatal("handoff admitted two owners")
	}
	select {
	case <-second.ready:
		t.Fatal("second waiter woke before the owner released")
	default:
	}
	gate.Unlock()
	<-second.ready
	if !gate.tryAcquireHandoff() {
		t.Fatal("second waiter failed to claim handoff")
	}
	gate.Unlock()
	// The first caller lost its claim and retries instead of assuming ownership.
	done := make(chan struct{})
	go func() {
		gate.Lock()
		//lint:ignore SA2001 Reacquisition proves a displaced notification still makes progress.
		gate.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("displaced waiter made no progress")
	}
}

func TestInvocationGateRevokedUncontended(t *testing.T) {
	for _, ordinary := range []bool{true, false} {
		var gate invocationGate
		gate.state.Store(invocationGateRevoked)
		for i := 0; i < 32; i++ {
			if ordinary {
				gate.Lock()
			} else {
				//lint:ignore SA1012 Explicitly exercise the internal nil-context admission path.
				if err := gate.lockContext(nil); err != nil {
					t.Fatal(err)
				}
			}
			if got := gate.state.Load(); got != invocationGateHeld|invocationGateRevoked {
				t.Fatalf("acquired state %d, want held and revoked", got)
			}
			gate.Unlock()
			if got := gate.state.Load(); got != invocationGateRevoked {
				t.Fatalf("released state %d, want revoked", got)
			}
		}
	}
}
