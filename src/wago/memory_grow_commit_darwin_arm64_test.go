//go:build darwin && arm64 && wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGuardedMemoryGrowCommitIsFunctionLocal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("plain", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x0b}),
		)),
	)
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !guardedMemoryGrowEntry(compiled.InternalEntry[0]) || !guardedMemoryGrowEntry(compiled.InternalEntry[1]) || guardedMemoryGrowEntry(compiled.InternalEntry[2]) {
		t.Fatalf("guarded grow facts = %t/%t/%t, want true/true/false", guardedMemoryGrowEntry(compiled.InternalEntry[0]), guardedMemoryGrowEntry(compiled.InternalEntry[1]), guardedMemoryGrowEntry(compiled.InternalEntry[2]))
	}

	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	plain, err := in.WasmFunc("plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Invoke(); err != nil {
		t.Fatal(err)
	}
	if got := in.guardCommitPages; got != 2 {
		t.Fatalf("plain call changed guarded commit watermark to %d, want 2", got)
	}
	grow, err := in.WasmFunc("grow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grow.Invoke(); err != nil {
		t.Fatal(err)
	}
	if got := in.guardCommitPages; got != 3 {
		t.Fatalf("grow call guarded commit watermark = %d, want 3", got)
	}
}

func TestGuardedMemoryGrowCommitAfterStart(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x1a, 0x0b}),
		)),
	)
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := in.guardCommitPages; got != 3 {
		t.Fatalf("start grow guarded commit watermark = %d, want 3", got)
	}
}
