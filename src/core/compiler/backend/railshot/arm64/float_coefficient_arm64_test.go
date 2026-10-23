//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func floatCoefficientModule(t testing.TB, wide, memory, divide bool, src, dst, pivot, length uint32, coefficient uint64) *wasm.Module {
	typ, load, store, mul, div := byte(0x7d), byte(0x2a), byte(0x38), byte(0x94), byte(0x95)
	if wide {
		typ, load, store, mul, div = 0x7c, 0x2b, 0x39, 0xa2, 0xa3
	}
	body := []byte{2, 5, 0x7f, 1, typ}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for i, x := range []uint32{src, dst, 0, pivot} {
		constant(x)
		body = append(body, 0x21, byte(i))
	}
	if wide {
		body = append(body, 0x44)
		var bits [8]byte
		binary.LittleEndian.PutUint64(bits[:], coefficient)
		body = append(body, bits[:]...)
	} else {
		body = append(body, 0x43)
		var bits [4]byte
		binary.LittleEndian.PutUint32(bits[:], uint32(coefficient))
		body = append(body, bits[:]...)
	}
	body = append(body, 0x21, 5)
	constant(123)
	body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x6a, 0x22, 4, 0x20, 0, 0x20, 2, 0x6a, load, 0, 0)
	if memory {
		body = append(body, 0x20, 3, load, 0, 0)
	} else {
		body = append(body, 0x20, 5)
	}
	op := mul
	if divide {
		op = div
	}
	body = append(body, op, store, 0, 0, 0x20, 2)
	step := uint32(4)
	if wide {
		step = 8
	}
	constant(step)
	body = append(body, 0x6a, 0x22, 2)
	constant(length)
	body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x73, 0x20, 4, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestFloatMapInvariantCoefficientsAndDivision(t *testing.T) {
	initial := make([]byte, 65536)
	for i := 0; i < len(initial)/8; i++ {
		binary.LittleEndian.PutUint64(initial[8*i:], uint64(i)*0x9e3779b97f4a7c15)
	}
	for _, wide := range []bool{false, true} {
		for _, memory := range []bool{false, true} {
			for _, divide := range []bool{false, true} {
				step := uint32(4)
				if wide {
					step = 8
				}
				bits := []uint64{0, 0x80000000, 0x3f900000, 0x7f800000, 0xff800000, 0x7fc00000, 1}
				if wide {
					bits = []uint64{0, 0x8000000000000000, 0x3ff2000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000000, 1}
				}
				for _, coefficient := range bits {
					for _, length := range []uint32{step, 4 * step, 128, 128 + step, 256} {
						for _, layout := range [][3]uint32{{4096, 0, 8192}, {0, 4096, 8192}, {4096, 0, 0}, {4096, 0, 1}, {4096, 0, step - 1}, {4096, 0, 128}, {4096, 0, 255}, {1, 4097, 65532}, {0, 65504, 65535}, {0xfffffff0, 4096, 8192}} {
							m := floatCoefficientModule(t, wide, memory, divide, layout[0], layout[1], layout[2], length, coefficient)
							want, wantMem, trapped := referenceFloatCoefficient(wide, memory, divide, layout[0], layout[1], layout[2], length, coefficient, initial)
							got, gotMem, gotErr := runFloatMapMemory(t, m, initial)
							if trapped != (gotErr != nil) || !trapped && got != want || !bytes.Equal(wantMem, gotMem) {
								t.Fatalf("wide%t memory%t divide%t coef%x length%d layout%v: got%x/%v want%x/%v equal%t", wide, memory, divide, coefficient, length, layout, got, gotErr, want, trapped, bytes.Equal(wantMem, gotMem))
							}
						}
					}
				}
			}
		}
	}
}

func referenceFloatCoefficient(wide, memory, divide bool, src, dst, pivot, length uint32, coefficient uint64, initial []byte) (uint32, []byte, bool) {
	mem := append([]byte(nil), initial...)
	size := uint32(4)
	if wide {
		size = 8
	}
	valid := func(p uint32) bool { return uint64(p)+uint64(size) <= uint64(len(mem)) }
	var index, lastDst uint32
	for {
		source := src + index
		lastDst = dst + index
		if !valid(source) {
			return 0, mem, true
		}
		bits := coefficient
		if memory {
			if !valid(pivot) {
				return 0, mem, true
			}
			if wide {
				bits = binary.LittleEndian.Uint64(mem[pivot:])
			} else {
				bits = uint64(binary.LittleEndian.Uint32(mem[pivot:]))
			}
		}
		if wide {
			x := math.Float64frombits(binary.LittleEndian.Uint64(mem[source:]))
			y := math.Float64frombits(bits)
			v := float64(x * y)
			if divide {
				v = float64(x / y)
			}
			if !valid(lastDst) {
				return 0, mem, true
			}
			binary.LittleEndian.PutUint64(mem[lastDst:], math.Float64bits(v))
		} else {
			x := math.Float32frombits(binary.LittleEndian.Uint32(mem[source:]))
			y := math.Float32frombits(uint32(bits))
			v := float32(x * y)
			if divide {
				v = float32(x / y)
			}
			if !valid(lastDst) {
				return 0, mem, true
			}
			binary.LittleEndian.PutUint32(mem[lastDst:], math.Float32bits(v))
		}
		index += size
		if index == length {
			break
		}
	}
	return 123 ^ index ^ lastDst, mem, false
}
