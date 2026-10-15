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

var simdAddressPattern = []byte{0, 0, 0, 0x80, 0x45, 0x23, 0xc1, 0x7f, 0xbc, 0x9a, 0x78, 0x56, 0x34, 0x12, 0xf0, 0x7f}

func simdAddressModule(wide, shared bool, index uint32) []byte {
	addr, c, and, add := wasm.I32, byte(0x41), byte(0x71), byte(0x6a)
	flags := byte(1)
	if wide {
		addr, c, and, add = wasm.I64, 0x42, 0x83, 0x7c
		flags |= 4
	}
	if shared {
		flags |= 2
	}
	memories := [][]byte{{1, 1, 2}, {1, 1, 2}}
	memories[index] = []byte{flags, 1, 2}
	expr := func(delta byte) []byte { return []byte{0x20, 0, c, 63, and, c, delta, add} }
	arg := func(memory uint32, offset byte) []byte {
		if memory == 0 {
			return []byte{0, offset}
		}
		return []byte{64, byte(memory), offset}
	}
	var types, funcs, bodies, exports [][]byte
	exports = append(exports, wasmtest.ExportEntry("memory", 2, index))
	appendFunc := func(name string, result wasm.ValType, body []byte) {
		i := uint32(len(funcs))
		types = append(types, wasmtest.FuncType([]wasm.ValType{addr}, []wasm.ValType{result}))
		funcs = append(funcs, wasmtest.ULEB(i))
		bodies = append(bodies, wasmtest.Code(body))
		exports = append(exports, wasmtest.ExportEntry(name, 0, i))
	}
	store := func(address []byte) []byte {
		body := append([]byte{}, address...)
		body = append(body, 0xfd, 0x0c)
		body = append(body, simdAddressPattern...)
		body = append(body, 0xfd, 0x0b)
		return append(body, arg(index, 3)...)
	}
	read := func(body, address []byte, memory uint32, offset byte, tc loadExtractCase) []byte {
		body = append(body, address...)
		body = append(body, 0xfd, 0)
		body = append(body, arg(memory, offset)...)
		return append(body, 0xfd, tc.sub, byte(tc.lane), 0x0b)
	}
	for _, tc := range loadExtractCases() {
		appendFunc(tc.name, tc.result, read(store(expr(1)), expr(1), index, 3, tc))
	}
	scalar := loadExtractCase{sub: 27, size: 4, result: wasm.I32}
	appendFunc("different_expression", wasm.I32, read(store(expr(1)), expr(2), index, 3, scalar))
	appendFunc("different_offset", wasm.I32, read(store(expr(1)), expr(1), index, 4, scalar))
	otherAddress := expr(1)
	if wide {
		otherAddress = append(otherAddress, 0xa7)
	}
	appendFunc("different_memory", wasm.I32, read(store(expr(1)), otherAddress, 1-index, 3, scalar))
	changed := store(expr(1))
	changed = append(changed, 0x20, 0, c, 1, add, 0x21, 0)
	appendFunc("changed_local", wasm.I32, read(changed, expr(1), index, 3, scalar))
	// The value mutates the address local after the first local.get snapshot.
	changed = []byte{0x20, 0, 0x20, 0, c, 1, add, 0x22, 0}
	if wide {
		changed = append(changed, 0xa7)
	}
	changed = append(changed, 0xfd, 0x11, 0xfd, 0x0b)
	changed = append(changed, arg(index, 3)...)
	appendFunc("value_changes_address", wasm.I32, read(changed, []byte{0x20, 0}, index, 3, scalar))
	appendFunc("full_span", wasm.I32, read(store([]byte{0x20, 0}), []byte{0x20, 0}, index, 3, scalar))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(5, wasmtest.Vec(memories...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func simdAddressExpected(bytes []byte, tc loadExtractCase) uint64 {
	offset := tc.lane * tc.size
	switch tc.size {
	case 1:
		if tc.signed {
			return uint64(uint32(int32(int8(bytes[offset]))))
		}
		return uint64(bytes[offset])
	case 2:
		v := binary.LittleEndian.Uint16(bytes[offset:])
		if tc.signed {
			return uint64(uint32(int32(int16(v))))
		}
		return uint64(v)
	case 4:
		return uint64(binary.LittleEndian.Uint32(bytes[offset:]))
	default:
		return binary.LittleEndian.Uint64(bytes[offset:])
	}
}

func TestSIMDStoreLoadAddressPublicExecutionAndTraps(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, index := range []uint32{0, 1} {
				features := CoreFeaturesV3
				if shared {
					features |= CoreFeatureThreads
				}
				for _, enabled := range []bool{false, true} {
					for _, narrow := range []bool{false, true} {
						for _, regABI := range []bool{false, true} {
							for _, mode := range modes {
								if shared && mode == BoundsChecksSignalsBased {
									continue
								}
								t.Run(fmt.Sprintf("wide=%t/shared=%t/index=%d/enabled=%t/narrow=%t/regabi=%t/bounds=%v", wide, shared, index, enabled, narrow, regABI, mode), func(t *testing.T) {
									c, err := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(mode).WithOptimization("simd-store-load-address", enabled).WithOptimization("simd-load-extract", narrow).WithOptimization("reg-abi", regABI).Compile(simdAddressModule(wide, shared, index))
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
										mem, err := in.ExportedMemory("memory")
										if err != nil {
											t.Fatal(err)
										}
										for _, tc := range loadExtractCases() {
											got, err := in.Invoke(tc.name, 0)
											want := simdAddressExpected(simdAddressPattern, tc)
											if err != nil || len(got) != 1 || got[0] != want {
												t.Fatalf("%s: %v,%v want=%x", tc.name, got, err, want)
											}
										}
										shifted := binary.LittleEndian.Uint32(simdAddressPattern[1:])
										for _, test := range []struct {
											name string
											want uint64
										}{{"different_expression", uint64(shifted)}, {"different_offset", uint64(shifted)}, {"different_memory", 0}, {"changed_local", uint64(shifted)}, {"value_changes_address", 0x01000000}} {
											clear(mem.UnsafeBytes()[:32])
											got, err := in.Invoke(test.name, 0)
											if err != nil || len(got) != 1 || got[0] != test.want {
												t.Fatalf("%s: %v,%v want=%x", test.name, got, err, test.want)
											}
										}
										for _, address := range []uint64{65518, 65525, 65536, ^uint64(0)} {
											_, err := in.Invoke("full_span", address)
											var trap *TrapError
											if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
												t.Fatalf("address=%x: %v", address, err)
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
}
