//go:build linux && amd64 && wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func guardedZeroMemoryGrowModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x00, 0x01})), // memory 0 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x0b}),       // memory.grow 1
			wasmtest.Code([]byte{0x41, 0x00, 0x2d, 0x00, 0x00, 0x0b}), // i32.load8_u 0
		)),
	)
}

func TestGuardedGrowCommitsNon64KAlignedReservations(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, guardedZeroMemoryGrowModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	// Linux only guarantees 4 KiB mmap alignment. Keeping several reservations
	// live makes this exercise addresses with different 64 KiB residues instead
	// of repeatedly reusing the one-slot guarded-memory cache.
	const instances = 16
	live := make([]*Instance, 0, instances)
	defer func() {
		for _, in := range live {
			_ = in.Close()
		}
	}()
	for i := 0; i < instances; i++ {
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %d: %v", i, err)
		}
		live = append(live, in)
	}
	for i, in := range live {
		result, err := in.Invoke("grow")
		if err != nil {
			t.Fatalf("grow %d: %v", i, err)
		}
		if got := AsI32(result[0]); got != 0 {
			t.Fatalf("grow %d returned %d, want previous size 0", i, got)
		}
		result, err = in.Invoke("load")
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
		if got := AsI32(result[0]); got != 0 {
			t.Fatalf("load %d = %d, want zero-filled byte", i, got)
		}
	}
}
