//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type hostEventObservation struct{ value, phase int32 }

func checkHostEventObservations(got, want []hostEventObservation) error {
	if len(got) != len(want) {
		return fmt.Errorf("event count: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].value != want[i].value {
			return fmt.Errorf("event order at %d", i)
		}
		if got[i].phase != want[i].phase {
			return fmt.Errorf("delivery phase at %d: got %d want %d", i, got[i].phase, want[i].phase)
		}
	}
	return nil
}

func hostEventBoundaryModule() []byte {
	event := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	event = append(event, 0, 0)
	global := append(wasmtest.Name("env"), wasmtest.Name("phase")...)
	global = append(global, 3, 0x7f, 1)
	// (func (param $trap i32) (result i32) (local $seen i32)
	//   global.set phase(1); event(11); local.set seen(global.get phase);
	//   global.set phase(2); event(22); if trap { unreachable };
	//   global.set phase(3); return seen)
	body := []byte{1, 1, 0x7f,
		0x41, 1, 0x24, 0, 0x41, 11, 0x10, 0, 0x23, 0, 0x21, 1,
		0x41, 2, 0x24, 0, 0x41, 22, 0x10, 0,
		0x20, 0, 0x04, 0x40, 0x00, 0x0b,
		0x41, 3, 0x24, 0, 0x20, 1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(event, global)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func qualifyHostEventPath(t *testing.T, raw []byte, in *Instance) {
	t.Helper()
	cache := in.c.codeCache
	if cache == nil || cache.base != in.base || len(cache.mem) < len(in.c.code) {
		t.Fatal("loaded code image unavailable")
	}
	loaded := cache.mem[:len(in.c.code)]
	if !bytes.Equal(loaded, in.c.code) {
		t.Fatal("loaded code differs from compiled code")
	}
	t.Logf("Go=%s target=%s/%s bounds=explicit sync=%v required-AMD64=%x wasm=%x loaded=%x log-bytes=%d", runtime.Version(), runtime.GOOS, runtime.GOARCH, in.syncMode, in.c.requiredAMD64Features, sha256.Sum256(raw), sha256.Sum256(loaded), len(in.hostLog))
	if !compilerTelemetryEnabled {
		return
	}
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	slots, err := moduleSyncHostSlotCapacity(m)
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats railshotModuleStats
	cm, err := railshotCompileModuleWith(m, railshotCompileOptions{
		Workers: 1, DeferCodeMapping: true, SyncHostSlots: slots,
		Optimizations: cfg.optimizations, OptimizationSnapshot: cfg.optimizationSnapshot, OptimizationDeltas: cfg.optimizationDeltas,
		ImportBindings:   []railshotImportBinding{{Dynamic: true, ImportIndex: 0}},
		BitCountFeatures: bitCountHostFeaturesSupported(), Interruptible: !wruntime.HostInterruptSupported(), Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if !bytes.Equal(cm.Code, loaded) {
		t.Fatal("diagnostic compiler did not reproduce loaded bytes")
	}
	if len(stats.Funcs) != 1 || stats.Funcs[0].SharedScalar {
		t.Fatal("expected established compiler path")
	}
}

func TestHostEventNativeReturnBoundary(t *testing.T) {
	raw := hostEventBoundaryModule()
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred=%v", deferred), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1), append([]byte(nil), raw...))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			phase := NewGlobalI32(0, true)
			defer phase.Close()
			var got []hostEventObservation
			observe := func(value int32) { got = append(got, hostEventObservation{value, AsI32(phase.Get())}) }
			var callback any = I32HostEvent(observe)
			if !deferred {
				callback = i32HostFunc(func(value int32) {
					observe(value)
					if err := phase.Set(I32(100 + value)); err != nil {
						panic(HostTrap{Err: err})
					}
				})
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.event", callback, "env.phase", phase)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.syncMode == deferred {
				t.Fatal("wrong host delivery mode")
			}
			qualifyHostEventPath(t, raw, in)
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			for repeat := 0; repeat < 3; repeat++ {
				got = nil
				out, err := fn.Invoke(I32(0))
				if err != nil {
					t.Fatal(err)
				}
				want, wantResult := []hostEventObservation{{11, 1}, {22, 2}}, int32(111)
				if deferred {
					want, wantResult = []hostEventObservation{{11, 3}, {22, 3}}, 1
				}
				if err := checkHostEventObservations(got, want); err != nil {
					t.Fatal(err)
				}
				if len(out) != 1 || AsI32(out[0]) != wantResult || AsI32(phase.Get()) != 3 {
					t.Fatalf("result/state=%v/%d", out, AsI32(phase.Get()))
				}
			}
		})
	}
}

func TestHostEventTrapAndReplayFailureBoundary(t *testing.T) {
	for _, guestTrap := range []bool{false, true} {
		t.Run(fmt.Sprintf("guest-trap=%v", guestTrap), func(t *testing.T) {
			raw := hostEventBoundaryModule()
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), append([]byte(nil), raw...))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			phase := NewGlobalI32(0, true)
			defer phase.Close()
			var got []hostEventObservation
			sentinel := errors.New("stop event replay")
			fail := !guestTrap
			callback := I32HostEvent(func(value int32) {
				got = append(got, hostEventObservation{value, AsI32(phase.Get())})
				if fail {
					panic(HostTrap{Err: sentinel})
				}
			})
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.event", callback, "env.phase", phase)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			trapArg := I32(0)
			if guestTrap {
				trapArg = I32(1)
			}
			_, err = fn.Invoke(trapArg)
			want := []hostEventObservation{{11, 3}}
			if guestTrap {
				want = nil
				var trap *wruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != wruntime.TrapUnreachable {
					t.Fatalf("guest trap: %v", err)
				}
				if AsI32(phase.Get()) != 2 {
					t.Fatal("guest continued after trap")
				}
			} else if !errors.Is(err, sentinel) || AsI32(phase.Get()) != 3 {
				t.Fatalf("replay error/state: %v/%d", err, AsI32(phase.Get()))
			}
			if err := checkHostEventObservations(got, want); err != nil {
				t.Fatal(err)
			}
			fail = false
			got = nil
			out, err := fn.Invoke(I32(0))
			if err != nil || len(out) != 1 || AsI32(out[0]) != 1 {
				t.Fatalf("reuse after failure: %v/%v", out, err)
			}
			if err := checkHostEventObservations(got, []hostEventObservation{{11, 3}, {22, 3}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHostEventBoundaryObserverControls(t *testing.T) {
	want := []hostEventObservation{{11, 3}, {22, 3}}
	if err := checkHostEventObservations(want, want); err != nil {
		t.Fatal(err)
	}
	raw := hostEventBoundaryModule()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	phase := NewGlobalI32(0, true)
	defer phase.Close()
	var got []hostEventObservation
	var pending []int32
	observe := func(value int32) { got = append(got, hostEventObservation{value, AsI32(phase.Get())}) }
	// This test adapter uses the supported synchronous API to deliver the first
	// event early. It buffers the second until return; no native code is patched.
	early := i32HostFunc(func(value int32) {
		if value == 11 {
			observe(value)
		} else {
			pending = append(pending, value)
		}
	})
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.event", early, "env.phase", phase)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", I32(0)); err != nil {
		t.Fatal(err)
	}
	for _, value := range pending {
		observe(value)
	}
	if err := checkHostEventObservations(got, want); err == nil || err.Error() != "delivery phase at 0: got 1 want 3" {
		t.Fatalf("early delivery control: %v", err)
	}
	if err := checkHostEventObservations([]hostEventObservation{want[1], want[0]}, want); err == nil || err.Error() != "event order at 0" {
		t.Fatalf("order control: %v", err)
	}
	if err := checkHostEventObservations(want[:1], want); err == nil || err.Error() != "event count: got 1 want 2" {
		t.Fatalf("count control: %v", err)
	}
}

// This benchmark measures the distinct API contracts without the phase
// observer's global reads. Each row verifies the exact event count and sum.
func BenchmarkHostEventBoundary(b *testing.B) {
	for _, deferred := range []bool{false, true} {
		for _, count := range []int32{1, 16, 1024} {
			b.Run(fmt.Sprintf("deferred=%v/events=%d", deferred, count), func(b *testing.B) {
				c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), hostEventLoopModule())
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				var sum, calls int64
				observe := func(value int32) { sum += int64(value); calls++ }
				var callback any = i32HostFunc(observe)
				if deferred {
					callback = I32HostEvent(observe)
				}
				in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.event", callback)})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				if _, err := fn.Invoke(I32(count)); err != nil {
					b.Fatal(err)
				}
				sum, calls = 0, 0
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := fn.Invoke(I32(count)); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				wantCalls := int64(b.N) * int64(count)
				wantSum := int64(b.N) * int64(count) * int64(count+1) / 2
				if calls != wantCalls || sum != wantSum {
					b.Fatalf("calls/sum=%d/%d want %d/%d", calls, sum, wantCalls, wantSum)
				}
				b.ReportMetric(float64(len(in.hostLog)), "log-B")
			})
		}
	}
}
