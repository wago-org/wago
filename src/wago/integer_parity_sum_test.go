//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerParitySumModule(inverted bool, oddA, oddB, evenA, evenB int32) []byte {
	body := []byte{0x03, 0x40, 0x20, 1, 0x20, 2, 0x41, 1, 0x71}
	if inverted {
		body = append(body, 0x45)
	}
	body = append(body, 0x04, 0x7f)
	term := func(a, b int32) {
		body = append(body, 0x20, 2, 0x41)
		body = append(body, wasmtest.SLEB32(a)...)
		body = append(body, 0x6c, 0x41)
		body = append(body, wasmtest.SLEB32(b)...)
		body = append(body, 0x6a)
	}
	term(oddA, oddB)
	body = append(body, 0x05)
	term(evenA, evenB)
	body = append(body, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, 0x7f}, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerParitySumPublicExecution(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			for _, inverted := range []bool{false, true} {
				for _, coefficients := range [][4]int32{{3, 0, 1, 7}, {-7, 19, 5, -11}, {0, -1, 0, 17}, {1 << 30, -2147483648, -2147483647, 2147483647}} {
					t.Run(fmt.Sprintf("enabled=%t/regabi=%t/inverted=%t/coefficients=%v", enabled, regABI, inverted, coefficients), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI)
						compiled, err := cfg.Compile(integerParitySumModule(inverted, coefficients[0], coefficients[1], coefficients[2], coefficients[3]))
						if err != nil {
							t.Fatal(err)
						}
						defer compiled.Close()
						for _, c := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
							in, err := Instantiate(c)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, bound := range []uint32{0, 1, 2, 3, 17, 64, 4096, 65535} {
								seed := uint32(0xfedcba98)
								want, counter := seed, uint32(0)
								for {
									branch := counter&1 != 0
									if inverted {
										branch = !branch
									}
									a, b := coefficients[2], coefficients[3]
									if branch {
										a, b = coefficients[0], coefficients[1]
									}
									want += counter*uint32(a) + uint32(b)
									counter++
									if counter >= bound {
										break
									}
								}
								got, err := in.Invoke("sum", uint64(bound), uint64(seed))
								if err != nil || len(got) != 2 || got[0] != uint64(want) || got[1] != uint64(counter) {
									t.Fatalf("bound=%d: %v, %v; want [%d %d]", bound, got, err, want, counter)
								}
							}
						}
					})
				}
			}
		}
	}
}
