//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

func memoryRecurrenceFixture(t *testing.T, op byte, two bool) *wasm.Module {
	locals := byte(1)
	if two {
		locals = 2
	}
	b := []byte{1, locals, 0x7c, 3, 0x40}
	lanes := byte(1)
	if two {
		lanes = 2
	}
	for lane := byte(0); lane < lanes; lane++ {
		dst := byte(0)
		if lane == 1 {
			dst = 4
		}
		b = append(b, 0x20, dst, 0x20, dst, 0x2b, 0, 0, 0x20, 1, 0x20, 2, 0x41, 3, 0x74, 0x6a, 0x2b, 0, 0, op, 0x22, 5+lane, 0x39, 0, 0)
	}
	b = append(b, 0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 3, 0x47, 0x0d, 0, 0xb, 0x20, 5, 0xbd)
	if two {
		b = append(b, 0x20, 6, 0xbd, 0x85)
	}
	b = append(b, 0x20, 2, 0xad, 0x85, 0xb)
	// Five parameters keep the local indices identical in one/two-cell fixtures.
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, b)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func memoryRecurrencePlan(t *testing.T, two bool) regionLoopPlan {
	m := memoryRecurrenceFixture(t, 0xa0, two)
	b := m.Code[0].BodyBytes
	at := bytes.Index(b, []byte{3, 0x40})
	r := wasm.NewReader(b[at+2:])
	var p regionLoopPlan
	types := []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtF64}
	if two {
		types = append(types, mtF64)
	}
	if !inspectRegionLoop(*r, types, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("fixture not parsed")
	}
	return p
}

func TestScalarMemoryRecurrenceAdmissionAndSnapshots(t *testing.T) {
	for _, two := range []bool{false, true} {
		original := memoryRecurrencePlan(t, two)
		p := original
		if !p.scalarMemoryRecurrence() || !p.scalar || p.iterationsPerVector() != 1 {
			t.Fatal("not admitted")
		}
		prefix, keep := p.prepareMemoryRecurrence()
		want := uint8(1)
		if two {
			want = 2
		}
		if prefix != want {
			t.Fatal("wrong prefix", prefix)
		}
		var rest []byte
		for _, id := range original.events[:original.eventN] {
			if id&0x80 != 0 || !keep[id] {
				rest = append(rest, id)
			}
		}
		if !bytes.Equal(rest, p.events[prefix:p.eventN]) || p.eventN != original.eventN || p.nodes != original.nodes || p.stores != original.stores {
			t.Fatal("changed operations/order")
		}
		// A surviving direct snapshot cannot share the updated accumulator home.
		q := original
		self := p.reductionLoad[0]
		for i := range q.locals[:q.localN] {
			if q.locals[i].typ == mtF64 {
				q.locals[i].value = self
				q.locals[i].written = true
				break
			}
		}
		if q.scalarMemoryRecurrence() {
			t.Fatal("live snapshot admitted")
		}
		// A distinct repeated read needs a version contract; conservatively reject.
		q = original
		q.loads[q.loadN] = q.loads[0]
		q.loadN++
		if q.scalarMemoryRecurrence() {
			t.Fatal("duplicate cell read admitted")
		}
	}
}

func TestScalarMemoryRecurrenceNativeStateAndTraps(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, whole, force := scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() { scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = saved, whole, force }()
	regionLoopEnabled = false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for op := byte(0xa0); op <= 0xa3; op++ {
			for _, two := range []bool{false, true} {
				m := memoryRecurrenceFixture(t, op, two)
				for _, tc := range []struct {
					dst, src, other, start, end uint64
					trap, fast                  bool
				}{{1024, 128, 2048, 0, 1, false, true}, {1024, 132, 2052, 0, 3, false, true}, {65528, 128, 2048, 0, 8, false, true}, {1024, 128, 2048, 3, 8, false, true}, {128, 128, 2048, 0, 3, false, false}, {136, 128, 2048, 0, 3, false, false}, {1024, 128, 1024, 0, 3, false, false}, {1024, 128, 1028, 0, 3, false, false}, {1024, 65520, 2048, 0, 3, true, false}, {65529, 128, 2048, 0, 1, true, false}, {1024, 65528, 2048, 0, 0, true, false}} {
					for _, bits := range []uint64{0, 0x8000000000000000, math.Float64bits(1.25), 0x7ff8000000001234, 0x7ff0000000004321} {
						var want uint64
						var expected []byte
						var trap bool
						for _, on := range []bool{false, true} {
							scalarMemoryRecurrenceEnabled = on
							regionLoopTestFast = on && tc.fast
							init := func(mem []byte) {
								for i := uint64(0); i < 16; i++ {
									if tc.src+8*i+8 <= uint64(len(mem)) {
										binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+2)))
									}
								}
								if tc.dst+8 <= uint64(len(mem)) {
									binary.LittleEndian.PutUint64(mem[tc.dst:], bits)
								}
								if two && tc.other+8 <= uint64(len(mem)) {
									binary.LittleEndian.PutUint64(mem[tc.other:], bits)
								}
							}
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, tc.dst, tc.src, tc.start, tc.end, tc.other)
							if !on {
								want, expected, trap = got, append([]byte(nil), mem...), err != nil
							} else if (err != nil) != trap || (!trap && got != want) || !bytes.Equal(mem, expected) {
								t.Fatal("changed state", features, op, two, tc, bits, on, got, want, err)
							}
							if (err != nil) != tc.trap {
								t.Fatal("wrong trap", tc, on, err)
							}
							if on && stats.Funcs[0].Peephole["region-loop-scalar-memory-recurrence"] != 1 {
								t.Fatal("not emitted", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
