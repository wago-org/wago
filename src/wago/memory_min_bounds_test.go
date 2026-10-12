//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

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
	exports := [][]byte{wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1)}
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
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("memory-min-bounds", enabled).WithOptimization("reg-abi", regABI)
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
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).Compile(memoryMinimumBoundsModule(wide, true, index, 0))
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
		}
	}
}
