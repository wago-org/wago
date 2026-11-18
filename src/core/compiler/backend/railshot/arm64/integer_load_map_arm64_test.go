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
		for _, layout := range []struct{ dst, start, count int32 }{{8192, 0, 64}, {8192, 0, 67}, {8192, 13, 64}, {68, 0, 67}, {65532, 0, 64}} {
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
			constant(layout.start + layout.count*int32(step))
			body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
			got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector-live-out")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
				t.Fatalf("step=%d layout=%+v: got %x/%v, want %x/%v, memory equal=%v", step, layout, got, err, want, wantErr, bytes.Equal(actual, expected))
			}
			if diagnosticsEnabled && hits == 0 {
				t.Fatalf("step=%d layout=%+v: vector live-out was not emitted", step, layout)
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

func TestIntegerLoadMapTypedAfterLoadScreen(t *testing.T) {
	for _, tc := range []struct {
		code []byte
		want bool
	}{
		{[]byte{2, 0, 0x41, 0x36, 0x6a, 0x36, 2, 0}, true},
		{[]byte{2, 0, 0x41, 0x36, 0x10, 0, 0x36, 2, 0}, false},
		{[]byte{0x42, 1, 0, 0x36, 2, 0}, false},
		{[]byte{2, 0, 0x28, 0x42, 1, 0, 0x36, 2, 0}, false},
		{[]byte{2, 0, 0x04, 0x40, 0x36, 2, 0}, false},
		{append([]byte{2, 0}, bytes.Repeat([]byte{0x6a}, 12)...), true},
	} {
		f := fn{}
		reader := wasm.ReaderFrom(tc.code)
		if allocs := testing.AllocsPerRun(10, func() {
			f.integerLoadMapWork = 0
			if f.integerLoadMapAfterLoad(reader) != tc.want || reader.Offset() != 0 {
				t.Fatalf("screen code=%x did not preserve the reader or match admission=%v", tc.code, tc.want)
			}
		}); allocs != 0 {
			t.Fatalf("screen code=%x allocations=%g", tc.code, allocs)
		}
	}
}

func TestIntegerLoadMapAffineExit(t *testing.T) {
	old := integerMapAffineExitEnabled
	defer func() { integerMapAffineExitEnabled = old }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	for _, tc := range []struct {
		src, dst, start, trips, bias int32
		compound                     bool
	}{
		{64, 8192, 0, 64, 0, false}, {64, 8192, 0, 67, 0, false}, {64, 8192, 20, 67, -17, false}, {64, 8192, 20, 64, -27, false},
		{64, 68, 0, 64, 3, false}, {64, 65532, 0, 64, 3, false}, {65532, 8192, 0, 64, 3, false},
		{64, 8192, 20, 64, -27, true}, {64, 8192, 20, 67, -27, true},
	} {
		body := []byte{1, 4, 0x7f}
		constant := func(v int32) {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(v)...)
		}
		for i, v := range []int32{tc.src, tc.dst, tc.start} {
			constant(v)
			body = append(body, 0x21, byte(i))
		}
		body = append(body, 0x03, 0x40, 0x20, 2)
		if tc.compound {
			// Save 3*counter - 2*sourceBase + bias.
			body = append(body, 0x41, 3, 0x6c, 0x20, 0, 0x41, 1, 0x74, 0x6b)
		}
		if tc.bias != 0 {
			constant(tc.bias)
			body = append(body, 0x6a)
		}
		body = append(body, 0x21, 3,
			0x20, 1, 0x20, 2, 0x6a,
			0x20, 0, 0x20, 2, 0x6a, 0x28, 2, 0, 0x36, 2, 0,
			0x20, 2, 0x41, 4, 0x6a, 0x22, 2)
		constant(tc.start + tc.trips*4)
		body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		for _, enabled := range []bool{false, true} {
			integerMapAffineExitEnabled = enabled
			got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-affine-exit")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
				t.Fatalf("enabled=%v case=%+v: got %x/%v, want %x/%v, memory equal=%v", enabled, tc, got, err, want, wantErr, bytes.Equal(actual, expected))
			}
			if diagnosticsEnabled && (hits != 0) != enabled {
				t.Fatalf("enabled=%v case=%+v: affine exit emitted %d times", enabled, tc, hits)
			}
			if diagnosticsEnabled && !enabled && tc.bias == 0 {
				_, _, _, vectors := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
				if vectors == 0 {
					t.Fatalf("case=%+v: disabling affine exit also disabled vectorization", tc)
				}
			}
		}
	}
}

