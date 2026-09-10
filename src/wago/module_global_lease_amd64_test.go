//go:build amd64 && !wago_precompiled

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func moduleGlobalLeaseResultBody(twoResults bool) []byte {
	body := []byte{0x01, 0x14, 0x7f, 0x41, 0x00, 0x28, 0x02, 0x00, 0x1a}
	for x := byte(0); x < 20; x++ {
		body = append(body, 0x41, x+1, 0x21, x)
	}
	body = append(body, 0x20, 0x00)
	for x := byte(1); x < 20; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	if twoResults {
		body = append(body, 0x41, 0x09)
	}
	return append(body, 0x0b)
}

func moduleGlobalLeaseCode(body []byte) []byte {
	return append(wasmtest.ULEB(uint32(len(body))), body...)
}

func moduleGlobalLeaseRegressionModule() []byte {
	hotGlobal := []byte{0x00, 0x03, 0x40}
	for range 20 {
		hotGlobal = append(hotGlobal, 0x23, 0x00, 0x1a)
	}
	hotGlobal = append(hotGlobal, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 37, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("one", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("two", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			moduleGlobalLeaseCode(moduleGlobalLeaseResultBody(false)),
			moduleGlobalLeaseCode(moduleGlobalLeaseResultBody(true)),
			moduleGlobalLeaseCode(hotGlobal),
		)),
	)
}

func TestModuleGlobalRegionalLeasePreservesResultSlotsAMD64(t *testing.T) {
	module := moduleGlobalLeaseRegressionModule()
	decoded, err := wasm.DecodeModule(module)
	if err != nil {
		t.Fatal(err)
	}
	var stats amd64.ModuleStats
	cm, err := amd64.CompileModuleWith(decoded, amd64.CompileOptions{Workers: 1, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	for i := 0; i < 2; i++ {
		if got := stats.Funcs[i].Peephole["module-global-regional-lease"]; got != 1 {
			t.Fatalf("function %d lease count = %d, want 1: %v", i, got, stats.Funcs[i].Peephole)
		}
	}

	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("one"); err != nil || len(got) != 1 || AsI32(got[0]) != 210 {
		t.Fatalf("one result = %v, %v; want [210]", got, err)
	}
	if got, err := in.Global("g"); err != nil || AsI32(got) != 37 {
		t.Fatalf("global after one result = %d, %v; want 37", AsI32(got), err)
	}
	if got, err := in.Invoke("two"); err != nil || len(got) != 2 || AsI32(got[0]) != 210 || AsI32(got[1]) != 9 {
		t.Fatalf("two results = %v, %v; want [210 9]", got, err)
	}
	if got, err := in.Global("g"); err != nil || AsI32(got) != 37 {
		t.Fatalf("global after two results = %d, %v; want 37", AsI32(got), err)
	}
}
