//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func invariantPrefixFixture(t *testing.T, op byte) *wasm.Module {
	body := []byte{1, 2, 0x7c, 0x03, 0x40}
	var constant [8]byte
	binary.LittleEndian.PutUint64(constant[:], math.Float64bits(1.5))
	for lane := byte(0); lane < 2; lane++ {
		body = append(body, 0x20, 0, 0x20, 2, 0x2b, 0, 0, 0x44)
		body = append(body, constant[:]...)
		body = append(body, op, 0x20, 1, 0x2b, 0, lane*8, 0xa2, 0x22, 5+lane, 0x39, 0, lane*8)
	}
	for _, i := range []byte{0, 1} {
		body = append(body, 0x20, i, 0x41, 16, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x22, 3, 0x20, 4, 0x47, 0x0d, 0, 0x0b, 0x20, 5, 0xbd, 0x20, 6, 0xbd, 0x42, 17, 0x89, 0x85, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestInvariantLoopPrefixResultsAndFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, old, force, forms, hoist := regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms, regionInvariantPrefixEnabled
	defer func() {
		regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms, regionInvariantPrefixEnabled = saved, old, force, forms, hoist
	}()
	regionLoopEnabled = false
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := invariantPrefixFixture(t, op)
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, tc := range []struct {
				dst, src, inv, n uint64
				trap             bool
			}{{1024, 128, 512, 1, false}, {1024, 132, 516, 3, false}, {1024, 128, 65528, 3, false}, {1024, 128, 1024, 3, false}, {1024, 65528, 512, 1, true}} {
				for _, value := range []uint64{0x8000000000000000, math.Float64bits(2.25), 0x7ff8000000001234, 0x7ff0000000004321} {
					var want uint64
					var expected []byte
					var wantTrap bool
					for mode := 0; mode < 3; mode++ {
						regionAdjacentEnabled = mode != 0
						regionInvariantPrefixEnabled = mode == 2
						regionLoopMemForms = true
						regionLoopTestFast = mode != 0 && tc.inv != tc.dst && !tc.trap
						init := func(mem []byte) {
							for i := uint64(0); i < 2*tc.n; i++ {
								if tc.src+8*i+8 <= uint64(len(mem)) {
									binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+2)))
								}
							}
							binary.LittleEndian.PutUint64(mem[tc.inv:], value)
						}
						var stats ModuleStats
						got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, tc.dst, tc.src, tc.inv, 0, tc.n)
						if mode == 0 {
							want, expected, wantTrap = got, append([]byte(nil), mem...), err != nil
						} else if (err != nil) != wantTrap || (!wantTrap && got != want) || !bytes.Equal(mem, expected) {
							t.Fatal("invariant prefix mismatch", op, features, tc, value, mode, got, want, err)
						}
						if (err != nil) != tc.trap {
							t.Fatal("wrong trap", tc, mode, err)
						}
						if mode == 2 && stats.Funcs[0].Peephole["region-loop-invariant-prefix"] != 1 {
							t.Fatal("prefix not emitted", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestInvariantLoopPrefixBoundedAdmission(t *testing.T) {
	m := invariantPrefixFixture(t, 0xa2)
	body := m.Code[0].BodyBytes
	at := bytes.Index(body, []byte{3, 0x40})
	r := wasm.NewReader(body[at+2:])
	var p regionLoopPlan
	if !inspectRegionLoop(*r, []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtF64, mtF64}, wasm.NewModuleInstructionClassifier(m, true), &p) || !p.packAdjacentOutputs() {
		t.Fatal("fixture not admitted")
	}
	prefix, keep := p.invariantPrefix(true)
	if prefix != 3 {
		t.Fatal("wrong prefix", prefix)
	}
	roots := 0
	for _, v := range keep {
		if v {
			roots++
		}
	}
	if roots != 1 {
		t.Fatal("wrong permanent homes", roots)
	}
	if off, _ := p.invariantPrefix(false); off != 0 {
		t.Fatal("disabled policy admitted")
	}
	p.nodes[p.events[0]].left = p.nodes[p.loads[p.loadN-1].node].left
	if count, _ := p.invariantPrefix(true); count != 0 {
		t.Fatal("moving load admitted as invariant")
	}
}
