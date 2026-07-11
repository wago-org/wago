//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Allocator-pressure regression net, ported from amd64/{reg_pressure,
// regalloc_memref_spill,brtable_regalloc,allocation}_test.go. The amd64 versions
// pin their assertions to x86 fixed-role registers (RAX/RDX/RCX); the arm64 file
// keeps the same module shapes as end-to-end CORRECTNESS oracles, since arm64's
// orthogonal register file reaches the pressure differently (it has ~2× the
// allocatable GPRs and no fixed div/shift registers). Compile-success at extreme
// deferred-tree depth is itself the regression: these shapes used to hard-fail
// with "no register available to spill".

// regHeavyShiftChainArm64 builds a one-function module (with linear memory, so a
// register is reserved for memBytes) whose body computes a deep left-spine of
// variable-count shifts inside a loop: acc = ((((p0 << c1) << c2) ...). Each shift
// pins a value and a count register; nesting `depth` of them drives register
// pressure and, past the deferred-tree cap, forces the chain to be broken into
// register-sized segments. Mirrors amd64's regHeavyShiftChain.
func regHeavyShiftChainArm64(t *testing.T, nParams, depth int) *wasm.Module {
	t.Helper()
	params := make([]wasm.ValType, nParams)
	for i := range params {
		params[i] = wasm.I32
	}
	acc := byte(nParams)                       // accumulator local index (after the params)
	body := []byte{0x01, 0x01, 0x7f}           // one run of one i32 local
	body = append(body, 0x20, 0x00, 0x21, acc) // acc = p0
	body = append(body, 0x03, 0x40)            // loop (void) — runs once, boosts local scores
	body = append(body, 0x20, acc)             // acc
	for c := 0; c < depth; c++ {
		body = append(body, 0x20, byte(1+c%(nParams-1)), 0x74) // local.get p ; i32.shl
	}
	body = append(body, 0x21, acc) // acc = spine
	body = append(body, 0x0b)      // end loop
	body = append(body, 0x20, acc, 0x0b)

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	memType := append([]byte{0x00}, wasmtest.ULEB(1)...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestExecRegHeavyShiftChainArm64 is the register-pressure regression: a deep
// nested-shift tree must compile (via the deferred-tree depth cap breaking it into
// register-sized segments, or the pinning-off retry) instead of failing to link,
// and must still compute the right value. Depths past ~14 used to hard-fail with
// "no register available to spill". Covers amd64's TestExecRegHeavyUnpinnedRetry
// and TestExecRegHeavyDeepCapped.
func TestExecRegHeavyShiftChainArm64(t *testing.T) {
	const nParams = 8
	for _, depth := range []int{7, 15, 20, 40, 100} {
		m := regHeavyShiftChainArm64(t, nParams, depth)
		if _, err := CompileModuleWith(m, CompileOptions{}); err != nil {
			t.Fatalf("depth %d: compile: %v", depth, err)
		}
		args := make([]uint64, nParams)
		args[0] = 5
		for i := 1; i < nParams; i++ {
			args[i] = 1 // every shift count is 1, so the result is 5 << depth (0 once depth ≥ 32)
		}
		want := uint32(5) << depth
		if got := uint32(runArm64u(t, m, args...)); got != want {
			t.Fatalf("depth %d: shift chain = %d, want %d", depth, got, want)
		}
	}
}

// TestMemRefSpillKeepsLoadArm64 is the deferred-load (stMemRef) eviction
// regression: a deferred integer load holds its effective address in a register
// with the load not yet emitted; when that register is reclaimed under pressure,
// spill() must materialize the load rather than storing the address and silently
// dropping it (which would later use the address as if it were the loaded value).
// This miscompiled AssemblyScript's Unicode casemap() on amd64. The body forces the
// load's address through a div-heavy chain so the address register is reclaimed.
// For x = 12: mem[8] = 200, addr = 96/12 = 8, load8(8) = 200, x/3 = 4, result = 800.
func TestMemRefSpillKeepsLoadArm64(t *testing.T) {
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,       // 0 local decls
		0x41, 0x08, // i32.const 8
		0x41, 0xc8, 0x01, // i32.const 200
		0x3a, 0x00, 0x00, // i32.store8 (mem[8] = 200)
		0x41, 0xe0, 0x00, // i32.const 96
		0x20, 0x00, // local.get 0 (x)
		0x6e,             // i32.div_u  -> 96/x
		0x2d, 0x00, 0x00, // i32.load8_u [96/x]  (deferred load owns the address reg)
		0x20, 0x00, // local.get 0 (x)
		0x41, 0x03, // i32.const 3
		0x6e, // i32.div_u  -> x/3 (drives the reclaim that spills the pending load)
		0x6c, // i32.mul   (loaded byte * (x/3))
		0x0b, // end
	})
	if got := runArm64(t, m, 12); got != 800 {
		t.Fatalf("deferred load spilled as its address instead of its value: got %d, want 800", got)
	}
}

