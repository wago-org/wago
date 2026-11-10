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

func zeroCounterFixture(t *testing.T, step byte) *wasm.Module {
	m := vectorMapFixture(t)
	b := m.Code[0].BodyBytes
	b = bytes.Replace(b, []byte{0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 3, 0x47}, []byte{0x20, 2, 0x41, step, 0x6a, 0x22, 2}, 1)
	b = bytes.Replace(b, []byte{0x20, 6, 0xbd, 0x0b}, []byte{0x20, 6, 0xbd, 0x20, 2, 0xad, 0x85, 0x0b}, 1)
	m.Code[0].BodyBytes = b
	return m
}

func TestZeroCounterLoopAdmission(t *testing.T) {
	saved := regionZeroCounterEnabled
	defer func() { regionZeroCounterEnabled = saved }()
	for _, step := range []byte{1, 2, 4, 8, 3} {
		m := zeroCounterFixture(t, step)
		r := wasm.ReaderFrom(m.Code[0].BodyBytes)
		if err := r.Step(2); err != nil {
			t.Fatal(err)
		} // loop opcode and empty type, locals excluded
		types := []machineType{mtI32, mtI32, mtI32, mtI32, mtF64, mtF64, mtF64}
		for _, on := range []bool{false, true} {
			regionZeroCounterEnabled = on
			var p regionLoopPlan
			before := r.Offset()
			ok := inspectRegionLoop(r, types, wasm.NewModuleInstructionClassifier(m, true), &p)
			if r.Offset() != before {
				t.Fatal("reader changed")
			}
			if ok != (on && step != 3) {
				t.Fatal("admission", step, on, ok)
			}
			if ok && (!p.zeroTerminated || p.nodes[p.limit].op != 0 || p.nodes[p.limit].uses != 0 || p.nodes[p.limit].bits != 0) {
				t.Fatal("zero limit not proven")
			}
		}
	}
}

func TestZeroCounterLoopArithmeticExitAndFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, old, force := regionZeroCounterEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() { regionZeroCounterEnabled, regionLoopEnabled, regionLoopTestFast = saved, old, force }()
	regionLoopEnabled = false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, step := range []byte{1, 2, 4, 8} {
			for _, n := range []uint64{1, 2, 3, 4, 8} {
				for _, src := range []uint64{128, 256, 260, 65520} {
					if src == 65520 && n > 2 {
						continue
					}
					for _, bits := range []uint64{0, 0x8000000000000000, 0x7ff8000000001234, math.Float64bits(3.25)} {
						var want uint64
						var expected []byte
						for _, on := range []bool{false, true} {
							regionZeroCounterEnabled = on
							regionLoopTestFast = on && n%2 == 0
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, zeroCounterFixture(t, step), CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, func(mem []byte) {
								for i := uint64(0); i < n; i++ {
									binary.LittleEndian.PutUint64(mem[src+8*i:], bits)
								}
							}, 128, src, uint64(uint32(0-uint32(n)*uint32(step))), 0, math.Float64bits(2), math.Float64bits(1.5))
							if err != nil {
								t.Fatal(features, step, n, src, on, err)
							}
							if !on {
								want, expected = got, append([]byte(nil), mem...)
							} else {
								if got != want || !bytes.Equal(mem, expected) {
									t.Fatal("result/memory/exit mismatch", features, step, n, src, on)
								}
								if stats.Funcs[0].Peephole["region-loop-fast"] != 1 {
									t.Fatal("fast path not emitted")
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestZeroCounterLoopNonterminatingFallbackTrapOrder(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, old, force := regionZeroCounterEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() { regionZeroCounterEnabled, regionLoopEnabled, regionLoopTestFast = saved, old, force }()
	regionLoopEnabled, regionLoopTestFast = false, false
	for _, counter := range []uint32{0, 1, 0xfffffffd} {
		var expected []byte
		var wantErr string
		for _, on := range []bool{false, true} {
			regionZeroCounterEnabled = on
			_, mem, err := runMemAmd64WithOptions(t, zeroCounterFixture(t, 2), CompileOptions{}, func(mem []byte) { binary.LittleEndian.PutUint64(mem[65528:], math.Float64bits(3)) }, 128, 65528, uint64(counter), 0, math.Float64bits(2), math.Float64bits(1.5))
			if err == nil {
				t.Fatal("expected load trap", counter, on)
			}
			if !on {
				expected, wantErr = append([]byte(nil), mem...), err.Error()
			} else if !bytes.Equal(mem, expected) || err.Error() != wantErr {
				t.Fatal("fallback trap/order mismatch", counter, on)
			}
			if binary.LittleEndian.Uint64(mem[128:]) != math.Float64bits(7.5) {
				t.Fatal("first store erased", counter, on)
			}
		}
	}
}
