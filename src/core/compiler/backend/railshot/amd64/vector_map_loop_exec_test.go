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

func vectorMapFixture(t *testing.T) *wasm.Module {
	// Independent outputs; the final temporary must expose the last scalar lane.
	body := []byte{1, 1, 0x7c, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x2b, 0, 0, 0x20, 4, 0xa2, 0x20, 5, 0xa0, 0x22, 6, 0x39, 0, 0,
		0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 8, 0x6a, 0x21, 1,
		0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 3, 0x47, 0x0d, 0, 0x0b,
		0x20, 6, 0xbd, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestVectorMapIndependentLanesAndLastValue(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	m := vectorMapFixture(t)
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, src := range []uint64{128, 256, 260} {
			for _, n := range []uint64{2, 4, 8} {
				var want uint64
				var expected []byte
				for _, on := range []bool{false, true} {
					regionLoopEnabled, regionLoopTestFast = on, on
					init := func(mem []byte) {
						for i := uint64(0); i < n; i++ {
							binary.LittleEndian.PutUint64(mem[src+8*i:], math.Float64bits(float64(i+1)))
						}
					}
					var stats ModuleStats
					got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, 128, src, 0, n, math.Float64bits(2), math.Float64bits(1.5))
					// Partial overlap (132) must take fallback; exact overlap and disjoint
					// arrays qualify. The forced-fast check is used only for those cases.
					if err != nil {
						t.Fatal(features, src, n, on, got, err)
					}
					if !on {
						want, expected = got, append([]byte(nil), mem...)
					} else if got != want || !bytes.Equal(mem, expected) {
						t.Fatal("lane or memory mismatch", features, src, n, got, want)
					}
					if on && diagnosticsEnabled && stats.Funcs[0].Peephole["region-loop-fast"] != 1 {
						t.Fatal("no paired fast path", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}

func TestVectorMapRawFloatResults(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	m := vectorMapFixture(t)
	for _, bits := range []uint64{0, 1, 0x8000000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000123, 0x0010000000000000} {
		var want uint64
		var expected []byte
		for _, on := range []bool{false, true} {
			regionLoopEnabled, regionLoopTestFast = on, on
			init := func(mem []byte) {
				binary.LittleEndian.PutUint64(mem[256:], math.Float64bits(3))
				binary.LittleEndian.PutUint64(mem[264:], bits)
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, 128, 256, 0, 2, math.Float64bits(2), math.Float64bits(-0.0))
			if err != nil {
				t.Fatal(bits, on, err)
			}
			if !on {
				want, expected = got, append([]byte(nil), mem...)
			} else if got != want || !bytes.Equal(mem, expected) {
				t.Fatal("FP bits changed", bits, got, want)
			}
		}
	}
}

func TestVectorMapOddCountAliasingAndWrappingFallback(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	m := vectorMapFixture(t)
	for _, tc := range []struct{ src, start, limit uint64 }{{256, 0, 1}, {256, 0, 3}, {256, 0, 7}, {120, 0, 4}, {136, 0, 4}, {132, 0, 4}, {256, 0xfffffffe, 0}} {
		var want uint64
		var expected []byte
		for _, on := range []bool{false, true} {
			regionLoopEnabled, regionLoopTestFast = on, false
			init := func(mem []byte) {
				for i := uint64(0); i < 8; i++ {
					binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+1)))
				}
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, 128, tc.src, tc.start, tc.limit, math.Float64bits(2), math.Float64bits(1.5))
			if err != nil {
				t.Fatal(tc, on, err)
			}
			if !on {
				want, expected = got, append([]byte(nil), mem...)
			} else if got != want || !bytes.Equal(mem, expected) {
				t.Fatal("fallback changed source behavior", tc, got, want)
			}
		}
	}
}

func TestVectorMapBoundsFallbackPreservesEarlierStores(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	m := vectorMapFixture(t)
	var expected []byte
	for _, on := range []bool{false, true} {
		regionLoopEnabled, regionLoopTestFast = on, false
		init := func(mem []byte) {
			binary.LittleEndian.PutUint64(mem[65520:], math.Float64bits(3))
			binary.LittleEndian.PutUint64(mem[65528:], math.Float64bits(5))
		}
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, 128, 65520, 0, 4, math.Float64bits(2), math.Float64bits(1.5))
		if err == nil || binary.LittleEndian.Uint64(mem[128:]) != math.Float64bits(7.5) || binary.LittleEndian.Uint64(mem[136:]) != math.Float64bits(11.5) {
			t.Fatal(on, err)
		}
		if !on {
			expected = append([]byte(nil), mem...)
		} else if !bytes.Equal(mem, expected) {
			t.Fatal("early range trap erased prior stores")
		}
	}
}

func TestVectorMapIntegerExitState(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	base := vectorMapFixture(t)
	body := append([]byte{1, 1, 0x7c}, base.Code[0].BodyBytes...)
	body = body[:len(body)-1]
	body = append(body, 0x20, 2, 0xad, 0x42, 32, 0x86, 0x85, 0x20, 0, 0xad, 0x42, 16, 0x86, 0x85, 0x20, 1, 0xad, 0x85, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
	m.Memories = base.Memories
	for _, n := range []uint64{2, 4, 8} {
		var want uint64
		for _, on := range []bool{false, true} {
			regionLoopEnabled, regionLoopTestFast = on, on
			init := func(mem []byte) {
				for i := uint64(0); i < n; i++ {
					binary.LittleEndian.PutUint64(mem[256+8*i:], math.Float64bits(float64(i+1)))
				}
			}
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, 128, 256, 0, n, math.Float64bits(2), math.Float64bits(1.5))
			if err != nil {
				t.Fatal(n, on, err)
			}
			expected := math.Float64bits(float64(n)*2+1.5) ^ n<<32 ^ (128+n*8)<<16 ^ (256 + n*8)
			if got != expected {
				t.Fatal("exit state", n, on, got, expected)
			}
			if !on {
				want = got
			} else if got != want {
				t.Fatal("exit differs", got, want)
			}
		}
	}
}

func TestVectorMapTwoOrderedStores(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	body := []byte{2, 1, 0x7c, 1, 0x7f, 0x20, 0, 0x41}
	body = append(body, 0x80, 0x02) // signed LEB128 256; keep this fixture portable.
	body = append(body, 0x6a, 0x21, 7, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x2b, 0, 0, 0x20, 4, 0xa2, 0x20, 5, 0xa0, 0x22, 6, 0x39, 0, 0,
		0x20, 7, 0x20, 6, 0x20, 4, 0xa3, 0x39, 0, 0,
		0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 8, 0x6a, 0x21, 1,
		0x20, 7, 0x41, 8, 0x6a, 0x21, 7,
		0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 3, 0x47, 0x0d, 0, 0x0b,
		0x20, 6, 0xbd, 0x20, 7, 0xad, 0x85, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, src := range []uint64{256, 384} {
		var want uint64
		var expected []byte
		for _, on := range []bool{false, true} {
			regionLoopEnabled, regionLoopTestFast = on, on
			init := func(mem []byte) {
				for i := uint64(0); i < 8; i++ {
					binary.LittleEndian.PutUint64(mem[src+8*i:], math.Float64bits(float64(i+1)))
				}
			}
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: &stats}, init, 128, src, 0, 8, math.Float64bits(2), math.Float64bits(1.5))
			if err != nil {
				t.Fatal(src, on, err)
			}
			if !on {
				want, expected = got, append([]byte(nil), mem...)
			} else if got != want || !bytes.Equal(mem, expected) {
				t.Fatal("two outputs", src, got, want)
			}
			if on && diagnosticsEnabled && stats.Funcs[0].Peephole["region-loop-fast"] != 1 {
				t.Fatal("two-output fast path absent", stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestVectorMapInvariantLoadOverlap(t *testing.T) {
	saved, force := regionLoopEnabled, regionLoopTestFast
	defer func() { regionLoopEnabled, regionLoopTestFast = saved, force }()
	base := vectorMapFixture(t)
	body := append([]byte{1, 1, 0x7c}, base.Code[0].BodyBytes...)
	at := bytes.Index(body, []byte{0x20, 5, 0xa0})
	if at < 0 {
		t.Fatal("fixture bias missing")
	}
	body = append(append(append([]byte(nil), body[:at]...), 0x20, 3, 0x41, 3, 0x74, 0x2b, 0, 0), body[at+2:]...)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
	m.Memories = base.Memories
	for _, n := range []uint64{4, 16} {
		var want uint64
		var expected []byte
		for _, on := range []bool{false, true} {
			regionLoopEnabled, regionLoopTestFast = on, on && n == 4
			init := func(mem []byte) {
				for i := uint64(0); i < n; i++ {
					binary.LittleEndian.PutUint64(mem[512+8*i:], math.Float64bits(float64(i+1)))
				}
				binary.LittleEndian.PutUint64(mem[n*8:], math.Float64bits(3))
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, 128, 512, 0, n, math.Float64bits(2), 0)
			if err != nil {
				t.Fatal(n, on, err)
			}
			if !on {
				want, expected = got, append([]byte(nil), mem...)
			} else if got != want || !bytes.Equal(mem, expected) {
				t.Fatal("invariant alias", n, got, want)
			}
		}
	}
}

func TestVectorMapRejectsLoopCarriedFloat(t *testing.T) {
	base := vectorMapFixture(t)
	body := append([]byte(nil), base.Code[0].BodyBytes...)
	at := bytes.Index(body, []byte{0x20, 5, 0xa0})
	if at < 0 {
		t.Fatal("fixture bias missing")
	}
	body[at+1] = 6
	r := wasm.NewReader(body)
	if op, err := r.Byte(); err != nil || op != 3 {
		t.Fatal(op, err)
	}
	if bt, err := r.Byte(); err != nil || bt != 0x40 {
		t.Fatal(bt, err)
	}
	types := []machineType{mtI32, mtI32, mtI32, mtI32, mtF64, mtF64, mtF64}
	var p regionLoopPlan
	if !inspectRegionLoop(*r, types, wasm.NewModuleInstructionClassifier(base, true), &p) {
		t.Fatal("recurrence should be represented by the source model")
	}
	if p.independentLanes() {
		t.Fatal("loop-carried FP reassociation admitted")
	}
}

func vectorMapMemoryFixture(t *testing.T, op byte) *wasm.Module {
	body := []byte{1, 1, 0x7c, 0x03, 0x40, 0x20, 0, 0x20, 1, 0x2b, 0, 0, 0x20, 2, 0x2b, 0, 0, op, 0x22, 5, 0x39, 0, 0}
	for i := byte(0); i < 3; i++ {
		body = append(body, 0x20, i, 0x41, 8, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x22, 3, 0x20, 4, 0x47, 0x0d, 0, 0x0b, 0x20, 5, 0xbd, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestVectorMapPackedMemoryArithmetic(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, force, forms := regionLoopEnabled, regionLoopTestFast, regionLoopMemForms
	defer func() { regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = saved, force, forms }()
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := vectorMapMemoryFixture(t, op)
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, src := range []uint64{512, 516, 65520} {
				for _, rhs := range []uint64{0x8000000000000000, 0x3ff4000000000000, 0x7ff0000000000000, 0x7ff8000000001234, 0x7ff0000000004321} {
					var want uint64
					var expected []byte
					for _, on := range []bool{false, true} {
						regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = on, on, on
						init := func(mem []byte) {
							for i := uint64(0); i < 2; i++ {
								binary.LittleEndian.PutUint64(mem[128+8*i:], math.Float64bits(float64(i+3)))
								binary.LittleEndian.PutUint64(mem[src+8*i:], rhs)
							}
						}
						var stats ModuleStats
						got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, 1024, 128, src, 0, 2)
						if err != nil {
							t.Fatal(op, features, src, rhs, on, err)
						}
						if !on {
							want, expected = got, append([]byte(nil), mem...)
						} else {
							if got != want || !bytes.Equal(mem, expected) {
								t.Fatal("packed memory changed bits", op, features, src, rhs, got, want)
							}
							hits := stats.Funcs[0].Peephole["region-loop-fold-load"]
							if (hits > 0) != features.Has(shared.AMD64AVX) {
								t.Fatal("incorrect feature admission", features, hits)
							}
						}
					}
				}
			}
		}
	}
}

func TestVectorMapMemoryStoreBarrier(t *testing.T) {
	// The RHS load precedes an observable store and is consumed afterwards.
	// Delaying it could see the new stored value. The original pair has a
	// separate explicit load and must retain that form even on AVX.
	p := regionLoopPlan{nodeN: 4, eventN: 4, storeN: 1, localN: 1}
	p.locals[0].step = 8
	p.nodes[1] = regionLoopNode{coeff: [regionLoopMaxLocals]uint32{1}}
	p.nodes[2] = regionLoopNode{op: 0x2b, left: 1}
	p.nodes[3] = regionLoopNode{op: 0x44, bits: math.Float64bits(1)}
	p.nodes[4] = regionLoopNode{op: 0xa1, left: 3, right: 2}
	p.stores[0] = regionLoopStore{value: 3}
	p.events = [regionLoopMaxOps]uint8{2, 3, 0x80, 4}
	folded, memory := p.memoryForms(true)
	if folded[2] || memory[4] != 0 {
		t.Fatal("load crossed a store")
	}
	p.events = [regionLoopMaxOps]uint8{2, 3, 4, 0x80}
	folded, memory = p.memoryForms(true)
	if !folded[2] || memory[4] != 2 {
		t.Fatal("safe RHS was not folded")
	}
	p.locals[0].step = 0
	folded, memory = p.memoryForms(true)
	if folded[2] || memory[4] != 0 {
		t.Fatal("scalar broadcast widened")
	}
}

func TestVectorMapMemoryBoundsFallback(t *testing.T) {
	saved, force, forms := regionLoopEnabled, regionLoopTestFast, regionLoopMemForms
	defer func() { regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = saved, force, forms }()
	m := vectorMapMemoryFixture(t, 0xa0)
	var expected []byte
	for _, on := range []bool{false, true} {
		regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = on, false, on
		init := func(mem []byte) {
			binary.LittleEndian.PutUint64(mem[128:], math.Float64bits(3))
			binary.LittleEndian.PutUint64(mem[136:], math.Float64bits(5))
			binary.LittleEndian.PutUint64(mem[65528:], math.Float64bits(7))
		}
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, 1024, 128, 65528, 0, 2)
		if err == nil || binary.LittleEndian.Uint64(mem[1024:]) != math.Float64bits(10) {
			t.Fatal("range fallback erased first store", on, err)
		}
		if !on {
			expected = append([]byte(nil), mem...)
		} else if !bytes.Equal(mem, expected) {
			t.Fatal("trap memory mismatch")
		}
	}
}
