//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcLifecycleModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("read", 0, 2), wasmtest.ExportEntry("trap", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 0x16, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0, 0x0b}),
		)),
	)
}

func newGCLifecycleInstance(t *testing.T) (*Runtime, *Instance) {
	t.Helper()
	requireCompleteCore3Backend(t)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	t.Cleanup(func() { _ = rt.Close() })
	mod, err := rt.Compile(gcLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mod.Close() })
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return rt, in
}

func awaitGCLifecycle(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("state transition did not complete")
		}
		goruntime.Gosched()
	}
}

func TestCollectGCLifetimeReservation(t *testing.T) {
	_, in := newGCLifecycleInstance(t)
	var releases atomic.Int32
	in.referenceLifetime().afterPhysicalRelease(func() { releases.Add(1) })
	domain := in.gcInvocationDomain()
	// Collection claims the complete-call domain before taking the collector lock.
	domain.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- in.CollectGC() }()
	awaitGCLifecycle(t, func() bool {
		domain.invocationState.Lock()
		defer domain.invocationState.Unlock()
		return domain.invocationOwner != 0
	})
	if in.invocationState.Load()&instanceInvocationCount == 0 {
		domain.mu.Unlock()
		<-done
		t.Fatal("admitted collection has no instance lifetime reservation")
	}
	if err := in.Close(); err != nil {
		t.Error(err)
	}
	if state := in.referenceLifetime().snapshot(); !state.LogicallyClosed || !state.PhysicalResources || releases.Load() != 0 {
		t.Errorf("active collection close state = %+v, releases = %d", state, releases.Load())
	}
	domain.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if in.hasPhysicalResources() || releases.Load() != 1 {
		t.Fatal("collection did not release closed instance exactly once")
	}
	if err := in.CollectGC(); err == nil {
		t.Fatal("collection accepted a closed instance")
	}
	_ = in.Close()
	if releases.Load() != 1 {
		t.Fatal("duplicate physical release")
	}
}

func lifecycleCaller(t *testing.T, in *Instance, mode, export string) func(...uint64) ([]uint64, error) {
	t.Helper()
	if mode == "ordinary" {
		return func(args ...uint64) ([]uint64, error) { return in.Invoke(export, args...) }
	}
	fn, err := in.WasmFunc(export)
	if err != nil {
		t.Fatal(err)
	}
	return fn.Invoke
}

func TestWasmFuncGCGlobalMaintenance(t *testing.T) {
	for _, mode := range []string{"ordinary", "resolved"} {
		t.Run(mode, func(t *testing.T) {
			_, in := newGCLifecycleInstance(t)
			call := lifecycleCaller(t, in, mode, "set")
			for i := uint64(1); i <= 100; i++ {
				out, err := call(i)
				if err != nil || len(out) != 1 || out[0] != i {
					t.Fatalf("set = %v, %v", out, err)
				}
				// A different tenant can collect immediately after the complete-call lease.
				// Do not synchronize this instance's globals as CollectGC would do.
				lease := in.lockGCInvocation(newInvocationID())
				domain := in.lockGCCollector()
				err = in.gc.CollectFull(gc.EmptyRoots{})
				unlockGCCollector(domain)
				lease.unlock()
				if err != nil {
					t.Fatal(err)
				}
				if live := in.gc.Stats().LiveObjects; live != 1 {
					t.Fatalf("live global objects = %d, want 1", live)
				}
				ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], ValAnyRef)))
				if value, err := in.gc.StructGet(ref, 0); err != nil || uint64(value.I32()) != i {
					t.Fatalf("global object = %v, %v; want %d", value, err, i)
				}
			}
		})
	}
}

