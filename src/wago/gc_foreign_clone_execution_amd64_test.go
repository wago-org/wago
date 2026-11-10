//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// These fixtures deliberately stay outside the Runtime's shared collector
// topology. Its collector mutex is not a substitute for either local native
// execution or the complete invocation lease.
func newForeignCloneExecutionInstance(t *testing.T, threaded bool) *Instance {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithIndependentInstanceExecution(true)
	var in *Instance
	if threaded {
		cfg = cfg.WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
		memory, err := NewSharedMemory(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = memory.Close() })
		rt := NewRuntime(WithRuntimeConfig(cfg))
		t.Cleanup(func() { _ = rt.Close() })
		compiled, err := rt.Compile(gcAtomicWaitReferenceResultModule())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = compiled.Close() })
		in, err = rt.Instantiate(context.Background(), compiled,
			WithGC(GCConfig{VerifyAfterCollect: true}), WithImports(testImports("env.memory", memory)))
		if err != nil {
			t.Fatal(err)
		}
	} else {
		compiled, err := Compile(cfg, gcReferenceTokenProducerModule())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = compiled.Close() })
		in, err = Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = in.Close() })
	if !threaded {
		// Free Instantiate attaches its private reference store lazily at the
		// first public reference boundary. Warm it through the ordinary API.
		values, err := in.Invoke("new")
		if err != nil || len(values) != 1 || values[0] == 0 {
			t.Fatalf("private fixture warm-up = %v, %v", values, err)
		}
		if err := in.ReleaseGCRef(GCRef{token: values[0]}); err != nil {
			t.Fatal(err)
		}
	}
	domain := in.gcInvocationDomain()
	if domain == nil || !domain.private || domain.collector != in.gc || in.refStore.ownsGCCollector(in.gc) {
		t.Fatal("fixture must have a private GC invocation domain outside the shared collector topology")
	}
	if threaded {
		if !in.threadedMemoryZero || in.memoryDir == nil {
			t.Fatal("fixture did not select threaded instance-local native execution")
		}
	} else if !in.usesIndependentExecution() {
		t.Fatal("fixture did not select independent native execution")
	}
	return in
}

func foreignCloneExecutionNativeMu(in *Instance) *sync.Mutex {
	if in.usesIndependentExecution() {
		return in.independentNativeExecutionMu()
	}
	return &in.memoryDir.nativeMu
}

func foreignCloneExecutionToken(t *testing.T, source *Instance) GCRef {
	t.Helper()
	values, err := source.Invoke("new")
	if err != nil || len(values) != 1 || values[0] == 0 {
		t.Fatalf("source new = %v, %v; want one GC token", values, err)
	}
	token := GCRef{token: values[0]}
	t.Cleanup(func() { _ = source.ReleaseGCRef(token) })
	return token
}

func checkForeignCloneExecutionToken(t *testing.T, target *Instance, token GCRef) {
	t.Helper()
	if token.IsNull() {
		t.Fatal("clone returned a null token")
	}
	t.Cleanup(func() { _ = target.ReleaseGCRef(token) })
	if err := target.CollectGC(); err != nil {
		t.Fatal(err)
	}
	target.refStore.mu.Lock()
	entry, ok := target.refStore.gcByToken[token.token]
	target.refStore.mu.Unlock()
	if !ok || entry.owner != target {
		t.Fatal("clone token has the wrong owner")
	}
	value, err := target.gc.StructGet(target.gc.GlobalSlot(entry.slot), 0)
	if err != nil || value.Bits != 1 {
		t.Fatalf("cloned field after collection = %v, %v; want 1", value, err)
	}
}

// Use an observable queued waiter, rather than a sleep, for complete-invocation
// tests. An old implementation instead finishes without ever joining the gate.
func foreignCloneExecutionWaitForGate(t *testing.T, in *Instance, done <-chan error) bool {
	t.Helper()
	gate := &in.gcInvocationDomain().invocationMu
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			t.Errorf("clone bypassed the complete GC invocation lease: %v", err)
			return true
		default:
		}
		if gate.state.Load()&invocationGateWaiters != 0 {
			return false
		}
		if time.Now().After(deadline) {
			t.Error("clone neither queued on the GC invocation lease nor completed")
			return false
		}
		runtime.Gosched()
	}
}

func foreignCloneExecutionJoin(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not resume after execution admission was released")
	}
}

