//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// rhsRelocateFixture builds the smallest synthetic valent tree that forces
// condenseBinary's deferred-RHS fixed-register relocation:
//
//	(13 - 5) - (11 - 4)
//
// The ordinary registers are pinned so the right subtree condenses into RAX.
// Since the left subtree is also deferred, RAX is a fixed-role hazard and the
// RHS must move to the only hazard-free register, R8.
func rhsRelocateFixture(f *fn) (root, right *elem) {
	value := func(v int64) *elem {
		return f.s.pushValue(storage{kind: stConst, typ: mtI64, cval: v})
	}
	deferredSub := func(left, right *elem) *elem {
		e := f.s.alloc()
		e.setElemKind(ekDeferred)
		e.setDeferredOp(opSub)
		e.setValueType(mtI64)
		e.arg0, e.arg1 = left, right
		return f.s.pushDeferred(e)
	}

	left := deferredSub(value(13), value(5))
	right = deferredSub(value(11), value(4))
	root = deferredSub(left, right)
	return root, right
}

var rhsRelocateFixturePins = maskOf(RDI, RSI, RBP, R9, R10, R11, R12, R13, R14, R15)

func resetRHSRelocateFixture(f *fn) (root, right *elem) {
	f.s.reset()
	f.a.B = f.a.B[:0]
	f.regUser = [16]*elem{}
	f.pinned = rhsRelocateFixturePins
	f.maxSpill = 0
	return rhsRelocateFixture(f)
}

func TestDeferredRHSRelocationRetainsArenaOwner(t *testing.T) {
	requireCompilerDiagnostics(t)
	stats := new(CodegenStats)
	f := &fn{
		a:     &encoder.Asm{},
		s:     newStackWithCap(16),
		stats: stats,
	}
	root, right := resetRHSRelocateFixture(f)

	result := f.condenseBinary(root, regNone)
	if diagnosticsEnabled {
		if got := stats.Peephole["rhs-relocate"]; got != 1 {
			t.Fatalf("RHS relocations = %d, want 1", got)
		}
	}
	if result != RAX {
		t.Fatalf("result register = %v, want RAX", result)
	}
	if !right.isValue() || right.st.kind != stReg || right.st.reg != R8 {
		t.Fatalf("arena RHS after relocation = kind %v, storage %+v; want stReg R8", right.elemKind(), right.st)
	}
	if right.prev != nil || right.next != nil {
		t.Fatal("consumed arena RHS remains linked on the operand stack")
	}
	if f.regUser[R8] != nil {
		t.Fatal("consumed relocated RHS still owns R8")
	}
	if f.regUser[RAX] != root {
		t.Fatal("result register is not owned by the root node")
	}
	if f.pinned != rhsRelocateFixturePins {
		t.Fatalf("temporary pins leaked: got %#x, want %#x", f.pinned, rhsRelocateFixturePins)
	}
}

func TestDeferredRHSRelocationTracksForcedSpill(t *testing.T) {
	f := &fn{
		a: &encoder.Asm{},
		s: newStackWithCap(16),
	}
	right := f.s.pushValue(storage{kind: stReg, typ: mtI64, reg: R8})
	f.regUser[R8] = right
	f.pinned = maskOf(R8)

	// Fixed-role users spill their target directly and therefore bypass the
	// ordinary allocator's pin mask. The arena owner must follow that move so the
	// pending ALU reads the slot rather than a clobbered register.
	f.spillIfUsed(R8)
	if right.st.kind != stSlot {
		t.Fatalf("forced spill left RHS in storage %v, want slot", right.st.kind)
	}
	if f.regUser[R8] != nil {
		t.Fatal("forced spill did not release R8")
	}
	before := f.a.Len()
	f.applyALU(aluTable[opSub], RAX, right, true)
	if f.a.Len() == before {
		t.Fatal("pending ALU did not consume the relocated RHS spill slot")
	}
}

