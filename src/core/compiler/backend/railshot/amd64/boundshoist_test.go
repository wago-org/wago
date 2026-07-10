//go:build amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// readLoopBody is a loop that accumulates mem[$ptr] (invariant base) 4 times and
// returns the sum: (param $ptr) (local $i $sum). Used to exercise both the fast
// (in-bounds → elided) and slow (OOB → checked, traps) versioned paths.
var readLoopBody = []byte{
	0x01, 0x02, 0x7f, // 2 i32 locals
	0x03, 0x40, // loop void
	0x20, 0x02, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x21, 0x02, // $sum += mem[$ptr]
	0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // $i++
	0x20, 0x01, 0x41, 0x04, 0x48, 0x0d, 0x00, // if $i<4 br 0
	0x0b, 0x20, 0x02, 0x0b, // end loop; local.get $sum; end
}

// TestLoopPrecheckSlowTrap: an out-of-bounds invariant base fails the precheck and
// runs the SLOW (checked) body, which must trap at the access — preserving exact
// trap semantics (a hoisted check would have trapped before the loop regardless).
func TestLoopPrecheckSlowTrap(t *testing.T) {
	withLoopPrecheck(t, func() {
		i32 := wasm.I32
		m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, readLoopBody)
		// In bounds: precheck passes, fast body, sum = 4 * mem[16].
		got, _, err := runMemAmd64(t, m, func(lin []byte) { binary.LittleEndian.PutUint32(lin[16:], 7) }, 16)
		if err != nil {
			t.Fatalf("in-bounds read trapped: %v", err)
		}
		if uint32(got) != 28 {
			t.Errorf("in-bounds sum = %d, want 28", uint32(got))
		}
		// Out of bounds (ptr beyond the 64 KiB page): precheck fails, slow body traps.
		if _, _, err := runMemAmd64(t, m, nil, 100000); err == nil {
			t.Errorf("OOB invariant base did not trap (slow path missing its check)")
		}
	})
}

// withLoopPrecheck runs fn with WAGO_LOOP_PRECHECK force-enabled and the benefit
// threshold lowered to 1, so the small test loops (one elided check each) still
// version.
func withLoopPrecheck(t *testing.T, fn func()) {
	t.Helper()
	prevEn, prevK := loopPrecheckEnabled, loopPrecheckMinChecks
	loopPrecheckEnabled, loopPrecheckMinChecks = true, 1
	defer func() { loopPrecheckEnabled, loopPrecheckMinChecks = prevEn, prevK }()
	fn()
}

// TestLoopPrecheckBenefitGate checks that a loop below the elided-check threshold
// is NOT versioned (the fast/slow bodies double the loop code, so a loop that
// would elide too few checks is not worth it).
func TestLoopPrecheckBenefitGate(t *testing.T) {
	i32 := wasm.I32
	m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, readLoopBody) // 1 elided check/iter
	prevEn, prevK := loopPrecheckEnabled, loopPrecheckMinChecks
	defer func() { loopPrecheckEnabled, loopPrecheckMinChecks = prevEn, prevK }()
	loopPrecheckEnabled = true

	loopPrecheckMinChecks = 3 // 1 < 3 → not versioned
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms.Funcs[0].Peephole["loop-precheck"] != 0 {
		t.Errorf("loop with 1 elided check should NOT version at K=3")
	}

	loopPrecheckMinChecks = 1 // 1 >= 1 → versioned
	var ms2 ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms2}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms2.Funcs[0].Peephole["loop-precheck"] == 0 {
		t.Errorf("loop should version at K=1")
	}
}

// TestLoopPrecheckExec versions a loop that reads mem[$ptr] (a loop-invariant
// base local) each iteration: it stores 10 at $ptr, then loops 4× accumulating
// mem[$ptr], returning 40. Exercises the precheck + fast (elided) body.
func TestLoopPrecheckExec(t *testing.T) {
	withLoopPrecheck(t, func() {
		i32 := wasm.I32
		// (param $ptr) (local $i $sum):
		//   mem[$ptr] = 10
		//   loop { $sum += mem[$ptr]; $i++; if $i<4 continue }
		//   return $sum
		body := []byte{
			0x01, 0x02, 0x7f, // 2 i32 locals ($i=1, $sum=2)
			0x20, 0x00, 0x41, 0x0a, 0x36, 0x02, 0x00, // mem[$ptr] = 10
			0x03, 0x40, // loop void
			0x20, 0x02, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x21, 0x02, // $sum += mem[$ptr]
			0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // $i++
			0x20, 0x01, 0x41, 0x04, 0x48, 0x0d, 0x00, // if $i<4 br 0
			0x0b,       // end loop
			0x20, 0x02, // local.get $sum
			0x0b, // end func
		}
		m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
		if got := runAmd64(t, m, 16); got != 40 {
			t.Errorf("versioned loop sum = %d, want 40", got)
		}
		// Confirm it actually versioned (precheck emitted).
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatalf("compile: %v", err)
		}
		if ms.Funcs[0].Peephole["loop-precheck"] == 0 {
			t.Errorf("loop was not versioned (no loop-precheck peep); peeps=%v", ms.Funcs[0].Peephole)
		}
	})
}