func TestGCAdmissionPublicCancellation(t *testing.T) {
	_, in := newGCLifecycleInstance(t)
	owner := newInvocationID()
	lease := in.lockGCInvocation(owner)
	released := false
	defer func() {
		if !released {
			lease.unlock()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "set", 42); done <- err }()
	domain := in.gcInvocationDomain()
	awaitGCLifecycle(t, func() bool { return domain.invocationMu.state.Load()&invocationGateWaiters != 0 })
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("admission error = %v", err)
		}
	case <-time.After(time.Second):
		lease.unlock()
		released = true
		<-done
		t.Fatal("canceled invocation waited for the GC domain owner")
	}
	if !in.ownsGCInvocation(owner) {
		t.Fatal("cancellation changed the original owner")
	}
	lease.unlock()
	released = true
	if _, err := in.Invoke("set", 42); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedGCResultAndTrapMaintenance(t *testing.T) {
	for _, mode := range []string{"ordinary", "resolved"} {
		t.Run(mode, func(t *testing.T) {
			_, in := newGCLifecycleInstance(t)
			if _, err := in.Invoke("set", 42); err != nil {
				t.Fatal(err)
			}
			var token uint64
			t.Run("result", func(t *testing.T) {
				out, err := lifecycleCaller(t, in, mode, "get")()
				if err != nil || len(out) != 1 || out[0]>>32 == 0 {
					t.Fatalf("result = %v, %v", out, err)
				}
				token = out[0]
			})
			if token == 0 {
				t.Fatal("no public token")
			}
			if _, err := in.Invoke("set", 99); err != nil {
				t.Fatal(err)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
			t.Run("read", func(t *testing.T) {
				call := lifecycleCaller(t, in, mode, "read")
				for i := 0; i < 100; i++ {
					out, err := call(token)
					if err != nil || len(out) != 1 || out[0] != 42 {
						t.Fatalf("read = %v, %v", out, err)
					}
				}
			})
			t.Run("trap", func(t *testing.T) {
				call := lifecycleCaller(t, in, mode, "trap")
				for i := 0; i < 10; i++ {
					if _, err := call(token); err == nil {
						t.Fatal("unreachable did not trap")
					}
					public := in.existingPublicGCState()
					if public.argumentRootCount != 0 {
						t.Fatal("trap retained temporary argument roots")
					}
					for i := uint32(0); i < public.argumentRootsMade; i++ {
						if !in.gc.GlobalSlot(public.argumentRootSlots[i]).IsNull() {
							t.Fatal("trap left an argument slot rooted")
						}
					}
					lease := in.lockGCInvocation(newInvocationID())
					lease.unlock()
				}
			})
			if in.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatal("call or session remained admitted")
			}
			if _, err := in.Invoke("read", token); err != nil {
				t.Fatalf("call after trap: %v", err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, token).GCRef()); err != nil {
				t.Fatal(err)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
			if live := in.gc.Stats().LiveObjects; live != 1 {
				t.Fatalf("released token retained %d objects, want global only", live)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if in.hasPhysicalResources() || in.hasResourceRoots() {
				t.Fatal("reference maintenance left instance retained")
			}
		})
	}
}

func TestCollectGCHostCallbackLifetime(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	mod, err := rt.Compile(gcHostImportLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	called := false
	in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.mark", slotHostFunc(func(h HostModule, _ []uint64, _ []uint64) {
		called = true
		if err := h.(GCHostModule).CollectGC(); err != nil {
			t.Error(err)
		}
	}))))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !called {
		t.Fatal("host callback did not run")
	}
}

// gcAdmissionDeadlineContext lets the test trigger deadline expiry at admission.
type gcAdmissionDeadlineContext struct{ context.Context }

func (c gcAdmissionDeadlineContext) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func TestGCAdmissionPublicDeadline(t *testing.T) {
	_, in := newGCLifecycleInstance(t)
	owner := newInvocationID()
	lease := in.lockGCInvocation(owner)
	released := false
	defer func() {
		if !released {
			lease.unlock()
		}
	}()
	base, cancel := context.WithCancel(context.Background())
	ctx := gcAdmissionDeadlineContext{Context: base}
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "set", 42); done <- err }()
	domain := in.gcInvocationDomain()
	awaitGCLifecycle(t, func() bool { return domain.invocationMu.state.Load()&invocationGateWaiters != 0 })
	cancel() // The controlled deadline expires after the waiter reaches admission.
	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Fatalf("deadline = %v", err)
		}
	case <-time.After(5 * time.Second):
		lease.unlock()
		released = true
		<-done
		t.Fatal("deadline waited for domain release")
	}
	if !in.ownsGCInvocation(owner) {
		t.Fatal("deadline changed the original owner")
	}
	lease.unlock()
	released = true
	if _, err := in.Invoke("set", 42); err != nil {
		t.Fatal(err)
	}
}

