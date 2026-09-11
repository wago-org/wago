package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Entry 0 calls the host, entry 1 traps after that call, and entry 2 loops
// after that call. The nested export returns a value to verify isolated scratch.
func managedReentryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("nested", 0, 4))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 1, 2, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
		)),
	)
}

type managedReservationSnapshot struct {
	live                                    uint32
	memory                                  uint64
	owned, callers, instances, reservations int
	mappings                                uint32
	operations, plugin                      uint64
}

func managedReservations(manager *InstanceManager, gate *pluginCallGate) managedReservationSnapshot {
	manager.mu.Lock()
	s := managedReservationSnapshot{live: manager.live, memory: manager.memoryBytes, owned: len(manager.instances), callers: len(manager.byInstance)}
	manager.mu.Unlock()
	rt := manager.rt
	rt.mu.Lock()
	s.instances, s.reservations = len(rt.instances), len(rt.instanceReservations)
	s.mappings, s.operations = rt.nativeMemoryMappings, rt.activeOperations
	rt.mu.Unlock()
	s.plugin = gate.state.Load()
	return s
}

func managedStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			append(append(wasmtest.Name("env"), wasmtest.Name("fork")...), 0, 0),
			append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 1),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})), // one page, bounded maximum
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3))),
		wasmtest.Section(8, wasmtest.ULEB(2)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 1, 0x04, 0x40, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
		)),
	)
}

func TestManagedForkContext(t *testing.T) {
	for _, mode := range []string{"canceled", "before-create", "during-start", "after-start", "live", "nil", "mapping-limit"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "during-start" && !nativeCancellationSupported() {
				t.Skip("native cancellation requires a concurrent scheduler")
			}
			mappingLimit := uint32(2)
			if mode == "mapping-limit" {
				mappingLimit = 1
			}
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithNativeMemoryMappingLimit(mappingLimit)))
			manager := newPendingInstanceManager("fork-test", AuthorityScope{MaxInstances: 2, MaxMemoryBytes: 2 * 65536})
			manager.activate(rt)
			t.Cleanup(func() { _ = manager.close(); _ = rt.CloseContext(context.Background()) })
			gate := newPluginCallGate("fork-test")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			var forkCtx context.Context = ctx
			if mode == "nil" {
				forkCtx = nil
			}
			var parent, partial *Instance
			var childClosed int
			rt.storeHooks(&hookRegistry{
				operationGates: []*pluginCallGate{gate},
				beforeInstantiate: []func(InstantiationRequest) error{func(InstantiationRequest) error {
					if parent != nil && mode == "before-create" {
						cancel()
					}
					return nil
				}},
				afterCreate: []func(InstantiationEvent) error{func(event InstantiationEvent) error {
					if parent != nil {
						partial = event.Instance.value
					}
					return nil
				}},
				afterInstantiate: []func(InstantiationEvent){func(InstantiationEvent) {
					if parent != nil && mode == "after-start" {
						cancel()
					}
				}},
				afterClose: []func(InstanceCloseEvent){func(event InstanceCloseEvent) {
					if event.Instance.value == partial {
						childClosed++
					}
				}},
			})
			mod, err := rt.Compile(managedStartModule())
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			started, release := make(chan struct{}), make(chan struct{})
			imports := Imports{
				"env.start": gate.wrapCaller(func(caller Caller, _, out []uint64) {
					if parent == nil {
						out[0] = 0
						return
					}
					if caller.invocationID == 0 || !caller.reservation.allows(gate) {
						t.Error("start lacks invocation identity or reservation")
					}
					if mode == "during-start" {
						if activeHostInvocationContext(caller.in).parent != ctx {
							t.Error("start lost its parent context")
						}
						close(started)
						<-release
						out[0] = 1
					} else {
						out[0] = 0
					}
				}),
				"env.fork": gate.wrapCaller(func(caller Caller, _, _ []uint64) {
					before := managedReservations(manager, gate)
					child, err := manager.Fork(forkCtx, caller)
					wantCanceled := mode == "canceled" || mode == "before-create" || mode == "during-start" || mode == "after-start"
					if wantCanceled {
						if child != nil || !errors.Is(err, context.Canceled) {
							t.Errorf("Fork = %v, %v; want context.Canceled", child, err)
						}
					} else if mode == "mapping-limit" {
						if child != nil || !errors.Is(err, ErrResourceLimit) {
							t.Errorf("Fork = %v, %v; want mapping limit", child, err)
						}
					} else if err != nil || child == nil {
						t.Errorf("Fork = %v, %v", child, err)
					}
					if child != nil {
						if child.Instance().currentInvocationID() != 0 {
							t.Error("start identity leaked")
						}
						if err := child.Close(); err != nil {
							t.Error(err)
						}
					}
					if after := managedReservations(manager, gate); after != before {
						t.Errorf("reservations: before=%+v after=%+v", before, after)
					}
				}),
			}
			owned, err := manager.Instantiate(nil, mod, WithImports(imports))
			if err != nil {
				t.Fatal(err)
			}
			parent = owned.Instance()
			if mode == "canceled" {
				// Cancellation must win before the capacity check, even at capacity.
				manager.budget.MaxInstances = 1
			}
			done := make(chan error, 1)
			go func() { _, err := parent.Invoke("run"); done <- err }()
			if mode == "during-start" {
				<-started
				cancel()
				close(release)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			manager.pending.Wait()
			if partial != nil {
				if childClosed != 1 || partial.referenceLifetime().snapshot().PhysicalResources {
					t.Fatalf("partial child close count=%d, resources=%+v", childClosed, partial.referenceLifetime().snapshot())
				}
				assertManagedEntryCleanAfterClose(t, partial)
			} else if mode == "during-start" || mode == "live" || mode == "nil" {
				t.Fatal("child was not created")
			}
		})
	}
}