func TestExecDeferredRHSRelocation(t *testing.T) {
	requireCompilerDiagnostics(t)
	params := make([]wasm.ValType, 10)
	for i := range params {
		params[i] = wasm.I64
	}
	body := []byte{
		0x00,             // no declared locals
		0x02, 0x40, 0x0b, // empty block disables straight-line interval pinning
	}
	// Keep every parameter hot so all eight local-pin registers are occupied. Two
	// outer deferred RHS results occupy RDI/RSI; the nested RHS then reaches
	// fixed-role RAX and must relocate to R8.
	for i := byte(0); i < byte(len(params)); i++ {
		body = append(body, 0x20, i, 0x1a, 0x20, i, 0x1a)
	}
	body = append(body,
		0x20, 0x00, 0x20, 0x01, 0x7d, // p0 - p1
		0x20, 0x02, 0x20, 0x03, 0x7d, // p2 - p3
		0x7d,                               // (p0-p1) - (p2-p3)
		0x20, 0x04, 0x20, 0x05, 0x7d, 0x7d, // ... - (p4-p5)
		0x20, 0x06, 0x20, 0x07, 0x7d, 0x7d, // ... - (p6-p7)
		0x0b,
	)
	m := mod1(t, params, []wasm.ValType{wasm.I64}, body)
	stats := new(ModuleStats)
	if _, err := CompileModuleWith(m, CompileOptions{Stats: stats}); err != nil {
		t.Fatal(err)
	}
	if diagnosticsEnabled {
		if got := stats.Funcs[0].Peephole["rhs-relocate"]; got != 2 {
			t.Fatalf("RHS relocations = %d, want 2 (pins=%d, peepholes=%v)", got,
				stats.Funcs[0].PinnedLocals, stats.Funcs[0].Peephole)
		}
	}
	args := []uint64{100, 0, 10, 0, 20, 0, 30, 0, 31, 37}
	if got := runAmd64u(t, m, args...); got != 40 {
		t.Fatalf("(100-0)-(10-0)-(20-0)-(30-0) = %d, want 40", got)
	}
}

func TestDeferredRHSRelocationDoesNotAllocate(t *testing.T) {
	f := &fn{
		a: &encoder.Asm{B: make([]byte, 0, 128)},
		s: newStackWithCap(16),
	}
	allocs := testing.AllocsPerRun(1000, func() {
		root, _ := resetRHSRelocateFixture(f)
		f.condenseBinary(root, regNone)
	})
	if allocs != 0 {
		t.Fatalf("allocations per RHS relocation = %.2f, want 0", allocs)
	}
}

func TestLocalSinkKeepsRegionalDestination(t *testing.T) {
	for _, tee := range []bool{false, true} {
		stats := new(CodegenStats)
		f := &fn{
			a: &encoder.Asm{}, s: newStackWithCap(16), stats: stats,
			nLocals: 1, localType: []machineType{mtI64}, localSlot: []uint32{0},
			locals:      []localDef{{typ: mtI64, reg: R12, state: lsReg}},
			intervalReg: []Reg{RSP}, intervalScore: []uint32{2}, intervalActive: 1,
			pinnedLocalMask: maskOf(R12),
		}
		for r := range f.intervalOwner {
			f.intervalOwner[r] = -1
		}
		f.intervalOwner[R12] = 0
		value := func(v int64) *elem { return f.s.pushValue(storage{kind: stConst, typ: mtI64, cval: v}) }
		sub := func(left, right *elem) *elem {
			e := f.s.alloc()
			e.setElemKind(ekDeferred)
			e.setDeferredOp(opSub)
			e.setValueType(mtI64)
			e.arg0, e.arg1 = left, right
			return f.s.pushDeferred(e)
		}
		left := sub(value(13), value(5))
		rightLeft := sub(value(11), value(4))
		rightRight := sub(value(9), value(3))
		sub(left, sub(rightLeft, rightRight))
		// A nested RHS relocation occurs before the outer binary lowering pins its
		// destination. It must spill instead of evicting the pending assignment.
		f.pinned = rhsRelocateFixturePins.remove(R12).add(R8)
		f.setLocal(nil, 0, tee)
		if f.locals[0].reg != R12 || f.locals[0].state != lsReg || f.intervalOwner[R12] != 0 {
			t.Fatalf("assignment lost its destination: local=%+v owner=%d", f.locals[0], f.intervalOwner[R12])
		}
		if diagnosticsEnabled {
			if stats.Residency.Evictions != 0 || stats.Spills == 0 {
				t.Fatalf("expected RHS spill without destination eviction: %+v", stats)
			}
		}
		if f.reserved != 0 {
			t.Fatalf("temporary destination reservation leaked: %#x", f.reserved)
		}
		if tee && (f.s.back().st.kind != stLocalReg || f.s.back().st.reg != R12) {
			t.Fatal("tee lost its borrowed result")
		}
	}
}
