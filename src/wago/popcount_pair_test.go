//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/bits"
	"runtime"
	"testing"
)

func TestPopcountPairPublicExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				for _, sink := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/on=%t/regabi=%t/sink=%t", wide, enabled, regABI, sink), func(t *testing.T) {
						typ, pop, add := wasm.I32, byte(0x69), byte(0x6a)
						if wide {
							typ, pop, add = wasm.I64, 0x7b, 0x7c
						}
						// Retain both inputs after the reduction; sink may alias an input register.
						body := []byte{0, 0x20, 0, pop, 0x20, 1, pop, 0x20, 2, add, add}
						if sink {
							body = append(body, 0x21, 2, 0x20, 2)
						}
						body = append(body, 0x20, 0, add, 0x20, 1, add, 0x0b)
						mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
						cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
						if runtime.GOARCH == "arm64" {
							cfg = cfg.WithOptimization("popcount-pair", enabled)
						}
						c, err := cfg.Compile(mod)
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
							seed := uint64(0xf0e1d2c3b4a59687)
							for i := 0; i < 128; i++ {
								seed ^= seed << 13
								seed ^= seed >> 7
								seed ^= seed << 17
								a := seed
								b := ^seed
								acc := seed * 97
								if i == 0 {
									a, b, acc = 0, 0, 0
								}
								if i == 1 {
									a, b, acc = ^uint64(0), ^uint64(0), ^uint64(0)
								}
								count := bits.OnesCount64(a) + bits.OnesCount64(b)
								if !wide {
									count = bits.OnesCount32(uint32(a)) + bits.OnesCount32(uint32(b))
								}
								want := uint64(count) + acc + a + b
								if !wide {
									want = uint64(uint32(want))
								}
								got, err := in.Invoke("run", a, b, acc)
								if err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("a=%x b=%x acc=%x got=%v err=%v want=%x", a, b, acc, got, err, want)
								}
							}
						}
					})
				}
			}
		}
	}
}
