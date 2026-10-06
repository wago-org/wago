//go:build (linux || darwin) && (arm64 || amd64) && !tinygo && go1.22 && !go1.28

package wago

import (
	"context"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestBoundedMultiTypedThenUnwrittenViews(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "typed" (func $typed (param i32) (result i32)))
 (import "env" "view" (func $view (param i32) (result i32)))
 (import "env" "caller" (func $caller (param i32) (result i32)))
 (func (export "run") (result i32) i32.const 43 call $typed call $view call $caller))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	counts := [3]int{}
	imports := NewImports()
	imports.HostFunc("env", "typed", func(v int32) int32 { counts[0]++; return v + 28 }).Params(ValI32).Results(ValI32)
	imports.HostFunc("env", "view", func(call HostCall) {
		counts[1]++
		if call.I32(0) != 71 || call.ResultSlots()[0] != 0 {
			t.Fatal("view received stale typed result")
		}
		// An unwritten borrowed result must remain zero.
	}).Params(ValI32).Results(ValI32)
	imports.HostFunc("env", "caller", func(caller Caller, call HostCall) {
		counts[2]++
		if !caller.valid() || call.I32(0) != 0 || call.ResultSlots()[0] != 0 {
			t.Fatal("Caller received stale result or inactive authority")
		}
		call.SetI32(0, 3)
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	layout := in.ensurePluginState().multiHost
	if layout == nil {
		t.Fatal("mixed bindings were not admitted")
	}
	for range 3 {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 3 {
			t.Fatalf("run=%v, %v", got, err)
		}
	}
	if counts != [3]int{3, 3, 3} {
		t.Fatal(counts)
	}
}

func TestBoundedMultiHostMixedShapes(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "first" (func $first (result i32 i64)))
 (import "env" "second" (func $second (param i32 i64 f64) (result f64)))
 (import "env" "third" (func $third (param i32) (result i32)))
 (func (export "run") (result f64 i32)
 call $first f64.const -3.5 call $second i32.const 17 call $third))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	counts := [3]int{}
	imports := NewImports()
	imports.HostFunc("env", "first", func(call HostCall) {
		if len(call.ParamSlots()) != 0 || len(call.ResultSlots()) != 2 {
			t.Fatal("first shape")
		}
		if call.ResultSlots()[0] != 0 || call.ResultSlots()[1] != 0 {
			t.Fatal("stale first results")
		}
		if inlineWagoGrow(32) != 528 {
			t.Fatal("stack growth")
		}
		if counts[0] == 0 {
			runtime.GC() // Trace the live owner, callback closure and shape table.
		}
		counts[0]++
		call.SetI32(0, -7)
		call.SetI64(1, 0x123456789abcdef)
	}).Results(ValI32, ValI64)
	imports.HostFunc("env", "second", func(call HostCall) {
		if len(call.ParamSlots()) != 3 || len(call.ResultSlots()) != 1 || cap(call.ParamSlots()) != 3 || cap(call.ResultSlots()) != 1 {
			t.Fatal("second shape")
		}
		if call.I32(0) != -7 || call.I64(1) != 0x123456789abcdef || call.F64(2) != -3.5 {
			t.Fatal("mixed bits")
		}
		if call.ResultSlots()[0] != 0 {
			t.Fatal("stale second results")
		}
		counts[1]++
		call.SetF64(0, -7.25)
	}).Params(ValI32, ValI64, ValF64).Results(ValF64)
	imports.HostFunc("env", "third", func(v int32) int32 { counts[2]++; return v + 1 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.hasBoundedMultiHostView() {
		t.Fatal("mixed shapes not admitted")
	}
	for range 3 {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 2 || got[0] != math.Float64bits(-7.25) || got[1] != 18 {
			t.Fatalf("result %v %v", got, err)
		}
	}
	if counts != [3]int{3, 3, 3} {
		t.Fatal(counts)
	}
	// A corrupted admission must fail before invoking the selected callback.
	shape := in.ensurePluginState().multiHost.shapes[1]
	in.ensurePluginState().multiHost.shapes[1] ^= 1 << 16
	if _, err := in.Invoke("run"); err == nil {
		t.Fatal("mismatched shape admitted")
	}
	if counts[1] != 3 || counts[2] != 3 {
		t.Fatal("callback ran after shape failure", counts)
	}
	in.ensurePluginState().multiHost.shapes[1] = shape
	in.ensurePluginState().multiHost.shapes = in.ensurePluginState().multiHost.shapes[:2]
	if _, err := in.Invoke("run"); err == nil {
		t.Fatal("out of range import admitted")
	}
	if counts[2] != 3 {
		t.Fatal("out of range callback ran")
	}

}

func TestBoundedMultiCallerCleanupAndPublication(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "first" (func $first (param i32) (result i32)))
 (import "env" "second" (func $second (param i32) (result i32)))
 (memory (export "memory") 1)
 (func (export "run") (param i32) (result i32) local.get 0 call $first call $second))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, publish := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "published"}[publish], func(t *testing.T) {
			var in *Instance
			var stale Caller
			panicNow := true
			counts := [2]int{}
			imports := NewImports()
			imports.HostFunc("env", "first", func(caller Caller, call HostCall) {
				if !caller.valid() {
					t.Fatal("invalid first scope")
				}
				stale = caller
				if panicNow {
					panic("multi callback panic")
				}
				if publish && counts[0] >= 2 {
					if _, err := in.ExportedMemory("memory"); err != nil {
						t.Fatal(err)
					}
				}
				counts[0]++
				call.SetI32(0, call.I32(0)+1)
			}).Params(ValI32).Results(ValI32)
			imports.HostFunc("env", "second", func(caller Caller, call HostCall) {
				if !caller.valid() || stale.valid() {
					t.Fatal("scope generation")
				}
				if inlineWagoGrow(32) != 528 {
					t.Fatal("stack growth")
				}
				var pcs [64]uintptr
				frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
				found := false
				for {
					f, more := frames.Next()
					found = found || strings.Contains(f.Function, "inlineHostMultiViewEnter")
					if !more {
						break
					}
				}
				if !codeProfileEnabled && in.hasBoundedMultiHostView() && os.Getenv("WAGO_"+strings.ToUpper(runtime.GOARCH)+"_NO_INLINE_HOST") != "1" && !found {
					t.Fatal("missing live multi-import owner")
				}
				counts[1]++
				stale = caller
				call.SetI32(0, call.I32(0)+2)
			}).Params(ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("missing panic")
					}
				}()
				_, _ = in.Invoke("run", 5)
			}()
			if stale.valid() {
				t.Fatal("scope survived panic")
			}
			panicNow = false
			for range 3 {
				got, err := in.Invoke("run", 5)
				if err != nil || len(got) != 1 || got[0] != 8 {
					t.Fatalf("result %v %v", got, err)
				}
				if stale.valid() {
					t.Fatal("scope survived return")
				}
			}
			if counts != [2]int{3, 3} {
				t.Fatal(counts)
			}
			if publish && in.usesIndependentExecution() {
				t.Fatal("publication retained independent lease")
			}
		})
	}
}