// brTableComputedIndexArm64 builds a br_table whose dispatch index is produced by a
// div (i32.div_u), the shape that stresses jump-table register allocation: the
// index must survive the table-base load. Six nested blocks give the br_table 5
// labels + a default (≥ brTableJumpMin, so the jump-table form fires); each arm
// returns 1000+label. Mirrors amd64's brTableIndexInRAX.
func brTableComputedIndexArm64(t *testing.T) *wasm.Module {
	t.Helper()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	body := []byte{0x00} // no locals
	for i := 0; i < 6; i++ {
		body = append(body, 0x02, 0x40) // block (void)
	}
	body = append(body, 0x20, 0x00, 0x20, 0x01, 0x6e) // local.get 0; local.get 1; i32.div_u
	// br_table with 5 case labels [0..4] + default 5 (count 5 >= brTableJumpMin).
	body = append(body, 0x0e, 0x05, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05)
	for i := 0; i < 6; i++ {
		body = append(body, 0x0b) // end block i
		body = append(body, 0x41) // i32.const
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		body = append(body, 0x0f) // return
	}
	body = append(body, 0x0b) // end func

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestExecBrTableComputedIndexArm64 is the br_table jump-table register-allocation
// regression: the dispatch index (from a div) must survive the table-base load and
// dispatch to the correct arm. It also asserts the jump-table lowering actually
// fired (not an if-chain fallback).
func TestExecBrTableComputedIndexArm64(t *testing.T) {
	m := brTableComputedIndexArm64(t)

	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms.Funcs[0].Peephole["br-table-jump"] == 0 {
		t.Fatalf("br_table did not use the jump-table form: %v", ms.Funcs[0].Peephole)
	}

	// index = a/b, clamped to the default (label 5) when >= 5; arm returns 1000+idx.
	for _, c := range []struct{ a, b uint64 }{
		{0, 1}, {1, 1}, {4, 1}, {5, 1}, {9, 1}, {8, 4}, {100, 1},
	} {
		idx := c.a / c.b
		want := uint64(1000) + idx
		if idx >= 5 {
			want = 1005
		}
		if got := runArm64u(t, m, c.a, c.b); got != want {
			t.Fatalf("f(%d,%d): idx=%d got=%d want=%d", c.a, c.b, idx, got, want)
		}
	}
}

// TestStackArenaOverflowKeepsExistingPointersStableArm64 checks the operand-stack
// arena: growing past its fixed capacity must keep already-handed-out element
// pointers valid and linked (they must not move when the arena "overflows" to
// heap-allocated elems). Ported from amd64's identically-named test; the stack is
// architecture-neutral.
func TestStackArenaOverflowKeepsExistingPointersStableArm64(t *testing.T) {
	s := newStack()
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	for i := 0; i < defaultStackArenaCap+8; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i + 2)})
	}
	if first.kind != ekValue || first.st.cval != 1 {
		t.Fatalf("first arena elem changed after overflow: kind=%v cval=%d", first.kind, first.st.cval)
	}
	if s.head.next != first {
		t.Fatal("first elem is no longer linked after arena overflow")
	}
	if cap(s.arena) != defaultStackArenaCap {
		t.Fatalf("arena cap = %d, want fixed cap %d", cap(s.arena), defaultStackArenaCap)
	}
}
