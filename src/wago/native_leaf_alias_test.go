//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func nativeLeafAliasModule() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, []byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x10, 1, 0x0b}))))
}

func TestNativeLeafAliasesAdmissionAndValues(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("native-leaf-host", enabled).Compile(nativeLeafAliasModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for index, product := range []*Compiled{c} {
			in, err := Instantiate(product, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrement)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if admitted := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; admitted != (enabled && index == 0) {
				t.Fatalf("enabled=%t codec=%d native=%t", enabled, index, admitted)
			}
			if in.boundedHostSegments() && in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
				t.Fatal("native alias treated as Go safe point")
			}
			for _, x := range []int32{0, -1, 2147483647, -2147483648} {
				got, err := in.Invoke("run", I32(x))
				if err != nil || len(got) != 1 || got[0] != I32(x+2) {
					t.Fatalf("x=%d got=%v err=%v", x, got, err)
				}
			}
		}
		calls := 0
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", func(x int32) int32 { calls++; return x + 5 })})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
			t.Fatal("mixed effectful callback admitted")
		}
		got, err := in.Invoke("run", 7)
		if err != nil || got[0] != 13 || calls != 1 {
			t.Fatalf("mixed=%v err=%v calls=%d", got, err, calls)
		}
	}
}

//go:noinline
func capturedNativeLeafAlias(delta int32) func(int32) int32 {
	return func(x int32) int32 { return x + delta }
}

func TestNativeLeafAliasesRejectDifferentBodiesAndCapturedState(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := NewRuntimeConfig().Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for name, callbacks := range map[string][2]func(int32) int32{
		"different-pure-bodies":       {nativeLeafIncrement, nativeLeafIncrementFive},
		"same-code-distinct-captures": {capturedNativeLeafAlias(1), capturedNativeLeafAlias(5)},
	} {
		t.Run(name, func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", callbacks[0], "env.b", callbacks[1])})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
				t.Fatal("nonalias or captured body admitted")
			}
			got, err := in.Invoke("run", 7)
			if err != nil || len(got) != 1 || got[0] != 13 {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
}

func TestNativeLeafAliasesLoopScheduling(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, []byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x20, 1, 0x10, 0, 0x10, 1, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}))))
	testNativeLeafLoopScheduling(t, module, testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrement))
}

