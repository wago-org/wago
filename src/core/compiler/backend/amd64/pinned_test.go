//go:build linux && amd64

package amd64

import "testing"

// Integer locals are pinned to dedicated registers (first len(pinnedPool));
// the rest stay frame-resident. These tests exercise the boundary cases that
// the register-pinned-locals lowering introduces.

// TestPinnedManyLocals sums six i32 params: indices 0..3 are pinned, 4..5 are
// frame-resident, so this mixes pinned and spilled operands in one expression.
func TestPinnedManyLocals(t *testing.T) {
	m := watToModule(t, `(module (func (export "f")
		(param i32 i32 i32 i32 i32 i32) (result i32)
		local.get 0 local.get 1 i32.add
		local.get 2 i32.add local.get 3 i32.add
		local.get 4 i32.add local.get 5 i32.add))`)
	if got := runI32(t, m, 1, 2, 4, 8, 16, 32); got != 63 {
		t.Fatalf("sum6 = %d, want 63", got)
	}
}

// TestPinnedDoubleUse reads the same pinned local twice in one expression.
func TestPinnedDoubleUse(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param i32) (result i32)
		local.get 0 local.get 0 i32.add))`)
	if got := runI32(t, m, 21); got != 42 {
		t.Fatalf("2*x = %d, want 42", got)
	}
	m64 := watToModule(t, `(module (func (export "f") (param i64) (result i64)
		local.get 0 local.get 0 i64.mul))`)
	if got := runI64(t, m64, 1<<20); got != 1<<40 {
		t.Fatalf("x*x = %d, want %d", got, int64(1)<<40)
	}
}

// TestPinnedAliasCapture guards materializeLocalRefs for vPinned: a pending
// lazy read of a pinned local must keep the OLD value when local.tee overwrites
// that local. If capture is missing, the first local.get sees the new value.
func TestPinnedAliasCapture(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param i32) (result i32)
		local.get 0 i32.const 5 local.tee 0 i32.add))`)
	// old(arg) + 5; broken capture would give 5 + 5 = 10 regardless of arg.
	if got := runI32(t, m, 3); got != 8 {
		t.Fatalf("alias capture = %d, want 8 (3+5)", got)
	}
	if got := runI32(t, m, 100); got != 105 {
		t.Fatalf("alias capture = %d, want 105", got)
	}
}

// TestPinnedLoopAccumulator drives a pinned local across loop back-edges and a
// br: the canonical-slot join must agree with the register-resident local.
func TestPinnedLoopAccumulator(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param $n i32) (result i32)
		(local $acc i32) (local $i i32)
		(local.set $i (i32.const 1))
		(block $brk (loop $cont
			(br_if $brk (i32.gt_s (local.get $i) (local.get $n)))
			(local.set $acc (i32.add (local.get $acc) (local.get $i)))
			(local.set $i (i32.add (local.get $i) (i32.const 1)))
			(br $cont)))
		(local.get $acc)))`)
	if got := runI32(t, m, 5); got != 15 {
		t.Fatalf("sum 1..5 = %d, want 15", got)
	}
	if got := runI32(t, m, 100); got != 5050 {
		t.Fatalf("sum 1..100 = %d, want 5050", got)
	}
}

// TestPinnedIfElseJoin sets a pinned local on both arms of an if/else and reads
// it afterward; both edges must leave the value in the same pinned register.
func TestPinnedIfElseJoin(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param $c i32) (result i32)
		(local $r i32)
		(if (local.get $c)
			(then (local.set $r (i32.const 10)))
			(else (local.set $r (i32.const 20))))
		(local.get $r)))`)
	if got := runI32(t, m, 1); got != 10 {
		t.Fatalf("if-true = %d, want 10", got)
	}
	if got := runI32(t, m, 0); got != 20 {
		t.Fatalf("if-false = %d, want 20", got)
	}
}
