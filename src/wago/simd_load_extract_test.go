//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type loadExtractCase struct {
	name   string
	sub    byte
	size   int
	signed bool
	result wasm.ValType
	lane   int
}

func loadExtractCases() []loadExtractCase {
	var out []loadExtractCase
	for _, kind := range []loadExtractCase{
		{sub: 21, size: 1, signed: true, result: wasm.I32}, {sub: 22, size: 1, result: wasm.I32},
		{sub: 24, size: 2, signed: true, result: wasm.I32}, {sub: 25, size: 2, result: wasm.I32},
		{sub: 27, size: 4, result: wasm.I32}, {sub: 29, size: 8, result: wasm.I64},
		{sub: 31, size: 4, result: wasm.F32}, {sub: 33, size: 8, result: wasm.F64},
	} {
		for lane := 0; lane < 16/kind.size; lane++ {
			c := kind
			c.lane = lane
			c.name = fmt.Sprintf("extract%d_%d", c.sub, lane)
			out = append(out, c)
		}
	}
	return out
}

func loadExtractModule(wide, shared bool, index uint32) []byte {
	flags, addr := byte(1), wasm.I32
	if wide {
		flags |= 4
		addr = wasm.I64
	}
	if shared {
		flags |= 2
	}
	memories := [][]byte{{1, 1, 2}, {1, 1, 2}}
	memories[index] = []byte{flags, 1, 2}
	memories = memories[:index+1]
	var types, funcs, bodies, exports [][]byte
	exports = append(exports, wasmtest.ExportEntry("memory", 2, index))
	for i, c := range loadExtractCases() {
		types = append(types, wasmtest.FuncType([]wasm.ValType{addr}, []wasm.ValType{c.result}))
		funcs = append(funcs, wasmtest.ULEB(uint32(i)))
		body := []byte{0x20, 0, 0xfd, 0}
		if index == 0 {
			body = append(body, 4, 0)
		} else {
			body = append(body, 68, 1, 0)
		}
		body = append(body, 0xfd, c.sub, byte(c.lane), 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
		exports = append(exports, wasmtest.ExportEntry(c.name, 0, uint32(i)))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(5, wasmtest.Vec(memories...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestSIMDLoadExtractPublicExecutionAndFullSpanTraps(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			features := CoreFeaturesV2
			if wide {
				features |= CoreFeatureMemory64
			}
			if shared {
				features |= CoreFeatureThreads
			}
			for _, index := range []uint32{0, 1} {
				moduleFeatures := features
				if index != 0 {
					moduleFeatures |= CoreFeatureMultiMemory
				}
				for _, enabled := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						for _, mode := range modes {
							if shared && mode == BoundsChecksSignalsBased {
								continue // Threads require explicit bounds checks.
							}
							t.Run(fmt.Sprintf("wide=%t/shared=%t/index=%d/enabled=%t/regabi=%t/bounds=%v", wide, shared, index, enabled, regABI, mode), func(t *testing.T) {
								c, err := NewRuntimeConfig().WithCoreFeatures(requireNativeTestFeatures(t, moduleFeatures)).WithBoundsChecks(mode).WithOptimization("simd-load-extract", enabled).WithOptimization("reg-abi", regABI).Compile(loadExtractModule(wide, shared, index))
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
									bytes := mem.UnsafeBytes()
									for i := 0; i < 16; i++ {
										bytes[i] = byte(0x80 + i)
									}
									for _, tc := range loadExtractCases() {
										offset := tc.lane * tc.size
										var want uint64
										switch tc.size {
										case 1:
											want = uint64(bytes[offset])
											if tc.signed {
												want = uint64(uint32(int32(int8(bytes[offset]))))
											}
										case 2:
											want = uint64(binary.LittleEndian.Uint16(bytes[offset:]))
											if tc.signed {
												want = uint64(uint32(int32(int16(want))))
											}
										case 4:
											want = uint64(binary.LittleEndian.Uint32(bytes[offset:]))
										case 8:
											want = binary.LittleEndian.Uint64(bytes[offset:])
										}
										got, err := in.Invoke(tc.name, 0)
										if err != nil || len(got) != 1 || got[0] != want {
											t.Fatalf("%s: %v,%v want=%x", tc.name, got, err, want)
										}
										// Lane zero can fit here while the original 16-byte load cannot. The
										// optimization must retain that trap in explicit and signal-based modes.
										for _, address := range []uint64{65521, 65528, 65536, ^uint64(0)} {
											_, err := in.Invoke(tc.name, address)
											var trap *TrapError
											if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
												t.Fatalf("%s address=%x: %v", tc.name, address, err)
											}
										}
									}

									for _, tc := range loadExtractCases() {
										if tc.result != wasm.F32 && tc.result != wasm.F64 {
											continue
										}
										patterns := []uint64{0x80000000, 0x7f800000, 0x7fc12345, 0x7f812345}
										if tc.size == 8 {
											patterns = []uint64{0x8000000000000000, 0x7ff0000000000000, 0x7ff8123456789abc, 0x7ff0123456789abc}
										}
										for _, bits := range patterns {
											offset := 1 + tc.lane*tc.size
											if tc.size == 4 {
												binary.LittleEndian.PutUint32(bytes[offset:], uint32(bits))
											} else {
												binary.LittleEndian.PutUint64(bytes[offset:], bits)
											}
											got, err := in.Invoke(tc.name, 1)
											if err != nil || len(got) != 1 || got[0] != bits {
												t.Fatalf("%s unaligned floating bits: %v,%v want=%x", tc.name, got, err, bits)
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
