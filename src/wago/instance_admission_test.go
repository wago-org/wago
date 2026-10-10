package wago

import (
	"sync"
	"testing"
	"time"
)

func TestOrdinaryCombinedAdmissionRelease(t *testing.T) {
	in := &Instance{}
	state := in.ensurePluginState()
	if !in.tryBeginOrdinaryInvocation(state) {
		t.Fatal("ordinary combined admission failed")
	}
	if in.invocationState.Load() != 1 || state.invokeMu.state.Load() != invocationGateHeld {
		t.Fatal("ordinary combined admission did not publish both owners")
	}
	in.endOrdinaryInvocation(state)
	if in.invocationState.word.Load() != 0 {
		t.Fatal("ordinary combined admission retained an owner")
	}
}

func TestOrdinaryCombinedAdmissionRejectsRuntimeOwnedInstance(t *testing.T) {
	in := &Instance{rt: new(Runtime)}
	if in.tryBeginOrdinaryInvocation(in.ensurePluginState()) {
		t.Fatal("ordinary combined admission bypassed Runtime operation accounting")
	}
	if in.invocationState.word.Load() != 0 {
		t.Fatal("rejected Runtime admission changed instance ownership")
	}
}

func TestOrdinaryCombinedAdmissionDoesNotBlockRevocation(t *testing.T) {
	in := &Instance{}
	state := in.ensurePluginState()
	if !in.tryBeginOrdinaryInvocation(state) {
		t.Fatal("ordinary combined admission failed")
	}
	done := make(chan struct{})
	go func() {
		state.invokeMu.revokeFast()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ordinary owner blocked callback resource publication")
	}
	if got := state.invokeMu.state.Load(); got != invocationGateHeld|invocationGateRevoked {
		t.Fatalf("revoked ordinary gate = %d, want held|revoked", got)
	}
	in.endOrdinaryInvocation(state)
	if in.invocationState.Load() != 0 || state.invokeMu.state.Load() != invocationGateRevoked {
		t.Fatal("ordinary release did not preserve permanent revocation")
	}
}

func TestOrdinaryCombinedAdmissionReleaseHandsOffWaiter(t *testing.T) {
	in := &Instance{}
	state := in.ensurePluginState()
	gate := &state.invokeMu
	if !in.tryBeginOrdinaryInvocation(state) {
		t.Fatal("ordinary combined admission failed")
	}
	if err := in.beginInstanceInvocation(); err != nil {
		t.Fatal(err)
	}
	waiter := &invocationGateWaiter{ready: make(chan struct{}), queued: true}
	gate.slow = &invocationGateSlowState{head: waiter, tail: waiter}
	gate.state.Store(invocationGateHeld | invocationGateWaiters)
	in.endOrdinaryInvocation(state)
	select {
	case <-waiter.ready:
	default:
		t.Fatal("waiting invocation was not notified")
	}
	if in.invocationState.Load() != 1 || !gate.tryAcquireHandoff() {
		t.Fatal("handoff did not preserve the waiting lifetime lease")
	}
	gate.Unlock()
	in.endInvocation()
	if in.invocationState.word.Load() != 0 {
		t.Fatal("handoff retained admission ownership")
	}
}

func TestOrdinaryCombinedAdmissionCloseKeepsResourcesUntilRelease(t *testing.T) {
	in, _ := narrowPreparedFixture(t)
	state := in.ensurePluginState()
	if !in.tryBeginOrdinaryInvocation(state) {
		t.Fatal("ordinary combined admission rejected private instance")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if in.resourcesClosed || in.invocationState.Load() != instanceInvocationClosed|1 {
		t.Fatal("close released an admitted ordinary owner")
	}
	in.endOrdinaryInvocation(state)
	if !in.resourcesClosed || in.invocationState.Load() != instanceInvocationClosed {
		t.Fatal("last ordinary owner did not finalize closed resources")
	}
}

func TestSharedAdmissionViewsPreserveOtherHalf(t *testing.T) {
	in := &Instance{}
	gate := &in.ensurePluginState().invokeMu
	in.invocationState.word.Store(instanceFastAdmission)
	if in.invocationState.Load() != 1 || gate.state.Load() != invocationGateHeld|invocationGateFast {
		t.Fatal("combined admission did not publish both owners")
	}
	if !in.invocationState.CompareAndSwap(1, 2) || gate.state.Load() != invocationGateHeld|invocationGateFast {
		t.Fatal("waiting invocation changed gate ownership")
	}
	const revoked = invocationGateHeld | invocationGateFast | invocationGateRevoked | invocationGateWaiters
	if !gate.state.CompareAndSwap(invocationGateHeld|invocationGateFast, revoked) || in.invocationState.Load() != 2 {
		t.Fatal("revocation changed lifetime ownership")
	}
	in.invocationState.Store(instanceInvocationClosed | 2)
	if gate.state.Load() != revoked {
		t.Fatal("close publication changed gate state")
	}
	gate.state.Store(invocationGateRevoked)
	if in.invocationState.Load() != instanceInvocationClosed|2 {
		t.Fatal("gate release erased close or waiting leases")
	}
}

func TestSharedAdmissionConcurrentHalves(t *testing.T) {
	in := &Instance{}
	gate := &in.ensurePluginState().invokeMu
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			if !in.invocationState.CompareAndSwap(0, 1) || !in.invocationState.CompareAndSwap(1, 0) {
				t.Error("gate mutation interfered with lifetime CAS")
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			if !gate.state.CompareAndSwap(0, invocationGateHeld) || !gate.state.CompareAndSwap(invocationGateHeld, 0) {
				t.Error("lifetime mutation interfered with gate CAS")
				return
			}
		}
	}()
	wg.Wait()
	if in.invocationState.word.Load() != 0 {
		t.Fatal("shared admission retained an owner")
	}
}

func TestCombinedAdmissionReleaseHandsOffWaitingLifetime(t *testing.T) {
	in := &Instance{}
	state := in.ensurePluginState()
	gate := &state.invokeMu
	if !in.invocationState.word.CompareAndSwap(0, instanceFastAdmission) {
		t.Fatal("admission failed")
	}
	if err := in.beginInstanceInvocation(); err != nil {
		t.Fatal(err)
	}
	waiter := &invocationGateWaiter{ready: make(chan struct{}), queued: true}
	gate.slow = &invocationGateSlowState{head: waiter, tail: waiter}
	gate.state.Store(invocationGateHeld | invocationGateFast | invocationGateWaiters)
	in.endFastInvocation(state)
	select {
	case <-waiter.ready:
	default:
		t.Fatal("waiting invocation was not notified")
	}
	if in.invocationState.Load() != 1 || !gate.tryAcquireHandoff() {
		t.Fatal("handoff did not preserve the waiting lifetime lease")
	}
	gate.Unlock()
	in.endInvocation()
	if in.invocationState.word.Load() != 0 {
		t.Fatal("handoff retained admission ownership")
	}
}

func TestCombinedAdmissionCloseKeepsResourcesUntilRelease(t *testing.T) {
	in, _ := narrowPreparedFixture(t)
	fn, err := in.WasmFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.tryBeginFastInvocation() {
		t.Fatal("combined admission rejected private instance")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if in.resourcesClosed || in.invocationState.Load() != instanceInvocationClosed|1 {
		t.Fatal("close released an admitted native owner")
	}
	in.endFastInvocation(in.pluginState.Load())
	if !in.resourcesClosed || in.invocationState.Load() != instanceInvocationClosed {
		t.Fatal("last combined owner did not finalize closed resources")
	}
}
