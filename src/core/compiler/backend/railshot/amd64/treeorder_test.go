//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTreeRegisterNeed(t *testing.T) {
	leaf := func(kind storageKind) *elem {
		return testValueElem(storage{kind: kind, typ: mtI32})
	}
	node := func(left, right *elem) *elem {
		e := testDeferredElem(opAdd, mtI32, left, right)
		labelDeferredNode(e)
		return e
	}

	balanced := node(node(leaf(stConst), leaf(stConst)), node(leaf(stConst), leaf(stConst)))
	if got := treeRegisterNeed(balanced); got != 3 {
		t.Fatalf("balanced register need = %d, want 3", got)
	}
	if balanced.registerNeed() != 3 {
		t.Fatalf("balanced stored register need = %d, want 3", balanced.registerNeed())
	}
	unbalanced := node(balanced, leaf(stConst))
	if got := treeRegisterNeed(unbalanced); got != 3 {
		t.Fatalf("unbalanced register need = %d, want 3", got)
	}
	if !treeReorderSafe(unbalanced) {
		t.Fatal("pure integer tree should be reorder-safe")
	}
	if treeReorderSafe(node(balanced, leaf(stMemRef))) {
		t.Fatal("tree containing a deferred load must preserve trap order")
	}
	trapping := testDeferredElem(opDivS, mtI32, leaf(stReg), leaf(stReg))
	if treeReorderSafe(trapping) {
		t.Fatal("tree containing division must preserve trap order")
	}
}

func TestValentTreeOrder(t *testing.T) {
	// (a + b) + c: the root's left subtree needs two registers and its right
	// leaf needs one. The two-address condense path normally emits right first;
	// tree ordering commutes the root so the larger subtree is emitted first.
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x20, 0x02, 0x6a, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)

	saved := treeOrderEnabled
	defer func() { treeOrderEnabled = saved }()

	treeOrderEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 11, 13); got != 31 {
		t.Fatalf("enabled result = %d, want 31", got)
	}
	if hits := on.Peephole["tree-order"]; hits != 1 {
		t.Fatalf("tree-order = %d, want 1 (all: %v)", hits, on.Peephole)
	}

	treeOrderEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 11, 13); got != 31 {
		t.Fatalf("disabled result = %d, want 31", got)
	}
	if hits := off.Peephole["tree-order-candidate"]; hits != 1 {
		t.Fatalf("tree-order-candidate = %d, want 1 (all: %v)", hits, off.Peephole)
	}
	if hits := off.Peephole["tree-order"]; hits != 0 {
		t.Fatalf("disabled tree-order = %d, want 0", hits)
	}
}
