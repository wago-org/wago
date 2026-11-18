//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerLoadMapModule(t testing.TB, packets int, src, dst, start, limit, neighbor, loadOffset, storeOffset uint32, localBase byte, coefficient ...uint32) *wasm.Module {
	body := []byte{1, 6 + localBase, 0x7f}
	if len(coefficient) != 0 {
		body[1]++
	}
	constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	for i, v := range []uint32{src, dst, start} {
		constant(v)
		body = append(body, 0x21, byte(i)+localBase)
	}
	if len(coefficient) != 0 {
		v := coefficient[0]
		if v == ^uint32(0) {
			v = 7
		}
		constant(v)
		body = append(body, 0x21, 6+localBase)
	}
	constant(123)
	body = append(body, 0x03, 0x40)
	address := func(base, alias byte, bias uint32) {
		body = append(body, 0x20, base+localBase, 0x20, 2+localBase, 0x6a)
		constant(bias)
		body = append(body, 0x6a, 0x22, alias+localBase)
	}
	for i := 0; i < packets; i++ {
		bias := uint32(i) * 4
		address(1, 4, bias)
		address(0, 3, bias)
		body = append(body, 0x28, 0)
		body = append(body, wasmtest.ULEB(loadOffset)...)
		address(0, 3, bias+neighbor)
		body = append(body, 0x28, 0)
		body = append(body, wasmtest.ULEB(loadOffset)...)
		body = append(body, 0x6a)
		if len(coefficient) == 0 {
			constant(7)
		} else {
			body = append(body, 0x20, 6+localBase)
			if coefficient[0] != ^uint32(0) {
				body = append(body, 0x28, 0, 0)
			}
		}
		body = append(body, 0x6c)
		constant(0xdeadbeef)
		body = append(body, 0x73, 0x22, 5+localBase, 0x36, 0)
		body = append(body, wasmtest.ULEB(storeOffset)...)
	}
	body = append(body, 0x20, 2+localBase)
	constant(uint32(packets) * 4)
	body = append(body, 0x6a, 0x22, 2+localBase)
	if limit != 0 {
		constant(limit)
		body = append(body, 0x47)
	}
	body = append(body, 0x0d, 0, 0x0b)
	for _, local := range []byte{2, 3, 4, 5} {
		body = append(body, 0x20, local+localBase, 0x73)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func integerLoadMapOracle(initial []byte, packets int, src, dst, start, limit, neighbor, loadOffset, storeOffset uint32, coefficient ...uint32) (uint32, []byte, bool) {
	memory := append([]byte(nil), initial...)
	counter := start
	var s, d, value uint32
	for iterations := 0; iterations < 20000; iterations++ {
		for lane := 0; lane < packets; lane++ {
			bias := uint32(lane) * 4
			d = dst + counter + bias
			s = src + counter + bias
			first := uint64(s) + uint64(loadOffset)
			if first+4 > uint64(len(memory)) {
				return 0, memory, true
			}
			a := binary.LittleEndian.Uint32(memory[first:])
			s = src + counter + bias + neighbor
			second := uint64(s) + uint64(loadOffset)
			if second+4 > uint64(len(memory)) {
				return 0, memory, true
			}
			b := binary.LittleEndian.Uint32(memory[second:])
			factor := uint32(7)
			if len(coefficient) != 0 && coefficient[0] != ^uint32(0) {
				address := uint64(coefficient[0])
				if address+4 > uint64(len(memory)) {
					return 0, memory, true
				}
				factor = binary.LittleEndian.Uint32(memory[address:])
			}
			value = (a+b)*factor ^ 0xdeadbeef
			output := uint64(d) + uint64(storeOffset)
			if output+4 > uint64(len(memory)) {
				return 0, memory, true
			}
			binary.LittleEndian.PutUint32(memory[output:], value)
		}
		counter += uint32(packets) * 4
		if counter == limit {
			return 123 ^ counter ^ s ^ d ^ value, memory, false
		}
	}
	panic("test oracle exceeded bounded iterations")
}

func TestIntegerLoadMapMemoryAliasesAndTails(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(uint32(i)*197 + uint32(i>>4)*43)
	}
	cases := []struct {
		src, dst, neighbor, loadOffset, storeOffset uint32
		localBase                                   byte
	}{
		{32, 8192, 4, 0, 0, 0}, {32, 8192, 4, 7, 13, 0}, {256, 256, 4, 0, 0, 0},
		{256, 260, 4, 0, 0, 0}, {256, 264, 4, 0, 0, 0}, {256, 268, 4, 0, 0, 0}, {256, 272, 4, 0, 0, 0}, {256, 276, 4, 0, 0, 0},
		{260, 256, 4, 0, 0, 0}, {256, 257, 4, 0, 0, 0}, {257, 256, 4, 0, 0, 0},
		{32, 8192, 0, 0, 0, 70}, {65516, 8192, 4, 0, 0, 0}, {32, 65516, 4, 0, 0, 0}, {32, 8192, 4, 0xfffffff0, 0, 0},
	}
	for _, packets := range []int{1, 2, 3, 4} {
		for _, iterations := range []uint32{1, 2, 3, 4, 5, 9, 16, 33} {
			for ci, c := range cases {
				limit := iterations * uint32(packets) * 4
				m := integerLoadMapModule(t, packets, c.src, c.dst, 0, limit, c.neighbor, c.loadOffset, c.storeOffset, c.localBase)
				want, memory, traps := integerLoadMapOracle(initial, packets, c.src, c.dst, 0, limit, c.neighbor, c.loadOffset, c.storeOffset)
				for _, enabled := range []bool{false, true} {
					got, after, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": enabled}, initial, "integer-load-map-vector")
					if (err != nil) != traps || !traps && got != want || !bytes.Equal(after, memory) {
						t.Fatalf("packets=%d iterations=%d case=%d enabled=%t result=%x want=%x trap=%v wantTrap=%t memoryEqual=%t", packets, iterations, ci, enabled, got, want, err, traps, bytes.Equal(after, memory))
					}
					if diagnosticsEnabled && enabled && integerLoadMapEnabled && ci == 0 && limit >= 128 && hits == 0 {
						t.Fatalf("integer map not emitted packets=%d iterations=%d", packets, iterations)
					}
				}
			}
		}
	}
}

