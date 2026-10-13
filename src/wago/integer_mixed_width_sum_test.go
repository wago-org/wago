//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerMixedWidthSumModule(factor uint64, shift uint64) []byte {
	body := []byte{0x23, 0, 0x03, 0x40, 0x20, 1, 0x02, 0x7f, 0x20, 2, 0xad, 0x42}
	body = append(body, wasmtest.SLEB64(int64(factor))...)
	body = append(body, 0x7e, 0x24, 0, 0x23, 0, 0xa7, 0x23, 0, 0x42)
	body = append(body, wasmtest.SLEB64(int64(shift))...)
	body = append(body, 0x88, 0xa7, 0x6a, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x23, 0, 0x0b)
	code := append([]byte{1, 1, 0x7f}, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64, wasm.I32, wasm.I32, wasm.I64}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7e, 1, 0x42, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0), wasmtest.ExportEntry("global", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...), wasmtest.Code([]byte{0x23, 0, 0x0b}))))
}

func TestIntegerMixedWidthSumExecution(t *testing.T) {
	for _, tc := range []struct{ factor, shift, coefficient uint64 }{
		{0x100000001, 32, 2}, {0x100000000, 32, 1}, {0x10000, 16, 0x10001}, {0x10000, 0, 0x20000}, {0x10000, 63, 0x10000},
	} {
		for _, enabled := range []bool{false, true} {
			for _, abi := range []bool{false, true} {
				t.Run(fmt.Sprintf("factor=%x/shift=%d/opt=%t/abi=%t", tc.factor, tc.shift, enabled, abi), func(t *testing.T) {
					c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi).Compile(integerMixedWidthSumModule(tc.factor, tc.shift))
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
						previous := uint64(0)
						bounds := []uint64{0, 1, 2, 63, 4096, 65537}
						if enabled {
							bounds = append(bounds, 0x80000000, 0xffffffff)
						}
						for _, n := range bounds {
							count := n
							if count == 0 {
								count = 1
							}
							seed := uint64(0xfffffff1)
							triangle := count * (count - 1) / 2
							sum := uint64(uint32(seed + tc.coefficient*triangle))
							last := (count - 1) * tc.factor
							got, err := in.Invoke("sum", n, seed)
							want := []uint64{previous, sum, count, last}
							if err != nil || len(got) != len(want) {
								t.Fatalf("n=%x got=%v err=%v", n, got, err)
							}
							for j := range want {
								if got[j] != want[j] {
									t.Fatalf("n=%x result%d=%x expected=%x", n, j, got[j], want[j])
								}
							}
							g, err := in.Invoke("global")
							if err != nil || len(g) != 1 || g[0] != last {
								t.Fatalf("global=%v err=%v", g, err)
							}
							previous = last
						}
					}
				})
			}
		}
	}
}

func TestIntegerMixedWidthSumNarrowGlobal(t *testing.T) {
	// An i64 reduction may publish a wrapping i32 value even when its result
	// is constant. The final store must truncate, not widen, the counter.
	body := []byte{1, 1, 0x7e, 0x03, 0x40, 0x20, 1, 0x02, 0x7e, 0x20, 2, 0xa7, 0x24, 0, 0x42, 3, 0x0b, 0x7c, 0x21, 1, 0x20, 2, 0x42, 1, 0x7c, 0x21, 2, 0x20, 2, 0x20, 0, 0x54, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x23, 0, 0x0b}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I64, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	for _, enabled := range []bool{false, true} {
		for _, abi := range []bool{false, true} {
			t.Run(fmt.Sprintf("opt=%t/abi=%t", enabled, abi), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi).Compile(mod)
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
					bounds := []uint64{0, 1, 2, 4096}
					if enabled {
						bounds = append(bounds, 0x100000001, 0x8000000000000000, ^uint64(0))
					}
					for _, n := range bounds {
						count := n
						if count == 0 {
							count = 1
						}
						seed := uint64(91)
						got, err := in.Invoke("sum", n, seed)
						want := []uint64{seed + 3*count, count, uint64(uint32(count - 1))}
						if err != nil || len(got) != len(want) {
							t.Fatalf("n=%x got=%v err=%v", n, got, err)
						}
						for j := range want {
							if got[j] != want[j] {
								t.Fatalf("n=%x result%d=%x expected=%x", n, j, got[j], want[j])
							}
						}
					}
				}
			})
		}
	}
}
