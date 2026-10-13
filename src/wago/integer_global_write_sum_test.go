//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerGlobalWriteSumModule(wide bool) []byte {
	typ, c, add, sub, lt, bt := wasm.I32, byte(0x41), byte(0x6a), byte(0x6b), byte(0x49), byte(0x7f)
	if wide {
		typ, c, add, sub, lt, bt = wasm.I64, 0x42, 0x7c, 0x7d, 0x54, 0x7e
	}
	// Prefix values must survive the reduction, including the old global value.
	body := []byte{0x20, 1, 0x23, 0, 0x03, 0x40, 0x20, 1, 0x02, bt,
		0x20, 2, 0x24, 0, 0x20, 2, c, 3, add, 0x24, 1, 0x23, 0, 0x24, 2, 0x23, 1, 0x24, 0, 0x23, 2, 0x24, 1, 0x23, 0, 0x23, 1, sub, 0x0b,
		add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x23, 0, 0x23, 1, 0x23, 2, 0x0b}
	code := append([]byte{1, 1}, wasm.MustEncodeValType(typ))
	code = append(code, body...)
	global := func() []byte { return []byte{wasm.MustEncodeValType(typ), 1, c, 41, 0x0b} }
	results := []wasm.ValType{typ, typ, typ, typ, typ, typ, typ}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, results), wasmtest.FuncType(nil, []wasm.ValType{typ, typ, typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))), wasmtest.Section(6, wasmtest.Vec(global(), global(), global())),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0), wasmtest.ExportEntry("globals", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...), wasmtest.Code([]byte{0x23, 0, 0x23, 1, 0x23, 2, 0x0b}))))
}

func TestIntegerGlobalWriteSumExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, abi := range []bool{false, true} {
				t.Run(fmt.Sprintf("wide=%t/opt=%t/abi=%t", wide, enabled, abi), func(t *testing.T) {
					c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi).Compile(integerGlobalWriteSumModule(wide))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, product := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						in, err := Instantiate(product)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						previous := uint64(41)
						bounds := []uint64{0, 1, 2, 63, 4096}
						if enabled {
							if wide {
								bounds = append(bounds, 0x8000000000000000, ^uint64(0))
							} else {
								bounds = append(bounds, 0x80000000, 0xffffffff)
							}
						}
						mask := ^uint64(0)
						if !wide {
							mask = 0xffffffff
						}
						for _, n := range bounds {
							count := n
							if count == 0 {
								count = 1
							}
							seed := uint64(91)
							a := (count + 2) & mask
							b := (count - 1) & mask
							want := []uint64{seed, previous, (seed + 3*count) & mask, count, a, b, b}
							got, err := in.Invoke("sum", n, seed)
							if err != nil {
								t.Fatal(err)
							}
							if len(got) != len(want) {
								t.Fatalf("got=%v", got)
							}
							for j := range want {
								if got[j] != want[j] {
									t.Fatalf("n=%x result%d=%x expected=%x", n, j, got[j], want[j])
								}
							}
							globals, err := in.Invoke("globals")
							if err != nil || len(globals) != 3 || globals[0] != a || globals[1] != b || globals[2] != b {
								t.Fatalf("published globals=%v err=%v", globals, err)
							}
							previous = a
						}
					}
				})
			}
		}
	}
}
