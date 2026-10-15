//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func requireNativeTestFeatures(t *testing.T, features CoreFeatures) CoreFeatures {
	t.Helper()
	if !platformCoreFeatures().IsEnabled(features) {
		t.Skipf("module features %v are unavailable on this backend", features)
	}
	return features
}

func nativeMemoryTestFeatures(wide bool, index uint32) CoreFeatures {
	features := CoreFeaturesV2 | CoreFeatureThreads
	if wide {
		features |= CoreFeatureMemory64
	}
	if index != 0 {
		features |= CoreFeatureMultiMemory
	}
	return features
}

func memoryMinimumBoundsModule(wide, shared bool, index uint32, minimum byte) []byte {
	typ, constant, and, shl, add := wasm.I32, byte(0x41), byte(0x71), byte(0x74), byte(0x6a)
	selected := byte(1)
	if shared {
		selected |= 2
	}
	if wide {
		selected |= 4
		typ, constant, and, shl, add = wasm.I64, 0x42, 0x83, 0x86, 0x7c
	}
	memories := [][]byte{{1, 1, 2}, {1, 1, 2}}
	memories[index] = []byte{selected, minimum, 2}
	memories = memories[:index+1]
	address := func(large, aligned bool) []byte {
		mask := []byte{0xff, 0x3f}
		shift := byte(2)
		if large {
			mask = []byte{0xff, 0xff, 3}
			shift = 0
		}
		b := append([]byte{0x20, 0, constant}, mask...)
		b = append(b, and, constant, shift, shl)
		if !aligned {
			b = append(b, constant, 1, add)
		}
		return b
	}
	memarg := func(align byte) []byte {
		if index == 0 {
			return []byte{align, 0}
		}
		return []byte{align | 64, 1, 0}
	}
	var types, funcs, bodies [][]byte
	exports := [][]byte{wasmtest.ExportEntry("memory0", 2, 0)}
	if index != 0 {
		exports = append(exports, wasmtest.ExportEntry("memory1", 2, 1))
	}
	addFunc := func(name string, store bool, body []byte) {
		params := []wasm.ValType{typ}
		if store {
			params = append(params, wasm.I32)
		}
		types = append(types, wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))
		funcs = append(funcs, wasmtest.ULEB(uint32(len(funcs))))
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(len(bodies))))
		bodies = append(bodies, wasmtest.Code(append(body, 0x0b)))
	}
	load := append(address(false, false), 0x28)
	load = append(load, memarg(0)...)
	addFunc("read", false, load)
	store := append(address(false, false), 0x20, 1, 0x36)
	store = append(store, memarg(0)...)
	store = append(store, load...)
	addFunc("store", true, store)
	simd := append(address(false, false), 0x20, 1, 0xfd, 0x11, 0xfd, 0x0b)
	simd = append(simd, memarg(0)...)
	simd = append(simd, address(false, false)...)
	simd = append(simd, 0xfd, 0)
	simd = append(simd, memarg(0)...)
	simd = append(simd, 0xfd, 0x1b, 3)
	addFunc("simd", true, simd)
	atomic := append(address(false, true), 0xfe, 0x10)
	atomic = append(atomic, memarg(2)...)
	addFunc("atomic", false, atomic)
	invalid := append(address(true, false), 0x28)
	invalid = append(invalid, memarg(0)...)
	addFunc("invalid", false, invalid)
	unaligned := append(address(false, false), 0xfe, 0x10)
	unaligned = append(unaligned, memarg(2)...)
	addFunc("unaligned", false, unaligned)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(5, wasmtest.Vec(memories...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestMemoryMinimumBoundsPublicExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, index := range []uint32{0, 1} {
				for _, enabled := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						t.Run(fmt.Sprintf("wide=%t/shared=%t/index=%d/enabled=%t/regabi=%t", wide, shared, index, enabled, regABI), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(requireNativeTestFeatures(t, nativeMemoryTestFeatures(wide, index))).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("memory-min-bounds", enabled).WithOptimization("reg-abi", regABI)
							c, err := cfg.Compile(memoryMinimumBoundsModule(wide, shared, index, 1))
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
								for _, address := range []uint64{0, 1, 8191, 8192, 1 << 32, ^uint64(0)} {
									for _, name := range []string{"store", "simd"} {
										got, err := in.Invoke(name, address, 0xabcdef01)
										if err != nil || len(got) != 1 || got[0] != 0xabcdef01 {
											t.Fatalf("%s address=%x: %v,%v", name, address, got, err)
										}
										got, err = in.Invoke("read", address)
										if err != nil || got[0] != 0xabcdef01 {
											t.Fatalf("reload address=%x: %v,%v", address, got, err)
										}
									}
								}
								// A later host write must remain observable; the bounds proof never forwards
								// shared values or removes the actual load.
								memory, err := in.ExportedMemory(fmt.Sprintf("memory%d", index))
								if err != nil {
									t.Fatal(err)
								}
								memory.UnsafeBytes()[1] = 0x42
								got, err := in.Invoke("read", 0)
								if err != nil || got[0]&255 != 0x42 {
									t.Fatalf("host write: %v,%v", got, err)
								}
								if _, err := in.Invoke("atomic", 0); err != nil {
									t.Fatal(err)
								}
								for name, want := range map[string]TrapCode{"invalid": TrapLinMemOutOfBounds, "unaligned": TrapAtomicUnaligned} {
									argument := uint64(0)
									if name == "invalid" {
										argument = 65535
									}
									_, err := in.Invoke(name, argument)
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != want {
										t.Fatalf("%s: %v want=%v", name, err, want)
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

func TestMemoryMinimumBoundsZeroMinimumUsesLiveGrowth(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []uint32{0, 1} {
			t.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithCoreFeatures(requireNativeTestFeatures(t, nativeMemoryTestFeatures(wide, index))).WithBoundsChecks(BoundsChecksExplicit).Compile(memoryMinimumBoundsModule(wide, true, index, 0))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				_, err = in.Invoke("read", 0)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
					t.Fatalf("zero-minimum initial read: %v", err)
				}
				memory, err := in.ExportedMemory(fmt.Sprintf("memory%d", index))
				if err != nil {
					t.Fatal(err)
				}
				if old := memory.growShared(1, 2); old != 0 {
					t.Fatalf("grow old=%d", old)
				}
				got, err := in.Invoke("read", 0)
				if err != nil || got[0] != 0 {
					t.Fatalf("grown extent read: %v,%v", got, err)
				}
			})
		}
	}
}

func memoryMinimumCopyModule(wide bool, minima [2]byte, length uint32) []byte {
	typ, c, and, shl := wasm.I32, byte(0x41), byte(0x71), byte(0x74)
	flags := byte(1)
	if wide {
		typ, c, and, shl, flags = wasm.I64, 0x42, 0x83, 0x86, 5
	}
	var bodies, exports, funcs [][]byte
	for _, kind := range []struct {
		name     string
		dst, src byte
		masked   bool
	}{{"private", 1, 0, true}, {"shared", 0, 1, true}, {"overlap", 0, 0, true}, {"raw", 1, 0, false}} {
		b := []byte{}
		for x := byte(0); x < 2; x++ {
			b = append(b, 0x20, x)
			if kind.masked {
				b = append(b, c, 0xff, 0x3f, and, c, 2, shl, c, 1)
				if wide {
					b = append(b, 0x7c)
				} else {
					b = append(b, 0x6a)
				}
			}
		}
		b = append(b, c)
		b = append(b, wasmtest.SLEB64(int64(length))...)
		b = append(b, 0xfc, 10, kind.dst, kind.src, 0x0b)
		exports = append(exports, wasmtest.ExportEntry(kind.name, 0, uint32(len(bodies))))
		bodies = append(bodies, wasmtest.Code(b))
		funcs = append(funcs, []byte{0})
	}
	exports = append(exports, wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, nil))), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(5, wasmtest.Vec([]byte{flags | 2, minima[0], 2}, []byte{flags, minima[1], 2})), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestMemoryMinimumCopyPublicExecution(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, wide := range []bool{false, true} {
		for _, minima := range [][2]byte{{1, 1}, {0, 1}, {1, 0}, {0, 0}} {
			for _, length := range []uint32{0, 4, 33, 64} {
				for _, enabled := range []bool{false, true} {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("memory-min-bounds", enabled)
					c, err := cfg.Compile(memoryMinimumCopyModule(wide, minima, length))
					if err != nil {
						t.Fatal(err)
					}
					variants := []*Compiled{c, publicArtifactRoundTrip(t, c)}
					for _, compiled := range variants {
						in, err := Instantiate(compiled)
						if err != nil {
							t.Fatal(err)
						}
						mem := [2][]byte{}
						for i := range mem {
							m, err := in.ExportedMemory(fmt.Sprintf("memory%d", i))
							if err != nil {
								t.Fatal(err)
							}
							mem[i] = m.UnsafeBytes()
						}
						for _, kind := range []struct {
							name     string
							dst, src int
							masked   bool
						}{{"private", 1, 0, true}, {"shared", 0, 1, true}, {"overlap", 0, 0, true}, {"raw", 1, 0, false}} {
							for _, args := range [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {8191, 8190}, {65536, 65536}, {65537, 0}, {0, 65537}, {^uint64(0), ^uint64(0)}} {
								for i := range mem {
									for x := range mem[i] {
										mem[i][x] = byte(x*37 + i*101)
									}
								}
								before := [2][]byte{bytes.Clone(mem[0]), bytes.Clone(mem[1])}
								want := [2][]byte{bytes.Clone(mem[0]), bytes.Clone(mem[1])}
								dst, src := args[0], args[1]
								if !wide {
									dst, src = uint64(uint32(dst)), uint64(uint32(src))
								}
								if kind.masked {
									dst = ((dst & 8191) << 2) + 1
									src = ((src & 8191) << 2) + 1
								}
								valid := dst <= uint64(len(mem[kind.dst])) && src <= uint64(len(mem[kind.src])) && uint64(length) <= uint64(len(mem[kind.dst]))-dst && uint64(length) <= uint64(len(mem[kind.src]))-src
								if valid {
									copy(want[kind.dst][int(dst):int(dst)+int(length)], want[kind.src][int(src):int(src)+int(length)])
								}
								_, err := in.Invoke(kind.name, args[0], args[1])
								if valid {
									if err != nil {
										t.Fatalf("wide=%t minima=%v n=%d enabled=%t kind=%s args=%v: %v", wide, minima, length, enabled, kind.name, args, err)
									}
								} else {
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
										t.Fatalf("expected bounds trap: %v", err)
									}
									want = before
								}
								if !bytes.Equal(mem[0], want[0]) || !bytes.Equal(mem[1], want[1]) {
									t.Fatalf("copy/trap changed wrong bytes: wide=%t minima=%v n=%d enabled=%t kind=%s args=%v", wide, minima, length, enabled, kind.name, args)
								}
							}
						}
						in.Close()
					}
					for _, compiled := range variants {
						compiled.Close()
					}
				}
			}
		}
	}
}