func TestIntegerLoadMapScaledCounterAddress(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	for _, tc := range []struct {
		src, dst, start, shift, count int32
		mixed                         bool
	}{
		{64, 8192, 0, 2, 64, false}, {64, 8192, 7, 3, 67, false},
		{64, 8192, 7, 3, 67, true},
		{64, 8192, 1 << 30, 2, 67, false}, {64, 65, 0, 2, 67, false},
		{64, 65532, 0, 2, 67, false}, {65532, 8192, 0, 2, 67, false},
	} {
		body := []byte{1, 3, 0x7f}
		constant := func(v int32) {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(v)...)
		}
		for i, v := range []int32{tc.src, tc.dst, tc.start} {
			constant(v)
			body = append(body, 0x21, byte(i))
		}
		body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x41, 4, 0x6c, 0x6a,
			0x20, 0, 0x20, 2)
		constant(tc.shift)
		body = append(body, 0x74, 0x6a)
		// Exercise a negative dynamic bias before the widened load offset.
		body = append(body, 0x41, 4, 0x6b, 0x28, 2, 1)
		if tc.mixed {
			// Distinct coefficients with identical base/index need distinct streams.
			body = append(body, 0x20, 0, 0x20, 2, 0x41, 2, 0x74, 0x6a, 0x41, 4, 0x6b, 0x28, 2, 1, 0x73)
			// Same stride and index as the first load, with a positive bias.
			body = append(body, 0x20, 0, 0x20, 2, 0x41, byte(tc.shift), 0x74, 0x6a,
				0x41, 4, 0x6a, 0x28, 2, 1, 0x73)
		}
		body = append(body, 0x36, 2, 0,
			0x20, 2, 0x41, 1, 0x6a, 0x22, 2)
		constant(tc.start + tc.count)
		body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
		if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
			t.Fatalf("case=%+v: got %x/%v, want %x/%v, memory equal=%v", tc, got, err, want, wantErr, bytes.Equal(actual, expected))
		}
		if diagnosticsEnabled && hits == 0 {
			t.Fatalf("case=%+v: scaled addresses were not vectorized", tc)
		}
	}
}

func TestIntegerLoadMapAffineLimit(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	for _, reversed := range []bool{false, true} {
		for _, tc := range []struct{ dst, start, count int32 }{
			{8192, 0, 64}, {8192, 9, 67}, {68, 0, 67}, {65532, 0, 64},
		} {
			body := []byte{1, 6, 0x7f}
			constant := func(v int32) {
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(v)...)
			}
			// Bound is 4*local 3 - local 4 + 5, including odd starts.
			bound := tc.start + 4*tc.count
			for i, v := range []int32{64, tc.dst, tc.start, 200, 805 - bound} {
				constant(v)
				body = append(body, 0x21, byte(i))
			}
			body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x6a,
				0x20, 0, 0x20, 2, 0x6a, 0x28, 2, 0, 0x41, 7, 0x73, 0x22, 5, 0x36, 2, 0)
			limit := []byte{0x20, 3, 0x41, 2, 0x74, 0x20, 4, 0x6b, 0x41, 5, 0x6a}
			advance := []byte{0x20, 2, 0x41, 4, 0x6a, 0x22, 2}
			if reversed {
				body = append(body, limit...)
				body = append(body, advance...)
			} else {
				body = append(body, advance...)
				body = append(body, limit...)
			}
			body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 5, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
			got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
				t.Fatalf("reversed=%v case=%+v: got %x/%v, want %x/%v, memory equal=%v", reversed, tc, got, err, want, wantErr, bytes.Equal(actual, expected))
			}
			if diagnosticsEnabled && hits == 0 {
				t.Fatalf("reversed=%v case=%+v: affine limit was not vectorized", reversed, tc)
			}
		}
	}
}

