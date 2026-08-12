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
		e.kind, e.op, e.typ = ekDeferred, opSub, mtI64
		e.arg0, e.arg1 = left, right
		return f.s.push(e)
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
	stats := new(CodegenStats)
	f := &fn{
		a:     &encoder.Asm{},
		s:     newStackWithCap(16),
		stats: stats,
	}
	root, right := resetRHSRelocateFixture(f)

	result := f.condenseBinary(root, regNone)
	if got := stats.Peephole["rhs-relocate"]; got != 1 {
		t.Fatalf("RHS relocations = %d, want 1", got)
	}
	if result != RAX {
		t.Fatalf("result register = %v, want RAX", result)
	}
	if right.kind != ekValue || right.st.kind != stReg || right.st.reg != R8 {
		t.Fatalf("arena RHS after relocation = kind %v, storage %+v; want stReg R8", right.kind, right.st)
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
	if got := stats.Funcs[0].Peephole["rhs-relocate"]; got != 2 {
		t.Fatalf("RHS relocations = %d, want 2 (pins=%d, peepholes=%v)", got,
			stats.Funcs[0].PinnedLocals, stats.Funcs[0].Peephole)
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