func TestNativeLeafOwnedAliasesLifetime(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	owner := NewRuntime()
	defer owner.Close()
	ref, err := owner.NewHostFuncRef(nativeLeafIncrement, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	c, err := owner.Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := owner.Instantiate(context.Background(), c, WithImports(NewImports().Function("env", "a", ref).Function("env", "b", ref)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("owned aliases did not lower")
	}
	got, err := in.Invoke("run", 7)
	if err != nil || got[0] != 9 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	// Logical close preserves the references already retained by this instance.
	ref.Close()
	got, err = in.Invoke("run", 7)
	if err != nil || got[0] != 9 {
		t.Fatalf("retained aliases got=%v err=%v", got, err)
	}
}

func TestNativeLeafAliasesPreserveInstanceContext(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("native-leaf-host", enabled).Compile(nativeLeafAliasModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for index, code := range []*Compiled{c} {
			in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrement)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			ptr := in.jm.CaptureInstanceContext().ImportDispatch
			cells := unsafe.Slice((*byte)(offHeapPtr(ptr)), 2*wruntime.ImportDispatchEntryBytes)
			for cell := 0; cell < 2; cell++ {
				word := binary.LittleEndian.Uint64(cells[cell*wruntime.ImportDispatchEntryBytes+wruntime.ImportDispatchCallerContextOffset:])
				if tagged := word&wruntime.ImportDispatchCallerNativeLeafTag != 0; tagged != (enabled && index == 0 && !codeProfileEnabled) {
					t.Fatalf("native context contract=%t enabled=%t artifact=%d", tagged, enabled, index)
				}
			}
		}
	}
}

func TestNativeLeafImportPreservesLocalsGlobalsAndSharedMemory(t *testing.T) {
	if !platformCoreFeatures().IsEnabled(CoreFeatureThreads) {
		t.Skip("threads are unavailable on this platform")
	}
	module := watToWasm(t, `(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (memory 1 1)
  (global $g (mut i32) (i32.const 7))
  (func (export "run") (param $n i32) (result i32)
   (local $i i32) (local $a i32) (local $b i32) (local $c i32) (local $d i32)
   (local $e i32) (local $f i32) (local $h i32) (local $j i32)
   (local.set $a (i32.add (local.get $n) (i32.const 1)))
   (local.set $b (i32.add (local.get $n) (i32.const 2)))
   (local.set $c (i32.add (local.get $n) (i32.const 3)))
   (local.set $d (i32.add (local.get $n) (i32.const 4)))
   (local.set $e (i32.add (local.get $n) (i32.const 5)))
   (local.set $f (i32.add (local.get $n) (i32.const 6)))
   (local.set $h (i32.add (local.get $n) (i32.const 7)))
   (local.set $j (i32.add (local.get $n) (i32.const 8)))
   (loop $loop
    (i32.store (i32.const 0) (local.get $i))
    (global.set $g (i32.add (global.get $g) (call $step (local.get $i))))
    (local.set $i (i32.add (local.get $i) (i32.const 1)))
    (br_if $loop (i32.lt_u (local.get $i) (local.get $n))))
   (i32.add (global.get $g) (i32.add (i32.load (i32.const 0))
    (i32.add (local.get $a) (i32.add (local.get $b) (i32.add (local.get $c)
    (i32.add (local.get $d) (i32.add (local.get $e) (i32.add (local.get $f)
    (i32.add (local.get $h) (local.get $j))))))))))))`)
	// The standard WAT helper leaves the optional threads parser disabled.
	// Change only the single memory section's flags from max-present to shared.
	memorySection := []byte{5, 4, 1, 1, 1, 1}
	if bytes.Count(module, memorySection) != 1 {
		t.Fatal("unexpected memory section")
	}
	module = bytes.Replace(module, memorySection, []byte{5, 4, 1, 3, 1, 1}, 1)
	for _, regabi := range []bool{false, true} {
		for _, cache := range []bool{false, true} {
			cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regabi).WithOptimization("shared-memory-base", cache)
			c, err := Compile(cfg, module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, native := range []bool{false, true} {
				calls := 0
				var callback any = nativeLeafIncrement
				if !native {
					callback = func(v int32) int32 { calls++; return v + 1 }
				}
				in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", callback)})
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []uint64{88, 98} {
					got, err := in.Invoke("run", 4)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("regabi=%t cache=%t native=%t: got %v,%v want %d", regabi, cache, native, got, err, want)
					}
				}
				in.Close()
				if !native && calls != 8 {
					t.Fatalf("calls=%d", calls)
				}
			}
		}
	}
}

func TestNativeLeafImportPreservesFloatLocalsAcrossBindings(t *testing.T) {
	module := watToWasm(t, `(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (import "env" "alias" (func (param i32) (result i32)))
  (memory 1)
  (func (export "run") (param $x f64) (param $n i32) (result f64)
   (local $a f64) (local $b f64) (local $c f64) (local $d f64)
   (local.set $a (f64.add (local.get $x) (f64.const 1)))
   (local.set $b (f64.add (local.get $x) (f64.const 2)))
   (local.set $c (f64.add (local.get $x) (f64.const 3)))
   (local.set $d (f64.add (local.get $x) (f64.const 4)))
   (local.set $n (call $step (local.get $n)))
   (f64.add (f64.add (local.get $a) (local.get $b))
    (f64.add (f64.add (local.get $c) (local.get $d))
     (f64.convert_i32_s (local.get $n))))))`)
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, code := range []*Compiled{c} {
		for _, native := range []bool{false, true} {
			calls := 0
			var callback any = nativeLeafIncrement
			if !native {
				callback = func(v int32) int32 { calls++; return v + 1 }
			}
			in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", callback, "env.alias", callback)})
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range []float64{1.5, -3, 123.25} {
				got, err := in.Invoke("run", F64(x), I32(7))
				if err != nil || len(got) != 1 || got[0] != F64(4*x+18) {
					t.Fatalf("native=%t x=%v: got %v,%v", native, x, got, err)
				}
			}
			in.Close()
			if !native && calls != 3 {
				t.Fatalf("Go callback calls=%d", calls)
			}
		}
	}
}

