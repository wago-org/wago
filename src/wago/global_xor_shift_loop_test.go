//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func globalXorShiftModule(step, shift int64) []byte {
	b := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x02, 0x7f, 0x23, 0, 0x42}
	b = append(b, wasmtest.SLEB64(step)...)
	b = append(b, 0x7c, 0x24, 0, 0x23, 0, 0xa7, 0x23, 0, 0x42)
	b = append(b, wasmtest.SLEB64(shift)...)
	b = append(b, 0x88, 0xa7, 0x73, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x73, 0x0b)
	get := []byte{0, 0x23, 0, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})), wasmtest.Section(6, wasmtest.Vec([]byte{0x7e, 1, 0x42, 0x70, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("global", 0, 1))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...), append(wasmtest.ULEB(uint32(len(get))), get...))))
}

func TestGlobalXorShiftPublicExecution(t *testing.T) {
	for _, step := range []int64{0, 1, -1, 4294967313, -9223372036854775791} {
		for _, shift := range []int64{0, 13, 32, 63, 77, -1} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("step=%d/shift=%d/on=%t/abi=%t", step, shift, enabled, regABI), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
						if runtime.GOARCH == "arm64" {
							cfg = cfg.WithOptimization("integer-sum-loop", enabled)
						}
						c, err := cfg.Compile(globalXorShiftModule(step, shift))
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
							global := ^uint64(15)
							for _, bound := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 31, 63, 64, 65, 257, 4096} {
								initial := uint32(0xfedcba98 ^ bound)
								want, count := initial, bound
								if count == 0 {
									count = 1
								}
								for i := uint32(0); i < count; i++ {
									global += uint64(step)
									want += uint32(global) ^ uint32(global>>(uint64(shift)&63))
								}
								got, err := in.Invoke("run", uint64(bound), uint64(initial))
								if err != nil || len(got) != 1 || got[0] != uint64(want^count) {
									t.Fatalf("bound=%d got=%v want=%x err=%v", bound, got, want^count, err)
								}
								g, err := in.Invoke("global")
								if err != nil || len(g) != 1 || g[0] != global {
									t.Fatalf("bound=%d global=%v want=%x err=%v", bound, g, global, err)
								}
							}
						}
					})
				}
			}
		}
	}
}