func TestBoundedMultiCallerReentry(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "first" (func $first (param i32) (result i32)))
 (import "env" "second" (func $second (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $first call $second))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	var retired []Caller
	imports := NewImports()
	imports.HostFunc("env", "first", func(caller Caller, call HostCall) {
		n := call.I32(0)
		retired = append(retired, caller)
		if n == 0 {
			call.SetI32(0, 0)
			return
		}
		got, err := in.InvokeFromHost(context.Background(), caller, "run", uint64(n-1))
		if err != nil || len(got) != 1 {
			t.Fatalf("nested result %v %v", got, err)
		}
		if !caller.valid() {
			t.Fatal("parent scope lost after reentry")
		}
		call.SetI32(0, int32(got[0]))
	}).Params(ValI32).Results(ValI32)
	imports.HostFunc("env", "second", func(v int32) int32 { return v + 1 }).Params(ValI32).Results(ValI32)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for range 3 {
		got, err := in.Invoke("run", 3)
		if err != nil || len(got) != 1 || got[0] != 4 {
			t.Fatalf("result %v %v", got, err)
		}
	}
	for _, caller := range retired {
		if caller.valid() {
			t.Fatal("retired nested scope valid")
		}
	}
}

func TestBoundedMultiCancellation(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "first" (func $first (param i32) (result i32)))
 (import "env" "second" (func $second (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $first call $second))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelNow := true
	second := 0
	imports := NewImports()
	imports.HostFunc("env", "first", func(v int32) int32 {
		if cancelNow {
			cancel()
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	imports.HostFunc("env", "second", func(v int32) int32 { second++; return v + 1 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.InvokeContext(ctx, "run", 5); err == nil {
		t.Fatal("cancelled invocation succeeded")
	}
	cancelNow = false
	got, err := in.Invoke("run", 5)
	if err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("reuse result %v %v", got, err)
	}
	if second < 1 || second > 2 {
		t.Fatal("second callback count", second)
	}
}

func TestBoundedMultiRejectsLegacyAndReferences(t *testing.T) {
	for _, kind := range []string{"legacy", "reference"} {
		t.Run(kind, func(t *testing.T) {
			secondSig := "(param i32) (result i32)"
			body := "local.get 0 call $first call $second"
			if kind == "reference" {
				secondSig = "(param externref)"
				body = "local.get 0 call $first"
			}
			c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "first" (func $first (param i32) (result i32)))
 (import "env" "second" (func $second `+secondSig+`))
 (func (export "run") (param i32) (result i32) `+body+`))`))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			imports := NewImports()
			imports.HostFunc("env", "first", func(v int32) int32 { return v + 1 }).Params(ValI32).Results(ValI32)
			if kind == "legacy" {
				imports.bind("env", "second", slotHostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 }))
			} else {
				imports.HostFunc("env", "second", func(_ HostCall) {}).Params(ValExternRef)
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.hasBoundedMultiHostView() || in.ensurePluginState().multiHost != nil {
				t.Fatal("unsupported binding admitted")
			}
			want := uint64(7)
			if kind == "reference" {
				want = 6
			}
			got, err := in.Invoke("run", 5)
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("fallback result %v %v", got, err)
			}
		})
	}
}

func TestBoundedMultiWarmContextAndArity(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains general entry")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "first" (func $first (param i32) (result i32)))
 (import "env" "second" (func $second (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $first call $second))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "first", func(call HostCall) { calls++; call.SetI32(0, call.I32(0)+1) }).Params(ValI32).Results(ValI32)
	imports.HostFunc("env", "second", func(v int32) int32 { calls++; return v + 2 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	invoke := func() {
		t.Helper()
		got, err := in.Invoke("run", I32(-17))
		if err != nil || len(got) != 1 || AsI32(got[0]) != -14 {
			t.Fatalf("result %v %v", got, err)
		}
	}
	invoke()
	invoke()
	state := in.ensurePluginState()
	if state.boundedViewVersion == 0 {
		t.Fatal("warm multi entry did not bind context")
	}
	version := state.nativeContextVersion.Load()
	invoke()
	if state.nativeContextVersion.Load() != version {
		t.Fatal("valid context rebound")
	}
	if _, err := in.Invoke("run"); err == nil {
		t.Fatal("wrong arity accepted")
	}
	if calls != 6 {
		t.Fatal("wrong arity reached callback", calls)
	}
	in.acquireInstanceNativeStateForHostAccess().Unlock()
	version = state.nativeContextVersion.Load()
	invoke()
	if state.nativeContextVersion.Load() <= version {
		t.Fatal("guarded access failed to rebind")
	}
	if calls != 8 || state.invocationID != 0 || in.invocationState.Load() != 0 || state.activations.boundedID.Load() != 0 {
		t.Fatal("retained authority or wrong count")
	}
}

func BenchmarkBoundedMultiNumericSingle(b *testing.B) {
	if codeProfileEnabled {
		b.Skip("profiling retains general entry")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(b, `(module
 (import "env" "first" (func $first (param i32) (result i32)))
 (import "env" "second" (func $second (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $first call $second))`))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		b.Run(kind, func(b *testing.B) {
			calls := 0
			step := func(v int32) int32 { calls++; return v + 1 }
			var callback any = step
			if kind == "HostCall" {
				callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
			}
			if kind == "Caller" {
				callback = func(caller Caller, call HostCall) {
					if !caller.valid() {
						b.Fatal("invalid Caller")
					}
					call.SetI32(0, step(call.I32(0)))
				}
			}
			imports := NewImports()
			imports.HostFunc("env", "first", callback).Params(ValI32).Results(ValI32)
			imports.HostFunc("env", "second", callback).Params(ValI32).Results(ValI32)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			for range 3 {
				if _, err := in.Invoke("run", 41); err != nil {
					b.Fatal(err)
				}
			}
			calls = 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 41)
				if err != nil || len(got) != 1 || got[0] != 43 {
					b.Fatalf("result %v %v", got, err)
				}
			}
			b.StopTimer()
			if calls != 2*b.N {
				b.Fatalf("callback count %d != %d", calls, 2*b.N)
			}
		})
	}
}