func TestNativeLeafAliasPairArgumentResultOverlap(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	imp := func(name string) []byte {
		return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0, 0)
	}
	body := []byte{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x10, 0, 0x21, 2,
		0x20, 2, 0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x10, 1, 0x6a, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp("a"), imp("b"))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for index, code := range []*Compiled{c} {
		for _, native := range []bool{false, true} {
			calls := 0
			var callback any = nativeLeafPolynomial
			if !native {
				callback = func(a, b int32) int32 { calls++; return nativeLeafPolynomial(a, b) }
			}
			in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.a", callback, "env.b", callback)})
			if err != nil {
				t.Fatal(err)
			}
			if !codeProfileEnabled && (in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0) != (native && index == 0) {
				t.Fatal("unexpected native pair admission")
			}
			state := uint32(0x9e3779b9)
			for i := 0; i < 1024; i++ {
				state = state*1664525 + 1013904223
				a := int32(state)
				state = state*1664525 + 1013904223
				b := int32(state)
				want := I32(nativeLeafPolynomial(a, b) + nativeLeafPolynomial(a+1, b))
				got, err := in.Invoke("run", I32(a), I32(b))
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("native=%t run(%d,%d)=%v,%v want %d", native, a, b, got, err, want)
				}
			}
			in.Close()
			if !native && calls != 2048 {
				t.Fatalf("Go callback calls=%d", calls)
			}
		}
	}
}

func fixedSharedHostLoopModule(trap bool) []byte {
	body := []byte{
		0x03, 0x40,
		0x41, 0, 0x20, 2, 0x36, 2, 0,
		0x20, 1, 0x41, 0, 0x28, 2, 0, 0x10, 0, 0x6a, 0x21, 1,
		0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 0, 0x49, 0x0d, 0,
		0x0b, 0x20, 1, 0x0b,
	}
	if trap {
		body = []byte{0x20, 0, 0x10, 0, 0x1a, 0x41, 0x7f, 0x28, 2, 0, 0x0b}
	}
	raw := append([]byte{1, 2, 0x7f}, body...)
	code := append(wasmtest.ULEB(uint32(len(raw))), raw...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "step", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(code, wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x0b}))),
	)
}

func TestNativeLeafSharedCodeSurvivesInstanceClose(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := NewRuntimeConfig().Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	instances := make([]*Instance, 2)
	for i := range instances {
		instances[i], err = Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrement)})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].Close()
		if instances[i].executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
			t.Fatal("native callback was not admitted")
		}
	}
	target := func(in *Instance) uint64 {
		ptr := in.jm.CaptureInstanceContext().ImportDispatch
		cell := unsafe.Slice((*byte)(offHeapPtr(ptr)), wruntime.ImportDispatchEntryBytes)
		return binary.LittleEndian.Uint64(cell[wruntime.ImportDispatchCodePtrOffset:])
	}
	if target(instances[0]) != target(instances[1]) {
		t.Fatal("instances did not share immutable callback code")
	}
	instances[0].Close()
	for _, x := range []int32{0, 41, -1, 2147483647, -2147483648} {
		got, err := instances[1].Invoke("run", I32(x))
		if err != nil || len(got) != 1 || got[0] != I32(x+2) {
			t.Fatalf("retained instance x=%d: %v,%v", x, got, err)
		}
	}
}
