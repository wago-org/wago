//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runModuleI32 compiles the whole module, maps the combined code blob, and
// executes the local function localFuncIdx with the given i32 args, returning
// the first i32 result. Unlike runI32 (which uses CompileFunction and rejects
// calls/relocs), this drives a full CompileModule blob so tests can exercise
// internal generated-to-generated wasm calls.
func runModuleI32(t *testing.T, m *wasm.Module, localFuncIdx int, args ...int32) int32 {
	t.Helper()

	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	if localFuncIdx < 0 || localFuncIdx >= len(cm.Entry) {
		t.Fatalf("local function index %d out of range", localFuncIdx)
	}

	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)

	serArgs := ar.Alloc(128)
	results := ar.Alloc(128)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint32(serArgs[i*8:], uint32(a))
	}

	fn := entry + uintptr(cm.Entry[localFuncIdx])
	if err := eng.Call(fn, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return int32(binary.LittleEndian.Uint32(results))
}

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

// TestPinnedLocalsSurviveInternalCallClobberingAllPinnedRegs proves a caller's
// pinned locals survive an internal generated wasm call whose callee uses (and
// thus clobbers) all four pinned registers. It fails if emitWrapperCall does not
// spill/reload pinned locals around the call. runModuleI32 is required because
// CompileFunction rejects calls/relocs.
func TestPinnedLocalsSurviveInternalCallClobberingAllPinnedRegs(t *testing.T) {
	m := watToModule(t, `(module
		(func $clobber (param i32 i32 i32 i32) (result i32)
			local.get 0 local.get 1 i32.add
			local.get 2 i32.add
			local.get 3 i32.add)

		(func (export "f") (param $x i32) (result i32)
			(local $a i32) (local $b i32) (local $c i32)

			local.get $x i32.const 10 i32.add local.set $a
			local.get $x i32.const 20 i32.add local.set $b
			local.get $x i32.const 30 i32.add local.set $c

			i32.const 1 i32.const 2 i32.const 3 i32.const 4
			call $clobber
			drop

			local.get $x
			local.get $a i32.add
			local.get $b i32.add
			local.get $c i32.add))`)

	// $clobber is local function 0; the exported caller is local function 1.
	// x=5, a=15, b=25, c=35 => 80. If the caller's pinned regs are not
	// spilled/reloaded around the call, this tends to observe the callee's
	// pinned values instead.
	if got := runModuleI32(t, m, 1, 5); got != 80 {
		t.Fatalf("pinned locals after internal call = %d, want 80", got)
	}
}

// TestPinnedAliasCaptureMultiplePendingReadsCrossPinnedSet stresses
// materializeLocalRefs for vPinned: several pending lazy reads of a pinned local
// are live before local.tee overwrites that same pinned local. All pending reads
// must capture the OLD value, not the value tee'd in.
func TestPinnedAliasCaptureMultiplePendingReadsCrossPinnedSet(t *testing.T) {
	m := watToModule(t, `(module
		(func (export "f") (param $a i32) (param $b i32) (result i32)
			local.get $a
			local.get $a
			local.get $b
			local.tee $a
			i32.add
			i32.add))`)

	// old(a) + old(a) + b = 7 + 7 + 11 = 25. Without materializing the pending
	// vPinned reads before local.tee overwrites $a, this becomes 11 + 11 + 11.
	if got := runI32(t, m, 7, 11); got != 25 {
		t.Fatalf("multi-alias capture = %d, want 25", got)
	}
}
