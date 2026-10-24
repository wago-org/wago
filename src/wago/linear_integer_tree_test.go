//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"testing"
)

func TestLinearIntegerTreePublicExecution(t *testing.T) {
	for _, coeff := range [][7]int32{{7, 11, 13, -17, 19, 2, 0}, {0x40000000, -5, -1, 0, 1, 2, 17}, {3, 5, -12, -20, 0, 2, 17}, {1, 1, -6, -3, 1, 2, 9}, {-77, 31, 11, -9, 53, 37, -123}} {
		for _, enabled := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				for _, captured := range []bool{false, true} {
					t.Run(fmt.Sprintf("coeff=%v/on=%t/abi=%t/captured=%t", coeff, enabled, regABI, captured), func(t *testing.T) {
						body := []byte{0, 0x03, 0x7f}
						if captured {
							body = append(body, 0x20, 0, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0)
						}
						term := func(index byte, c int32) {
							body = append(body, 0x20, index, 0x41)
							body = append(body, wasmtest.SLEB32(c)...)
							body = append(body, 0x6c)
						}
						term(0, coeff[0])
						term(1, coeff[1])
						body = append(body, 0x6a, 0x41)
						body = append(body, wasmtest.SLEB32(coeff[5])...)
						body = append(body, 0x74)
						term(0, coeff[2])
						term(1, coeff[3])
						body = append(body, 0x6a, 0x6a)
						term(0, coeff[4])
						body = append(body, 0x6a, 0x41)
						body = append(body, wasmtest.SLEB32(coeff[6])...)
						body = append(body, 0x6a)
						// Force a destination alias, then retain the other source after lowering.
						body = append(body, 0x21, 0, 0x20, 0, 0x20, 1, 0x6a)
						if captured {
							body = append(body, 0x6a)
						}
						body = append(body, 0x0b, 0x0b)
						module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
						cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
						if runtime.GOARCH == "arm64" {
							cfg = cfg.WithOptimization("linear-integer-tree", enabled)
						}
						c, err := cfg.Compile(module)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							seed := uint32(0xabcdef01)
							for i := 0; i < 128; i++ {
								seed ^= seed << 13
								seed ^= seed >> 17
								seed ^= seed << 5
								x, y := seed, ^seed
								if i == 0 {
									x, y = 0, 0
								}
								if i == 1 {
									x, y = 0xffffffff, 0xffffffff
								}
								if i == 2 {
									x, y = 0x80000000, 0x7fffffff
								}
								old := x
								if captured {
									x++
								}
								want := ((uint32(coeff[0])*x + uint32(coeff[1])*y) << (uint32(coeff[5]) & 31)) + uint32(coeff[2])*x + uint32(coeff[3])*y + uint32(coeff[4])*x + uint32(coeff[6]) + y
								if captured {
									want += old
								}
								got, err := in.Invoke("run", uint64(old), uint64(y))
								if err != nil || len(got) != 1 || got[0] != uint64(want) {
									t.Fatalf("x=%x y=%x got=%v err=%v want=%x", old, y, got, err, want)
								}
							}
						}
					})
				}
			}
		}
	}
}