func TestIntegerLoadMapInvariantOverlapPrefix(t *testing.T) {
	old, oldExit := integerMapInvariantPrefixEnabled, integerMapAffineExitEnabled
	defer func() { integerMapInvariantPrefixEnabled, integerMapAffineExitEnabled = old, oldExit }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	for _, limit := range []int32{256, 268} {
		for _, addresses := range [][2]int32{
			{1020, -1}, {1021, -1}, {1024, -1}, {1151, -1}, {1152, -1}, {1153, -1},
			{1280, -1}, {4096, -1}, {65533, -1},
			{1040, 1216}, {1216, 1040}, {1023, 1103}, {1152, 1153}, {1152, 65533},
		} {
			invariant, second := addresses[0], addresses[1]
			body := []byte{1, 6, 0x7f}
			constant := func(v int32) {
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(v)...)
			}
			for i, v := range []int32{8192, 1024, invariant} {
				constant(v)
				body = append(body, 0x21, byte(i))
			}
			if second >= 0 {
				constant(second)
				body = append(body, 0x21, 5)
			}
			body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 3, 0x6a,
				0x20, 0, 0x20, 3, 0x6a, 0x28, 2, 0,
				0x20, 2, 0x28, 2, 0, 0x6a)
			if second >= 0 {
				body = append(body, 0x20, 5, 0x28, 2, 0, 0x6a)
			}
			body = append(body, 0x22, 4, 0x36, 2, 0,
				0x20, 3, 0x41, 4, 0x6a, 0x22, 3)
			constant(limit)
			body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
			for _, affineExit := range []bool{false, true} {
				integerMapAffineExitEnabled = affineExit
				for _, enabled := range []bool{false, true} {
					integerMapInvariantPrefixEnabled = enabled
					got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-invariant-reentry")
					if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
						t.Fatalf("enabled=%v invariants=%v: got %x/%v, want %x/%v, memory equal=%v", enabled, addresses, got, err, want, wantErr, bytes.Equal(actual, expected))
					}
					if diagnosticsEnabled && (hits != 0) != enabled {
						t.Fatalf("enabled=%v invariants=%v: alias prefix emitted %d times", enabled, addresses, hits)
					}
				}
			}
		}
	}
}

func TestIntegerLoadMapMixedBiasDependence(t *testing.T) {
	// The store at base+i+4 changes the load at base+i-4 two iterations later.
	// The compiler must preserve that dependence within a four-word packet.
	body := []byte{1, 2, 0x7f, 0x41, 0x80, 8, 0x21, 0, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6a,
		0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6b, 0x28, 2, 0,
		0x41, 3, 0x6a, 0x36, 2, 0,
		0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x41, 0x80, 2, 0x47, 0x0d, 0, 0x0b,
		0x20, 1, 0x0b}
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*197 + i/17)
	}
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
	want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
	got, actual, err, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial)
	if err != nil || wantErr != nil || got != want || !bytes.Equal(actual, expected) {
		t.Fatalf("got %x/%v, want %x/%v, memory equal=%v", got, err, want, wantErr, bytes.Equal(actual, expected))
	}
}

func TestIntegerLoadMapThreeIndependentStores(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, bases := range [][4]int32{
		{1024, 2048, 4096, 8192},
		{1024, 2048, 1028, 8192},
		{1024, 2048, 4096, 4100},
		{1024, 2048, 65532, 8192},
	} {
		body := []byte{1, 6, 0x7f}
		for i, base := range bases {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(base)...)
			body = append(body, 0x21, byte(i))
		}
		body = append(body, 0x03, 0x40,
			0x20, 3, 0x20, 4, 0x6a, 0x28, 2, 0, 0x21, 5,
			0x20, 0, 0x20, 4, 0x6a, 0x20, 5, 0x36, 2, 0,
			0x20, 1, 0x20, 4, 0x6a, 0x20, 5, 0x41, 3, 0x6a, 0x36, 2, 0,
			0x20, 2, 0x20, 4, 0x6a, 0x20, 5, 0x41, 7, 0x73, 0x36, 2, 0,
			0x20, 4, 0x41, 4, 0x6a, 0x22, 4, 0x41, 0x8c, 2, 0x47, 0x0d, 0, 0x0b,
			0x20, 5, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
		if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
			t.Fatalf("bases=%v: got %x/%v, want %x/%v, memory equal=%v", bases, got, err, want, wantErr, bytes.Equal(actual, expected))
		}
		if diagnosticsEnabled && hits == 0 {
			t.Fatalf("bases=%v: three stores were not vectorized", bases)
		}
	}
}

