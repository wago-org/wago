//go:build wago_guardpage && (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestSignalIndexedMemoryGrowCommitsWholeWasmPageThroughPrimaryOwner(t *testing.T) {
	requireCompleteCore3Backend(t)
	// Exercise the final byte of the grown page. ARM64 handlers commit a 64 KiB
	// range, so this catches reservations whose linear-memory base is not aligned
	// to that range rather than only checking the first byte after memory.grow.
	addr := append([]byte{0x41}, wasmtest.SLEB32(2*65536-1)...)
	body := []byte{0x41, 0x01, 0x40, 0x01, 0x1a} // grow memory 1 by one; drop old size
	body = append(body, addr...)
	body = append(body, 0x2d, 0x40, 0x01, 0x00, 0x0b) // i32.load8_u memory 1
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x01, 0x02},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow_load", 0, 0),
			wasmtest.ExportEntry("memory1", 2, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := cfg.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("grow_load")
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("grow then load final byte of indexed guarded memory = %v, %v; want [0]", got, err)
	}
	memory1, err := in.ExportedMemory("memory1")
	if err != nil {
		t.Fatalf("export memory1: %v", err)
	}
	if got := len(memory1.Bytes()); got != 2*65536 {
		t.Fatalf("grown indexed guarded memory length = %d, want %d", got, 2*65536)
	}
}
