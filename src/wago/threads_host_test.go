//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsHostModule(imported bool) []byte {
	host := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	host = append(host, 0, 0)
	imports := [][]byte{host}
	if imported {
		memory := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		imports = append(imports, append(memory, 2, 3, 1, 3))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
	}
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 3})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0, 0x20, 0, 0x36, 2, 0,
			0x20, 0, 0x10, 0,
			0x41, 0, 0x28, 2, 0, 0x6a, 0x3f, 0, 0x6a, 0x0b,
		}))),
	)
	return wasmtest.Module(sections...)
}

func TestThreadsSharedHostCallbackGrowthAndCodec(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, cache := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/cache=%t/regabi=%t", imported, cache, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).
						WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
					c, err := cfg.Compile(threadsHostModule(imported))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					growth, err := cfg.Compile(threadsGrowModule(true, 1, 3))
					if err != nil {
						t.Fatal(err)
					}
					defer growth.Close()
					for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						var memory *Memory
						var grower *Instance
						called := false
						host := slotHostFunc(func(h HostModule, args, results []uint64) {
							called = true
							bytes := h.Memory()
							if len(bytes) != 65536 || binary.LittleEndian.Uint32(bytes) != uint32(args[0]) {
								t.Fatal("host observed incorrect shared backing")
							}
							if got, err := grower.Invoke("grow", 1); err != nil || got[0] != 1 {
								t.Fatalf("callback grow: %v, %v", got, err)
							}
							bytes = h.Memory()
							if len(bytes) != 131072 {
								t.Fatalf("callback did not observe growth: %d", len(bytes))
							}
							binary.LittleEndian.PutUint32(bytes, uint32(args[0])+5)
							binary.LittleEndian.PutUint32(bytes[65536:], 0x12345678)
							results[0] = uint64(uint32(args[0]) * 3)
						})
						imports := testImports("env.host", host)
						if imported {
							memory, err = NewSharedMemory(1, 3)
							if err != nil {
								t.Fatal(err)
							}
							defer memory.Close()
							imports = testImports("env.host", host, "env.memory", memory)
						}
						in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if !imported {
							memory, err = in.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
						}
						grower, err = Instantiate(growth, testImports("env.memory", memory))
						if err != nil {
							t.Fatal(err)
						}
						defer grower.Close()
						got, err := in.Invoke("run", 7)
						if err != nil || len(got) != 1 || got[0] != 35 || !called {
							t.Fatalf("callback resume: %v, %v, called=%t", got, err, called)
						}
						if value, ok := in.ReadUint32Le(65536); !ok || value != 0x12345678 {
							t.Fatalf("grown bytes: %x, %t", value, ok)
						}
					}
				})
			}
		}
	}
}

