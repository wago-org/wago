//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestBoundsFactsElision checks P6.1 straight-line elision: one check may prove
// same-source fixed-offset loads across a pure range,
// and a later covered access needs no check of its own. Correctness at scale is
// covered by
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

	// A larger later extent in the same pure range is certified by the first
	// access's lookahead, so it still emits only one check.
	grow := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, // off 0 → proves p+4
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // off 4 → needs p+8 > p+4 → checked
		0x0b}
	s = compileWithStats(t, modMem(t, 1, i32, nil, grow), false).Funcs[0]
	if s.BoundsChecks != 1 || s.BoundsChecksElidable != 1 {
		t.Errorf("grow: bounds=%d elidable=%d, want 1/1", s.BoundsChecks, s.BoundsChecksElidable)
	}

	// A shared memory may grow concurrently between the two loads. Looking ahead
	// would then be able to trap on a range that becomes valid before its original
	// access, so shared memory keeps the ordinary forward-only certificates.
	shared := modMem(t, 1, i32, nil, grow)
	shared.Memories[0].Shared = true
	s = compileWithStats(t, shared, false).Funcs[0]
	if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("shared: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	// Potentially trapping integer arithmetic is a hard range barrier: the later
	// OOB check must not move before a possible division trap.
	divBarrier := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a,
		0x41, 0x01, 0x41, 0x00, 0x6e, 0x1a, // i32.div_u by zero
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x0b}
	s = compileWithStats(t, modMem(t, 1, i32, nil, divBarrier), false).Funcs[0]
	if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("division barrier: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
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

	// The NoBoundsFacts compile option disables elision → both accesses checked.
	var ms ModuleStats
	if _, err := CompileModuleWith(modMem(t, 1, i32, nil, covered), CompileOptions{Stats: &ms, NoBoundsFacts: true}); err != nil {
		t.Fatal(err)
	}
	if s := ms.Funcs[0]; s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("NoBoundsFacts: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	// Guard mode elides all inline checks regardless — no facts machinery involved.
	g := compileWithStats(t, modMem(t, 1, i32, nil, covered), true).Funcs[0]
	if g.BoundsChecks != 0 || g.BoundsChecksElidable != 0 {
		t.Errorf("guard: bounds=%d elidable=%d, want 0/0", g.BoundsChecks, g.BoundsChecksElidable)
	}
}
