//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerWordMapFixture(t testing.TB, unroll int, reverse, dynamic bool, start, count, dest, bias, offset, seed, shift uint32) *wasm.Module {
	body := []byte{1, 6, 0x7f}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for i, x := range []uint32{start, dest, start + count, 0, seed} {
		constant(x)
		body = append(body, 0x21, byte(i))
	}
	body = append(body, 0x03, 0x40)
	for j := 0; j < unroll; j++ {
		lane := j
		if reverse {
			lane = unroll - 1 - j
		}
		body = append(body, 0x20, 1)
		constant(bias + uint32(4*lane))
		body = append(body, 0x6a)
		body = append(body, 0x20, 0)
		constant(uint32(lane))
		body = append(body, 0x6a, 0x20, 4, 0x6a, 0x22, 3)
		constant(shift)
		body = append(body, 0x76, 0x20, 3, 0x73)
		constant(0x85ebca6b)
		body = append(body, 0x6c, 0x22, 3)
		constant(13)
		body = append(body, 0x76, 0x20, 3, 0x73)
		constant(255)
		body = append(body, 0x71, 0x22, 5, 0x36, 0)
		body = append(body, wasmtest.ULEB(offset)...)
	}
	body = append(body, 0x20, 1)
	constant(uint32(4 * unroll))
	body = append(body, 0x6a, 0x21, 1)
	body = append(body, 0x20, 0)
	constant(uint32(unroll))
	body = append(body, 0x6a, 0x22, 0)
	if dynamic {
		body = append(body, 0x20, 2)
	} else {
		constant(start + count)
	}
	body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 0, 0x20, 1, 0x73, 0x20, 3, 0x73, 0x20, 5, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestIntegerWordMapExactArithmeticTailAndTraps(t *testing.T) {
	old := integerWordMapEnabled
	defer func() { integerWordMapEnabled = old }()
	for _, unroll := range []int{1, 2, 4} {
		for _, reverse := range []bool{false, true} {
			for _, dynamic := range []bool{false, true} {
				for _, count := range []uint32{uint32(unroll), uint32(2 * unroll), uint32(5 * unroll), uint32(9 * unroll)} {
					for _, tc := range []struct{ start, dest, bias, offset, seed, shift uint32 }{
						{0, 128, 0, 0, 0x9e3779b9, 16},
						{0x80000000, 131, 4, 17, 0xffffffff, 0},
						{0xffffffe0, 256, 0, 0, 1, 32},
						{7, 65512, 0, 0, 0x7fffffff, 63},
						{7, 0xfffffff0, 32, 0, 0x80000000, 1},
						{7, 0xfffffff0, 0, 32, 123, 31},
					} {
						// Only terminating, nonwrapping source index intervals here;
						// dynamic address wrap and widened immediates are independent.
						if uint64(tc.start)+uint64(count) > 0xffffffff {
							continue
						}
						m := integerWordMapFixture(t, unroll, reverse, dynamic, tc.start, count, tc.dest, tc.bias, tc.offset, tc.seed, tc.shift)
						initial := bytes.Repeat([]byte{0xa5}, 65536)
						var want uint32
						var wantMemory []byte
						var wantError string
						for _, on := range []bool{false, true} {
							integerWordMapEnabled = on
							got, memory, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-word-map-vector")
							if !on {
								want, wantMemory, wantError = got, memory, fmt.Sprint(err)
							} else {
								if got != want || !bytes.Equal(memory, wantMemory) || fmt.Sprint(err) != wantError {
									t.Fatalf("unroll=%d reverse=%v dynamic=%v count=%d case=%+v got=%x want=%x err=%v wantError=%s", unroll, reverse, dynamic, count, tc, got, want, err, wantError)
								}
								if diagnosticsEnabled && hits != 1 {
									t.Fatalf("map not admitted: unroll=%d reverse=%v dynamic=%v count=%d case=%+v", unroll, reverse, dynamic, count, tc)
								}
							}
						}
						// Independent word oracle, including the effect prefix on traps.
						oracle := append([]byte(nil), initial...)
						trapped := false
						for base := uint32(0); base < count && !trapped; base += uint32(unroll) {
							for j := 0; j < unroll; j++ {
								lane := j
								if reverse {
									lane = unroll - 1 - j
								}
								i := tc.start + base + uint32(lane)
								x := i + tc.seed
								x ^= x >> (tc.shift & 31)
								x *= 0x85ebca6b
								x ^= x >> 13
								x &= 255
								addr := uint64(tc.dest+4*base+tc.bias+uint32(4*lane)) + uint64(tc.offset)
								if addr+4 > uint64(len(oracle)) {
									trapped = true
									break
								}
								binary.LittleEndian.PutUint32(oracle[addr:], x)
							}
						}
						if !bytes.Equal(wantMemory, oracle) || (wantError != "<nil>") != trapped {
							t.Fatal("independent source oracle mismatch", unroll, reverse, count, tc, wantError)
						}
					}
				}
			}
		}
	}
}

