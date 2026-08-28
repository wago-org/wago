package dragline

import (
	"runtime"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompilerEmitsCompactSelectionOnHostTarget(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("Dragline native emission requires amd64 or arm64")
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x02, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target := corecompiler.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	full, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	compact, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, SelectedFunctions: []uint32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(compact.Entry) != 2 || compact.Entry[0] != 0 || compact.InternalEntry[0] != 0 || compact.Entry[1] == 0 || compact.InternalEntry[1] == 0 {
		t.Fatalf("compact entries=%v internal=%v", compact.Entry, compact.InternalEntry)
	}
	if len(compact.Code) >= len(full.Code) {
		t.Fatalf("compact/full native bytes = %d/%d", len(compact.Code), len(full.Code))
	}
}