func TestThreadsSharedNumericHostBindings(t *testing.T) {
	for _, regABI := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			for _, kind := range []string{"typed", "host-call", "caller"} {
				t.Run(fmt.Sprintf("regabi=%t/owned=%t/%s", regABI, owned, kind), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					mod, err := rt.Compile(threadsHostModule(false))
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					var callback any
					switch kind {
					case "typed":
						callback = func(n int32) int32 { return n * 3 }
					case "host-call":
						callback = func(call HostCall) { call.SetI32(0, call.I32(0)*3) }
					case "caller":
						callback = callerSlotHostFunc(func(h Caller, args, results []uint64) {
							if len(h.Memory()) != 65536 {
								t.Fatal("caller memory view")
							}
							results[0] = uint64(uint32(args[0]) * 3)
						})
					}
					var owner *HostFuncRef
					if owned {
						owner, err = rt.NewHostFuncRef(callback, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
						if err != nil {
							t.Fatal(err)
						}
						callback = owner
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", callback)))
					if err != nil {
						t.Fatal(err)
					}
					if got, err := in.Invoke("run", 7); err != nil || len(got) != 1 || got[0] != 29 {
						t.Fatalf("host binding: %v, %v", got, err)
					}
					if owner != nil {
						if err := owner.Close(); err == nil {
							t.Fatal("owner closed with live importer")
						}
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if owner != nil {
						if err := owner.Close(); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestThreadsSharedConcurrentHostCallbacks(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsHostModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const workers = 8
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	host := slotHostFunc(func(h HostModule, args, results []uint64) {
		entered <- struct{}{}
		<-release
		bytes := h.Memory()
		binary.LittleEndian.PutUint32(bytes, uint32(args[0])+5)
		results[0] = uint64(uint32(args[0]) * 3)
	})
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		in, err := Instantiate(c, testImports("env.host", host))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		wg.Add(1)
		go func(in *Instance, n uint64) {
			defer wg.Done()
			got, err := in.Invoke("run", n)
			if err != nil {
				errors <- err
				return
			}
			if len(got) != 1 || got[0] != 4*n+6 {
				errors <- fmt.Errorf("concurrent callback n=%d: %v", n, got)
			}
		}(in, uint64(worker+1))
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for worker := 0; worker < workers; worker++ {
		select {
		case <-entered:
		case <-timeout.C:
			unblock()
			wg.Wait()
			t.Fatal("callbacks did not overlap")
		}
	}
	unblock()
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func TestThreadsSharedImportedFunctionRestoresBacking(t *testing.T) {
	for _, cache := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache=%t/regabi=%t", cache, regABI), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).
					WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
				c, err := cfg.Compile(threadsHostModule(false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				growth, err := cfg.Compile(threadsGrowModule(true, 1, 3))
				if err != nil {
					t.Fatal(err)
				}
				defer growth.Close()
				for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
					var grower *Instance
					host := slotHostFunc(func(h HostModule, args, results []uint64) {
						if len(h.Memory()) != 65536 {
							t.Fatal("imported function callback used wrong instance")
						}
						if _, err := grower.Invoke("grow", 1); err != nil {
							t.Fatal(err)
						}
						bytes := h.Memory()
						if len(bytes) != 131072 {
							t.Fatal("imported function callback missed growth")
						}
						binary.LittleEndian.PutUint32(bytes, uint32(args[0])+5)
						results[0] = uint64(uint32(args[0]) * 3)
					})
					provider, err := Instantiate(compiled, testImports("env.host", host))
					if err != nil {
						t.Fatal(err)
					}
					defer provider.Close()
					memory, err := provider.ExportedMemory("memory")
					if err != nil {
						t.Fatal(err)
					}
					grower, err = Instantiate(growth, testImports("env.memory", memory))
					if err != nil {
						t.Fatal(err)
					}
					defer grower.Close()
					target, err := provider.ExportedFunc("run")
					if err != nil {
						t.Fatal(err)
					}
					consumer, err := Instantiate(compiled, testImports("env.host", target))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if got, err := consumer.Invoke("run", 7); err != nil || len(got) != 1 || got[0] != 43 {
						t.Fatalf("imported function resume: %v, %v", got, err)
					}
					if got, ok := consumer.ReadUint32Le(0); !ok || got != 7 || len(consumer.mem()) != 65536 {
						t.Fatal("callee context leaked into caller")
					}
					if got, ok := provider.ReadUint32Le(0); !ok || got != 12 || len(provider.mem()) != 131072 {
						t.Fatal("provider shared backing changed incorrectly")
					}
				}
			})
		}
	}
}

func threadsImportedHostMinimalModule() []byte {
	host := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(append(host, 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))),
	)
}

func TestThreadsSharedImportedHostMinimal(t *testing.T) {
	module := threadsImportedHostMinimalModule()
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", false)
	c, err := cfg.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	provider, err := Instantiate(c, testImports("env.host", slotHostFunc(func(_ HostModule, args, results []uint64) { results[0] = uint64(uint32(args[0]) + 1) })))
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	target, err := provider.ExportedFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := Instantiate(c, testImports("env.host", target))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if got, err := consumer.Invoke("run", 7); err != nil || len(got) != 1 || got[0] != 8 {
		t.Fatalf("minimal imported callback: %v, %v", got, err)
	}
}

func threadsImportedWaitModule() []byte {
	entry := append(wasmtest.Name("env"), wasmtest.Name("wait")...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(append(entry, 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("wait", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}))),
	)
}

func TestThreadsSharedImportedWaitReleasesLeaseAndPreservesFrames(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(definedSharedAtomicModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	relayCompiled, err := cfg.Compile(threadsImportedWaitModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCompiled.Close()
	provider, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	target, err := provider.ExportedFunc("wait")
	if err != nil {
		t.Fatal(err)
	}
	relays := make([]*Instance, 2)
	for i := range relays {
		relays[i], err = Instantiate(relayCompiled, testImports("env.wait", target))
		if err != nil {
			t.Fatal(err)
		}
		defer relays[i].Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 2)
	for _, relay := range relays {
		go func(in *Instance) {
			got, err := in.InvokeContext(ctx, "wait", 0, I64(-1))
			if err == nil && (len(got) != 1 || got[0] != uint64(memoryWaitNotified)) {
				err = fmt.Errorf("imported wait: %v", got)
			}
			done <- err
		}(relay)
	}
	waitForMemoryWaiters(t, memory, 2)
	// This is a global-lease entry on the same producer whose controls are parked.
	// It must wake both roots and leave each distinct saved frame resumable.
	if got, err := provider.Invoke("notify", 2); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("producer notify: %v, %v", got, err)
	}
	for range relays {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestThreadsSharedImportedWaitCancellation(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(definedSharedAtomicModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	relayCompiled, err := cfg.Compile(threadsImportedWaitModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCompiled.Close()
	provider, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	target, err := provider.ExportedFunc("wait")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := Instantiate(relayCompiled, testImports("env.wait", target))
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := relay.InvokeContext(ctx, "wait", 0, I64(-1)); done <- err }()
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	waitForMemoryWaiters(t, memory, 1)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled imported wait succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("imported wait ignored caller cancellation")
	}
}

func TestThreadsSharedForeignCallbacksKeepIndependentScopes(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", false)
	c, err := cfg.Compile(threadsImportedHostMinimalModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	entered := make(chan uint64, 2)
	secondEntered := make(chan struct{})
	firstChecked := make(chan struct{})
	errors := make(chan error, 2)
	host := slotHostFunc(func(h HostModule, args, results []uint64) {
		entered <- args[0]
		if args[0] == 1 {
			<-secondEntered
			if len(h.Memory()) != 65536 {
				errors <- fmt.Errorf("first foreign scope expired while second was active")
			}
			close(firstChecked)
		} else {
			close(secondEntered)
			<-firstChecked
			if len(h.Memory()) != 65536 {
				errors <- fmt.Errorf("second foreign scope expired while first returned")
			}
		}
		results[0] = args[0] + 1
	})
	provider, err := Instantiate(c, testImports("env.host", host))
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	target, err := provider.ExportedFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for i := uint64(1); i <= 2; i++ {
		relay, err := Instantiate(c, testImports("env.host", target))
		if err != nil {
			t.Fatal(err)
		}
		defer relay.Close()
		go func(in *Instance, n uint64) {
			got, err := in.Invoke("run", n)
			if err == nil && (len(got) != 1 || got[0] != n+1) {
				err = fmt.Errorf("foreign callback n=%d: %v", n, got)
			}
			done <- err
		}(relay, i)
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("foreign callback failed to enter")
		}
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if _, ok := hostInvocationContexts.Load(offHeapSlicePtr(provider.ctrl)); ok {
		t.Fatal("foreign callback left an ambient producer context")
	}
}

func TestThreadsSharedHostPublicationMigratesLease(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsHostModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	host := slotHostFunc(func(_ HostModule, args, results []uint64) {
		in.markNativeControlShared()
		results[0] = uint64(uint32(args[0]) * 3)
	})
	in, err = Instantiate(c, testImports("env.host", host))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run", 7); err != nil || len(got) != 1 || got[0] != 29 {
		t.Fatalf("publication resume: %v, %v", got, err)
	}
	if in.usesPrivateThreadedExecution() {
		t.Fatal("published control retained private execution")
	}
	if got, err := in.Invoke("run", 8); err != nil || len(got) != 1 || got[0] != 33 {
		t.Fatalf("published entry: %v, %v", got, err)
	}
}

func TestThreadsSharedImportedWaitRetainsGCRoots(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureGC)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	body := []byte{1, 1, 0x63, 0, 0x20, 0, 0xfb, 0, 0, 0x21, 2,
		// A preceding zero-timeout wait must consume its source root mask
		// when inline lowering removes the native call.
		0x41, 0, 0x41, 1, 0x42, 0, 0xfe, 1, 2, 0, 0x1a,
		0x41, 0, 0x41, 0, 0x20, 1, 0xfe, 1, 2, 0, 0x1a,
		0x20, 2, 0xfb, 2, 0, 0, 0x0b}
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0), wasmtest.ExportEntry("wait", 0, 0), wasmtest.ExportEntry("notify", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...),
			wasmtest.Code([]byte{0x41, 0, 0x20, 0, 0xfe, 0, 2, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	provider, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	target, err := provider.ExportedFunc("wait")
	if err != nil {
		t.Fatal(err)
	}
	relayMod, err := rt.Compile(threadsImportedWaitModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayMod.Close()
	relays := make([]*Instance, 3)
	for i := range relays {
		relays[i], err = rt.Instantiate(context.Background(), relayMod, WithImports(testImports("env.wait", target)))
		if err != nil {
			t.Fatal(err)
		}
		defer relays[i].Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	startWait := func(index int, value uint64) <-chan error {
		done := make(chan error, 1)
		go func() {
			got, err := relays[index].InvokeContext(ctx, "wait", value, I64(-1))
			if err == nil && (len(got) != 1 || got[0] != value) {
				err = fmt.Errorf("parked GC root %d: %v", value, got)
			}
			done <- err
		}()
		return done
	}
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	first := startWait(0, 42)
	waitForMemoryWaiters(t, memory, 1)
	second := startWait(1, 43)
	waitForMemoryWaiters(t, memory, 2)
	if err := provider.CollectGC(); err != nil {
		t.Fatal(err)
	}
	notify := func() {
		if got, err := provider.Invoke("notify", 1); err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("GC producer notify: %v, %v", got, err)
		}
	}
	notify()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	// Reuse the first activation slot while the second is still suspended.
	third := startWait(2, 44)
	waitForMemoryWaiters(t, memory, 2)
	if err := provider.CollectGC(); err != nil {
		t.Fatal(err)
	}
	notify()
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if err := provider.CollectGC(); err != nil {
		t.Fatal(err)
	}
	notify()
	if err := <-third; err != nil {
		t.Fatal(err)
	}
}
