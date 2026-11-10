//go:build (linux || darwin) && (arm64 || amd64) && !tinygo && go1.22 && !go1.28

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

//go:noinline
func inlineWagoGrow(depth int) int {
	var buffer [2048]byte
	buffer[0] = byte(depth)
	if depth == 0 {
		runtime.GC()
		return 0
	}
	value := inlineWagoGrow(depth-1) + int(buffer[0])
	runtime.KeepAlive(&buffer)
	return value
}

func TestInlineWagoStackGrowth(t *testing.T) {
	if os.Getenv("WAGO_"+strings.ToUpper(runtime.GOARCH)+"_NO_INLINE_HOST") == "1" {
		t.Skip("live Go bridge disabled at startup")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		for _, mode := range []string{"instance", "function", "session"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				calls := 0
				var in *Instance
				imports := NewImports()
				callback := func(v int32) int32 {
					// The shared parked-frame ABI remains usable while Go runs.
					sp := uintptr(binary.LittleEndian.Uint64(in.ctrl))
					if sp < in.eng.StackLimit() || sp >= in.eng.StackTop() {
						t.Fatal("parked native SP outside engine stack")
					}
					if home := uintptr(binary.LittleEndian.Uint64(in.ctrl[inlineWagoSavedHomeOffset():])); home != in.jm.LinMemBase() && (in.eng.PreparedScalarHost() == nil || home != in.eng.PreparedScalarHost().DetachedNumericContextBase()) {
						t.Fatal("parked native home")
					}
					var pcs [64]uintptr
					frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
					found := false
					foundSmall := false
					for {
						f, more := frames.Next()
						foundSmall = foundSmall || strings.Contains(f.Function, "tryInvokePrivateTypedI32")
						found = found || strings.Contains(f.Function, "inlineHostEnter") || strings.Contains(f.Function, "inlineHostContextEnter") || strings.Contains(f.Function, "inlineHostViewEnter") || strings.Contains(f.Function, "inlineHostViewContextEnter") || strings.Contains(f.Function, "inlineHostIntegerContextEnter") || strings.Contains(f.Function, "inlineHostIntegerI32Enter") || strings.Contains(f.Function, "inlineHostIntegerViewContextEnter")
						if !more {
							break
						}
					}
					if !found {
						t.Fatal("compiled Wasm did not use live Go owner")
					}
					if calls >= 4 && smallTypedEntryEnabled && privateNumericLiveRouteEnabled && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && kind == "typed" && mode == "instance" && in.eng.PreparedScalarHost() != nil && in.eng.PreparedScalarHost().IntegerGuestContext() && !foundSmall {
						t.Fatal("small typed driver was not reached")
					}

					if inlineWagoGrow(32) != 528 {
						t.Fatal("callback stack growth")
					}
					calls++
					return v + 1
				}
				var binding any = callback
				if kind == "HostCall" {
					binding = func(call HostCall) { call.SetI32(0, callback(call.I32(0))) }
				}
				if kind == "Caller" {
					binding = func(_ Caller, call HostCall) { call.SetI32(0, callback(call.I32(0))) }
				}
				imports.HostFunc("env", "step", binding).Params(ValI32).Results(ValI32)
				in, err = Instantiate(c, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				invoke := func() ([]uint64, error) { return in.Invoke("run", 4, 0) }
				if mode == "function" {
					invoke = func() ([]uint64, error) { return fn.Invoke(4, 0) }
				}
				if mode == "session" {
					s, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close()
					invoke = func() ([]uint64, error) { return s.Invoke2(4, 0) }
				}
				for i := 0; i < 3; i++ {
					got, err := invoke()
					if err != nil || len(got) != 1 || got[0] != 4 {
						t.Fatalf("result %v, %v", got, err)
					}
				}
				if calls != 12 {
					t.Fatal("callback count", calls)
				}
			})
		}
	}
}

