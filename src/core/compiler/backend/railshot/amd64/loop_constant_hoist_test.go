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

func constantHoistPlan(bits uint64) regionLoopPlan {
	var p regionLoopPlan
	address := p.add(regionLoopNode{})
	load := p.add(regionLoopNode{op: 0x2b, left: address})
	a := p.add(regionLoopNode{op: 0x44, bits: bits, pos: 10})
	mul := p.add(regionLoopNode{op: 0xa2, left: load, right: a})
	b := p.add(regionLoopNode{op: 0x44, bits: bits, pos: 20})
	sum := p.add(regionLoopNode{op: 0xa0, left: mul, right: b})
	p.events = [regionLoopMaxOps]uint8{load, a, mul, b, sum, 0x80}
	p.eventN, p.storeN = 6, 1
	p.stores[0] = regionLoopStore{address: address, value: sum}
	return p
}

func TestLoopConstantHoistBoundedPlan(t *testing.T) {
	for _, bits := range []uint64{0, 0x8000000000000000, 0x7ff8000000000123, 0x7ff0000000000432} {
		p := constantHoistPlan(bits)
		before := p
		if prefix, _ := p.hoistConstants(false, false, false); prefix != 0 || p != before {
			t.Fatal("disabled policy mutated plan")
		}
		prefix, keep := p.hoistConstants(true, false, false)
		if prefix != 1 || p.eventN != before.eventN-1 {
			t.Fatal("literal not shared/hoisted", prefix, p.eventN)
		}
		id := p.events[0]
		if !keep[id] || p.nodes[id].bits != bits || p.nodes[p.events[2]].right != id || p.nodes[p.events[3]].right != id {
			t.Fatal("raw literal/operand identity lost")
		}
		want := [4]uint8{before.events[0], before.events[2], before.events[4], before.events[5]}
		for i, event := range want {
			if p.events[i+1] != event {
				t.Fatal("effect/arithmetic order changed")
			}
		}
	}
	p := constantHoistPlan(0)
	p.nodes[p.events[3]].bits = 0x8000000000000000
	if n, keep := p.hoistConstants(true, false, false); n != 2 || !keep[p.events[0]] || !keep[p.events[1]] {
		t.Fatal("signed zeros merged")
	}
	p = constantHoistPlan(1)
	c := p.add(regionLoopNode{op: 0x44, bits: 2})
	d := p.add(regionLoopNode{op: 0x44, bits: 3})
	p.events[p.eventN], p.events[p.eventN+1] = c, d
	p.eventN += 2
	before := p
	if n, _ := p.hoistConstants(true, false, false); n != 0 || p != before {
		t.Fatal("constant cap failed or changed rejected plan")
	}
	// Retaining two constants may exceed the physical register budget. Rejection
	// must leave the original vector plan available, rather than disable it.
	p = constantHoistPlan(1)
	p.nodes[p.events[3]].bits = 2
	at := uint8(0)
	p.events[at] = p.events[1]
	at++
	p.events[at] = p.events[3]
	at++
	var loads [10]uint8
	for i := range loads {
		loads[i] = p.add(regionLoopNode{op: 0x2b})
		p.events[at] = loads[i]
		at++
	}
	value := loads[0]
	for _, id := range loads[1:] {
		value = p.add(regionLoopNode{op: 0xa0, left: value, right: id})
		p.events[at] = value
		at++
	}
	value = p.add(regionLoopNode{op: 0xa0, left: value, right: p.events[0]})
	p.events[at] = value
	at++
	value = p.add(regionLoopNode{op: 0xa0, left: value, right: p.events[1]})
	p.events[at] = value
	at++
	p.stores[0].value = value
	p.events[at] = 0x80
	at++
	p.eventN = at
	before = p
	if n, _ := p.hoistConstants(true, false, false); n != 0 || p != before {
		t.Fatal("pressure rejection lost original plan")
	}
}

func constantHoistFixture(t *testing.T, bits uint64) *wasm.Module {
	m := vectorMapFixture(t)
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], bits)
	for _, local := range []byte{4, 5} {
		m.Code[0].BodyBytes = bytes.ReplaceAll(m.Code[0].BodyBytes, []byte{0x20, local}, append([]byte{0x44}, raw[:]...))
	}
	return m
}

func TestLoopConstantHoistExactResultsAndFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, adjacent, force, hoist, prefix := regionLoopEnabled, regionAdjacentEnabled, regionLoopTestFast, regionConstantHoistEnabled, regionInvariantPrefixEnabled
	defer func() {
		regionLoopEnabled, regionAdjacentEnabled, regionLoopTestFast, regionConstantHoistEnabled, regionInvariantPrefixEnabled = old, adjacent, force, hoist, prefix
	}()
	regionAdjacentEnabled, regionInvariantPrefixEnabled = false, false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, bits := range []uint64{0, 0x8000000000000000, math.Float64bits(1.5), 0x7ff8000000000123, 0x7ff0000000000432} {
			m := constantHoistFixture(t, bits)
			for _, tc := range []struct {
				dst, src, n uint64
				trap        bool
			}{{128, 256, 2, false}, {128, 256, 4, false}, {128, 256, 3, false}, {128, 128, 4, false}, {128, 132, 4, false}, {128, 65528, 2, true}} {
				var want uint64
				var expected []byte
				var trapped bool
				for mode := 0; mode < 3; mode++ {
					regionLoopEnabled, regionConstantHoistEnabled = mode != 0, mode == 2
					regionLoopTestFast = mode != 0 && tc.n%2 == 0 && (tc.dst == tc.src || tc.src == 256) && !tc.trap
					init := func(mem []byte) {
						for i := uint64(0); i < tc.n; i++ {
							if tc.src+8*i+8 <= uint64(len(mem)) {
								binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+2)))
							}
						}
					}
					var stats ModuleStats
					got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, tc.dst, tc.src, 0, tc.n, 0, 0)
					if mode == 0 {
						want, expected, trapped = got, append([]byte(nil), mem...), err != nil
					} else if (err != nil) != trapped || (!trapped && got != want) || !bytes.Equal(mem, expected) {
						t.Fatal("hoisted bits/memory/trap differ", features, bits, tc, mode, got, want, err)
					}
					if (err != nil) != tc.trap {
						t.Fatal("wrong trap", tc, mode, err)
					}
					if mode == 2 && stats.Funcs[0].Peephole["region-loop-constant-hoist"] != 1 {
						t.Fatal("constant policy not emitted", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
