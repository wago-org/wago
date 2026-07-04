//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestBoundsFactsElision checks P6.1 straight-line elision (docs/no-ir-plan.md
// P6): a second access on the same address source, within the extent a prior
// check proved, needs no check of its own. Correctness at scale is covered by
// TestCorpusDifferential on the compute kernels (nbody/fannkuch/sha256/raytrace);
// this pins the counter behaviour and the invalidation points.
func TestBoundsFactsElision(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}

	// func(p i32){ local.get p; i32.load off=4; drop;   // check proves p+8 <= mem
	//              local.get p; i32.load off=0; drop }   // p+4 <= p+8 → elided
	covered := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a,
		0x0b}
	s := compileWithStats(t, modMem(t, 1, i32, nil, covered), false).Funcs[0]
	if s.BoundsChecks != 1 || s.BoundsChecksElidable != 1 {
		t.Errorf("covered: bounds=%d elidable=%d, want 1/1", s.BoundsChecks, s.BoundsChecksElidable)
	}

	// A LARGER later extent is not covered by a smaller prior one → both checked.
	grow := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, // off 0 → proves p+4
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // off 4 → needs p+8 > p+4 → checked
		0x0b}
	s = compileWithStats(t, modMem(t, 1, i32, nil, grow), false).Funcs[0]
	if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("grow: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	// A local.set of the certified base between the two accesses invalidates the
	// certificate (the base value changed) → the second access is re-checked.
	reset := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // load off 4 → proves p+8
		0x20, 0x00, 0x21, 0x00, // local.set p (p = p)
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, // load off 0 → re-checked
		0x0b}
	s = compileWithStats(t, modMem(t, 1, i32, nil, reset), false).Funcs[0]
	if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("reset-by-set: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	// Guard mode elides all inline checks regardless — no facts machinery involved.
	g := compileWithStats(t, modMem(t, 1, i32, nil, covered), true).Funcs[0]
	if g.BoundsChecks != 0 || g.BoundsChecksElidable != 0 {
		t.Errorf("guard: bounds=%d elidable=%d, want 0/0", g.BoundsChecks, g.BoundsChecksElidable)
	}
}