func TestInlineWagoTrapAndPanicCleanup(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (func (export "run") (param i32) (result i32)
  local.get 0 call $step drop local.get 0 if unreachable end i32.const 7))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			action := ""
			imports := NewImports()
			callback := func(v int32) int32 {
				if action == "panic" {
					panic("inline callback panic")
				}
				if action == "gc" && inlineWagoGrow(32) != 528 {
					t.Fatal("growth")
				}
				return v + 1
			}
			var binding any = callback
			if kind == "HostCall" {
				binding = func(call HostCall) { call.SetI32(0, callback(call.I32(0))) }
			}
			if kind == "Caller" {
				binding = func(_ Caller, call HostCall) { call.SetI32(0, callback(call.I32(0))) }
			}
			imports.HostFunc("env", "step", binding).Params(ValI32).Results(ValI32)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, kind := range []string{"gc", "panic", "gc"} {
				action = kind
				if kind == "panic" {
					var recovered any
					func() { defer func() { recovered = recover() }(); _, _ = in.Invoke("run", 0) }()
					if recovered != "inline callback panic" {
						t.Fatalf("panic = %v", recovered)
					}
				} else {
					_, err := in.Invoke("run", 1)
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable {
						t.Fatalf("trap = %v", err)
					}
				}
				if state := in.pluginState.Load(); state != nil && state.activations.boundedID.Load() != 0 {
					t.Fatal("bounded native identity survived unwind")
				}
				action = ""
				got, err := in.Invoke("run", 0)
				if err != nil || len(got) != 1 || got[0] != 7 {
					t.Fatalf("after unwind %v, %v", got, err)
				}
			}
		})
	}
}

// Slice ABI arguments need six register spill slots, independently of the
// Wasm signature. Exercise empty, mixed-bit and maximum-size borrowed buffers.
func TestInlineWagoViewShapes(t *testing.T) {
	disabled := os.Getenv("WAGO_"+strings.ToUpper(runtime.GOARCH)+"_NO_INLINE_HOST") == "1"
	for _, n := range []int{0, 4, 64} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			types := []ValType{ValI32, ValI64, ValF32, ValF64}
			names := []string{"i32", "i64", "f32", "f64"}
			var signature, body strings.Builder
			sig := make([]ValType, n)
			args := make([]uint64, n)
			for i := range n {
				sig[i] = types[i%4]
				fmt.Fprintf(&signature, " %s", names[i%4])
				fmt.Fprintf(&body, " local.get %d", i)
				args[i] = uint64(0x123456789abcdef0 + i)
				if i%4 == 0 || i%4 == 2 {
					args[i] = uint64(uint32(args[i]))
				}
			}
			shape := ""
			if n != 0 {
				shape = "(param" + signature.String() + ") (result" + signature.String() + ")"
			}
			wat := fmt.Sprintf(`(module (import "env" "step" (func $step %s))
 (func (export "run") %s %s call $step))`, shape, shape, body.String())
			c, err := Compile(goHostSegmentConfig(), watToWasm(t, wat))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.boundedHostSegments() {
				t.Fatal("signature fixture lacks segment proof")
			}
			calls := 0
			imports := NewImports()
			callback := func(call HostCall) {
				if len(call.ParamSlots()) != n || len(call.ResultSlots()) != n {
					t.Fatal("borrowed buffer lengths")
				}
				for i, value := range call.ParamSlots() {
					// Scalar f32/i32 slots define only their low 32 bits.
					if i%4 == 0 || i%4 == 2 {
						value = uint64(uint32(value))
					}
					if value != args[i] || call.ResultSlots()[i] != 0 {
						t.Fatalf("slot %d: arg %#x, result %#x", i, value, call.ResultSlots()[i])
					}
				}
				var pcs [64]uintptr
				frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
				found := false
				for {
					frame, more := frames.Next()
					found = found || strings.Contains(frame.Function, "inlineHostViewEnter") || strings.Contains(frame.Function, "inlineHostViewContextEnter") || strings.Contains(frame.Function, "inlineHostIntegerViewContextEnter")
					if !more {
						break
					}
				}
				if !found && !disabled {
					t.Fatal("view did not use live Go owner")
				}
				if inlineWagoGrow(32) != 528 {
					t.Fatal("stack growth")
				}
				copy(call.ResultSlots(), args)
				calls++
			}
			var binding any = callback
			if n == 64 {
				// The existing large HostCall crossover uses its generic view;
				// bounded Caller views admit the full inline capacity.
				binding = func(_ Caller, call HostCall) { callback(call) }
			}
			imports.HostFunc("env", "step", binding).Params(sig...).Results(sig...)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for range 3 {
				got, err := in.Invoke("run", args...)
				if err != nil || !slices.Equal(got, args) {
					t.Fatalf("result %v, %v; want %v", got, err, args)
				}
			}
			if calls != 3 {
				t.Fatal("callback count", calls)
			}
		})
	}
}

func inlineWagoSavedHomeOffset() int {
	if runtime.GOARCH == "amd64" {
		return 8
	}
	return 80
}