func TestIntegerWordMapProbeBudgetsAndOwnedReads(t *testing.T) {
	m := integerWordMapFixture(t, 2, true, true, 0, 32, 128, 4, 17, 0x9e3779b9, 16)
	body := m.Code[0].BodyBytes
	at := bytes.Index(body, []byte{0x03, 0x40})
	if at < 0 {
		t.Fatal("missing loop")
	}
	body = body[at+2:]
	carried := append([]byte{0x20, 3, 0x41, 1, 0x6a, 0x21, 3}, body...)
	for _, tc := range []struct {
		name   string
		body   []byte
		accept bool
		work   uint16
	}{
		{"accepted", body, true, 0},
		{"carried", carried, false, 0},
		{"nested", append([]byte{0x02, 0x40, 0x0b}, body...), false, 0},
		{"load", append([]byte{0x41, 0, 0x28, 0, 0, 0x1a}, body...), false, 0},
		{"exhausted", body, false, integerWordMapWorkLimit},
		{"last-unit", body, false, integerWordMapWorkLimit - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{nLocals: 6, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtI32}, classifier: wasm.NewModuleInstructionClassifier(m, true), integerWordMapWork: tc.work}
			r := wasm.ReaderFrom(tc.body)
			p, ok := f.inspectIntegerWordMap(&r)
			if ok != tc.accept || r.Offset() != 0 || f.integerWordMapWork > integerWordMapWorkLimit {
				t.Fatal("probe contract", ok, r.Offset(), f.integerWordMapWork)
			}
			if ok {
				// A temporary overwritten twice still owns each earlier read.
				if p.storeN != 2 || !p.used[p.root] || p.nodes[p.root].Type != wasm.I32 {
					t.Fatal("lost source expressions")
				}
				for _, mutate := range []func(*integerWordMapPlan){
					func(p *integerWordMapPlan) { p.stores[1].Address = p.stores[0].Address },
					func(p *integerWordMapPlan) { p.stores[1].Offset++ },
					func(p *integerWordMapPlan) { p.stores[1].Value = p.nodes[p.stores[1].Value].Left },
				} {
					q := p
					mutate(&q)
					q.proofWork = 0
					q.proofLimit = 4096
					var reads [32]bool
					if q.finish(reads) {
						t.Fatal("near miss accepted")
					}
				}
			}
			if allocations := testing.AllocsPerRun(100, func() { f.integerWordMapWork = tc.work; f.inspectIntegerWordMap(&r) }); allocations != 0 {
				t.Fatal("probe allocation", allocations)
			}
		})
	}
}

