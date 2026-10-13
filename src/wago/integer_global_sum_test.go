//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerGlobalSumModule(wide, imported, mutable, fenced bool) []byte {
	typ, c, add, lt, vt := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(0x7f)
	if wide {
		typ, c, add, lt, vt = wasm.I64, 0x42, 0x7c, 0x54, 0x7e
	}
	mut := byte(0)
	if mutable {
		mut = 1
	}
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1}
	if fenced {
		body = append(body, 0x02, vt, 0xfe, 3, 0)
	}
	body = append(body, 0x23, 0)
	if fenced {
		body = append(body, 0x0b)
	}
	body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, vt}, body...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}), wasmtest.FuncType(nil, []wasm.ValType{typ})))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("g")...)
		entry = append(entry, 3, vt, mut)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))))
	if !imported {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{vt, mut, c, 41, 0x0b})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0), wasmtest.ExportEntry("get", 0, 1))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...), wasmtest.Code([]byte{0x23, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestIntegerInvariantGlobalSumPublicExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, mutable := range []bool{false, true} {
				for _, fenced := range []bool{false, true} {
					for _, enabled := range []bool{false, true} {
						for _, regABI := range []bool{false, true} {
							t.Run(fmt.Sprintf("wide=%t/imported=%t/mutable=%t/fenced=%t/opt=%t/abi=%t", wide, imported, mutable, fenced, enabled, regABI), func(t *testing.T) {
								cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithOptimization("integer-sum-loop", enabled).WithOptimization("inline", false).WithOptimization("reg-abi", regABI)
								c, err := cfg.Compile(integerGlobalSumModule(wide, imported, mutable, fenced))
								if err != nil {
									t.Fatal(err)
								}
								defer c.Close()
								// Compare initialization metadata edits against each canonical instance value;
								// the original compiled plan and codec products can retain different snapshots.
								if !imported {
									c.Globals[0].Bits = 91
								}
								for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
									for _, bits := range []uint64{91, ^uint64(0)} {
										if !imported && bits != 91 {
											continue
										}
										var imports *Imports
										if imported {
											g := NewGlobalI32(int32(bits), mutable)
											if wide {
												g = NewGlobalI64(int64(bits), mutable)
											}
											defer g.Close()
											imports = testImports("env.g", g)
										}
										in, err := Instantiate(compiled, imports)
										if err != nil {
											t.Fatal(err)
										}
										defer in.Close()
										actual, err := in.Invoke("get")
										if err != nil || len(actual) != 1 {
											t.Fatalf("global read: %v %v", actual, err)
										}
										value := actual[0]
										if imported {
											want := bits
											if !wide {
												want = uint64(uint32(want))
											}
											if value != want {
												t.Fatalf("imported global=%x want=%x", value, want)
											}
										}
										bounds := []uint64{0, 1, 2, 3, 63, 4096}
										if enabled && !mutable && !fenced {
											if wide {
												bounds = append(bounds, 0x8000000000000000, ^uint64(0))
											} else {
												bounds = append(bounds, 0x80000000, 0xffffffff)
											}
										}
										for _, n := range bounds {
											seed := uint64(0xfedcba9876543210)
											prefix := seed
											k := n
											if k == 0 {
												k = 1
											}
											want := seed + value*k
											if !wide {
												prefix = uint64(uint32(prefix))
												want = uint64(uint32(want))
											}
											got, err := in.Invoke("sum", n, seed)
											if err != nil || len(got) != 3 || got[0] != prefix || got[1] != want || got[2] != k {
												t.Fatalf("n=%x bits=%x got=%v err=%v want=[%x %x %x]", n, bits, got, err, prefix, want, k)
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
	}
}