func TestIntegerLoadMapInvariantInputs(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/4)
	}
	for _, coefficient := range []uint32{4096, 8196, 65535, ^uint32(0)} {
		for _, packets := range []int{1, 3} {
			for _, start := range []uint32{0, 0xffffff80} {
				src, dst, limit := uint32(256), uint32(8192), uint32(128)*uint32(packets)
				if start != 0 {
					limit = 0
				}
				m := integerLoadMapModule(t, packets, src, dst, start, limit, 4, 0, 0, 70, coefficient)
				want, memory, traps := integerLoadMapOracle(initial, packets, src, dst, start, limit, 4, 0, 0, coefficient)
				for _, enabled := range []bool{false, true} {
					got, after, err, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": enabled}, initial, "integer-load-map-vector")
					if (err != nil) != traps || !traps && got != want || !bytes.Equal(after, memory) {
						t.Fatalf("coefficient=%x packets=%d start=%x enabled=%t got=%x err=%v want=%x trap=%t memoryEqual=%t", coefficient, packets, start, enabled, got, err, want, traps, bytes.Equal(after, memory))
					}
				}
			}
		}
	}
}

func TestIntegerLoadMapBudgetLeavesReaderUnchanged(t *testing.T) {
	f := fn{nLocals: 1, localType: []machineType{mtI32}, integerLoadMapProbe: true, integerLoadMapWork: integerLoadMapWorkLimit - 1}
	reader := wasm.NewReader([]byte{0x41, 0, 0x21, 0, 0x41, 1, 0x1a})
	var p floatMapPlan
	if ok := f.inspectFloatMapLoop(reader, &p); ok || p.work <= p.workLimit {
		t.Fatal("exhausted integer analysis budget was not rejected")
	}
	if reader.Offset() != 0 {
		t.Fatal("rejected probe advanced source reader")
	}
	want := p
	if f.inspectFloatMapLoop(reader, &p) || p != want {
		t.Fatal("reused workspace changed the rejected proof")
	}
	if allocations := testing.AllocsPerRun(100, func() { var candidate floatMapPlan; f.inspectFloatMapLoop(reader, &candidate) }); allocations != 0 {
		t.Fatalf("rejected probe allocated %g objects", allocations)
	}
}