func TestIntegerWordMapPrefixDecoding(t *testing.T) {
	m := integerWordMapFixture(t, 2, true, true, 0, 32, 128, 4, 17, 0x9e3779b9, 16)
	for _, body := range [][]byte{
		{0x20, 0x80, 0x01, 0x41, 0x7f, 0x6a, 0x6c},
		{0x21, 0, 0x22, 1, 0x6b, 0x71},
		{0x20}, {0x20, 0x80}, {0x41}, {0x41, 0x80},
		{0x20, 0x80, 0x80, 0x80, 0x80, 0x10, 0x6c},
		{0x41, 0x80, 0x80, 0x80, 0x80, 0x10, 0x6c},
		{0x20, 0, 0x28, 0, 0},
	} {
		for _, work := range []uint16{0, integerWordMapWorkLimit - 1, integerWordMapWorkLimit} {
			classifier := wasm.NewModuleInstructionClassifier(m, true)
			r := wasm.ReaderFrom(body)
			scan := r
			want, expectedWork := false, work
			var imm wasm.InstructionImmediate
			for i := 0; i < 24 && expectedWork < integerWordMapWorkLimit; i++ {
				expectedWork++
				op, err := scan.Byte()
				if err != nil {
					break
				}
				if op == 0x6c || op == 0x71 || op == 0x72 || op == 0x73 || op == 0x74 || op == 0x76 {
					want = true
					break
				}
				if op != 0x20 && op != 0x21 && op != 0x22 && op != 0x41 && op != 0x6a && op != 0x6b {
					break
				}
				if classifier.ClassifyInto(&scan, op, &imm) != nil {
					break
				}
			}
			f := fn{classifier: classifier, integerWordMapWork: work}
			got := f.hasIntegerWordMapPrefix(&r)
			if got != want || f.integerWordMapWork != expectedWork || r.Offset() != 0 {
				t.Fatalf("body %x work %d: got %v/%d offset %d, want %v/%d", body, work, got, f.integerWordMapWork, r.Offset(), want, expectedWork)
			}
		}
	}
}

func TestIntegerWordMapOperationRecipes(t *testing.T) {
	old := integerWordMapEnabled
	defer func() { integerWordMapEnabled = old }()
	for _, op := range []byte{0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x76, 0x41} {
		for _, operand := range []uint32{0, 1, 31, 32, 63, 0x80000000, 0xffffffff} {
			body := []byte{1, 4, 0x7f, 0x41, 0, 0x21, 0, 0x41, 0x80, 1, 0x21, 1, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x41, 3, 0x6c}
			constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
			if op == 0x41 {
				// A pure computed temporary precedes a constant output. The last
				// scalar iteration must still publish the temporary, and a cached
				// constant output must retain its lease across the vector loop.
				body = append(body, 0x21, 3)
				constant(operand)
			} else {
				constant(operand)
				body = append(body, op, 0x22, 3)
			}
			body = append(body, 0x36, 0, 0, 0x20, 1, 0x41, 4, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x41, 32, 0x47, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			var want uint32
			var wantMemory []byte
			for _, enabled := range []bool{false, true} {
				integerWordMapEnabled = enabled
				got, mem, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, nil, "integer-word-map-vector")
				if err != nil {
					t.Fatal(op, operand, err)
				}
				if !enabled {
					want, wantMemory = got, mem
				} else if got != want || !bytes.Equal(mem, wantMemory) || diagnosticsEnabled && hits != 1 {
					t.Fatal("operation recipe", op, operand, got, want, hits)
				}
			}
			for i := uint32(0); i < 32; i++ {
				x := i * 3
				switch op {
				case 0x6a:
					x += operand
				case 0x6b:
					x -= operand
				case 0x6c:
					x *= operand
				case 0x71:
					x &= operand
				case 0x72:
					x |= operand
				case 0x73:
					x ^= operand
				case 0x74:
					x <<= operand & 31
				case 0x76:
					x >>= operand & 31
				case 0x41:
					x = operand
				}
				if binary.LittleEndian.Uint32(wantMemory[128+4*i:]) != x {
					t.Fatal("word oracle", op, operand, i, x)
				}
			}
		}
	}
}
