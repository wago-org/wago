//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The trip counter counts elements while two distinct mutable source locals
// advance in bytes. Both address locals and the last source value are live out.
func floatInductionModule(t testing.TB, wide, early bool, packets int, src, dst, start, limit uint32) *wasm.Module {
	body := []byte{2, 6, 0x7f, 1, 0x7d}
	size, load, store, add := uint32(4), byte(0x2a), byte(0x38), byte(0x92)
	if wide {
		body[4] = 0x7c
		size, load, store, add = 8, 0x2b, 0x39, 0xa0
	}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	get := func(x byte) { body = append(body, 0x20, x) }
	set := func(x byte) { body = append(body, 0x21, x) }
	for i, x := range []uint32{src, dst, start, limit, 0, 0} {
		constant(x)
		set(byte(i))
	}
	body = append(body, 0x03, 0x40)
	advance := func(x byte) { get(x); constant(uint32(packets) * size); body = append(body, 0x6a); set(x) }
	if early {
		advance(4)
		advance(5)
	}
	address := func(base, index byte, bias uint32) {
		get(base)
		get(index)
		body = append(body, 0x6a)
		constant(bias)
		body = append(body, 0x6a)
	}
	for lane := 0; lane < packets; lane++ {
		bias := uint32(lane) * size
		address(1, 5, bias)
		address(0, 4, bias)
		body = append(body, load, 0, 0)
		if wide {
			body = appendF64ConstForCacheTest(body, 1.125)
		} else {
			body = append(body, 0x43, 0, 0, 0x90, 0x3f)
		}
		body = append(body, add, 0x22, 6, store, 0, 0)
	}
	if !early {
		advance(4)
		advance(5)
	}
	get(2)
	constant(uint32(packets))
	body = append(body, 0x6a, 0x22, 2)
	get(3)
	body = append(body, 0x47, 0x0d, 0, 0x0b)
	get(2)
	get(4)
	body = append(body, 0x73)
	get(5)
	body = append(body, 0x73)
	get(6)
	if wide {
		body = append(body, 0xbd, 0xa7)
	} else {
		body = append(body, 0xbc)
	}
	body = append(body, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestFloatMapRelatedInduction(t *testing.T) {
	initial := make([]byte, 65536)
	for i := 0; i < len(initial); i += 8 {
		binary.LittleEndian.PutUint64(initial[i:], 0x3ff400003f800000+uint64(i))
	}
	for _, wide := range []bool{false, true} {
		for _, early := range []bool{false, true} {
			for _, packets := range []int{1, 2, 3, 4} {
				for _, c := range []struct{ src, dst, start, limit uint32 }{
					{0, 16384, 0, uint32(packets) * 128},
					{0, 16384, 3, uint32(packets)*128 + 3},
					{0, 16384, 0, uint32(packets)},
					{0, 0, 0, uint32(packets) * 128},
					{0, 4, 0, uint32(packets) * 128},
					{65500, 16384, 0, uint32(packets) * 128},
					{0, 65500, 0, uint32(packets) * 128},
					{0xfffffff0, 16384, 0, uint32(packets) * 128},
				} {
					m := floatInductionModule(t, wide, early, packets, c.src, c.dst, c.start, c.limit)
					got, mem, err, hits := runFloatMapMemory(t, m, true, initial)
					want, scalar, scalarErr, _ := runFloatMapMemory(t, m, false, initial)
					if got != want || (err == nil) != (scalarErr == nil) || !bytes.Equal(mem, scalar) {
						t.Fatalf("wide=%v early=%v packets=%d case=%+v: got=%x want=%x errors=%v/%v", wide, early, packets, c, got, want, err, scalarErr)
					}
					if diagnosticsEnabled && c.src == 0 && c.dst == 16384 && c.start == 0 && c.limit == uint32(packets)*128 && hits == 0 {
						t.Fatalf("new floating induction was not selected: wide=%v early=%v packets=%d", wide, early, packets)
					}
				}
			}
		}
	}
}
