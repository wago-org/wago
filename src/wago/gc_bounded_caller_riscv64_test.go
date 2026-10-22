//go:build linux && riscv64 && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64BoundedCollectorCallerModule() []byte {
	entry := append(append(wasmtest.Name("env"), wasmtest.Name("allocate")...), byte(wasm.ExternFunc), 0x03)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x43, 0, 0, 0x9a, 0x42, 0x43, 0, 0, 0, 0, 0x43, 0, 0, 0, 0, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x43, 0, 0, 0x28, 0x42, 0x43, 0, 0, 0, 0x40, 0x43, 0, 0, 0x40, 0x40, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x10, 0x00, 0x1a, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x92, 0x0b}
	// Matching the complete type directory puts caller and bounded target in
	// the same collector domain, even though the imported signature is numeric.
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x03, 0x7d, 0x00, 0x7d, 0x01, 0x7d, 0x00},
			[]byte{0x60, 0x00, 0x01, 0x6e},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7d},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.F32}),
			[]byte{0x60, 0x02, 0x64, 0x00, 0x7d, 0x01, 0x7d},
			wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(2, wasmtest.Vec(entry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestRV64BoundedCollectorKeepsOlderCallerRoots(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, host := range []bool{false, true} {
			name := map[bool]string{false: "source", true: "artifact"}[artifact] + "/" + map[bool]string{false: "native", true: "host-reentry"}[host]
			t.Run(name, func(t *testing.T) {
				cfg := rv64RootedScalarConfig()
				providerCode, err := cfg.Compile(rv64PublicGCStructBasicBytes(t))
				if err != nil {
					t.Fatal(err)
				}
				defer providerCode.Close()
				if artifact {
					data, err := providerCode.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(data)
					if err != nil {
						t.Fatal(err)
					}
					defer loaded.Close()
					providerCode = loaded
				}
				callerCode, err := cfg.Compile(rv64BoundedCollectorCallerModule())
				if err != nil {
					t.Fatal(err)
				}
				defer callerCode.Close()
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				pm, err := rt.Module(providerCode)
				if err != nil {
					t.Fatal(err)
				}
				defer pm.Close()
				gcConfig := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
				provider, err := rt.Instantiate(context.Background(), pm, WithGC(gcConfig))
				if err != nil {
					t.Fatal(err)
				}
				defer provider.Close()
				var initializerRefs [2]gc.Ref
				for index := range initializerRefs {
					initializerRefs[index] = gc.Ref(readGlobalObject(provider.globalCells[index], ValAnyRef))
				}
				allocate, err := provider.ExportedFunc("get_0_0")
				if err != nil {
					t.Fatal(err)
				}
				var binding any = allocate
				stop := errors.New("bounded collector reentry stopped")
				fail := false
				if host {
					binding = CallerHostCallFunc(func(caller Caller, call HostCall) {
						if fail {
							panic(HostTrap{Err: stop})
						}
						got, err := provider.InvokeFromHost(context.Background(), caller, "get_0_0")
						if err != nil {
							panic(HostTrap{Err: err})
						}
						call.ResultSlots()[0] = got[0]
					})
				}
				cm, err := rt.Module(callerCode)
				if err != nil {
					t.Fatal(err)
				}
				defer cm.Close()
				caller, err := rt.Instantiate(context.Background(), cm, WithGC(gcConfig), WithImports(testImports("env.allocate", binding)))
				if err != nil {
					t.Fatal(err)
				}
				defer caller.Close()
				callerAlias, err := rt.Instantiate(context.Background(), cm, WithGC(gcConfig), WithImports(testImports("env.allocate", binding)))
				if err != nil {
					t.Fatal(err)
				}
				defer callerAlias.Close()
				providerRoots, callerRoots := provider.c.genericGCFrameRoots(), caller.c.genericGCFrameRoots()
				if caller.gc != provider.gc || providerRoots == nil || callerRoots == nil || len(callerRoots.callsites) == 0 || railshotValidateCompiled(provider.c) != nil || railshotValidateCompiled(caller.c) != nil {
					t.Fatal("regression fixture lost shared domain or root-map distinction")
				}
				wantRun, err := caller.Invoke("run")
				if err != nil || len(wantRun) != 1 {
					t.Fatal("bounded caller baseline", wantRun, err)
				}
				for iteration := range 16 {
					for index, active := range []*Instance{caller, callerAlias} {
						if got, err := active.Invoke("run"); err != nil || !slices.Equal(got, wantRun) {
							t.Fatalf("caller %d after bounded collection at iteration %d = %v, %v", index, iteration, got, err)
						}
					}
				}
				for index, want := range initializerRefs {
					ref := gc.Ref(readGlobalObject(provider.globalCells[index], ValAnyRef))
					if ref != want {
						t.Fatalf("provider initializer %d reference=%d, want %d", index, ref, want)
					}
					for field, bits := range [2][3]uint64{{0x3f800000, 0x40000000, 0x40400000}, {}}[index] {
						if value, err := provider.gc.StructGet(ref, uint32(field)); err != nil || value.Bits != bits {
							t.Fatalf("provider initializer %d field %d=%v,%v, want %#x", index, field, value, err, bits)
						}
					}
				}
				if host {
					fail = true
					if _, err := caller.Invoke("run"); !errors.Is(err, stop) {
						t.Fatalf("host trap = %v", err)
					}
					fail = false
					if got, err := caller.Invoke("run"); err != nil || !slices.Equal(got, wantRun) {
						t.Fatalf("caller after trap = %v, %v", got, err)
					}
				}
				t.Logf("native root-set bytes=%d", unsafe.Sizeof(gcNativeFrameRoots{}))
			})
		}
	}
}

func testRV64BoundedCallerRootBuilder(t *testing.T, provider, caller *Instance) {
	t.Helper()
	const controlOffset = 80
	const viewOffset = controlOffset + railshot.HostFixedFrameBytes + 4096
	const frameBytes = viewOffset + abi.RV64InvocationViewBytes
	top := caller.eng.StackTop()
	record := top - abi.RV64InvocationViewBytes - frameBytes - 128
	data := unsafe.Slice((*byte)(offHeapPtr(record)), frameBytes)
	put := func(off int, value uintptr) { binary.LittleEndian.PutUint64(data[off:], uint64(value)) }
	callsite := &caller.c.genericGCFrameRoots().callsites[0]
	pc := caller.base + uintptr(callsite.returnOffset)
	put(0, pc)
	put(8, caller.jm.LinMemBase())
	put(16, caller.nativeContext)
	put(64, provider.nativeContext)
	put(72, top-abi.RV64InvocationViewBytes)
	put(viewOffset, uintptr(abi.RV64InvocationMagic))
	put(viewOffset+int(abi.RV64InvocationFenceOffset), caller.eng.StackLimit())
	put(viewOffset+int(abi.RV64InvocationTopOffset), top-abi.RV64InvocationViewBytes)
	put(viewOffset+int(abi.RV64InvocationArgumentsOffset), record+controlOffset+railshot.HostFixedFrameBytes)
	put(viewOffset+int(abi.RV64InvocationResultsOffset), record+controlOffset+railshot.HostFixedFrameBytes+2048)
	put(viewOffset+int(abi.RV64InvocationArgumentBytesOffset), 2048)
	put(viewOffset+int(abi.RV64InvocationResultBytesOffset), 2048)
	state := provider.publicGCState()
	saved := provider.jm.RV64InvocationView()
	provider.jm.SetRV64InvocationView(record + viewOffset)
	defer provider.jm.SetRV64InvocationView(saved)
	defer func() { state.frameRoots = gcNativeFrameRoots{} }()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	read := func() { provider.gcHelperRoots(record+controlOffset, state, 0, caller.eng) }
	if allocations := testing.AllocsPerRun(100, read); allocations != 0 {
		t.Fatalf("bounded caller root-builder allocations=%g", allocations)
	}
	t.Log("warm bounded caller root-builder: 0 allocations/run")
	for _, damage := range []struct {
		off     int
		value   uintptr
		message string
	}{{0, caller.base + 1, "no exact return map"}, {64, provider.nativeContext ^ 8, "no authenticated native caller activation"}} {
		old := binary.LittleEndian.Uint64(data[damage.off:])
		put(damage.off, damage.value)
		func() {
			defer func() {
				p := recover()
				failure, ok := p.(gcStructHelperError)
				if !ok || !strings.Contains(failure.Error(), damage.message) {
					t.Fatalf("damaged caller record panic=%v", p)
				}
			}()
			read()
		}()
		put(damage.off, uintptr(old))
	}
}

type boundedRootCounter struct{ count int }

func (sink *boundedRootCounter) VisitRootRef(gc.Ref) bool {
	sink.count++
	return true
}

func TestRV64BoundedCollectorKeepsConcurrentHostOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			cfg := rv64RootedScalarConfig()
			providerCode, err := cfg.Compile(rv64PublicGCStructBasicBytes(t))
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			if artifact {
				data, err := providerCode.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				providerCode, err = LoadTrustedArtifact(data)
				if err != nil {
					t.Fatal(err)
				}
				defer providerCode.Close()
			}
			callerCode, err := cfg.Compile(rv64BoundedCollectorCallerModule())
			if err != nil {
				t.Fatal(err)
			}
			defer callerCode.Close()
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			pm, err := rt.Module(providerCode)
			if err != nil {
				t.Fatal(err)
			}
			defer pm.Close()
			cm, err := rt.Module(callerCode)
			if err != nil {
				t.Fatal(err)
			}
			defer cm.Close()
			config := GCConfig{VerifyAfterCollect: true}
			provider, err := rt.Instantiate(context.Background(), pm, WithGC(config))
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			const count = gcHostActivationLimit + 1
			entered := make(chan struct{}, count)
			release := make(chan struct{})
			results := make(chan error, count)
			var once sync.Once
			unpark := func() { once.Do(func() { close(release) }) }
			defer unpark()
			for range count {
				host := CallerHostCallFunc(func(_ Caller, call HostCall) {
					entered <- struct{}{}
					<-release
					call.ResultSlots()[0] = 0
				})
				in, err := rt.Instantiate(context.Background(), cm, WithGC(config), WithImports(testImports("env.allocate", host)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { in.Close() })
				if in.gc != provider.gc {
					t.Fatal("parked caller has a different collector")
				}
				go func() {
					got, err := in.Invoke("run")
					if err == nil && !slices.Equal(got, []uint64{0x42ee0000}) {
						err = errors.New("parked caller lost local or global reference")
					}
					results <- err
				}()
			}
			for index := range count {
				select {
				case <-entered:
				case err := <-results:
					t.Fatalf("caller ended before park %d: %v", index, err)
				case <-time.After(10 * time.Second):
					t.Fatalf("only %d callers parked", index)
				}
			}
			for range 16 {
				if err := provider.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
			func() {
				nativeExecutionMu.Lock()
				defer nativeExecutionMu.Unlock()
				state := provider.publicGCState()
				state.mu.Lock()
				defer state.mu.Unlock()
				roots := provider.gcCollectFrameRoots(state)
				sink := &boundedRootCounter{}
				state.domainHostOwnersExtra = nil
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				roots.RangeRootRefs(sink)
				runtime.ReadMemStats(&after)
				coldBytes := after.TotalAlloc - before.TotalAlloc
				coldAllocations := after.Mallocs - before.Mallocs
				if coldAllocations != 1 || coldBytes != uint64(unsafe.Sizeof(gcSuspendedRootOwner{})) {
					t.Fatalf("cold ninth-owner snapshot=%d allocations/%d bytes", coldAllocations, coldBytes)
				}
				if allocations := testing.AllocsPerRun(100, func() { roots.RangeRootRefs(sink) }); allocations != 0 {
					t.Fatalf("warm overflow root-scan allocations=%g", allocations)
				}
				if cap(state.domainHostOwnersExtra) == 0 || sink.count == 0 {
					t.Fatal("overflow snapshot or native roots were not used")
				}
				for _, entry := range state.domainHostOwnersExtra[:cap(state.domainHostOwnersExtra)] {
					if entry.owner != nil || entry.state != nil {
						t.Fatal("reused overflow snapshot retains an owner or state")
					}
				}
				t.Logf("nine parked owners: cold %d allocation/%d bytes; 0 warm allocations; state header +%d bytes; root-set %d bytes", coldAllocations, coldBytes, unsafe.Sizeof(state.domainHostOwnersExtra), unsafe.Sizeof(roots))
			}()
			unpark()
			for range count {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