func TestForeignRuntimeGCCloneWaitsForCompleteInvocation(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		for _, side := range []string{"source", "target"} {
			t.Run(fmt.Sprintf("threaded=%t/%s", threaded, side), func(t *testing.T) {
				source := newForeignCloneExecutionInstance(t, threaded)
				target := newForeignCloneExecutionInstance(t, threaded)
				token := foreignCloneExecutionToken(t, source)
				blocked := source
				if side == "target" {
					blocked = target
				}
				lease := blocked.lockGCInvocation(newInvocationID())
				var cloned GCRef
				done := make(chan error, 1)
				go func() {
					var err error
					cloned, err = target.CloneGCRefFrom(source, token)
					done <- err
				}()
				completed := foreignCloneExecutionWaitForGate(t, blocked, done)
				// Capture must release the source before waiting for the target.
				// Otherwise simultaneous opposite-direction clones can deadlock.
				if side == "target" {
					domain := source.gcInvocationDomain()
					domain.invocationState.Lock()
					owner := domain.invocationOwner
					domain.invocationState.Unlock()
					if owner != 0 {
						t.Error("clone retained source invocation admission while waiting for target")
					}
				}
				lease.unlock()
				if !completed {
					foreignCloneExecutionJoin(t, done)
				}
				checkForeignCloneExecutionToken(t, target, cloned)
			})
		}
	}
}

func TestForeignRuntimeGCCloneHelpersUseSelectedNativeGuard(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		for _, phase := range []string{"capture", "restore", "cleanup"} {
			t.Run(fmt.Sprintf("threaded=%t/%s", threaded, phase), func(t *testing.T) {
				source := newForeignCloneExecutionInstance(t, threaded)
				target := newForeignCloneExecutionInstance(t, threaded)
				token := foreignCloneExecutionToken(t, source)
				objects, root, err := captureForeignGCGraph(source, token.token, target)
				if err != nil {
					t.Fatal(err)
				}
				withTargetInvocation := func(operation func() error) error {
					lease := target.lockGCInvocation(newInvocationID())
					defer lease.unlock()
					return operation()
				}
				restore := func() error {
					return withTargetInvocation(func() error {
						_, _, err := restoreForeignGCGraph(target, objects, root)
						return err
					})
				}
				cleanup := func() error {
					return withTargetInvocation(func() error { clearForeignCloneRoot(target, true); return nil })
				}
				blocked := target
				var operation func() error
				switch phase {
				case "capture":
					blocked = source
					operation = func() error {
						_, _, err := captureForeignGCGraph(source, token.token, target)
						return err
					}
				case "restore":
					operation = restore
				case "cleanup":
					if err := restore(); err != nil {
						t.Fatal(err)
					}
					operation = cleanup
				}
				mu := foreignCloneExecutionNativeMu(blocked)
				mu.Lock()
				started := make(chan struct{})
				done := make(chan error, 1)
				go func() { close(started); done <- operation() }()
				<-started
				completed := false
				select {
				case err := <-done:
					completed = true
					t.Errorf("%s bypassed the instance-selected native guard: %v", phase, err)
				case <-time.After(50 * time.Millisecond):
				}
				mu.Unlock()
				if !completed {
					foreignCloneExecutionJoin(t, done)
				}
				_ = cleanup()
			})
		}
	}
}

func TestForeignRuntimeGCCloneConcurrentGuestInvocation(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		for _, side := range []string{"source", "target"} {
			t.Run(fmt.Sprintf("threaded=%t/%s", threaded, side), func(t *testing.T) {
				source := newForeignCloneExecutionInstance(t, threaded)
				target := newForeignCloneExecutionInstance(t, threaded)
				token := foreignCloneExecutionToken(t, source)
				guest := source
				if side == "target" {
					guest = target
				}
				// Park a real invocation after GC admission but before native entry.
				// Even the broken clone can finish safely here: the guest cannot
				// allocate or collect until we have observed the clone's outcome.
				mu := foreignCloneExecutionNativeMu(guest)
				mu.Lock()
				guestDone := make(chan error, 1)
				var guestValues []uint64
				go func() {
					var err error
					guestValues, err = guest.Invoke("new")
					guestDone <- err
				}()
				domain := guest.gcInvocationDomain()
				deadline := time.Now().Add(5 * time.Second)
				for {
					domain.invocationState.Lock()
					owner := domain.invocationOwner
					domain.invocationState.Unlock()
					if owner != 0 {
						break
					}
					if time.Now().After(deadline) {
						mu.Unlock()
						foreignCloneExecutionJoin(t, guestDone)
						t.Fatal("guest invocation did not claim GC admission")
					}
					runtime.Gosched()
				}
				var cloned GCRef
				cloneDone := make(chan error, 1)
				go func() {
					var err error
					cloned, err = target.CloneGCRefFrom(source, token)
					cloneDone <- err
				}()
				completed := foreignCloneExecutionWaitForGate(t, guest, cloneDone)
				mu.Unlock()
				foreignCloneExecutionJoin(t, guestDone)
				if !completed {
					foreignCloneExecutionJoin(t, cloneDone)
				}
				if len(guestValues) != 1 || guestValues[0] == 0 {
					t.Fatalf("concurrent guest new = %v; want one GC token", guestValues)
				}
				checkForeignCloneExecutionToken(t, guest, GCRef{token: guestValues[0]})
				checkForeignCloneExecutionToken(t, target, cloned)
			})
		}
	}
}
