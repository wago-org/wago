//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerByteSumModule(lane byte, mask []byte) []byte {
	body := []byte{1, 1, 0x7f, 0x20, 1, 0x03, 0x40, 0x20, 1, 0x20, 2, 0xfd, 15}
	if mask != nil {
		body = append(body, 0xfd, 12)
		body = append(body, mask...)
		body = append(body, 0xfd, 14)
	}
	body = append(body, 0xfd, 22, lane, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestIntegerByteSumPublicExecution(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, abi := range []bool{false, true} {
			for _, lane := range []byte{0, 3, 15} {
				for _, index := range []int{-1, 0, 15, 16, 128, 255} {
					t.Run(fmt.Sprintf("opt=%t/abi=%t/lane=%d/index=%d", enabled, abi, lane, index), func(t *testing.T) {
						var mask []byte
						if index >= 0 {
							mask = make([]byte, 16)
							for i := range mask {
								mask[i] = byte(15 - i)
							}
							mask[lane] = byte(index)
						}
						cfg := NewRuntimeConfig().WithFeature(CoreFeatureSIMD, true).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi)
						c, err := cfg.Compile(integerByteSumModule(lane, mask))
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
							for _, bound := range []uint32{0, 1, 2, 255, 256, 257, 4096, 65535} {
								for _, seed := range []uint32{0, 0xfedcba98} {
									want := seed
									for i := uint32(0); i < max(bound, 1); i++ {
										if index < 16 {
											want += uint32(byte(i))
										}
									}
									got, err := in.Invoke("sum", uint64(bound), uint64(seed))
									if err != nil || len(got) != 3 || got[0] != uint64(seed) || got[1] != uint64(want) || got[2] != uint64(max(bound, 1)) {
										t.Fatalf("n=%d seed=%x: %v, %v; want [%d %d %d]", bound, seed, got, err, seed, want, max(bound, 1))
									}
								}
							}
						}
					})
				}
			}
		}
	}
}
