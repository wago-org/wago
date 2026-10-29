//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Each probe exposes both 64-bit words of its result, or writes memory and
// returns zeros. The ordinary unshared path supplies a differential oracle for
// the shared directory path, including exact float/vector bits and trap codes.
func threadsMemoryAccessModule(op byte, simd, shared, imported bool, offset uint32) []byte {
	return threadsMemoryAccessModuleForAddress(op, simd, shared, imported, uint64(offset), false)
}

func threadsMemoryAccessModuleForAddress(op byte, simd, shared, imported bool, offset uint64, address64 bool) []byte {
	body := []byte{0x20, 0} // dynamic address
	locals := []byte{0}
	store := op >= 0x36 && op <= 0x3e
	if simd {
		store = op == 11 || op >= 88 && op <= 91
		locals = []byte{1, 1, 0x7b} // one v128 local
		if store || op >= 84 && op <= 87 {
			body = append(body, 0xfd, 12)
			body = append(body, []byte{0x80, 0x7f, 0xff, 0x01, 0x33, 0x88, 0x00, 0xaa, 0x44, 0xdd, 0xee, 0x99, 0x7e, 0x55, 0x66, 0xbb}...)
		}
		body = append(body, 0xfd, op, 0)
		body = append(body, threadsULEB64(offset)...)
		if op >= 84 && op <= 91 {
			body = append(body, 1) // exercise a nonzero lane
		}
		if !store {
			body = append(body, 0x22, 2, 0xfd, 29, 0, 0x20, 2, 0xfd, 29, 1)
		}
	} else {
		if store {
			body = append(body, 0x20, 1) // dynamic i64 bits
			if op == 0x36 || op == 0x38 || op == 0x3a || op == 0x3b {
				body = append(body, 0xa7) // i32.wrap_i64
			}
			if op == 0x38 {
				body = append(body, 0xbe) // f32.reinterpret_i32
			} else if op == 0x39 {
				body = append(body, 0xbf) // f64.reinterpret_i64
			}
		}
		body = append(body, op, 0)
		body = append(body, threadsULEB64(offset)...)
		if !store {
			if op == 0x2a {
				body = append(body, 0xbc) // i32.reinterpret_f32
			} else if op == 0x2b {
				body = append(body, 0xbd) // i64.reinterpret_f64
			}
			if op == 0x28 || op == 0x2a || op >= 0x2c && op <= 0x2f {
				body = append(body, 0xad) // i64.extend_i32_u
			}
			body = append(body, 0x42, 0)
		}
	}
	if store {
		body = append(body, 0x42, 0, 0x42, 0)
	}
	body = append(body, 0x0b)
	raw := append(locals, body...)
	code := append(wasmtest.ULEB(uint32(len(raw))), raw...)
	limits := byte(1)
	if shared {
		limits = 3
	}
	addressType := wasm.I32
	if address64 {
		limits |= 4
		addressType = wasm.I64
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{addressType, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I64})))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entry = append(entry, 2, limits, 1, 1)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{limits, 1, 1})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("access", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	return wasmtest.Module(sections...)
}

func TestThreadsSharedMemoryAccessMatchesUnshared(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, simd := range []bool{false, true} {
		var ops []byte
		if simd {
			ops = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93}
		} else {
			for op := byte(0x28); op <= 0x3e; op++ {
				ops = append(ops, op)
			}
		}
		for _, op := range ops {
			for _, offset := range []uint32{0, 1, 4, 0xfffffff8} {
				t.Run(fmt.Sprintf("simd=%t/op=%x/off=%x", simd, op, offset), func(t *testing.T) {
					ordinary, err := cfg.Compile(threadsMemoryAccessModule(op, simd, false, false, offset))
					if err != nil {
						t.Fatal(err)
					}
					defer ordinary.Close()
					reference, err := Instantiate(ordinary, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer reference.Close()
					for _, imported := range []bool{false, true} {
						c, err := cfg.Compile(threadsMemoryAccessModule(op, simd, true, imported, offset))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()

						for _, compiled := range []*Compiled{c} {
							var opts any = InstantiateOptions{}
							if imported {
								memory, err := NewSharedMemory(1, 1)
								if err != nil {
									t.Fatal(err)
								}
								defer memory.Close()
								opts = testImports("env.memory", memory)
							}
							in, err := Instantiate(compiled, opts)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, addr := range []uint64{0, 1, 8, 65520, 65528, 65532, 65535, 65536, 1<<32 | 8, ^uint64(0)} {
								for _, instance := range []*Instance{reference, in} {
									mem := instance.mem()
									for i := range mem {
										mem[i] = byte(i*37 + 0x81)
									}
								}
								a, ae := reference.Invoke("access", addr, uint64(0x80000000ffffffff))
								b, be := in.Invoke("access", addr, uint64(0x80000000ffffffff))
								if (ae == nil) != (be == nil) {
									t.Fatalf("imported=%t addr=%x: ordinary=%v shared=%v", imported, addr, ae, be)
								}
								if ae != nil {
									at, aok := ae.(*TrapError)
									bt, bok := be.(*TrapError)
									if !aok || !bok || at.Code != bt.Code {
										t.Fatalf("trap codes: %v vs %v", ae, be)
									}
								} else if !slices.Equal(a, b) {
									t.Fatalf("imported=%t addr=%x: ordinary=%x shared=%x", imported, addr, a, b)
								}
								if !bytes.Equal(reference.mem(), in.mem()) {
									t.Fatalf("imported=%t addr=%x: memory contents differ", imported, addr)
								}
							}
						}
					}
				})
			}
		}
	}
}