func TestGCAdmissionInstantiationCancellation(t *testing.T) {
	for _, phase := range []string{"collector", "start"} {
		t.Run(phase, func(t *testing.T) {
			requireCompleteCore3Backend(t)
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			mod, err := rt.Compile(gcHostImportLifecycleModule())
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			imports := testImports("env.mark", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
			first, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			owner := newInvocationID()
			var held gcInvocationLease
			if phase == "collector" {
				held = first.lockGCInvocation(owner)
			}
			done := make(chan error, 1)
			ready := make(chan struct{})
			var failed *Instance
			if phase == "start" {
				rt.hooks.afterCreate = append(rt.hooks.afterCreate, func(event InstantiationEvent) error {
					if failed == nil {
						failed = event.Instance.value
						held = first.lockGCInvocation(owner)
						close(ready)
					}
					return nil
				})
			}
			go func() {
				if phase == "collector" {
					close(ready)
				}
				in, err := rt.Instantiate(ctx, mod, WithImports(imports))
				if in != nil {
					_ = in.Close()
				}
				done <- err
			}()
			<-ready
			released := false
			defer func() {
				if !released {
					held.unlock()
				}
			}()
			domain := first.gcInvocationDomain()
			awaitGCLifecycle(t, func() bool { return domain.invocationMu.state.Load()&invocationGateWaiters != 0 })
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("instantiate cancellation = %v", err)
				}
			case <-time.After(5 * time.Second):
				held.unlock()
				released = true
				<-done
				t.Fatal("canceled instantiation waited for domain release")
			}
			if !first.ownsGCInvocation(owner) {
				t.Fatal("instantiation cancellation changed owner")
			}
			held.unlock()
			released = true
			if failed != nil && failed.hasPhysicalResources() {
				t.Fatal("failed start retained physical resources")
			}
			next, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
			if err != nil {
				t.Fatal(err)
			}
			_ = next.Close()
		})
	}
}

func TestPreparedFuncrefProducerMaintenance(t *testing.T) {
	for _, container := range []string{"table", "global"} {
		for _, mode := range []string{"ordinary", "resolved"} {
			t.Run(container+"/"+mode, func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				imports := testImports()
				var imp, set, clear []byte
				if container == "table" {
					table, err := NewTable(1, 1)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					imports["env.state"] = table
					imp = append(append(wasmtest.Name("env"), wasmtest.Name("state")...), 1, 0x70, 1, 1, 1)
					set = []byte{0x41, 0, 0xd2, 0, 0x26, 0, 0x0b}
					clear = []byte{0x41, 0, 0xd0, 0x70, 0x26, 0, 0x0b}
				} else {
					global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
					if err != nil {
						t.Fatal(err)
					}
					defer global.Close()
					imports["env.state"] = global
					imp = append(append(wasmtest.Name("env"), wasmtest.Name("state")...), 3, 0x70, 1)
					set = []byte{0xd2, 0, 0x24, 0, 0x0b}
					clear = []byte{0xd0, 0x70, 0x24, 0, 0x0b}
				}
				code := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
					wasmtest.Section(2, wasmtest.Vec(imp)),
					wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("clear", 0, 1))),
					wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(set), wasmtest.Code(clear))),
				)
				mod, err := rt.Compile(code)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				producer, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				consumer, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if _, err := producer.Invoke("set"); err != nil {
					t.Fatal(err)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if !producer.hasPhysicalResources() || !producer.hasResourceRoots() {
					t.Fatal("container did not retain the closed producer")
				}
				t.Run("overwrite", func(t *testing.T) {
					if _, err := lifecycleCaller(t, consumer, mode, "clear")(); err != nil {
						t.Fatal(err)
					}
					if producer.hasPhysicalResources() || producer.hasResourceRoots() {
						t.Fatal("overwrite retained the closed producer")
					}
				})
			})
		}
	}
}

