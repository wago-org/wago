//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBoundsFactsElisionArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	covered := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a,
		0x0b}
	s := compileWithStats(t, modMem(t, 1, i32, nil, covered), false).Funcs[0]
	if s.BoundsChecks != 1 || s.BoundsChecksElidable != 1 {
		t.Errorf("covered: bounds=%d elidable=%d, want 1/1", s.BoundsChecks, s.BoundsChecksElidable)
	}

	grow := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x0b}
	s = compileWithStats(t, modMem(t, 1, i32, nil, grow), false).Funcs[0]
	if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("grow: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	reset := []byte{0x00,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x20, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a,
		0x0b}
	s = compileWithStats(t, modMem(t, 1, i32, nil, reset), false).Funcs[0]
	if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("reset-by-set: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	var ms ModuleStats
	if _, err := CompileModuleWith(modMem(t, 1, i32, nil, covered), CompileOptions{Stats: &ms, NoBoundsFacts: true}); err != nil {
		t.Fatal(err)
	}
	if s := ms.Funcs[0]; s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
		t.Errorf("NoBoundsFacts: bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
	}

	g := compileWithStats(t, modMem(t, 1, i32, nil, covered), true).Funcs[0]
	if g.BoundsChecks != 0 || g.BoundsChecksElidable != 0 {
		t.Errorf("guard: bounds=%d elidable=%d, want 0/0", g.BoundsChecks, g.BoundsChecksElidable)
	}
}
