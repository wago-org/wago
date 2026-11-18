//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

type integerInductionCase struct {
	parents                                                                   int
	packets                                                                   int
	ratio                                                                     uint32
	src, dst, indexA, indexB, count, limit, neighbor, loadOffset, storeOffset uint32
	dynamic, absolute, rich, early, conditionFirst                            bool
	localBase                                                                 byte
}

func integerInductionModule(t testing.TB, c integerInductionCase) *wasm.Module {
	body := []byte{1, 10 + c.localBase, 0x7f}
	constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	get := func(local byte) { body = append(body, 0x20, local+c.localBase) }
	set := func(local byte) { body = append(body, 0x21, local+c.localBase) }
	for i, v := range []uint32{c.src, c.dst, c.indexA, c.indexB, c.count, c.limit} {
		constant(v)
		set(byte(i))
	}
	constant(123)
	for i := 0; i < c.parents; i++ {
		body = append(body, 0x03, 0x40)
	}
	body = append(body, 0x03, 0x40)
	advance := func(local byte) { get(local); constant(uint32(c.packets) * 4); body = append(body, 0x6a); set(local) }
	if c.early {
		advance(2)
		advance(3)
	}
	address := func(base, index, alias byte, bias uint32) {
		if c.absolute {
			if base == 0 {
				constant(c.src)
			} else {
				constant(c.dst)
			}
		} else {
			get(base)
		}
		get(index)
		body = append(body, 0x6a)
		constant(bias)
		body = append(body, 0x6a, 0x22, alias+c.localBase)
	}
	for lane := 0; lane < c.packets; lane++ {
		bias := uint32(lane) * 4
		address(1, 3, 7, bias)
		address(0, 2, 6, bias)
		body = append(body, 0x28, 0)
		body = append(body, wasmtest.ULEB(c.loadOffset)...)
		if c.rich {
			address(0, 2, 6, bias+c.neighbor)
			body = append(body, 0x28, 0)
			body = append(body, wasmtest.ULEB(c.loadOffset)...)
			body = append(body, 0x6a)
			constant(7)
			body = append(body, 0x6c)
			constant(0xdeadbeef)
			body = append(body, 0x73)
		}
		body = append(body, 0x22, 8+c.localBase, 0x36, 0)
		body = append(body, wasmtest.ULEB(c.storeOffset)...)
	}
	if !c.early {
		advance(2)
		advance(3)
	}
	if c.conditionFirst {
		get(5)
	}
	get(4)
	constant(uint32(c.packets) * 4 / c.ratio)
	body = append(body, 0x6a, 0x22, 4+c.localBase)
	if c.dynamic || c.limit != 0 {
		if !c.conditionFirst {
			if c.dynamic {
				get(5)
			} else {
				constant(c.limit)
			}
		}
		body = append(body, 0x47)
	}
	body = append(body, 0x22, 9+c.localBase, 0x0d, 0, 0x0b)
	for i := 0; i < c.parents; i++ {
		body = append(body, 0x0b)
	}
	for _, local := range []byte{2, 3, 4, 6, 7, 8, 9} {
		get(local)
		body = append(body, 0x73)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func integerInductionOracle(initial []byte, c integerInductionCase) (uint32, []byte, bool) {
	memory := append([]byte(nil), initial...)
	a, b, count := c.indexA, c.indexB, c.count
	var source, dest, value uint32
	for iterations := 0; iterations < 20000; iterations++ {
		step := uint32(c.packets) * 4
		if c.early {
			a += step
			b += step
		}
		for lane := 0; lane < c.packets; lane++ {
			bias := uint32(lane) * 4
			source = c.src + a + bias
			dest = c.dst + b + bias
			first := uint64(source) + uint64(c.loadOffset)
			if first+4 > uint64(len(memory)) {
				return 0, memory, true
			}
			value = binary.LittleEndian.Uint32(memory[first:])
			if c.rich {
				source = c.src + a + bias + c.neighbor
				second := uint64(source) + uint64(c.loadOffset)
				if second+4 > uint64(len(memory)) {
					return 0, memory, true
				}
				value = (value+binary.LittleEndian.Uint32(memory[second:]))*7 ^ 0xdeadbeef
			}
			output := uint64(dest) + uint64(c.storeOffset)
			if output+4 > uint64(len(memory)) {
				return 0, memory, true
			}
			binary.LittleEndian.PutUint32(memory[output:], value)
		}
		if !c.early {
			a += step
			b += step
		}
		count += step / c.ratio
		if count == c.limit {
			return 123 ^ a ^ b ^ count ^ source ^ dest ^ value, memory, false
		}
	}
	panic("induction oracle exceeded bounded iterations")
}

func TestIntegerMapRelatedInductions(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(uint32(i)*197 + uint32(i>>3)*43)
	}
	for _, packets := range []int{1, 2, 3, 4} {
		for _, ratio := range []uint32{1, 2, 4, 8} {
			if uint32(packets)*4%ratio != 0 {
				continue
			}
			for _, iterations := range []uint32{1, 5, 9, 33} {
				for variant := 0; variant < 8; variant++ {
					c := integerInductionCase{packets: packets, ratio: ratio, src: 32, dst: 8192, limit: iterations * uint32(packets) * 4 / ratio, neighbor: 4, dynamic: true}
					switch variant {
					case 1:
						c.absolute = true
					case 2:
						c.rich = true
						c.localBase = 70
					case 3:
						c.early = true
					case 4:
						c.conditionFirst = true
					case 5:
						c.src = 256
						c.dst = 260
						c.indexB = 0 // forward partial overlap
					case 6:
						c.src = 256
						c.dst = 256
						c.indexB = 20 // safe inter-group dependence
					case 7:
						c.indexA = 0xfffffff0
						c.indexB = 0xfffffff0
						c.src = 256
						c.dst = 8192
						c.loadOffset = 7
						c.storeOffset = 13
					}
					checkIntegerInductionCase(t, initial, c, iterations >= 33 && variant == 0)
				}
			}
		}
	}
}

func checkIntegerInductionCase(t *testing.T, initial []byte, c integerInductionCase, admit bool) {
	t.Helper()
	m := integerInductionModule(t, c)
	want, memory, traps := integerInductionOracle(initial, c)
	for _, enabled := range []bool{false, true} {
		got, after, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": enabled}, initial, "integer-load-map-vector")
		if (err != nil) != traps || !traps && got != want || !bytes.Equal(after, memory) {
			t.Fatalf("case=%+v enabled=%t got=%x want=%x trap=%v wantTrap=%t memory=%t", c, enabled, got, want, err, traps, bytes.Equal(after, memory))
		}
		if admit && enabled && diagnosticsEnabled && integerLoadMapEnabled && hits == 0 {
			t.Fatalf("related-induction map not emitted: %+v", c)
		}
	}
}

func TestIntegerMapInductionBoundsAndContinuation(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*181 + i/17)
	}
	cases := []integerInductionCase{
		{packets: 4, ratio: 4, src: 32, dst: 8192, count: 0xffffff80, limit: 0, dynamic: false},
		{packets: 3, ratio: 4, src: 32, dst: 8192, count: 0xffffff80, limit: 0, dynamic: true},
		{packets: 4, ratio: 4, src: 32, dst: 8192, count: 0, limit: 0x40000010, dynamic: true},
		{packets: 3, ratio: 4, src: 32, dst: 8192, count: 0, limit: 100, dynamic: true},
		{packets: 4, ratio: 4, src: 65516, dst: 8192, count: 0, limit: 132, dynamic: true},
		{packets: 4, ratio: 4, src: 32, dst: 65516, count: 0, limit: 132, dynamic: true},
		{packets: 4, ratio: 4, src: 32, dst: 8192, count: 0, limit: 132, dynamic: true, loadOffset: 0xfffffff0},
		{packets: 4, ratio: 4, src: 32, dst: 8192, count: 100, limit: 100, dynamic: true},
	}
	for _, c := range cases {
		checkIntegerInductionCase(t, initial, c, false)
	}
}

func TestIntegerMapNestedCopyAdmission(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/7)
	}
	for _, parents := range []int{0, 1, 2, 3} {
		for _, rich := range []bool{false, true} {
			c := integerInductionCase{parents: parents, packets: 4, ratio: 4, src: 32, dst: 8192, limit: 132, dynamic: true, rich: rich, neighbor: 4}
			m := integerInductionModule(t, c)
			want, memory, traps := integerInductionOracle(initial, c)
			got, after, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
			if traps || err != nil || got != want || !bytes.Equal(after, memory) {
				t.Fatalf("parents=%d rich=%t result=%x want=%x err=%v memory=%t", parents, rich, got, want, err, bytes.Equal(after, memory))
			}
			if diagnosticsEnabled && integerLoadMapEnabled && (hits != 0) != (parents < 2 || rich) {
				t.Fatalf("unexpected nested admission parents=%d rich=%t hits=%d", parents, rich, hits)
			}
		}
	}
}