func TestIntegerLoadMapRejectedCandidateAllocatesZero(t *testing.T) {
	f := fn{}
	reader := wasm.NewReader([]byte{0x1a}) // unsupported candidate, no emitter needed
	allocations := testing.AllocsPerRun(100, func() {
		f.integerLoadMapWork = 0
		f.emitIntegerLoadMapCandidate(reader)
	})
	if allocations != 0 {
		t.Fatalf("rejected candidate allocated %g objects", allocations)
	}
	if reader.Offset() != 0 || f.integerLoadMapProbe {
		t.Fatal("rejected candidate leaked reader or admission state")
	}
}

func TestIntegerLoadMapSelect(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	clear(initial[80:92])
	type comparison struct {
		op   byte
		bias bool
	}
	cases := []comparison{{op: 0}}
	for op := byte(0x45); op <= 0x4f; op++ {
		cases = append(cases, comparison{op: op})
		if op >= 0x48 {
			// A different comparison operand prevents min/max recognition.
			cases = append(cases, comparison{op: op, bias: true})
		}
	}
	for _, tc := range cases {
		compare := tc.op
		for _, swapped := range []bool{false, true} {
			for _, dst := range []uint32{8192, 68, 65532} {
				body := []byte{1, 5, 0x7f}
				constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
				for i, v := range []uint32{64, dst, 0} {
					constant(v)
					body = append(body, 0x21, byte(i))
				}
				body = append(body, 0x03, 0x40)
				// Select between adjacent input words with either a raw or compared condition.
				body = append(body, 0x20, 1, 0x20, 2, 0x6a)
				body = append(body, 0x20, 0, 0x20, 2, 0x6a, 0x28, 2, 0, 0x22, 3)
				body = append(body, 0x20, 0, 0x20, 2, 0x6a, 0x28, 2, 4, 0x22, 4)
				left, right := byte(3), byte(4)
				if swapped {
					left, right = right, left
				}
				body = append(body, 0x20, left)
				if tc.bias {
					body = append(body, 0x41, 1, 0x6a)
				}
				if compare >= 0x46 {
					body = append(body, 0x20, right)
				}
				if compare != 0 {
					body = append(body, compare)
				}
				body = append(body, 0x1b, 0x36, 2, 0)
				body = append(body, 0x20, 2, 0x41, 4, 0x6a, 0x22, 2)
				constant(132)
				body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x20, 4, 0x73, 0x0b)
				m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
				want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial, "integer-load-map-vector")
				got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
				if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
					t.Fatalf("cmp=%x biased=%v swapped=%v dst=%d: got %x/%v, want %x/%v, memory equal=%v", compare, tc.bias, swapped, dst, got, err, want, wantErr, bytes.Equal(actual, expected))
				}
				if diagnosticsEnabled && dst == 8192 && hits == 0 {
					t.Fatalf("cmp=%x biased=%v swapped=%v: select map was not emitted", compare, tc.bias, swapped)
				}
			}
		}
	}
}