// Signal profiling may interrupt both foreign Wasm and the live Go callback.
// Exercise the real engine, rather than only a handwritten native-loop probe.
func TestInlineWagoCPUProfile(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			var counter atomic.Uint64
			step := func(v int32) int32 { counter.Add(1); return v + 1 }
			var binding any = step
			if kind == "HostCall" {
				binding = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
			}
			if kind == "Caller" {
				binding = func(_ Caller, call HostCall) { call.SetI32(0, step(call.I32(0))) }
			}
			imports := NewImports()
			imports.HostFunc("env", "step", binding).Params(ValI32).Results(ValI32)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			file, err := os.CreateTemp(t.TempDir(), "cpu-profile")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := pprof.StartCPUProfile(file); err != nil {
				t.Fatal(err)
			}
			defer pprof.StopCPUProfile()
			const iterations, count = 128, 65536
			for i := 0; i < iterations; i++ {
				got, err := in.Invoke("run", count, 0)
				if err != nil || len(got) != 1 || got[0] != count {
					t.Fatalf("result %v, %v", got, err)
				}
			}
			if counter.Load() != iterations*count {
				t.Fatal("profiled callback count")
			}
		})
	}
}

// Publishing backing during a callback migrates the native lease. The live
// owner must still resume safely and dispatch subsequent callbacks correctly.
func TestInlineWagoCallerPublication(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (memory (export "memory") 1)
 (func (export "run") (param i32) (result i32)
  local.get 0 call $step call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.boundedHostSegments() {
		t.Fatal("publication fixture lacks segment proof")
	}
	for _, mode := range []string{"instance", "function", "session"} {
		t.Run(mode, func(t *testing.T) {
			var in *Instance
			calls := 0
			imports := NewImports()
			imports.HostFunc("env", "step", func(caller Caller, call HostCall) {
				if !caller.valid() {
					t.Fatal("invalid callback scope")
				}
				if calls == 0 {
					if _, err := in.ExportedMemory("memory"); err != nil {
						t.Fatal(err)
					}
					if in.usesIndependentExecution() {
						t.Fatal("publication did not revoke independent execution")
					}
				}
				if inlineWagoGrow(32) != 528 {
					t.Fatal("stack growth after publication")
				}
				call.SetI32(0, call.I32(0)+1)
				calls++
			}).Params(ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			invoke := func() ([]uint64, error) { return in.Invoke("run", 5) }
			if mode == "function" {
				invoke = func() ([]uint64, error) { return fn.Invoke(5) }
			}
			if mode == "session" {
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				invoke = func() ([]uint64, error) { return session.Invoke1(5) }
			}
			for range 2 {
				got, err := invoke()
				if err != nil || len(got) != 1 || got[0] != 7 {
					t.Fatalf("result %v, %v", got, err)
				}
			}
			if calls != 4 {
				t.Fatal("callback count", calls)
			}
		})
	}
}

func TestInlineWagoNativeConsumerCycle(t *testing.T) {
	producerCode, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	var producer, consumer *Instance
	var outerCtrl uintptr
	var nestedCalls int
	imports := NewImports()
	imports.HostFunc("env", "step", func(caller Caller, call HostCall) {
		if call.I32(0) == 1 {
			outerCtrl = offHeapSlicePtr(producer.ctrl)
			got, err := consumer.InvokeFromHost(context.Background(), caller, "run", 0)
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("consumer result %v, %v", got, err)
			}
			if offHeapSlicePtr(producer.ctrl) != outerCtrl {
				t.Fatal("producer control frame not restored")
			}
		} else {
			if offHeapSlicePtr(producer.ctrl) == outerCtrl {
				t.Fatal("native import reused the parked producer control frame")
			}
			if inlineWagoGrow(32) != 528 {
				t.Fatal("nested callback stack growth")
			}
			runtime.GC()
			nestedCalls++
		}
		call.SetI32(0, call.I32(0)+1)
	}).Params(ValI32).Results(ValI32)
	producer, err = Instantiate(producerCode, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	consumerCode := MustCompile(watToWasm(t, `(module
 (import "env" "run" (func $run (param i32) (result i32)))
 (func (export "run") (param i32) (result i32) local.get 0 call $run))`))
	defer consumerCode.Close()
	consumer, err = Instantiate(consumerCode, testImports("env.run", export))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	for range 2 {
		got, err := producer.Invoke("run", 1)
		if err != nil || len(got) != 1 || got[0] != 2 {
			t.Fatalf("producer result %v, %v", got, err)
		}
	}
	if nestedCalls != 2 {
		t.Fatalf("nested callbacks = %d; want 2", nestedCalls)
	}
}