func TestGCLifecycleIntegration(t *testing.T) {
	rt, preparedInstance := newGCLifecycleInstance(t)
	mod, err := rt.Compile(gcLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	waiter, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	fn, err := preparedInstance.WasmFunc("set")
	if err != nil {
		t.Fatal(err)
	}
	domain := preparedInstance.gcInvocationDomain()
	for round := 0; round < 8; round++ {
		for i := uint64(0); i < 16; i++ {
			if _, err := fn.Invoke(i); err != nil {
				t.Fatal(err)
			}
		}
		collecting, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		if collecting.gcInvocationDomain() != domain || waiter.gcInvocationDomain() != domain {
			t.Fatal("instances do not share the Runtime GC domain")
		}
		var releases atomic.Int32
		collecting.referenceLifetime().afterPhysicalRelease(func() { releases.Add(1) })
		domain.mu.Lock()
		collectionDone := make(chan error, 1)
		go func() { collectionDone <- collecting.CollectGC() }()
		var owner invocationID
		awaitGCLifecycle(t, func() bool {
			domain.invocationState.Lock()
			defer domain.invocationState.Unlock()
			owner = domain.invocationOwner
			return owner != 0
		})
		ctx, cancel := context.WithCancel(context.Background())
		invocationDone := make(chan error, 1)
		go func() { _, err := waiter.InvokeContext(ctx, "set", 42); invocationDone <- err }()
		awaitGCLifecycle(t, func() bool { return domain.invocationMu.state.Load()&invocationGateWaiters != 0 })
		cancel()
		if err := <-invocationDone; err != context.Canceled {
			t.Errorf("concurrent invocation = %v", err)
		}
		if !collecting.ownsGCInvocation(owner) {
			t.Error("waiter changed collection ownership")
		}
		if err := collecting.Close(); err != nil {
			t.Error(err)
		}
		if !collecting.hasPhysicalResources() || releases.Load() != 0 {
			t.Error("active collection released resources")
		}
		domain.mu.Unlock()
		if err := <-collectionDone; err != nil {
			t.Fatal(err)
		}
		if releases.Load() != 1 || collecting.hasPhysicalResources() {
			t.Fatal("collection did not finalize")
		}
		if _, err := session.Invoke(42); err != nil {
			t.Fatal(err)
		}
	}
	session.Close()
	get, err := preparedInstance.WasmFunc("get")
	if err != nil {
		t.Fatal(err)
	}
	out, err := get.Invoke()
	if err != nil || len(out) != 1 {
		t.Fatalf("get = %v, %v", out, err)
	}
	token := ValueOf(ValAnyRef, out[0]).GCRef()
	if err := preparedInstance.Close(); err != nil {
		t.Fatal(err)
	}
	if !preparedInstance.hasPhysicalResources() {
		t.Fatal("public token did not retain its producer")
	}
	if err := preparedInstance.ReleaseGCRef(token); err != nil {
		t.Fatal(err)
	}
	if err := waiter.Close(); err != nil {
		t.Fatal(err)
	}
	for _, in := range []*Instance{preparedInstance, waiter} {
		if in.hasPhysicalResources() || in.hasResourceRoots() || in.invocationState.Load()&instanceInvocationCount != 0 {
			t.Fatal("completed operations left lifetime state active")
		}
	}
	domain.invocationState.Lock()
	owner := domain.invocationOwner
	domain.invocationState.Unlock()
	if owner != 0 || domain.invocationMu.state.Load()&invocationGateHeld != 0 {
		t.Fatal("completed operations left GC ownership active")
	}
	rt.mu.Lock()
	active := rt.activeOperations
	rt.mu.Unlock()
	if active != 0 {
		t.Fatalf("Runtime active operations = %d", active)
	}
	independentMod, err := rt.Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer independentMod.Close()
	independent, err := rt.Instantiate(context.Background(), independentMod)
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
	if out, err := independent.Invoke("f", 41); err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("independent operation = %v, %v", out, err)
	}
}

func BenchmarkPreparedGCMaintenance(b *testing.B) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	mod, err := rt.Compile(gcLifecycleModule())
	if err != nil {
		b.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{StressNurseryBytes: 128}))
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("set")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.Invoke(uint64(i)); err != nil {
			b.Fatal(err)
		}
	}
}