func TestIntegerLoadMapShifts(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	for _, op := range []byte{0x74, 0x75, 0x76} {
		for _, count := range []int32{0, 7, 31, 32, -1, -2} {
			for _, dst := range []int32{8192, 68, 65532} {
				body := []byte{1, 4, 0x7f}
				constant := func(x int32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(x)...) }
				for i, x := range []int32{64, dst} {
					constant(x)
					body = append(body, 0x21, byte(i))
				}
				body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x6a)
				body = append(body, 0x20, 0, 0x20, 2, 0x6a, 0x28, 2, 0)
				if count == -2 {
					// Read per-lane counts from a disjoint offset. The patterned
					// words include negative and oversized counts.
					body = append(body, 0x20, 0, 0x20, 2, 0x6a, 0x28, 2, 0x80, 0x20)
				} else {
					constant(count)
				}
				body = append(body, op, 0x22, 3, 0x36, 2, 0, 0x20, 2, 0x41, 4, 0x6a, 0x22, 2)
				constant(132)
				body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
				m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
				want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial, "integer-load-map-vector")
				got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
				if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
					t.Fatalf("op=%x shift=%d dst=%d: got %x/%v, want %x/%v, memory equal=%v", op, count, dst, got, err, want, wantErr, bytes.Equal(actual, expected))
				}
				if diagnosticsEnabled && dst == 8192 && hits == 0 {
					t.Fatalf("op=%x shift=%d: map was not emitted", op, count)
				}
			}
		}
	}
}

func TestIntegerLoadMapStridedStreams(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, stride := range []int32{4, 8, 12, 128} {
		for _, scatter := range []bool{false, true} {
			for _, layout := range []struct{ src, dst, count int32 }{
				{64, 32768, 64}, {64, 32768, 67}, {64, 68, 67},
				{65520, 32768, 67}, {-8, 32768, 67},
			} {
				body := []byte{1, 6, 0x7f}
				constant := func(v int32) {
					body = append(body, 0x41)
					body = append(body, wasmtest.SLEB32(v)...)
				}
				constant(layout.src)
				body = append(body, 0x21, 0)
				constant(layout.dst)
				body = append(body, 0x21, 1, 0x03, 0x40)
				dstIndex := byte(2)
				if scatter {
					dstIndex = 5
				}
				body = append(body,
					0x20, 1, 0x20, dstIndex, 0x6a,
					0x20, 0, 0x20, 3, 0x6a, 0x28, 2, 1, 0x41, 7, 0x73,
					0x22, 4, 0x36, 2, 1, 0x20, 3)
				constant(stride)
				body = append(body, 0x6a, 0x21, 3)
				if scatter {
					body = append(body, 0x20, 5, 0x41, 8, 0x6a, 0x21, 5)
				}
				body = append(body, 0x20, 2, 0x41, 4, 0x6a, 0x22, 2)
				constant(4 * layout.count)
				body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x20, 4, 0x73, 0x20, 5, 0x73, 0x0b)
				m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
				want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
				got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
				if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
					t.Fatalf("stride=%d scatter=%v layout=%+v: got %x/%v, want %x/%v, memory equal=%v", stride, scatter, layout, got, err, want, wantErr, bytes.Equal(actual, expected))
				}
				if diagnosticsEnabled && hits == 0 {
					t.Fatalf("stride=%d scatter=%v layout=%+v: vector map was not emitted", stride, scatter, layout)
				}
			}
		}
	}
}

