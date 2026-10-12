//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerSumModule(wide, fenced, nested bool) []byte {
	typ, c, add, lt, valueType := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(0x7f)
	if wide {
		typ, c, add, lt, valueType = wasm.I64, 0x42, 0x7c, 0x54, 0x7e
	}
	locals := byte(1)
	if nested {
		locals = 2
	}
	body := []byte{1, locals, valueType, 0x20, 1}
	if nested {
		body = append(body, 0x03, 0x40)
	}
	body = append(body, 0x03, 0x40, 0x20, 1)
	if fenced {
		body = append(body, 0x02, valueType, 0xfe, 3, 0)
	}
	body = append(body, 0x20, 2)
	if fenced {
		body = append(body, 0x0b)
	}
	body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b)
	if nested {
		body = append(body, 0x20, 3, c, 1, add, 0x21, 3, 0x20, 3, c, 2, lt, 0x0d, 0, 0x0b)
	}
	body = append(body, 0x20, 1, 0x20, 2, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(code)))
}

func TestIntegerSumLoopPublicExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, fenced := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/fence=%t/enabled=%t/regabi=%t", wide, fenced, enabled, regABI), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI)
						for _, nested := range []bool{false, true} {
							c, err := cfg.Compile(integerSumModule(wide, fenced, nested))
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
								for _, n := range []uint64{0, 1, 2, 3, 15, 63, 1023, 32768, 65536} {
									seed := uint64(0xfedcba9876543210)
									// An independent scalar loop checks do-while exit values, including
									// the enclosing loop's second entry with an already advanced counter.
									want, counter := seed, uint64(0)
									repeat := 1
									if nested {
										repeat = 2
									}
									for j := 0; j < repeat; j++ {
										for {
											want += counter
											counter++
											if counter >= n {
												break
											}
										}
									}
									prefix := seed
									if !wide {
										prefix = uint64(uint32(prefix))
										want = uint64(uint32(want))
									}
									got, err := in.Invoke("sum", n, seed)
									if err != nil || len(got) != 3 || got[0] != prefix || got[1] != want || got[2] != counter {
										t.Fatalf("nested=%t n=%d: %v,%v want=[%x,%x]", nested, n, got, err, want, counter)
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
