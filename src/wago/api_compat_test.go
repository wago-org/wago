package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicAPICompatibilityForms(t *testing.T) {
	c, err := Compile(signExtModule())
	if err != nil {
		t.Fatalf("Compile([]byte): %v", err)
	}
	in, err := Instantiate(c, Imports{})
	if err != nil {
		t.Fatalf("Instantiate(compiled, Imports): %v", err)
	}
	in.Close()
	in, err = Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate(compiled, nil): %v", err)
	}
	in.Close()

	cfg := NewRuntimeConfig().WithDeferBoundsChecks(true)
	if _, err := CompileWithConfig(cfg, signExtModule()); err != nil {
		t.Fatalf("CompileWithConfig: %v", err)
	}
}

func TestInvokeCacheKeepsAlternatingExports(t *testing.T) {
	c := MustCompile(alternatingExportsModule())
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	for i := int32(0); i < 8; i++ {
		f, err := in.Invoke("f", I32(i))
		if err != nil {
			t.Fatalf("Invoke f: %v", err)
		}
		if got := AsI32(f[0]); got != i+1 {
			t.Fatalf("f(%d) = %d, want %d", i, got, i+1)
		}
		g, err := in.Invoke("g", I32(i))
		if err != nil {
			t.Fatalf("Invoke g: %v", err)
		}
		if got := AsI32(g[0]); got != i+2 {
			t.Fatalf("g(%d) = %d, want %d", i, got, i+2)
		}
		if _, err := in.Invoke("__collect"); err != nil {
			t.Fatalf("Invoke __collect: %v", err)
		}
	}
}

func BenchmarkInvokeAlternatingExports(b *testing.B) {
	c := MustCompile(alternatingExportsModule())
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	x := I32(7)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("f", x); err != nil {
			b.Fatal(err)
		}
		if _, err := in.Invoke("g", x); err != nil {
			b.Fatal(err)
		}
		if _, err := in.Invoke("__collect"); err != nil {
			b.Fatal(err)
		}
	}
}

func alternatingExportsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("g", 0, 1),
			wasmtest.ExportEntry("__collect", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}), // local.get 0; i32.const 1; i32.add
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x02, 0x6a, 0x0b}), // local.get 0; i32.const 2; i32.add
			wasmtest.Code([]byte{0x0b}),
		)),
	)
}
