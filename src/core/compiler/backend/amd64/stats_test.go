//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"
)

// statsWAT has two functions: $add (straight-line) and $main, which does a
// bounds-checked load, a comparison fused into an `if`, and a direct call.
const statsWAT = `(module
  (memory 1)
  (func $add (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)
  (func $main (param i32) (result i32)
    local.get 0
    i32.load
    local.get 0
    i32.const 10
    i32.lt_s
    (if (result i32)
      (then local.get 0 i32.const 1 call $add)
      (else i32.const 0))
    i32.add))`

func TestCodegenStatsCounters(t *testing.T) {
	m := watToModule(t, statsWAT)

	var s CodegenStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &s}); err != nil {
		t.Fatalf("compile: %v", err)
	}

	if s.Functions != 2 {
		t.Errorf("Functions = %d, want 2", s.Functions)
	}
	if s.BytesEmitted <= 0 {
		t.Errorf("BytesEmitted = %d, want > 0", s.BytesEmitted)
	}
	if s.DirectCalls != 1 {
		t.Errorf("DirectCalls = %d, want 1 (main calls add)", s.DirectCalls)
	}
	if s.BoundsChecks != 1 {
		t.Errorf("BoundsChecks = %d, want 1 (one i32.load in default mode)", s.BoundsChecks)
	}
	if s.BoundsChecksElided != 0 {
		t.Errorf("BoundsChecksElided = %d, want 0 in default mode", s.BoundsChecksElided)
	}
	if s.CompareFusions != 1 {
		t.Errorf("CompareFusions = %d, want 1 (i32.lt_s fused into if)", s.CompareFusions)
	}
	if s.MaxStackDepth < 2 {
		t.Errorf("MaxStackDepth = %d, want >= 2", s.MaxStackDepth)
	}
}

// In guard-page mode the inline bounds check is elided, so the same load is
// counted as elided rather than checked.
func TestCodegenStatsBoundsElided(t *testing.T) {
	m := watToModule(t, statsWAT)

	var s CodegenStats
	if _, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Stats: &s}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if s.BoundsChecks != 0 {
		t.Errorf("BoundsChecks = %d, want 0 with ElideBoundsChecks", s.BoundsChecks)
	}
	if s.BoundsChecksElided != 1 {
		t.Errorf("BoundsChecksElided = %d, want 1 with ElideBoundsChecks", s.BoundsChecksElided)
	}
}

// Collecting stats must not perturb code generation: the bytes emitted with and
// without a Stats sink must be identical.
func TestCodegenStatsDoesNotAffectCodegen(t *testing.T) {
	m := watToModule(t, statsWAT)

	plain, err := CompileModuleWith(m, CompileOptions{})
	if err != nil {
		t.Fatalf("compile (plain): %v", err)
	}
	var s CodegenStats
	withStats, err := CompileModuleWith(m, CompileOptions{Stats: &s})
	if err != nil {
		t.Fatalf("compile (stats): %v", err)
	}
	if !bytes.Equal(plain.Code, withStats.Code) {
		t.Errorf("stats collection changed codegen: %d vs %d bytes", len(plain.Code), len(withStats.Code))
	}
}
