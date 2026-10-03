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

func adjacentMultiPairFixture(t *testing.T, op byte) *wasm.Module {
	t.Helper()
	body := []byte{1, 4, 0x7c, 0x03, 0x40}
	for lane := byte(0); lane < 4; lane++ {
		body = append(body, 0x20, 0, 0x20, 1, 0x2b, 0, lane*8, 0x20, 2, 0x2b, 0, lane*8, op, 0x22, 4+lane, 0x39, 0, lane*8)
	}
	for i := byte(0); i < 3; i++ {
		body = append(body, 0x20, i, 0x41, 32, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 0, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0xbd)
	for lane := byte(1); lane < 4; lane++ {
		body = append(body, 0x20, 4+lane, 0xbd, 0x42, 11*lane, 0x89, 0x85)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAdjacentMultiPairPlan(t *testing.T) {
	old := regionAdjacentMultiEnabled
	defer func() { regionAdjacentMultiEnabled = old }()
	m := adjacentMultiPairFixture(t, 0xa0)
	body := m.Code[0].BodyBytes
	at := bytes.Index(body, []byte{0x03, 0x40})
	types := []machineType{mtI32, mtI32, mtI32, mtI32, mtF64, mtF64, mtF64, mtF64}
	var p regionLoopPlan
	regionAdjacentMultiEnabled = false
	if inspectRegionLoop(wasm.ReaderFrom(body[at+2:]), types, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("four stores admitted with policy disabled")
	}
	regionAdjacentMultiEnabled = true
	if !inspectRegionLoop(wasm.ReaderFrom(body[at+2:]), types, wasm.NewModuleInstructionClassifier(m, true), &p) || p.storeN != 4 {
		t.Fatal("four-store plan rejected")
	}
	if !p.packAdjacentOutputs() || p.storeN != 2 || p.loadN != 4 || p.contiguousAdjacentIterations() || p.iterationsPerVector() != 1 {
		t.Fatal("wrong strided pair contract", p.storeN, p.loadN, p.exitHigh)
	}
	for i, l := range p.locals[:p.localN] {
		if (p.exitHigh&(1<<i) != 0) != (l.index == 5 || l.index == 7) {
			t.Fatal("wrong exit lane", l.index, p.exitHigh)
		}
	}
}

func TestAdjacentMultiPairExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, force := regionAdjacentMultiEnabled, regionLoopTestFast
	defer func() { regionAdjacentMultiEnabled, regionLoopTestFast = old, force }()
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := adjacentMultiPairFixture(t, op)
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, n := range []uint64{1, 2, 3, 8} {
				for _, bits := range []uint64{0x8000000000000000, 0x0000000000000001, 0x3ff4000000000000, 0x7ff8000000001234, 0x7ff0000000004321} {
					var expected []byte
					var want uint64
					for _, enabled := range []bool{false, true} {
						regionAdjacentMultiEnabled, regionLoopTestFast = enabled, enabled
						var stats ModuleStats
						got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, func(mem []byte) {
							for i := uint64(0); i < n*4; i++ {
								binary.LittleEndian.PutUint64(mem[128+i*8:], math.Float64bits(float64(i+3)))
								v := bits
								if i%2 != 0 {
									v = math.Float64bits(2.25)
								}
								binary.LittleEndian.PutUint64(mem[516+i*8:], v)
							}
						}, 2048, 128, 516, 2048+n*32)
						if err != nil {
							t.Fatal(op, features, n, bits, enabled, err)
						}
						if !enabled {
							want, expected = got, append([]byte(nil), mem...)
						} else if got != want || !bytes.Equal(mem, expected) || stats.Funcs[0].Peephole["region-loop-multi-pair"] != 1 {
							t.Fatal("two-pair output or exit mismatch", op, features, n, bits, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestAdjacentMultiPairAliasAndTraps(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, force := regionAdjacentMultiEnabled, regionLoopTestFast
	defer func() { regionAdjacentMultiEnabled, regionLoopTestFast = old, force }()
	regionLoopTestFast = false
	m := adjacentMultiPairFixture(t, 0xa0)
	for _, tc := range []struct{ dst, src, n uint64 }{
		{2048, 512, 3}, {128, 512, 3}, {132, 512, 3}, {136, 512, 3},
		{144, 512, 3}, {160, 512, 3}, {120, 512, 3}, {96, 512, 3},
		{2048, 65512, 1}, {2048, 65528, 1}, {2048, 0xfffffff8, 1},
		{65512, 512, 1}, {2048, 512, 0},
	} {
		var want uint64
		var expected []byte
		var wantTrap bool
		for _, enabled := range []bool{false, true} {
			regionAdjacentMultiEnabled = enabled
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, func(mem []byte) {
				for i := 0; i+8 <= len(mem); i += 8 {
					binary.LittleEndian.PutUint64(mem[i:], math.Float64bits(float64(i/8+1)))
				}
			}, tc.dst, 128, tc.src, tc.dst+32*tc.n)
			if !enabled {
				want, expected, wantTrap = got, append([]byte(nil), mem...), err != nil
			} else if (err != nil) != wantTrap || (!wantTrap && got != want) || !bytes.Equal(mem, expected) {
				t.Fatal("alias/trap fallback changed effects", tc, got, want, err)
			}
		}
	}
}
