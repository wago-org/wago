//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func floatGlobalPinModule(wide bool) []byte {
	typ, constant, add, valueType := wasm.F32, byte(0x43), byte(0x92), byte(0x7d)
	zero := make([]byte, 4)
	if wide {
		typ, constant, add, valueType = wasm.F64, 0x44, 0xa0, 0x7c
		zero = make([]byte, 8)
	}
	// Keep an older global.get below the self-update operands. It must remain a
	// read of the old value when the same global's register is overwritten.
	body := []byte{2, 1, 0x7f, 1, valueType, 0x20, 1, 0x24, 0, 0x03, 0x40, 0x23, 0, 0x23, 0, 0x20, 2, add, 0x24, 0, 0x21, 4, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x20, 3, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	success := append(append([]byte{}, body...), 0x20, 4, 0x23, 0, 0x20, 3, 0x0b)
	trapped := append(append([]byte{}, body...), 0x00, 0x0b)
	getter := []byte{0, 0x23, 0, 0x0b}
	integerType, reinterpret := wasm.I32, byte(0xbc)
	if wide {
		integerType, reinterpret = wasm.I64, 0xbd
	}
	storeBits := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 0, 0x24, 0, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x23, 0, reinterpret, 0x0b}
	code := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	global := append([]byte{valueType, 1, constant}, zero...)
	global = append(global, 0x0b)
	params := []wasm.ValType{wasm.I32, typ, typ}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{typ, typ, wasm.I32}), wasmtest.FuncType(params, nil), wasmtest.FuncType(nil, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{integerType}))), wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{3})), wasmtest.Section(6, wasmtest.Vec(global)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("update", 0, 0), wasmtest.ExportEntry("trap", 0, 1), wasmtest.ExportEntry("state", 0, 2), wasmtest.ExportEntry("store_bits", 0, 3))), wasmtest.Section(10, wasmtest.Vec(code(success), code(trapped), code(getter), code(storeBits))))
}

func TestFloatGlobalPinPublicExecutionAndTrapCoherence(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("wide=%t/regabi=%t", wide, regABI), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithOptimization("reg-abi", regABI).Compile(floatGlobalPinModule(wide))
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
					bitPattern := func(v float64) uint64 {
						if wide {
							return math.Float64bits(v)
						}
						return uint64(math.Float32bits(float32(v)))
					}
					patterns := []uint64{0, 0x80000000, 1, 0x7f800000, 0xff800000, 0x7fa12345, 0xffc54321}
					if wide {
						patterns = []uint64{0, 1 << 63, 1, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff123456789abcd, 0xfff923456789abcd}
					}
					for _, bits := range patterns {
						got, err := in.Invoke("store_bits", bits, 127)
						if err != nil || len(got) != 1 || got[0] != bits {
							t.Fatalf("stored bits got=%v err=%v want=%x", got, err, bits)
						}
						got, err = in.Invoke("state")
						if err != nil || len(got) != 1 || got[0] != bits {
							t.Fatalf("visible stored bits got=%v err=%v want=%x", got, err, bits)
						}
					}
					boundary := float64(1 << 24)
					if wide {
						boundary = 1 << 53
					}
					for _, seed := range []float64{0, boundary, math.Copysign(0, -1)} {
						for _, step := range []float64{1, 3, math.Copysign(0, -1)} {
							for _, bound := range []uint64{0, 1, 2, 3, 127, 4096} {
								old, state := seed, seed
								for i := uint64(0); i < max(bound, 1); i++ {
									old = state
									if wide {
										state += step
									} else {
										state = float64(float32(state) + float32(step))
									}
								}
								got, err := in.Invoke("update", bound, bitPattern(seed), bitPattern(step))
								if err != nil || len(got) != 3 || got[0] != bitPattern(old) || got[1] != bitPattern(state) || got[2] != max(bound, 1) {
									t.Fatalf("seed=%x step=%x bound=%d got=%v err=%v want [%x %x %d]", bitPattern(seed), bitPattern(step), bound, got, err, bitPattern(old), bitPattern(state), max(bound, 1))
								}
								got, err = in.Invoke("state")
								if err != nil || len(got) != 1 || got[0] != bitPattern(state) {
									t.Fatalf("normal exit state=%v err=%v want=%x", got, err, bitPattern(state))
								}
								if _, err = in.Invoke("trap", bound, bitPattern(seed), bitPattern(step)); err == nil {
									t.Fatal("expected unreachable trap")
								}
								got, err = in.Invoke("state")
								if err != nil || len(got) != 1 || got[0] != bitPattern(state) {
									t.Fatalf("trap exit state=%v err=%v want=%x", got, err, bitPattern(state))
								}
							}
						}
					}
				}
			})
		}
	}
}
