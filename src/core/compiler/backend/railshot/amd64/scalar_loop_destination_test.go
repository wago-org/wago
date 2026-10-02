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

func scalarLoopDestinationFixture(t *testing.T, op byte, two, right bool) *wasm.Module {
	t.Helper()
	m := scalarLoopMemoryFixture(t, op, two)
	if right {
		for _, dst := range []byte{0, 4} {
			if dst == 4 && !two {
				continue
			}
			accumulator := []byte{0x20, dst, 0x2b, 0, 0}
			input := []byte{0x20, 1, 0x20, 2, 0x41, 3, 0x74, 0x6a, 0x2b, 0, 0}
			old := append(append([]byte(nil), accumulator...), input...)
			next := append(append([]byte(nil), input...), accumulator...)
			if bytes.Count(m.Code[0].BodyBytes, old) != 1 {
				t.Fatal("operand-order fixture mismatch")
			}
			m.Code[0].BodyBytes = bytes.Replace(m.Code[0].BodyBytes, old, next, 1)
		}
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestScalarLoopDestinationLastUse(t *testing.T) {
	for _, two := range []bool{false, true} {
		p := scalarLoopMemoryPlan(t, two)
		if !p.scalarMemoryRecurrence() {
			t.Fatal("not a recurrence")
		}
		p.prepareMemoryRecurrence()
		uses := p.fpUses()
		for i, s := range p.stores[:p.storeN] {
			id := p.reductionLoad[i]
			if got := p.scalarRecurrenceDestination(s.value, &uses); got != id {
				t.Fatal("missing destination", got, id)
			}
			live := uses
			live[id]++
			if got := p.scalarRecurrenceDestination(s.value, &live); got != 0 {
				t.Fatal("old value is still live", got)
			}
			q := p
			q.scalar = false
			if q.scalarRecurrenceDestination(s.value, &uses) != 0 {
				t.Fatal("packed loop admitted")
			}
		}
	}
}

func TestScalarLoopDestinationExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, mem, rec, whole, force := scalarLoopDestinationEnabled, scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() {
		scalarLoopDestinationEnabled, scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = old, mem, rec, whole, force
	}()
	scalarMemoryRecurrenceEnabled = true
	regionLoopEnabled = false
	regionLoopTestFast = true
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, vex := range []bool{false, true} {
			for _, memory := range []bool{false, true} {
				scalarLoopMemoryFormsEnabled = memory
				for _, two := range []bool{false, true} {
					for _, right := range []bool{false, true} {
						for op := byte(0xa0); op <= 0xa3; op++ {
							m := scalarLoopDestinationFixture(t, op, two, right)
							for _, compact := range []bool{false, true} {
								for _, tc := range []struct{ src, n uint64 }{{128, 1}, {132, 5}, {65528, 1}} {
									for _, raw := range []uint64{0, 0x8000000000000000, math.Float64bits(1.25), 0x7ff8000000001234, 0x7ff0000000004321} {
										var want uint64
										var expected []byte
										for _, on := range []bool{false, true} {
											scalarLoopDestinationEnabled = on
											init := func(mem []byte) {
												for i := uint64(0); i < tc.n; i++ {
													binary.LittleEndian.PutUint64(mem[tc.src+8*i:], raw^((i%2)<<63))
												}
												binary.LittleEndian.PutUint64(mem[1024:], math.Float64bits(2.25))
												other := raw
												if raw&0x7ff0000000000000 == 0x7ff0000000000000 && raw&0x000fffffffffffff != 0 {
													other ^= 0x21 // distinguish the two NaN payloads
												}
												binary.LittleEndian.PutUint64(mem[2048:], other)
											}
											var stats ModuleStats
											got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, CompactNative: compact, Stats: &stats, Optimizations: map[string]bool{"vex-float-mem": vex}}, init, 1024, tc.src, 0, tc.n, 2048)
											if err != nil {
												t.Fatal("unexpected trap", features, vex, memory, two, right, op, compact, tc, raw, on, err)
											}
											if !on {
												want, expected = got, append([]byte(nil), mem...)
											} else if got != want || !bytes.Equal(mem, expected) {
												t.Fatal("changed results or recurrence homes", features, vex, memory, two, right, op, compact, tc, raw, got, want)
											}
											wantDest := 0
											if on && vex && features&shared.AMD64AVX != 0 {
												wantDest = 1
												if two {
													wantDest = 2
												}
											}
											if gotDest := stats.Funcs[0].Peephole["region-loop-scalar-destination"]; int(gotDest) != wantDest {
												t.Fatal("destination admission", gotDest, wantDest)
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// The first result has no final local consumer. Its store consumes its last
// logical use, but the physical accumulator must survive the other recurrence
// and the next iteration. Treating it as a scratch value corrupts that cell.
func TestScalarLoopDestinationRetainsUnobservedHome(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, mem, rec, whole, force := scalarLoopDestinationEnabled, scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() {
		scalarLoopDestinationEnabled, scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = old, mem, rec, whole, force
	}()
	scalarMemoryRecurrenceEnabled = true
	regionLoopEnabled = false
	regionLoopTestFast = true
	for _, memory := range []bool{false, true} {
		scalarLoopMemoryFormsEnabled = memory
		for _, right := range []bool{false, true} {
			m := scalarLoopDestinationFixture(t, 0xa2, true, right)
			m.Code[0].BodyBytes = bytes.Replace(m.Code[0].BodyBytes, []byte{0x22, 5}, nil, 1)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var expected []byte
			for _, on := range []bool{false, true} {
				scalarLoopDestinationEnabled = on
				var stats ModuleStats
				_, result, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Stats: &stats}, func(b []byte) {
					for i, v := range []float64{2, 3, 5} {
						binary.LittleEndian.PutUint64(b[128+i*8:], math.Float64bits(v))
					}
					binary.LittleEndian.PutUint64(b[1024:], math.Float64bits(1.25))
					binary.LittleEndian.PutUint64(b[2048:], math.Float64bits(2.25))
				}, 1024, 128, 0, 3, 2048)
				if err != nil {
					t.Fatal(err)
				}
				if !on {
					expected = result
				} else if !bytes.Equal(result, expected) {
					t.Fatal("permanent accumulator was released after its logical result died", memory, right)
				}
				for at, v := range map[int]float64{1024: 37.5, 2048: 67.5} {
					if got := binary.LittleEndian.Uint64(result[at:]); got != math.Float64bits(v) {
						t.Fatal("incorrect cell", at, got, v)
					}
				}
				if on && stats.Funcs[0].Peephole["region-loop-scalar-destination"] != 2 {
					t.Fatal("missing destination")
				}
			}
		}
	}
}

func TestScalarLoopDestinationKeepsLiveOldValue(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, mem, rec, whole, force := scalarLoopDestinationEnabled, scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() {
		scalarLoopDestinationEnabled, scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = old, mem, rec, whole, force
	}()
	scalarMemoryRecurrenceEnabled = true
	regionLoopEnabled = false
	regionLoopTestFast = true
	b := []byte{1, 2, 0x7c, 3, 0x40, 0x20, 0, 0x20, 0, 0x2b, 0, 0, 0x22, 5,
		0x20, 1, 0x20, 2, 0x41, 3, 0x74, 0x6a, 0x2b, 0, 0, 0xa0, // pending new value
		0x20, 5, 0x44}
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(2))
	b = append(b, 0xa2, 0x21, 6, 0x44) // use the old value after computing the new one
	b = binary.LittleEndian.AppendUint64(b, 0)
	b = append(b, 0x21, 5, 0x39, 0, 0, 0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 3, 0x47, 0x0d, 0, 0xb, 0x20, 6, 0xbd, 0xb)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, b)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, memory := range []bool{false, true} {
		scalarLoopMemoryFormsEnabled = memory
		for _, on := range []bool{false, true} {
			scalarLoopDestinationEnabled = on
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Stats: &stats}, func(b []byte) {
				binary.LittleEndian.PutUint64(b[1024:], math.Float64bits(1.25))
				binary.LittleEndian.PutUint64(b[128:], math.Float64bits(2))
				binary.LittleEndian.PutUint64(b[136:], math.Float64bits(3))
			}, 1024, 128, 0, 2, 2048)
			if err != nil || got != math.Float64bits(6.5) || binary.LittleEndian.Uint64(mem[1024:]) != math.Float64bits(6.25) {
				t.Fatal("overwrote a live old accumulator", memory, on, got, err)
			}
			if stats.Funcs[0].Peephole["region-loop-scalar-memory-recurrence"] != 1 {
				t.Fatal("fixture not admitted")
			}
			if stats.Funcs[0].Peephole["region-loop-scalar-destination"] != 0 {
				t.Fatal("live old value admitted")
			}
		}
	}
}
