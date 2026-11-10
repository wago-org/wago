//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64ManagedFunctionReclaimsAfterHostReentry(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	identity := rv64TableStageCompile(t, "managed-function-host-reentry-identity", rv64NativeTokenModule(false, true, false, false), false)
	defer identity.Close()
	nested := rv64NativeTokenInstantiate(t, rt, identity, NewImports())
	producer := rv64NativeTokenInstantiate(t, rt, identity, NewImports())
	code := rv64TableStageCompile(t, "managed-function-host-reentry-root", rv64LocalMutableReferenceImportModule(false), false)
	defer code.Close()
	var root *Instance
	in, _ := ownedExceptionTestInstance(t, gc.ProfileTiny, nil)
	in.refStore = rt.refStore
	state := in.managedFunctionCollector(nil)
	callback := CallerHostCallFunc(func(caller Caller, _ HostCall) {
		if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
			panic(err)
		}
		got, err := nested.InvokeFromHost(context.Background(), caller, "identity", 37)
		if err != nil || len(got) != 1 || got[0] != 37 {
			panic(fmt.Sprintf("nested invoke=%v %v", got, err))
		}
		if !producer.hasPhysicalResources() || state.graph.pendingReclaims.Load() != 1 {
			panic("nested host reentry drained the outer journal")
		}
	})
	root = rv64NativeTokenInstantiate(t, rt, code, NewImports().Function("env", "target", callback))
	if _, err := state.newManagedFunctionEnv(producer, gc.EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	called := 0
	producer.referenceLifetime().afterPhysicalRelease(func() {
		called++
		got, err := root.Invoke("check")
		if err != nil || len(got) != 1 || got[0] != 1 {
			panic(fmt.Sprintf("physical callback root reentry=%v %v", got, err))
		}
	})
	producer.Close()
	got, err := root.Invoke("saved")
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("outer invoke=%v %v", got, err)
	}
	if called != 1 || producer.hasPhysicalResources() || state.graph.pendingReclaims.Load() != 0 {
		t.Fatal("outer host return did not finish the physical journal")
	}
}

func TestRV64ManagedFunctionReclaimsAfterOutermostGate(t *testing.T) {
	for _, panicCallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic%t", panicCallback), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c := rv64TableStageCompile(t, "managed-function-postgate", rv64NativeTokenModule(false, true, false, false), false)
			defer c.Close()
			root := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			nested := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			producer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			in, _ := ownedExceptionTestInstance(t, gc.ProfileTiny, nil)
			in.refStore = rt.refStore
			state := in.managedFunctionCollector(nil)
			if _, err := state.newManagedFunctionEnv(producer, gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			called := 0
			producer.referenceLifetime().afterPhysicalRelease(func() {
				called++
				got, err := root.Invoke("identity", 37)
				if err != nil || len(got) != 1 || got[0] != 37 {
					panic(fmt.Sprintf("post-gate callback reentry=%v %v", got, err))
				}
				if panicCallback {
					panic("managed-function-test-finalizer")
				}
			})
			producer.Close()
			id := newInvocationID()
			rootGate, nestedGate := root.lockInvocation(id), nested.lockInvocation(id)
			if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if state.graph.pendingReclaims.Load() != 1 {
				t.Fatal("reclaim journal not queued")
			}
			state.graph.drainAfterGate(id)
			if called != 0 || !producer.hasPhysicalResources() {
				t.Fatal("reclaimed while outer invocation gate was held")
			}
			rootGate.unlockInvocation()
			if called != 0 || !producer.hasPhysicalResources() {
				t.Fatal("nested same-ID gate did not retain journal")
			}
			var recovered any
			func() { defer func() { recovered = recover() }(); nestedGate.unlockInvocation() }()
			if panicCallback && recovered != "managed-function-test-finalizer" || !panicCallback && recovered != nil {
				t.Fatalf("finalizer panic=%v", recovered)
			}
			if called != 1 || producer.hasPhysicalResources() || state.graph.pendingReclaims.Load() != 0 || state.graph.drainingReclaims {
				t.Fatal("outer return did not finish exactly one physical journal")
			}
			if !rootGate.invokeMu.state.CompareAndSwap(0, invocationGateHeld) {
				t.Fatal("callback left public gate held")
			}
			rootGate.invokeMu.Unlock()
			// Explicit collection/close uses the same post-lock handoff. An empty
			// repeated drain neither repeats callbacks nor acquires additional roots.
			drainManagedFunctionStoreReclaims(rt.refStore)
			in.gc.Close()
			if _, ok := managedFunctionInvocationStates.Load(rootGate); ok {
				t.Fatal("retired graph retained invocation-state registry")
			}
		})
	}
}

func TestRV64ManagedFunctionInvocationHookNoGraphAllocations(t *testing.T) {
	in := &Instance{}
	state := &instancePluginState{}
	if allocs := testing.AllocsPerRun(1000, func() {
		managedFunctionBindInvocationState(in, state)
		managedFunctionAfterInvocationUnlock(state, 1)
	}); allocs != 0 {
		t.Fatalf("no-graph invocation hooks allocated %v", allocs)
	}
}

func TestRV64ManagedFunctionFinalizationAfterTopologyReader(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	code := rv64TableStageCompile(t, "managed-function-reader-finalization", rv64NativeTokenModule(false, true, false, false), false)
	defer code.Close()
	caller := rv64NativeTokenInstantiate(t, rt, code, NewImports())
	producer := rv64NativeTokenInstantiate(t, rt, code, NewImports())
	in, _ := ownedExceptionTestInstance(t, gc.ProfileTiny, nil)
	in.refStore = rt.refStore
	state := in.managedFunctionCollector(nil)
	env, err := state.newManagedFunctionEnv(producer, gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	slot := in.gc.NewGlobalSlot(env)
	producer.Close()
	if !producer.retainResourceRootForFinalization() {
		t.Fatal("closed producer's physical resources were not retained")
	}
	id := newInvocationID()
	gate := caller.lockInvocation(id)
	topology := rt.refStore.ensureGCTopology()
	topology.RLock()
	producer.releaseResourceRoot()
	if state.graph.pendingFinalizers.Load() != 1 || !producer.hasPhysicalResources() {
		t.Fatal("reader-bound release did not defer producer finalization")
	}
	topology.RUnlock()
	gate.unlockInvocation()
	if state.graph.pendingFinalizers.Load() != 0 || !producer.hasPhysicalResources() {
		t.Fatal("outer gate did not drain finalization while preserving the Env pin")
	}
	if err := in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
		t.Fatal(err)
	}
	if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	drainManagedFunctionReclaims(in.gc)
	if producer.hasPhysicalResources() {
		t.Fatal("dropped Env retained finalized producer")
	}
}
