//go:build arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCtrlFrameSize(t *testing.T) {
	if got, want := unsafe.Sizeof(ctrlFrame{}), uintptr(88); got != want {
		t.Fatalf("ctrlFrame size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameMerge{}), uintptr(136); got != want {
		t.Fatalf("ctrlFrameMerge size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameRoots{}), uintptr(72); got != want {
		t.Fatalf("ctrlFrameRoots size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameEH{}), uintptr(48); got != want {
		t.Fatalf("ctrlFrameEH size = %d, want %d", got, want)
	}
}

func TestScalarBlockResultUsesInlineFrameTypeArm64(t *testing.T) {
	var f fn
	params, results, types, res0, err := f.blockType(wasm.NewReader([]byte{0x7f}))
	if err != nil {
		t.Fatal(err)
	}
	if params != nil || results != nil || types != nil || res0 != mtI32 {
		t.Fatalf("scalar block type = %v/%v/%v/%v, want nil/nil/nil/i32", params, results, types, res0)
	}
	fr := ctrlFrame{resultN: 1, res0: res0}
	var storage [1]machineType
	got := fr.appendResultTypes(storage[:0])
	if len(got) != 1 || got[0] != mtI32 {
		t.Fatalf("inline result types = %v, want [i32]", got)
	}
}

func TestControlBaseTypeArenaArm64(t *testing.T) {
	var f fn
	outer := ctrlFrame{}
	inner := ctrlFrame{}
	f.setFrameBaseTypes(&outer, []machineType{mtI32, mtV128})
	f.setFrameBaseTypes(&inner, []machineType{mtF64})
	f.releaseFrameBaseTypes(&ctrlFrame{}) // unreachable frames never acquire arena storage

	if got := f.frameBaseTypes(&outer); len(got) != 2 || got[0] != mtI32 || got[1] != mtV128 {
		t.Fatalf("outer base types = %v, want [i32 v128]", got)
	}
	if got := f.frameBaseTypes(&inner); len(got) != 1 || got[0] != mtF64 {
		t.Fatalf("inner base types = %v, want [f64]", got)
	}

	f.releaseFrameBaseTypes(&inner)
	f.releaseFrameBaseTypes(&outer)
	if f.controlBaseTypeN != 0 {
		t.Fatalf("released arena length = %d, want 0", f.controlBaseTypeN)
	}
	reused := ctrlFrame{}
	f.setFrameBaseTypes(&reused, []machineType{mtI64, mtF32})
	if got := f.frameBaseTypes(&reused); len(got) != 2 || got[0] != mtI64 || got[1] != mtF32 {
		t.Fatalf("reused base types = %v, want [i64 f32]", got)
	}
}

func TestControlBaseTypeArenaColdFallbackArm64(t *testing.T) {
	f := fn{controlBaseTypeN: uint8(maxScratchFunctionResults)}
	fr := ctrlFrame{resultN: 1, res0: mtI64}
	f.setFrameBaseTypes(&fr, []machineType{mtI32})
	if !fr.has(ctrlColdBaseTypes) {
		t.Fatal("overflow frame did not use cold type storage")
	}
	if got := f.frameBaseTypes(&fr); len(got) != 1 || got[0] != mtI32 {
		t.Fatalf("cold base types = %v, want [i32]", got)
	}
	if got := fr.appendResultTypes(nil); len(got) != 1 || got[0] != mtI64 {
		t.Fatalf("cold result types = %v, want [i64]", got)
	}
	f.releaseFrameBaseTypes(&fr)
	if f.controlBaseTypeN != uint8(maxScratchFunctionResults) {
		t.Fatalf("cold release changed arena length to %d", f.controlBaseTypeN)
	}
	typed := ctrlFrame{paramN: 1, resultN: 2, types: []machineType{mtF32, mtI32, mtF64}}
	f.setFrameBaseTypes(&typed, []machineType{mtV128})
	if got := typed.appendParameterTypes(nil); len(got) != 1 || got[0] != mtF32 {
		t.Fatalf("cold parameter types = %v, want [f32]", got)
	}
	if got := typed.appendResultTypes(nil); len(got) != 2 || got[0] != mtI32 || got[1] != mtF64 {
		t.Fatalf("cold multi-result types = %v, want [i32 f64]", got)
	}
}

func TestControlBaseTypeArenaRejectsOutOfOrderReleaseArm64(t *testing.T) {
	var f fn
	outer := ctrlFrame{}
	inner := ctrlFrame{}
	f.setFrameBaseTypes(&outer, []machineType{mtI32})
	f.setFrameBaseTypes(&inner, []machineType{mtI64})
	defer func() {
		if recover() == nil {
			t.Fatal("out-of-order release did not panic")
		}
	}()
	f.releaseFrameBaseTypes(&outer)
}

func TestFrameEndSitesInlineFirstArm64(t *testing.T) {
	var f fn
	fr := ctrlFrame{kind: cfBlock}
	f.appendFrameEnd(&fr, 4, false)
	f.appendFrameEnd(&fr, 12, true)
	f.appendFrameEnd(&fr, 20, false)
	first, second, overflow := f.frameEndSites(&fr)
	if first != 5 {
		t.Fatalf("first packed end site = %#x, want %#x", first, uint32(5))
	}
	if second != frameEndConditional|13 {
		t.Fatalf("second packed end site = %#x, want %#x", second, frameEndConditional|13)
	}
	if len(overflow) != 1 || overflow[0] != 21 {
		t.Fatalf("overflow packed end sites = %#x, want [0x15]", overflow)
	}
}

func TestPackedLocStatesArm64(t *testing.T) {
	states := make(packedLocStates, packedLocStateBytes(9))
	want := []locState{lsReg, lsStackReg, lsMem, lsConstZero, lsConstZero, lsMem, lsStackReg, lsReg, lsMem}
	for i, state := range want {
		states.set(i, state)
	}
	for i, state := range want {
		if got := states.get(i); got != state {
			t.Fatalf("state %d = %d, want %d", i, got, state)
		}
	}
	if got, wantBytes := len(states), 3; got != wantBytes {
		t.Fatalf("packed bytes = %d, want %d", got, wantBytes)
	}
}

func TestPushCtrlReusesMergeSlotAtDepth(t *testing.T) {
	f := fn{ctrl: make([]ctrlFrame, 0, 1)}
	first := ctrlFrame{}
	f.ensureCtrlMerge(&first).branchState = make(packedLocStates, 1)
	f.ensureCtrlRoots(&first).base = []bool{true}
	f.pushCtrl(&first)
	f.releaseCtrlMerge(&f.ctrl[0])
	f.ctrl = f.ctrl[:0]

	next := ctrlFrame{}
	f.ensureCtrlMerge(&next).branchState = make(packedLocStates, 2)
	f.ensureCtrlRoots(&next).base = []bool{false, true}
	f.pushCtrl(&next)

	if got, want := len(f.scratchState().ctrlMerges), 1; got != want {
		t.Fatalf("merge sidecar length = %d, want %d", got, want)
	}
	if got, want := f.ctrl[0].mergeIndex, uint32(1); got != want {
		t.Fatalf("merge index = %d, want %d", got, want)
	}
	if got, want := next.mergeIndex, uint32(1); got != want {
		t.Fatalf("caller merge index = %d, want %d", got, want)
	}
	if got, want := len(f.frameBranchState(&f.ctrl[0])), 2; got != want {
		t.Fatalf("moved branch state length = %d, want %d", got, want)
	}
	if got := f.frameBaseGCRoots(&f.ctrl[0]); len(got) != 2 || got[0] || !got[1] {
		t.Fatalf("moved GC roots = %v, want [false true]", got)
	}
	f.releaseCtrlMerge(&next)
	if got := f.frameBranchState(&f.ctrl[0]); got != nil {
		t.Fatalf("released branch state = %v, want nil", got)
	}
	if got := f.frameBaseGCRoots(&f.ctrl[0]); got != nil {
		t.Fatalf("released GC roots = %v, want nil", got)
	}
}

func TestGCRootFlagsAvoidsAllFalseBacking(t *testing.T) {
	roots := []*elem{{kind: ekValue}, {kind: ekDeferred}, {kind: ekValue}}
	if got := gcRootFlags(roots); got != nil {
		t.Fatalf("all-false roots = %v, want nil", got)
	}
	roots[1].kind = ekValue
	roots[1].st.setGCRoot(true)
	got := gcRootFlags(roots)
	if len(got) != len(roots) || got[0] || !got[1] || got[2] {
		t.Fatalf("roots = %v, want [false true false]", got)
	}
}
