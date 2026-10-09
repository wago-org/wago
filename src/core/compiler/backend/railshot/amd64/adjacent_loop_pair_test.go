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

func adjacentLoopFixture(t testing.TB, op byte) *wasm.Module {
	body := []byte{1, 2, 0x7c, 0x03, 0x40}
	for lane := byte(0); lane < 2; lane++ {
		body = append(body, 0x20, 0, 0x20, 1, 0x2b, 0, lane*8, 0x20, 2, 0x2b, 0, lane*8, op, 0x22, 5+lane, 0x39, 0, lane*8)
	}
	for i := byte(0); i < 3; i++ {
		body = append(body, 0x20, i, 0x41, 16, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x22, 3, 0x20, 4, 0x47, 0x0d, 0, 0x0b, 0x20, 5, 0xbd, 0x20, 6, 0xbd, 0x42, 17, 0x89, 0x85, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestAdjacentLoopPairArithmeticAndExitLanes(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, old, force, forms := regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms
	defer func() {
		regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = saved, old, force, forms
	}()
	regionLoopEnabled = false
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := adjacentLoopFixture(t, op)
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, src := range []uint64{512, 516, 65520} {
				for _, n := range []uint64{1, 2, 3} {
					if src == 65520 && n != 1 {
						continue
					}
					for _, bits := range []uint64{0x8000000000000000, 0x3ff4000000000000, 0x7ff8000000001234, 0x7ff0000000004321} {
						var want uint64
						var expected []byte
						for _, on := range []bool{false, true} {
							regionAdjacentEnabled, regionLoopTestFast, regionLoopMemForms = on, on, on
							init := func(mem []byte) {
								for i := uint64(0); i < 2*n; i++ {
									binary.LittleEndian.PutUint64(mem[128+8*i:], math.Float64bits(float64(i+3)))
									v := bits
									if i%2 != 0 {
										v = math.Float64bits(2.25)
									}
									binary.LittleEndian.PutUint64(mem[src+8*i:], v)
								}
							}
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, 1024, 128, src, 0, n)
							if err != nil {
								t.Fatal(op, features, src, n, bits, on, err)
							}
							if !on {
								want, expected = got, append([]byte(nil), mem...)
							} else {
								if got != want || !bytes.Equal(mem, expected) {
									t.Fatal("paired output/exit mismatch", op, features, src, n, bits, got, want)
								}
								if stats.Funcs[0].Peephole["region-loop-fast"] != 1 {
									t.Fatal("no adjacent fast path", stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestAdjacentLoopPairAliasAndBoundsFallback(t *testing.T) {
	saved, old, force, forms := regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms
	defer func() {
		regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = saved, old, force, forms
	}()
	regionLoopEnabled = false
	m := adjacentLoopFixture(t, 0xa0)
	for _, tc := range []struct {
		dst, src, n uint64
		trap        bool
	}{{1024, 512, 3, false}, {128, 512, 3, false}, {136, 512, 3, false}, {132, 512, 3, false}, {1024, 65528, 1, true}} {
		var want uint64
		var expected []byte
		var wantTrap bool
		for _, on := range []bool{false, true} {
			regionAdjacentEnabled, regionLoopTestFast, regionLoopMemForms = on, false, on
			init := func(mem []byte) {
				for i := uint64(0); i < 2*tc.n; i++ {
					binary.LittleEndian.PutUint64(mem[128+8*i:], math.Float64bits(float64(i+3)))
					if tc.src+8*i+8 <= uint64(len(mem)) {
						binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(7))
					}
				}
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, tc.dst, 128, tc.src, 0, tc.n)
			if !on {
				want, expected, wantTrap = got, append([]byte(nil), mem...), err != nil
			} else if (err != nil) != wantTrap || (!wantTrap && got != want) || !bytes.Equal(mem, expected) {
				t.Fatal("fallback mismatch", tc, on, got, want, err)
			}
			if (err != nil) != tc.trap {
				t.Fatal("wrong trap", tc, err)
			}
			if tc.trap && binary.LittleEndian.Uint64(mem[tc.dst:]) != math.Float64bits(10) {
				t.Fatal("trap erased first scalar store")
			}
		}
	}
}

func TestAdjacentLoopPairAdmission(t *testing.T) {
	makePlan := func() regionLoopPlan {
		m := adjacentLoopFixture(t, 0xa0)
		body := m.Code[0].BodyBytes
		at := bytes.Index(body, []byte{0x03, 0x40})
		if at < 0 {
			t.Fatal("loop not found")
		}
		r := wasm.NewReader(body[at+2:])
		var p regionLoopPlan
		types := []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtF64, mtF64}
		if !inspectRegionLoop(*r, types, wasm.NewModuleInstructionClassifier(m, true), &p) {
			t.Fatal("plan not admitted")
		}
		return p
	}
	p := makePlan()
	if !p.packAdjacentOutputs() || !p.adjacent || p.storeN != 1 || p.loadN != 2 || p.iterationsPerVector() != 1 {
		t.Fatal("valid pair not admitted")
	}
	for i, l := range p.locals[:p.localN] {
		if l.index == 5 && p.exitHigh&(1<<i) != 0 || l.index == 6 && p.exitHigh&(1<<i) == 0 {
			t.Fatal("wrong exit lane", l.index, p.exitHigh)
		}
	}
	for _, mutate := range []func(*regionLoopPlan){
		func(p *regionLoopPlan) { p.nodes[p.stores[1].value].op = 0xa1 },
		func(p *regionLoopPlan) { p.stores[1].offset = 16 },
		func(p *regionLoopPlan) { p.nodes[p.nodes[p.stores[1].value].right].bits++ },
		func(p *regionLoopPlan) { p.nodes[p.stores[1].value].left = p.nodes[p.stores[0].value].left },
	} {
		p := makePlan()
		mutate(&p)
		if p.packAdjacentOutputs() {
			t.Fatal("near miss admitted")
		}
	}
}
