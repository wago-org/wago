//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func atomicStraightModule(wide bool, index uint32) []byte {
	addr := wasm.I32
	flags := byte(3)
	if wide {
		addr = wasm.I64
		flags |= 4
	}
	memories := [][]byte{{1, 1, 2}, {1, 1, 2}}
	memories[index] = []byte{flags, 1, 2}
	memories = memories[:index+1]
	memarg := []byte{2, 0}
	ordinary := []byte{2, 0}
	if index != 0 {
		memarg = []byte{66, byte(index), 0}
		ordinary = memarg
	}
	var functions, bodies, exports [][]byte
	exports = append(exports, wasmtest.ExportEntry("memory", 2, index))
	for i, name := range []string{"add", "cas", "load", "fence", "load_before", "trap_before", "trap_after"} {
		prefix := []byte{0x20, 3}
		if name == "load_before" {
			prefix = append([]byte{0x20, 0, 0x28}, ordinary...)
		}
		if name == "trap_before" {
			prefix = append(prefix, 0x41, 0, 0x6e)
		}
		body := append(prefix, 0x02, 0x7f, 0x20, 0, 0x20, 1, 0xfe, 0x17)
		body = append(body, memarg...)
		if name == "fence" {
			body = append(body, 0xfe, 3, 0)
		}
		if name == "trap_after" {
			body = append(body, 0x41, 1, 0x41, 0, 0x6e)
		} else {
			body = append(body, 0x20, 0)
			switch name {
			case "load", "fence":
				body = append(body, 0xfe, 0x10)
			case "cas":
				body = append(body, 0x20, 1, 0x20, 2, 0xfe, 0x48)
			default:
				body = append(body, 0x20, 2, 0xfe, 0x1e)
			}
			body = append(body, memarg...)
		}
		body = append(body, 0x0b, 0x6a, 0x0b)
		functions = append(functions, wasmtest.ULEB(0))
		bodies = append(bodies, wasmtest.Code(body))
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{addr, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(functions...)), wasmtest.Section(5, wasmtest.Vec(memories...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestThreadsStraightBlockValuesAndTraps(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []uint32{0, 1} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/index=%d/block=%t/abi=%t", wide, index, enabled, regABI), func(t *testing.T) {
						c, err := NewRuntimeConfig().WithCoreFeatures(requireNativeTestFeatures(t, nativeMemoryTestFeatures(wide, index))).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("straight-line-block", enabled).WithOptimization("reg-abi", regABI).Compile(atomicStraightModule(wide, index))
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
							mem, err := in.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							b := mem.UnsafeBytes()
							for _, dst := range []uint64{0, 4, 65532} {
								for _, name := range []string{"add", "cas", "load", "fence", "load_before"} {
									binary.LittleEndian.PutUint32(b[dst:], 91)
									got, err := in.Invoke(name, dst, 17, 3, 123)
									want := uint64(140)
									stored := uint32(17)
									if name == "add" || name == "load_before" {
										stored = 20
									}
									if name == "cas" {
										stored = 3
									}
									if name == "load_before" {
										want = 108
									}
									if err != nil || len(got) != 1 || got[0] != want {
										t.Fatalf("%s dst=%d: %v %v want=%d", name, dst, got, err, want)
									}
									if got := binary.LittleEndian.Uint32(b[dst:]); got != stored {
										t.Fatalf("%s stored=%d want=%d", name, got, stored)
									}
								}
							}
							for _, name := range []string{"trap_before", "trap_after"} {
								binary.LittleEndian.PutUint32(b, 91)
								_, err := in.Invoke(name, 0, 17, 3, 123)
								var trap *TrapError
								if !errors.As(err, &trap) {
									t.Fatalf("%s missing trap: %v", name, err)
								}
								want := uint32(91)
								if name == "trap_after" {
									want = 17
								}
								if got := binary.LittleEndian.Uint32(b); got != want {
									t.Fatalf("%s stored=%d want=%d", name, got, want)
								}
							}
							for _, dst := range []uint64{1, 65536, ^uint64(0)} {
								for _, name := range []string{"add", "cas", "load", "fence"} {
									_, err := in.Invoke(name, dst, 17, 3, 123)
									var trap *TrapError
									if !errors.As(err, &trap) {
										t.Fatalf("%s dst=%d missing trap: %v", name, dst, err)
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
