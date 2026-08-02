//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func deadGCConstructorTreeModule(t *testing.T) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x63, 0x00, 0x00}, // immutable (ref null 0)
		[]byte{0x7f, 0x00},       // immutable i32
	)...)
	body := []byte{
		0x41, 0x2a, // hidden function result: 42
		0x41, 0x01, 0x41, 0x02,
		0xfb, 0x08, 0x00, 0x02, // array.new_fixed type 0, two elements
		0x41, 0x16,
		0xfb, 0x00, 0x01, // struct.new type 1
		0x1a, // drop the complete constructor tree
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
			structType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode dead constructor tree: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate dead constructor tree: %v", err)
	}
	return m
}

func TestDeadGCConstructorTreeElimination(t *testing.T) {
	m := deadGCConstructorTreeModule(t)
	compile := func() *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			GCArrayHelpers:  true,
			Stats:           &stats,
		}); err != nil {
			t.Fatalf("compile dead constructor tree: %v", err)
		}
		return stats.Funcs[0]
	}

	saved := deadGCNewEnabled
	defer func() { deadGCNewEnabled = saved }()
	deadGCNewEnabled = true
	on := compile()
	if got := on.Peephole["gc-dead-new"]; got != 2 {
		t.Fatalf("gc-dead-new = %d, want 2 (all: %v)", got, on.Peephole)
	}
	if got := on.Calls[callKindHostSync]; got != 0 {
		t.Fatalf("optimized synchronous helper calls = %d, want 0", got)
	}

	deadGCNewEnabled = false
	off := compile()
	if got := off.Peephole["gc-dead-new"]; got != 0 {
		t.Fatalf("disabled gc-dead-new = %d, want 0", got)
	}
	if got := off.Calls[callKindHostSync]; got != 2 {
		t.Fatalf("unoptimized synchronous helper calls = %d, want 2", got)
	}
}
