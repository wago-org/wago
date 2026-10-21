//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Contiguous packed fields make a too-wide store visible in its neighbors.
func TestGCPackedStructSetAdjacentFields(t *testing.T) {
	for _, storage := range []byte{0x78, 0x77} {
		t.Run(fmt.Sprintf("%x", storage), func(t *testing.T) {
			run := []byte{0x23, 0, 0x20, 0, 0xfb, 5, 0, 1}
			for _, field := range []byte{0, 1, 2} {
				run = append(run, 0x23, 0, 0xfb, 4, 0, field)
			}
			run = append(run, 0x0b)
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x50, 0, 0x5f, 3, storage, 1, storage, 1, storage, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x12, 0x41, 0, 0x41, 0x34, 0xfb, 0, 0, 0x24, 0, 0x0b}), wasmtest.Code(run))),
			)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			for _, bits := range []uint64{0, 0x80, 0xffff, 0xdeadbeef, 0xffffffff} {
				want := uint64(uint8(bits))
				if storage == 0x77 {
					want = uint64(uint16(bits))
				}
				out, err := in.Invoke("run", bits)
				if err != nil || len(out) != 3 || out[0] != 0x12 || out[1] != want || out[2] != 0x34 {
					t.Fatalf("bits%x: %x,%v want[12,%x,34]", bits, out, err, want)
				}
			}
		})
	}
}