func assertManagedEntryCleanAfterClose(t *testing.T, in *Instance) {
	t.Helper()
	if in.currentInvocationID() != 0 || in.invocationState.Load() != instanceInvocationClosed || in.constructionIsActive() {
		t.Fatal("closed child retained invocation or construction state")
	}
}

func assertManagedEntryClean(t *testing.T, in *Instance, caller HostModule) {
	t.Helper()
	state := in.ensurePluginState()
	if state.invocationID != 0 || in.invocationState.Load() != 0 {
		t.Fatalf("entry state leaked: identity=%d invocations=%d", state.invocationID, in.invocationState.Load())
	}
	if state.hostScope.active.Load() != 0 {
		t.Fatal("callback scope remained active")
	}
	a := &state.activations
	a.mu.Lock()
	clean := a.count == 0 && len(a.other) == 0 && a.reservation == nil && len(a.reservations) == 0
	a.mu.Unlock()
	if !clean {
		t.Fatal("activation or plugin reservation leaked")
	}
	if _, err := in.InvokeFromHost(nil, caller, "nested"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("stale callback reentry = %v", err)
	}
	if _, err := in.InvokeFromHost(nil, Caller{}, "nested"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unrelated callback reentry = %v", err)
	}
}

func TestManagedTableReentryCleanup(t *testing.T) {
	rt := NewRuntime()
	manager := newPendingInstanceManager("entry-test", AuthorityScope{})
	manager.activate(rt)
	t.Cleanup(func() { _ = manager.close(); _ = rt.CloseContext(context.Background()) })
	mod, err := rt.Compile(managedReentryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	var in *Instance
	var stale HostModule
	var lastID invocationID
	var cancel context.CancelFunc
	var hostPanic bool
	var callbacks int
	owned, err := manager.Instantiate(nil, mod, WithImports(Imports{"env.host": CallerHostFunc(func(caller Caller, _, _ []uint64) {
		callbacks++
		if caller.invocationID == 0 || caller.invocationID == lastID {
			t.Error("missing or reused invocation identity")
		}
		lastID = caller.invocationID
		if stale != nil {
			if _, err := in.InvokeFromHost(nil, stale, "nested"); !errors.Is(err, ErrPermissionDenied) {
				t.Errorf("previous callback retained authority: %v", err)
			}
		}
		stale = caller
		out, err := in.InvokeFromHost(nil, caller, "nested")
		if err != nil || len(out) != 1 || out[0] != 42 {
			t.Errorf("nested call = %v, %v", out, err)
		}
		if cancel != nil {
			cancel()
		}
		if hostPanic {
			panic("host panic")
		}
	})}))
	if err != nil {
		t.Fatal(err)
	}
	in = owned.Instance()
	for i := 0; i < 2; i++ {
		if err := owned.InvokeVoidTable(nil, 0); err != nil {
			t.Fatal(err)
		}
		assertManagedEntryClean(t, in, stale)
	}
	if err := owned.InvokeVoidTable(nil, 1); err == nil {
		t.Fatal("trap entry succeeded")
	}
	assertManagedEntryClean(t, in, stale)
	if nativeCancellationSupported() {
		ctx, cancelCall := context.WithCancel(context.Background())
		cancel = cancelCall
		if err := owned.InvokeVoidTable(ctx, 2); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled table call = %v", err)
		}
		cancelCall()
		cancel = nil
		assertManagedEntryClean(t, in, stale)
	}
	hostPanic = true
	func() {
		defer func() {
			if recover() != "host panic" {
				t.Error("host panic did not propagate")
			}
		}()
		_ = owned.InvokeVoidTable(nil, 0)
	}()
	assertManagedEntryClean(t, in, stale)
	hostPanic = false
	if err := owned.InvokeVoidTable(nil, 0); err != nil {
		t.Fatalf("call after failures: %v", err)
	}
	assertManagedEntryClean(t, in, stale)
	if callbacks < 5 {
		t.Fatalf("host calls = %d", callbacks)
	}
}
