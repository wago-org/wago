//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func scalarLoopMemoryFixture(t *testing.T, op byte, two bool) *wasm.Module {
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

func scalarLoopMemoryPlan(t *testing.T, two bool) regionLoopPlan {
	m := scalarLoopMemoryFixture(t, 0xa0, two)
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

func TestScalarLoopMemoryFormsPlan(t *testing.T) {
	for _, two := range []bool{false, true} {
		p := scalarLoopMemoryPlan(t, two)
		if !p.scalarMemoryRecurrence() {
			t.Fatal("recurrence rejected")
		}
		_, keep := p.prepareMemoryRecurrence()
		folded, mem := p.memoryForms(true)
		want := 1
		if two {
			want = 2
		}
		count := 0
		for consumer, id := range mem {
			if id == 0 {
				continue
			}
			count++
			if !folded[id] || keep[id] || p.nodes[consumer].right != id || p.stride(p.nodes[id].left) == 0 {
				t.Fatal("unsafe fold", consumer, id)
			}
		}
		if count != want {
			t.Fatalf("folds=%d want=%d", count, want)
		}
		if p.scratchNeedPermanent(folded, keep) >= p.scratchNeedPermanent([regionLoopMaxOps + 1]bool{}, keep) {
			t.Fatal("scratch demand did not decrease")
		}
		off, _ := p.memoryForms(false)
		if off != ([regionLoopMaxOps + 1]bool{}) {
			t.Fatal("disabled fold")
		}
		for consumer, id := range mem {
			if id == 0 {
				continue
			}
			q := p
			q.nodes[consumer].left = id // a two-use load cannot fold
			no, _ := q.memoryForms(true)
			if no[id] {
				t.Fatal("multi-use load folded")
			}
			q = p
			q.nodes[consumer].left, q.nodes[consumer].right = q.nodes[consumer].right, q.nodes[consumer].left
			no, _ = q.memoryForms(true)
			if no[id] {
				t.Fatal("left load commuted")
			}
			q = p
			for at, event := range q.events[:q.eventN] {
				if event == uint8(consumer) {
					// Place an existing store event between the load and this consumer.
					copy(q.events[at+1:], q.events[at:q.eventN])
					q.events[at] = 0x80
					q.eventN++
					break
				}
			}
			no, _ = q.memoryForms(true)
			if no[id] {
				t.Fatal("fold crossed store")
			}
		}
	}
}

func TestScalarLoopMemoryFormsExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, recurrence, whole, force := scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() {
		scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = old, recurrence, whole, force
	}()
	scalarMemoryRecurrenceEnabled = true
	regionLoopEnabled = false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for op := byte(0xa0); op <= 0xa3; op++ {
			for _, two := range []bool{false, true} {
				m := scalarLoopMemoryFixture(t, op, two)
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				for _, compact := range []bool{false, true} {
					for _, tc := range []struct {
						dst, src, other, start, end uint64
						trap, fast                  bool
					}{
						{1024, 128, 2048, 0, 1, false, true}, {1024, 132, 2052, 0, 3, false, true}, {1024, 128, 2048, 3, 8, false, true},
						{1024, 65528, 2048, 0, 1, false, true}, {65528, 128, 2048, 0, 8, false, true},
						{128, 128, 2048, 0, 3, false, false}, {136, 128, 2048, 0, 3, false, false},
						{1024, 128, 1028, 0, 3, false, false}, {1024, 65520, 2048, 0, 3, true, false}, {65529, 128, 2048, 0, 1, true, false},
					} {
						for _, raw := range []uint64{0, 0x8000000000000000, math.Float64bits(1.25), 0x7ff8000000001234, 0x7ff0000000004321} {
							var want uint64
							var expected []byte
							var trap bool
							for _, on := range []bool{false, true} {
								scalarLoopMemoryFormsEnabled = on
								regionLoopTestFast = tc.fast
								init := func(mem []byte) {
									for i := uint64(0); i < 16; i++ {
										if tc.src+8*i+8 <= uint64(len(mem)) {
											binary.LittleEndian.PutUint64(mem[tc.src+8*i:], raw^((i%2)<<63))
										}
									}
									if tc.dst+8 <= uint64(len(mem)) {
										binary.LittleEndian.PutUint64(mem[tc.dst:], math.Float64bits(2.25))
									}
									if two && tc.other+8 <= uint64(len(mem)) {
										binary.LittleEndian.PutUint64(mem[tc.other:], raw)
									}
								}
								var stats ModuleStats
								got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, CompactNative: compact, Stats: &stats}, init, tc.dst, tc.src, tc.start, tc.end, tc.other)
								if !on {
									want, expected, trap = got, append([]byte(nil), mem...), err != nil
								} else if (err != nil) != trap || (!trap && got != want) || !bytes.Equal(mem, expected) {
									t.Fatal("changed result/memory/trap", features, op, two, compact, tc, raw, got, want, err)
								}
								if (err != nil) != tc.trap {
									t.Fatal("wrong trap", tc, err)
								}
								if stats.Funcs[0].Peephole["region-loop-scalar-memory-recurrence"] != 1 {
									t.Fatal("fixture did not emit recurrence")
								}
								folds := stats.Funcs[0].Peephole["region-loop-scalar-fold-load"]
								expectedFolds := 0
								if on {
									expectedFolds = 1
									if two {
										expectedFolds = 2
									}
								}
								if int(folds) != expectedFolds {
									t.Fatal("wrong scalar fold count", folds, expectedFolds)
								}
							}
						}
					}
				}
			}
		}
	}
}
