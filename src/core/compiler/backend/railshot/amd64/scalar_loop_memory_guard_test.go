//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func runScalarLoopMemoryGuarded(t *testing.T, m *wasm.Module, features shared.AMD64Features, src, other, count uint64) (uint64, []byte, error) {
	t.Helper()
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["region-loop-scalar-memory-recurrence"] != 1 {
		t.Fatal("missing scalar recurrence")
	}
	if (stats.Funcs[0].Peephole["region-loop-scalar-fold-load"] != 0) != scalarLoopMemoryFormsEnabled {
		t.Fatal("incorrect memory form admission")
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemoryGuarded(65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	mem := jm.CurrentBytes()
	for i := uint64(0); i < 4; i++ {
		if src+i*8+8 <= 65536 {
			binary.LittleEndian.PutUint64(mem[src+i*8:], math.Float64bits(float64(i+2)))
		}
	}
	binary.LittleEndian.PutUint64(mem[1024:], math.Float64bits(1.25))
	if other+8 <= 65536 {
		binary.LittleEndian.PutUint64(mem[other:], math.Float64bits(-2.25))
	}
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	args, results, trap := ar.Alloc(40), ar.Alloc(8), ar.Alloc(runtime.TrapBufferBytes)
	for i, v := range []uint64{1024, src, 0, count, other} {
		binary.LittleEndian.PutUint64(args[i*8:], v)
	}
	err = eng.CallGuarded(entry+uintptr(cm.Entry[0]), args, jm.LinMemBase(), trap, results, jm)
	return binary.LittleEndian.Uint64(results), append([]byte(nil), mem...), err
}

func TestScalarLoopMemoryFormsGuardedWidthAndEffects(t *testing.T) {
	requireCompilerDiagnostics(t)
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	old, recurrence, whole, force := scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() {
		scalarLoopMemoryFormsEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = old, recurrence, whole, force
	}()
	scalarMemoryRecurrenceEnabled = true
	regionLoopEnabled = false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for op := byte(0xa0); op <= 0xa3; op++ {
			m := scalarLoopMemoryFixture(t, op, true)
			for _, tc := range []struct {
				src, other, count uint64
				trap, fast        bool
			}{
				{65528, 2048, 1, false, true}, {65520, 2048, 2, false, true}, {65524, 2048, 1, false, true},
				{65528, 2048, 2, true, false}, {65524, 2048, 2, true, false}, {65529, 2048, 1, true, false},
				{128, 65529, 2, true, false}, {128, 65528, 2, false, true}, {0xfffffff8, 2048, 2, true, false},
			} {
				var want uint64
				var memory []byte
				var trapped bool
				for _, on := range []bool{false, true} {
					scalarLoopMemoryFormsEnabled = on
					regionLoopTestFast = tc.fast
					got, mem, err := runScalarLoopMemoryGuarded(t, m, features, tc.src, tc.other, tc.count)
					if !on {
						want, memory, trapped = got, mem, err != nil
					} else if (err != nil) != trapped || (!trapped && got != want) || !bytes.Equal(mem, memory) {
						t.Fatal("changed guarded result or earlier stores", features, op, tc, got, want, err)
					}
					if (err != nil) != tc.trap {
						t.Fatal("incorrect trap", features, op, tc, err)
					}
				}
			}
		}
	}
}
