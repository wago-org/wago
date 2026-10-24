//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directReferenceABITrapsModule() []byte {
	globals := make([][]byte, 8)
	dirty := []byte{0x03, 0x40}
	for g := byte(0); g < 8; g++ {
		globals[g] = wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b})
		dirty = append(dirty, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
	}
	dirty = append(dirty, 0x0b)
	target := append(append([]byte(nil), dirty...), 0x20, 0, 0xfb, 2, 0, 0, 0x20, 1, 0x6d, 0x0b)
	caller := []byte{0x41, 42, 0xfb, 0, 0, 0x20, 0, 0x10, 0, 0x0b}
	// The memory trap must precede the callee's divide-by-zero and side effects.
	argTrap := append(append([]byte(nil), dirty...), 0x41, 42, 0xfb, 0, 0, 0x20, 0, 0x28, 2, 0, 0x10, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f}, directReferenceABIFuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{2})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("argTrap", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(caller), wasmtest.Code(argTrap))),
	)
}

func TestDirectReferenceABITrapsAndGlobals(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("direct-reference-abi", enabled), directReferenceABITrapsModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for _, candidate := range []*Compiled{c, loaded} {
			for _, tc := range []struct {
				entry string
				arg   uint64
				trap  TrapCode
			}{{"run", 0, TrapDivZero}, {"argTrap", 65536, TrapLinMemOutOfBounds}} {
				in, err := Instantiate(candidate)
				if err != nil {
					t.Fatal(err)
				}
				_, err = in.Invoke(tc.entry, tc.arg)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != tc.trap {
					in.Close()
					t.Fatalf("enabled%t %s trap=%v, want%d", enabled, tc.entry, err, tc.trap)
				}
				for g := 0; g < 8; g++ {
					if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
						in.Close()
						t.Fatalf("enabled%t %s global%d=%d, want%d", enabled, tc.entry, g, got, g+1)
					}
				}
				in.Close()
			}
		}
	}
}

func TestDirectReferenceABIReturnGlobalThenCall(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 3, 0x63, 0, 0x63, 0, 0x7f, 1, 0x63, 0}, []byte{0x60, 2, 0x63, 0, 0x63, 0, 1, 0x63, 0}, directReferenceABIFuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 2, 0x04, 0x40, 0x23, 0, 0xd4, 0x0f, 0x0b, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x20, 0, 0x20, 1, 0x10, 3, 0x1a, 0x23, 0, 0xd4, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x41, 35, 0xfb, 0, 0, 0x41, 42, 0xfb, 0, 0, 0x20, 0, 0x10, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	for _, enabled := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("direct-reference-abi", enabled), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		for mode, want := range []uint64{7, 7} {
			got, err := in.Invoke("run", uint64(mode))
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("enabled%t mode%d=%v,%v want%d", enabled, mode, got, err, want)
			}
		}
		in.Close()
	}
}