func TestIntegerLoadMapDiscardedLoadPreservesTrap(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, tc := range []struct {
		base      int32
		invariant bool
		trap      bool
	}{
		{8192, false, false},
		{1024, false, false}, // overlaps the output
		{65528, false, true}, // traps after earlier stores
		{8192, true, false},
		{65533, true, true},
	} {
		body := []byte{1, 4, 0x7f}
		for i, base := range []int32{2048, 1024, tc.base} {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(base)...)
			body = append(body, 0x21, byte(i))
		}
		body = append(body, 0x03, 0x40, 0x01, 0x20, 2)
		if !tc.invariant {
			body = append(body, 0x20, 3, 0x6a)
		}
		body = append(body, 0x28, 2, 0, 0x1a,
			0x20, 1, 0x20, 3, 0x6a, 0x20, 0, 0x20, 3, 0x6a, 0x28, 2, 0, 0x36, 2, 0,
			0x20, 3, 0x41, 4, 0x6a, 0x22, 3, 0x41, 0x8c, 2, 0x47, 0x0d, 0, 0x0b,
			0x20, 3, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
		if (wantErr != nil) != tc.trap || (err != nil) != tc.trap || got != want || !bytes.Equal(actual, expected) {
			t.Fatalf("case=%+v: got %x/%v, want %x/%v, memory equal=%v", tc, got, err, want, wantErr, bytes.Equal(actual, expected))
		}
		if diagnosticsEnabled && hits == 0 {
			t.Fatalf("case=%+v: discarded load prevented vectorization", tc)
		}
	}
}

func TestIntegerLoadMapPackedAdjacent(t *testing.T) {
	old := integerMapAdjacentSLPEnabled
	defer func() { integerMapAdjacentSLPEnabled = old }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, tc := range []struct{ dst, src, coefficient, count int32 }{
		{1024, 8192, 4096, 64}, {1024, 8192, 4096, 67}, {1024, 1020, 4096, 67},
		{1024, 1028, 4096, 67}, {1024, 8192, 1028, 67}, {1024, 65532, 4096, 67},
		{1024, 8192, 65533, 67},
	} {
		body := []byte{1, 4, 0x7f}
		constant := func(v int32) {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(v)...)
		}
		for i, base := range []int32{tc.dst, tc.src, tc.coefficient} {
			constant(base)
			body = append(body, 0x21, byte(i))
		}
		body = append(body, 0x03, 0x40,
			0x20, 0, 0x20, 3, 0x6a,
			0x20, 1, 0x20, 3, 0x6a, 0x28, 2, 0, 0x20, 2, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
			0x20, 0, 0x20, 3, 0x6a, 0x41, 4, 0x6a,
			0x20, 2, 0x28, 2, 0, 0x20, 1, 0x20, 3, 0x6a, 0x28, 2, 4, 0x6a, 0x36, 2, 0,
			0x20, 3, 0x41, 8, 0x6a, 0x22, 3)
		constant(8 * tc.count)
		body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		for _, enabled := range []bool{false, true} {
			integerMapAdjacentSLPEnabled = enabled
			got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-packed-adjacent")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
				t.Fatalf("case=%+v enabled=%v: got %x/%v, want %x/%v, memory equal=%v", tc, enabled, got, err, want, wantErr, bytes.Equal(actual, expected))
			}
			if diagnosticsEnabled && (hits != 0) != enabled {
				t.Fatalf("case=%+v enabled=%v: packed adjacent hits=%d", tc, enabled, hits)
			}
		}
	}
}

