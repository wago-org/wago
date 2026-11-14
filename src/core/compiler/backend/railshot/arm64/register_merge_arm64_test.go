//go:build arm64

package arm64

import (
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestRegisterOnlyMergeStates(t *testing.T) {
	saved := registerOnlyMergeEnabled
	defer func() { registerOnlyMergeEnabled = saved }()
	registerOnlyMergeEnabled = true
	for _, floating := range []bool{false, true} {
		for _, targetState := range []locState{lsReg, lsStackReg, lsMem} {
			for _, sourceState := range []locState{lsReg, lsStackReg, lsMem} {
				reg, typ := X19, mtI64
				if floating {
					reg, typ = 8, mtF64
				}
				f := fn{a: &a64.Asm{}, usesCalls: true, nLocals: 1, locals: []localDef{{reg: reg, isFloat: floating, state: sourceState}}, localType: []machineType{typ}, localSlot: []uint32{0}}
				if floating {
					f.fpinnedLocalMask = maskOf(reg)
				} else {
					f.pinnedLocalMask = maskOf(reg)
				}
				var target packedLocStates
				target.set(0, targetState)
				f.convergeEdgeTo(&target)
				want := 0
				if sourceState == lsReg && targetState != lsReg || sourceState == lsMem && targetState != lsMem {
					want = 4
				}
				if f.a.Len() != want {
					t.Fatalf("float=%v target=%v source=%v bytes=%d want=%d", floating, targetState, sourceState, f.a.Len(), want)
				}
				f.setLocalsState(target)
				if f.locals[0].state != targetState {
					t.Fatal("merge target not installed")
				}
			}
		}
	}
	for _, on := range []bool{false, true} {
		registerOnlyMergeEnabled = on
		f := fn{a: &a64.Asm{}, usesCalls: true, nLocals: 1, locals: []localDef{{reg: X19, state: lsReg}}, localType: []machineType{mtI64}, localSlot: []uint32{0}, pinnedLocalMask: maskOf(X19)}
		var target packedLocStates
		f.convergeEdgeTo(&target)
		if target.empty() {
			t.Fatal("all-dirty target mistaken for absent snapshot")
		}
		want := lsStackReg
		if on {
			want = lsReg
		}
		if target.get(0) != want {
			t.Fatalf("enabled=%v state=%v want=%v", on, target.get(0), want)
		}
	}
}

func TestRegisterOnlyMergeSnapshotPresence(t *testing.T) {
	var s packedLocStates
	if !s.empty() {
		t.Fatal("zero snapshot is present")
	}
	for i := 0; i < 64; i++ {
		s.set(i, lsReg)
	}
	if s.empty() {
		t.Fatal("all-dirty snapshot is absent")
	}
	s.set(63, lsConstZero)
	if s.get(63) != lsConstZero || s.get(0) != lsReg {
		t.Fatal("lazy zero and dirty register states overlap")
	}
}

func TestRegisterOnlyMergeSnapshotIndexBounds(t *testing.T) {
	for _, index := range []int{-1, 64, 95, 96} {
		var snapshot packedLocStates
		for _, operation := range []func(){func() { snapshot.get(index) }, func() { snapshot.set(index, lsReg) }} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("local index %d did not reject access outside 64 states", index)
					}
				}()
				operation()
			}()
		}
	}
}

func TestImplicitEntryMergeFirstEdge(t *testing.T) {
	saved := implicitEntryRegisterMergeEnabled
	defer func() { implicitEntryRegisterMergeEnabled = saved }()
	implicitEntryRegisterMergeEnabled = true
	for _, fixed := range []bool{false, true} {
		f := fn{a: &a64.Asm{}, usesCalls: true, nLocals: 2, locals: []localDef{{reg: X19, state: lsMem}, {reg: X20, state: lsMem}}, localType: []machineType{mtI64, mtI64}, localSlot: []uint32{0, 1}, pinnedLocalMask: maskOf(X19) | maskOf(X20)}
		var entry, target packedLocStates
		entry.set(0, lsReg)
		entry.set(1, lsStackReg)
		if fixed {
			target.set(0, lsMem)
			target.set(1, lsMem)
		}
		f.convergeEdgeToWithDead(&target, 0, 0, true, &entry)
		if fixed {
			if target.get(0) != lsMem || target.get(1) != lsMem || f.a.Len() != 0 {
				t.Fatal("an earlier edge's fixed contract changed")
			}
		} else if target.get(0) != lsReg || target.get(1) != lsMem || f.a.Len() != 4 || f.locals[0].state != lsStackReg {
			t.Fatal("first edge did not load just the implicit edge's required register")
		}
	}
}

func TestRegisterOnlyMergeLexicalTargets(t *testing.T) {
	savedMerge, savedLimit := registerOnlyMergeEnabled, registerMergeLoopLimitEnabled
	defer func() { registerOnlyMergeEnabled, registerMergeLoopLimitEnabled = savedMerge, savedLimit }()
	registerOnlyMergeEnabled, registerMergeLoopLimitEnabled = true, true
	for _, kind := range []ctrlKind{cfBlock, cfIf, cfLoop} {
		for _, acyclic := range []bool{false, true} {
			f := fn{a: &a64.Asm{}, usesCalls: true, nLocals: 1, locals: []localDef{{reg: X19, state: lsReg}}, localType: []machineType{mtI64}, localSlot: []uint32{0}, pinnedLocalMask: maskOf(X19)}
			fr := ctrlFrame{kind: kind}
			fr.set(ctrlAcyclicLocalMerge, acyclic)
			f.convergeFrameBranchState(&fr)
			want := lsStackReg
			if kind != cfLoop && acyclic {
				want = lsReg
			}
			if state := f.frameBranchState(&fr).get(0); state != want {
				t.Fatalf("kind=%d acyclic=%v state=%d want=%d", kind, acyclic, state, want)
			}
		}
	}
}
