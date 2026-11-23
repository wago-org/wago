package arm64

import "testing"

// A branch target between the proving ADD and the reusing access is entered by
// a predecessor that never set X16 (for example a br_table dispatch, whose X16
// holds the jump-table address). The reuse must not cross it.
func TestIndexedBaseReuseStopsAtNotedTarget(t *testing.T) {
	for _, store := range []bool{false, true} {
		a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
		a.LoadIdx(X1, X26, X22, 8, 4, false, false)
		a.NoteBranchTarget(a.Len())
		if store {
			a.StoreIdx(X26, X22, X2, 0xa8, 8)
		} else {
			a.LoadIdx(X3, X26, X22, 0xa8, 8, false, true)
		}
		if a.IndexedBaseReuses != 0 || a.IndexedReuseCount() != 0 {
			t.Fatalf("store=%t: reused X16 across a branch target (%d reuses)", store, a.IndexedBaseReuses)
		}
	}
}

// Without a target the same sequence still reuses X16, and validation accepts
// it. A later branch back into the proof window (as a loop back-edge or a
// patched forward branch would produce) makes validation fail.
func TestValidateIndexedReuseRejectsTargetInsideWindow(t *testing.T) {
	a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
	a.LoadIdx(X1, X26, X22, 8, 4, false, false)
	access := a.Len()
	a.LoadIdx(X3, X26, X22, 0xa8, 8, false, true)
	if a.IndexedReuseCount() != 1 {
		t.Fatalf("expected one recorded reuse, got %d (%d hits)", a.IndexedReuseCount(), a.IndexedBaseReuses)
	}
	if !a.ValidateIndexedReuse(nil, nil) {
		t.Fatal("straight-line reuse rejected")
	}
	// A jump-table (or entry) target at the reusing access is outside the
	// straight-line proof.
	if a.ValidateIndexedReuse([]int{access}, nil) {
		t.Fatal("target at the reusing access was accepted")
	}
	// An actual backward branch to the access is found by decoding the code.
	site := a.Branch()
	if !a.PatchBranch26(site, access) {
		t.Fatal("patch failed")
	}
	if a.ValidateIndexedReuse(nil, nil) {
		t.Fatal("backward branch into the reuse window was accepted")
	}
	// A target exactly at the proving ADD enters with X16 recomputed; it is safe.
	b := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
	b.LoadIdx(X1, X26, X22, 8, 4, false, false)
	b.LoadIdx(X3, X26, X22, 0xa8, 8, false, true)
	if !b.ValidateIndexedReuse([]int{0}, nil) {
		t.Fatal("target at the proving ADD was rejected")
	}
}

func TestIndexedReuseTargetOpcode(t *testing.T) {
	for opcode := range 256 {
		// Only B.cond also restricts a bit below the top byte.
		word := uint32(opcode) << 24
		_, branch := dominatedDirectBranchTarget(0, word)
		adr := word&0x9f000000 == 0x10000000
		if got, want := indexedReuseTargetOpcode[opcode], branch || adr; got != want {
			t.Errorf("first byte %#02x: got %t, want %t", opcode, got, want)
		}
	}
}
