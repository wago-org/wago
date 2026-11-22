//go:build ((linux || darwin || windows) && (amd64 || arm64) && !tinygo) || (linux && amd64 && tinygo)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

//go:norace
//go:noinline
func nativeLeafIncrement(v int32) int32 { return v + 1 }

func TestNativeScalarLeafAdmissionAndValues(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callback execution")
	}
	c, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("pure callback was not lowered")
	}
	if in.boundedHostSegments() {
		t.Fatal("native callback treated as Go safe point")
	}
	for _, v := range []int32{0, 1, -1, -2147483648, 2147483647} {
		got, err := in.Invoke("g", I32(v))
		if err != nil || len(got) != 1 || got[0] != I32(v+1) {
			t.Fatalf("g(%d) = %v, %v", v, got, err)
		}
	}
}

//go:norace
//go:noinline
func nativeLeafPolynomial(a, b int32) int32 { return a*b + a - b }

func TestNativeScalarLeafRandomizedPairAndFallback(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	c, err := Compile(NewRuntimeConfig(), returningImportModule(sig, []byte{0, 0x20, 0, 0x20, 1, 0x10, 0, 0x0b}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafPolynomial)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("polynomial not admitted")
	}
	state := uint32(0x9e3779b9)
	for i := 0; i < 2048; i++ {
		state = state*1664525 + 1013904223
		a := int32(state)
		state = state*1664525 + 1013904223
		b := int32(state)
		want := I32(nativeLeafPolynomial(a, b))
		got, err := in.Invoke("g", I32(a), I32(b))
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("g(%d,%d) = %v,%v; want %d", a, b, got, err, want)
		}
	}
	calls := 0
	fallback, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", func(a, b int32) int32 { calls++; return a*b + a - b })})
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	if fallback.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
		t.Fatal("side-effecting callback admitted")
	}
	got, err := fallback.Invoke("g", 7, 9)
	if err != nil || len(got) != 1 || got[0] != 61 || calls != 1 {
		t.Fatalf("fallback = %v,%v; calls %d", got, err, calls)
	}
}

func TestNativeScalarLeafCallbackLoop(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{
		1, 1, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0, 0x45, 0x0d, 1,
		0x20, 1, 0x10, 0, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
		0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b,
	}
	compiled, err := Compile(NewRuntimeConfig(), returningImportModule(sig, body))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	calls := 0
	fallback := func(v int32) int32 {
		calls++
		return v + 1
	}
	for _, callback := range []struct {
		name string
		fn   func(int32) int32
		leaf bool
	}{{"native", nativeLeafIncrement, true}, {"captured", fallback, false}} {
		t.Run(callback.name, func(t *testing.T) {
			instance, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.f", callback.fn)})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if got := instance.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; got != callback.leaf {
				t.Fatalf("native leaf = %t, want %t", got, callback.leaf)
			}
			run, err := instance.WasmFunc("g")
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 3, 257} {
				before := calls
				got, err := run.Invoke(n)
				if err != nil || len(got) != 1 || got[0] != n {
					t.Fatalf("run(%d) = %v, %v", n, got, err)
				}
				if !callback.leaf && calls-before != int(n) {
					t.Fatalf("run(%d) made %d Go callbacks", n, calls-before)
				}
			}
		})
	}
}