func TestIntegerLoadMapIndependentStores(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, tc := range []struct{ dst0, dst1, src0, src1 int32 }{
		{1024, 2048, 4096, 8192}, {1024, 1028, 4096, 8192},
		{1024, 2048, 4096, 1028}, {1024, 2048, 2044, 8192},
		{1024, 65532, 4096, 8192}, {1024, 2048, 4096, 65532},
	} {
		body := []byte{1, 7, 0x7f}
		constant := func(v int32) {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(v)...)
		}
		for i, v := range []int32{tc.dst0, tc.dst1, tc.src0, tc.src1} {
			constant(v)
			body = append(body, 0x21, byte(i))
		}
		body = append(body, 0x03, 0x40)
		// Independent i32 expressions, with observable result locals.
		body = append(body, 0x20, 0, 0x20, 4, 0x6a,
			0x20, 2, 0x20, 4, 0x6a, 0x28, 2, 0, 0x41, 3, 0x6a, 0x22, 5, 0x36, 2, 0,
			0x20, 1, 0x20, 4, 0x6a,
			0x20, 3, 0x20, 4, 0x6a, 0x28, 2, 0, 0x41, 7, 0x73, 0x22, 6, 0x36, 2, 0,
			0x20, 4, 0x41, 4, 0x6a, 0x22, 4)
		constant(268)
		body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 5, 0x20, 6, 0x73, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
		if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
			t.Fatalf("streams=%+v: got %x/%v, want %x/%v, memory equal=%v", tc, got, err, want, wantErr, bytes.Equal(actual, expected))
		}
		if diagnosticsEnabled && hits == 0 {
			t.Fatalf("streams=%+v: independent stores were not vectorized", tc)
		}
	}
}

func TestIntegerLoadMapCounterValue(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	for _, step := range []byte{1, 2, 4} {
		for _, layout := range []struct{ dst, start int32 }{{8192, 0}, {8192, 13}, {68, 0}, {65532, 0}} {
			body := []byte{1, 5, 0x7f}
			constant := func(v int32) {
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(v)...)
			}
			for i, v := range []int32{64, layout.dst, layout.start} {
				constant(v)
				body = append(body, 0x21, byte(i))
			}
			body = append(body, 0x03, 0x40,
				0x20, 1, 0x20, 3, 0x6a,
				0x20, 0, 0x20, 3, 0x6a, 0x28, 2, 0,
				0x20, 2, 0x6a, 0x22, 4, 0x36, 2, 0,
				0x20, 3, 0x41, 4, 0x6a, 0x21, 3,
				0x20, 2, 0x41, step, 0x6a, 0x22, 2)
			constant(layout.start + 67*int32(step))
			body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
			got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
				t.Fatalf("step=%d layout=%+v: got %x/%v, want %x/%v, memory equal=%v", step, layout, got, err, want, wantErr, bytes.Equal(actual, expected))
			}
			if diagnosticsEnabled && hits == 0 {
				t.Fatalf("step=%d layout=%+v: vector map was not emitted", step, layout)
			}
		}
	}
}

func TestIntegerLoadMapInterleavedStores(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, tc := range []struct{ dst, src, count int32 }{
		{1024, 8192, 64}, {1024, 8192, 67}, {1024, 1028, 67},
		{1024, 1020, 67}, {65532, 8192, 67}, {1024, 65532, 67},
	} {
		body := []byte{1, 5, 0x7f}
		constant := func(v int32) {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(v)...)
		}
		constant(tc.dst)
		body = append(body, 0x21, 0)
		constant(tc.src)
		body = append(body, 0x21, 1, 0x03, 0x40)
		body = append(body,
			0x20, 0, 0x20, 2, 0x6a,
			0x20, 1, 0x20, 2, 0x6a, 0x28, 2, 0, 0x41, 3, 0x6a, 0x22, 3, 0x36, 2, 0,
			0x20, 0, 0x20, 2, 0x6a, 0x41, 4, 0x6a,
			0x20, 1, 0x20, 2, 0x6a, 0x28, 2, 4, 0x41, 7, 0x73, 0x22, 4, 0x36, 2, 0,
			0x20, 2, 0x41, 8, 0x6a, 0x22, 2)
		constant(8 * tc.count)
		body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x20, 4, 0x73, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
		if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
			t.Fatalf("stream=%+v: got %x/%v, want %x/%v, memory equal=%v", tc, got, err, want, wantErr, bytes.Equal(actual, expected))
		}
		if diagnosticsEnabled && hits == 0 {
			t.Fatalf("stream=%+v: interleaved stores were not vectorized", tc)
		}
	}
}
