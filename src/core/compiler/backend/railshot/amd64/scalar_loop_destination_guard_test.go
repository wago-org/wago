//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime"
)

func TestScalarLoopDestinationGuardedWidthAndEffects(t *testing.T) {
	requireCompilerDiagnostics(t)
	oldMemory := scalarLoopMemoryFormsEnabled
	defer func() { scalarLoopMemoryFormsEnabled = oldMemory }()
	scalarLoopMemoryFormsEnabled = true
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	old, recurrence, whole, force := scalarLoopDestinationEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast
	defer func() {
		scalarLoopDestinationEnabled, scalarMemoryRecurrenceEnabled, regionLoopEnabled, regionLoopTestFast = old, recurrence, whole, force
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
					scalarLoopDestinationEnabled = on
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
