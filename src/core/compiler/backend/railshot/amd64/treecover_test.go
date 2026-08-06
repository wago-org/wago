//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAssociativeTreeCover(t *testing.T) {
	// (a + b) + (c + d): the balanced tree needs three registers normally.
	body := []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0x6a,
		0x20, 0x02, 0x20, 0x03, 0x6a,
		0x6a,
		0x41, 0x30, 0x46, // == 48; comparison materializes the add tree without a destination hint.
		0x0b,
	}
	m := mod1(t,
		[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32},
		[]wasm.ValType{wasm.I32}, body)

	saved := associativeTreeEnabled
	defer func() { associativeTreeEnabled = saved }()

	associativeTreeEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 11, 13, 17); got != 1 {
		t.Fatalf("enabled result = %d, want 1", got)
	}
	if hits := on.Peephole["assoc-tree"]; hits != 1 {
		t.Fatalf("assoc-tree = %d, want 1 (all: %v)", hits, on.Peephole)
	}

	associativeTreeEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 7, 11, 13, 17); got != 1 {
		t.Fatalf("disabled result = %d, want 1", got)
	}
	if hits := off.Peephole["assoc-tree-candidate"]; hits != 1 {
		t.Fatalf("assoc-tree-candidate = %d, want 1 (all: %v)", hits, off.Peephole)
	}
	if hits := off.Peephole["assoc-tree"]; hits != 0 {
		t.Fatalf("disabled assoc-tree = %d, want 0", hits)
	}
}

func TestTreeAccumulatorSafety(t *testing.T) {
	leaf := func(kind storageKind) *elem {
		return &elem{kind: ekValue, st: storage{kind: kind, typ: mtI32}}
	}
	constantShift := &elem{kind: ekDeferred, op: opShl, typ: mtI32,
		arg0: leaf(stReg), arg1: leaf(stConst)}
	if !treeAccumulatorSafe(constantShift) {
		t.Fatal("constant shift should honor a live accumulator")
	}
	variableShift := &elem{kind: ekDeferred, op: opShl, typ: mtI32,
		arg0: leaf(stReg), arg1: leaf(stReg)}
	if treeAccumulatorSafe(variableShift) {
		t.Fatal("variable shift can evict an RCX accumulator")
	}
}
