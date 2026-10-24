//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcScalarAdapterRootModule(caller bool) []byte {
	types := wasmtest.Section(1, wasmtest.Vec(
		[]byte{0x5f, 1, 0x7f, 0},
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	))
	if !caller {
		return wasmtest.Module(types,
			wasmtest.Section(3, []byte{1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("collect", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))),
		)
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("collect")...), 0, 1)
	body := []byte{1, 1, 0x63, 0, // local (ref null 0)
		0x41, 42, 0xfb, 0, 0, 0x21, 0, // local = struct.new(42)
		0x10, 0, // scalar cross-instance call
		0x20, 0, 0xfb, 2, 0, 0, 0x0b, // return local.field
	}
	return wasmtest.Module(types,
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, []byte{1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCScalarAdapterPreservesNativeCallerRoot(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	compile := func(caller bool) *Module {
		t.Helper()
		code, err := Compile(gcScalarAdapterRootModule(caller))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { code.Close() })
		mod, err := rt.Module(code)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { mod.Close() })
		return mod
	}
	config := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	provider, err := rt.Instantiate(context.Background(), compile(false), WithGC(config))
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	export, err := provider.ExportedFunc("collect")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := rt.Instantiate(context.Background(), compile(true), WithGC(config), WithImports(NewImports().Function("env", "collect", export)))
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	if caller.gc != provider.gc {
		t.Fatal("instances must share the collector")
	}
	got, err := caller.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("run = %v, %v; want [42]", got, err)
	}
}
