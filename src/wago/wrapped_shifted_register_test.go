//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestWrappedWideShiftPublicExecution(t *testing.T) {
	for _, count := range []int64{13, 32, 63, 77, -1} {
		for _, alias := range []byte{0, 1} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("count=%d/alias=%d/on=%t/abi=%t", count, alias, enabled, regABI), func(t *testing.T) {
						body := []byte{0, 0x20, 0, 0xa7, 0x20, 1, 0x42}
						body = append(body, wasmtest.SLEB64(count)...)
						body = append(body, 0x88, 0xa7, 0x73, 0xad, 0x21, alias, 0x20, alias, 0x20, 1-alias, 0x85, 0x0b)
						module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
						cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
						if runtime.GOARCH == "arm64" {
							cfg = cfg.WithOptimization("shifted-register-alu", enabled)
						}
						c, err := cfg.Compile(module)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, compiled := range []*Compiled{c} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							seed := uint64(0xabcdef0123456789)
							for i := 0; i < 128; i++ {
								seed ^= seed << 13
								seed ^= seed >> 7
								seed ^= seed << 17
								x, y := seed, ^seed
								if i == 0 {
									x, y = 0, 0
								}
								if i == 1 {
									x, y = ^uint64(0), ^uint64(0)
								}
								want := uint64(uint32(x) ^ uint32(y>>(uint(count)&63)))
								if alias == 0 {
									want ^= y
								} else {
									want ^= x
								}
								got, err := in.Invoke("run", x, y)
								if err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("x=%x y=%x got=%v want=%x err=%v", x, y, got, want, err)
								}
							}
						}
					})
				}
			}
		}
	}
}