func TestIntegerLoadMapRuntimeAdjacent(t *testing.T) {
	old := integerMapAdjacentSLPEnabled
	defer func() { integerMapAdjacentSLPEnabled = old }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*73 + i/19)
	}
	for _, tc := range []struct{ dst, src, high, coefficient, count int32 }{
		{1024, 8192, 8196, 4096, 64}, {1024, 8192, 8196, 4096, 67},
		{1024, 8192, 8200, 4096, 67}, {1024, 8192, 65532, 4096, 67},
		{1024, 1020, 1024, 4096, 67}, {1024, -4, 0, 4096, 67},
	} {
		body := []byte{1, 5, 0x7f}
		constant := func(v int32) {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(v)...)
		}
		for i, base := range []int32{tc.dst, tc.src, tc.coefficient} {
			constant(base)
			body = append(body, 0x21, byte(i))
		}
		constant(tc.high)
		body = append(body, 0x21, 4, 0x03, 0x40,
			0x20, 0, 0x20, 3, 0x6a,
			0x20, 1, 0x20, 3, 0x6a, 0x28, 2, 0, 0x20, 2, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
			0x20, 0, 0x20, 3, 0x6a, 0x41, 4, 0x6a,
			0x20, 2, 0x28, 2, 0, 0x20, 4, 0x20, 3, 0x6a, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
			0x20, 3, 0x41, 8, 0x6a, 0x22, 3)
		constant(8 * tc.count)
		body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		want, expected, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
		for _, enabled := range []bool{false, true} {
			integerMapAdjacentSLPEnabled = enabled
			got, actual, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-runtime-adjacent")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(actual, expected) {
				t.Fatalf("case=%+v enabled=%v: got %x/%v, want %x/%v, memory equal=%v", tc, enabled, got, err, want, wantErr, bytes.Equal(actual, expected))
			}
			if diagnosticsEnabled && (hits != 0) != enabled {
				t.Fatalf("case=%+v enabled=%v: packed adjacent hits=%d", tc, enabled, hits)
			}
			if diagnosticsEnabled && enabled {
				_, _, _, paths := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-vector")
				if paths != 2 {
					t.Fatalf("case=%+v: emitted %d vector paths, want 2", tc, paths)
				}
			}
		}
	}
}

func TestIntegerLoadMapInvariantReentryTwoStores(t *testing.T) {
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*101 + i/31)
	}
	for _, lateLoad := range []bool{false, true} {
		for _, invariant := range []int32{1024, 1025, 1144, 4096, 4099, 4216, 8192, 65533} {
			body := []byte{1, 6, 0x7f}
			constant := func(v int32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(v)...) }
			for i, v := range []int32{8192, 1024, 4096, invariant} {
				constant(v)
				body = append(body, 0x21, byte(i))
			}
			body = append(body, 0x03, 0x40)
			loadInvariant := []byte{0x20, 3, 0x28, 2, 0, 0x21, 5}
			if !lateLoad {
				body = append(body, loadInvariant...)
			}
			body = append(body,
				0x20, 1, 0x20, 4, 0x6a,
				0x20, 0, 0x20, 4, 0x6a, 0x28, 2, 0)
			if lateLoad {
				constant(7)
			} else {
				body = append(body, 0x20, 5)
			}
			body = append(body, 0x6a, 0x36, 2, 0)
			if lateLoad {
				body = append(body, loadInvariant...)
			}
			body = append(body,
				0x20, 2, 0x20, 4, 0x6a,
				0x20, 4, 0x20, 5, 0x73, 0x36, 2, 0,
				0x20, 4, 0x41, 4, 0x6a, 0x22, 4)
			constant(128)
			body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			want, wantMem, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false}, initial)
			got, gotMem, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-load-map-invariant-reentry")
			if (err != nil) != (wantErr != nil) || got != want || !bytes.Equal(gotMem, wantMem) {
				t.Fatalf("lateLoad=%v invariant=%d: got %x/%v want %x/%v, memory equal=%v", lateLoad, invariant, got, err, want, wantErr, bytes.Equal(gotMem, wantMem))
			}
			wantHits := 1
			if lateLoad {
				wantHits = 0
			}
			if diagnosticsEnabled && hits != wantHits {
				t.Fatalf("lateLoad=%v invariant=%d: got %d re-entry paths, want %d", lateLoad, invariant, hits, wantHits)
			}
		}
	}
}
