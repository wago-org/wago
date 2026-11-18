//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both streams deliberately use the same source local. Distinct base locals
// whose runtime values happen to match must retain runtime disambiguation.
func floatMapSharedBaseModule(t *testing.T, wide, duplicate bool, tc [5]uint32, length uint32) *wasm.Module {
	body := []byte{1, 4, 0x7f}
	constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	width, load, store, add, mul := uint32(4), byte(0x2a), byte(0x38), byte(0x92), byte(0x94)
	if wide {
		width, load, store, add, mul = 8, 0x2b, 0x39, 0xa0, 0xa2
	}
	constant(tc[0])
	body = append(body, 0x21, 0)
	constant(123)
	body = append(body, 0x03, 0x40)
	address := func(bias uint32, local byte) {
		body = append(body, 0x20, 0, 0x20, 1, 0x6a)
		constant(bias)
		body = append(body, 0x6a, 0x22, local)
	}
	address(tc[2], 3)
	address(tc[1], 2)
	body = append(body, load, 0)
	body = append(body, wasmtest.ULEB(tc[3])...)
	if duplicate {
		body = append(body, 0x20, 2, load, 0)
		body = append(body, wasmtest.ULEB(tc[3])...)
		body = append(body, add)
	}
	if wide {
		body = appendF64ConstForCacheTest(body, 1.125)
	} else {
		body = append(body, 0x43)
		var bits [4]byte
		binary.LittleEndian.PutUint32(bits[:], math.Float32bits(1.125))
		body = append(body, bits[:]...)
	}
	body = append(body, mul, store, 0)
	body = append(body, wasmtest.ULEB(tc[4])...)
	body = append(body, 0x20, 1)
	constant(width)
	body = append(body, 0x6a, 0x22, 1)
	constant(length)
	body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 1, 0x73, 0x20, 2, 0x73, 0x20, 3, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestFloatMapGuardSharedBasesAndDuplicateRanges(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width := uint32(4)
		if wide {
			width = 8
		}
		initial := make([]byte, 65536)
		for i := uint32(0); i < 65536/width; i++ {
			if wide {
				binary.LittleEndian.PutUint64(initial[i*width:], math.Float64bits(float64(int32(i%37)-18)*0.125))
			} else {
				binary.LittleEndian.PutUint32(initial[i*width:], math.Float32bits(float32(int32(i%37)-18)*0.125))
			}
		}
		cases := [][5]uint32{{0, 0, 0, 0, 0}, {0, 0, 0, 0, width}, {0, 0, 0, width, 0}, {0, 0, 0, 0, 1}, {0, 0, 0, 1, 0}, {4096, 0, 0, 0, 0}, {4096, 0, 0, 0, 4096}, {4096, 0, 0, 4096, 0}, {0, 0, 0, 4095, 0}, {0, 0, 0, 0, 4095}, {0, 0, 0, 4096, 0}, {0, 0, 0, 0, 4096}, {0, 0, 0, 1 << 20, 0}, {0, 0, 0, 0, 1 << 20}, {65504, 0, 0, 0, 0}, {65504, 0, 0, 0, width}, {0xfffffff0, 32, 32, 0, 0}, {0xfffffff0, 0, 32, 0, 0}, {0xfffffff0, 0, 0, 32, 32}, {0xfffffff0, 0, 0, 0, 0}}
		for _, tc := range cases {
			for _, length := range []uint32{width, 16, 16 + width, 32 + width, 64 + width, 128, 272 + width} {
				for _, duplicate := range []bool{false, true} {
					m := floatMapSharedBaseModule(t, wide, duplicate, tc, length)
					want, wm, we, _ := runFloatMapMemory(t, m, false, initial)
					got, gm, ge, hits := runFloatMapMemory(t, m, true, initial)
					if (we == nil) != (ge == nil) || we == nil && got != want || !bytes.Equal(wm, gm) {
						t.Fatalf("wide%t case%v length%d duplicate%t: got%x/%v want%x/%v memory%t", wide, tc, length, duplicate, got, ge, want, we, bytes.Equal(wm, gm))
					}
					dst, src := uint64(tc[2])+uint64(tc[4]), uint64(tc[1])+uint64(tc[3])
					rejected := floatMapPacketDependenceEnabled && dst > src && dst-src < floatMapVectorBytes
					if diagnosticsEnabled && (hits == 1) == rejected {
						t.Fatalf("map admission wide%t case%v hits%d rejected%t", wide, tc, hits, rejected)
					}
				}
			}
		}
	}
}
