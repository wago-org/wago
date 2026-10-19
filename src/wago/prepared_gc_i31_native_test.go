//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCI31ReductionGoEntry(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile entry")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcI31ReductionModule(false, 30, 1, 0x49))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.gcI31Induction {
		t.Fatal("compiler arithmetic proof missing")
	}
	native, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	native.gcI31Induction = false
	for _, n := range []uint64{0, 1, 7, 64, 4096, 0x7fffffff, 0x80000000, 0x80000001, 0xffffffff, 0x12345678ffffffff} {
		out, e := fn.Invoke(n)
		if e != nil {
			t.Fatal(e)
		}
		got := out[0]
		want, e := native.Invoke(n)
		if e != nil || got != want[0] {
			t.Fatalf("n=%x Go=%d native=%v err=%v", n, got, want, e)
		}
	}
	rt.RequestInterrupt(in.trap)
	_, err = fn.Invoke(7)
	requireInterruptedTrap(t, err)
	if in.invocationState.word.Load() != 0 {
		t.Fatal("interruption retained admission")
	}
	if out, err := fn.Invoke(7); err != nil || out[0] != 21 {
		t.Fatalf("recovery: %v %v", out, err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err := decoded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	di, err := Instantiate(&decoded, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer di.Close()
	df, err := di.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if df.gcI31Induction {
		t.Fatal("codec retained compiler-only arithmetic proof")
	}
	if out, err := df.Invoke(7); err != nil || out[0] != 21 {
		t.Fatalf("decoded fallback: %v %v", out, err)
	}
}

func TestGCI31ReductionGoRollback(t *testing.T) {
	for _, knob := range []string{"reg-abi", "prepared-direct-entry", "prepared-bounded-entry"} {
		t.Run(knob, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization(knob, false), gcI31ReductionModule(false, 30, 1, 0x49))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if fn.gcI31Induction {
				t.Fatal("disabled optimization retained arithmetic proof")
			}
			if out, err := fn.Invoke(7); err != nil || out[0] != 21 {
				t.Fatalf("fallback: %v %v", out, err)
			}
		})
	}
}

func TestGCI31ReductionGoParallelProofs(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile entry")
	}
	var bodies [2][]byte
	for i := range bodies {
		m, err := wasm.DecodeModule(gcI31ReductionModule(i != 0, 30, 1, 0x49))
		if err != nil {
			t.Fatal(err)
		}
		bodies[i] = append([]byte{1, 2, 0x7f}, m.Code[0].BodyBytes...)
	}
	var funcs, codes, exports [][]byte
	for i := 0; i < 8; i++ {
		body := bodies[i&1]
		funcs = append(funcs, []byte{0})
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("run%d", i), 0, uint32(i)))
	}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(codes...)))
	for _, workers := range []int{1, 4} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithFunctionWorkers(workers), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		for i := 0; i < 8; i++ {
			fn, err := in.WasmFunc(fmt.Sprintf("run%d", i))
			if err != nil {
				t.Fatal(err)
			}
			if fn.gcI31Induction != (i&1 == 0) {
				t.Fatalf("workers=%d function=%d proof=%v", workers, i, fn.gcI31Induction)
			}
			if out, err := fn.Invoke(7); err != nil || out[0] != 21 {
				t.Fatalf("result %v %v", out, err)
			}
		}
	}
}

func TestGCI31ReductionGoAdmissionFallback(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profile entry")
	}
	for _, mode := range []string{"revoked", "shared", "borrowed", "closed", "arity"} {
		t.Run(mode, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcI31ReductionModule(false, 30, 1, 0x49))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if !fn.gcI31Induction {
				t.Fatal("arithmetic proof missing")
			}
			state := in.ensurePluginState()
			switch mode {
			case "revoked":
				state.invokeMu.revokeFast()
			case "shared":
				in.markNativeControlShared()
			case "borrowed":
				state.guestStorageBorrow.Store(1)
				defer state.guestStorageBorrow.Store(0)
			case "closed":
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var out []uint64
			if mode == "arity" {
				out, err = fn.Invoke()
			} else {
				out, err = fn.Invoke(7)
			}
			if mode == "closed" || mode == "borrowed" || mode == "arity" {
				if err == nil {
					t.Fatalf("%s accepted: %v", mode, out)
				}
			} else if err != nil || len(out) != 1 || out[0] != 21 {
				t.Fatalf("%s fallback: %v %v", mode, out, err)
			}
			if got := in.invocationState.Load(); got&^instanceInvocationClosed != 0 {
				t.Fatalf("retained invocation: %d", got)
			}
			if state.invokeMu.state.Load()&(invocationGateHeld|invocationGateFast) != 0 {
				t.Fatal("retained gate")
			}
		})
	}
}
