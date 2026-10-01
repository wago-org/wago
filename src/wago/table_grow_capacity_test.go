//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func tableGrowCapacityModule(globals int, null bool) []byte {
	var grow []byte
	var globalEntries [][]byte
	for g := 0; g < globals; g++ {
		globalEntries = append(globalEntries, wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))
		grow = append(grow, 0x03, 0x40)
		for range 20 {
			grow = append(grow, 0x23, byte(g), 0x24, byte(g))
		}
		grow = append(grow, 0x0b)
	}
	if null {
		grow = append(grow, 0xd0, 0x70)
	} else {
		grow = append(grow, 0xd2, 0)
	}
	grow = append(grow, 0x20, 0, 0xfc, 15, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{0}, []byte{1}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 1, 4})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(6, wasmtest.Vec(globalEntries...)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 1), wasmtest.ExportEntry("size", 0, 2),
			wasmtest.ExportEntry("is_null", 0, 3), wasmtest.ExportEntry("call", 0, 4),
		)),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})), // declare ref.func 0
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code(grow),
			wasmtest.Code([]byte{0xfc, 16, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x11, 0, 0, 0x0b}),
		)),
	)
}

func TestTableGrowWithPinnedGlobals(t *testing.T) {
	for _, globals := range []int{0, 7} {
		for _, null := range []bool{false, true} {
			t.Run(fmt.Sprintf("globals_%d/null_%t", globals, null), func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), tableGrowCapacityModule(globals, null))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				in, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				invoke := func(name string, want uint64, args ...uint64) {
					t.Helper()
					got, err := in.Invoke(name, args...)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("%s%v = %v, %v; want %d", name, args, got, err, want)
					}
				}
				invoke("grow", 1, 0)
				invoke("grow", 1, 1)
				invoke("size", 2)
				invoke("grow", 2, 0)
				invoke("grow", 0xffffffff, 0xffffffff)
				invoke("size", 2)
				invoke("grow", 2, 2)
				invoke("grow", 0xffffffff, 1)
				invoke("size", 4)
				wantNull := uint64(0)
				if null {
					wantNull = 1
				}
				for index := uint64(1); index < 4; index++ {
					invoke("is_null", wantNull, index)
					if !null {
						invoke("call", 42, index)
					}
				}
			})
		}
	}
}
