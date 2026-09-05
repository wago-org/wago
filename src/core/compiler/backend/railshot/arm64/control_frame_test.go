//go:build arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestCtrlFrameSize(t *testing.T) {
	if got, want := unsafe.Sizeof(ctrlFrame{}), uintptr(72); got != want {
		t.Fatalf("ctrlFrame size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameMerge{}), uintptr(112); got != want {
		t.Fatalf("ctrlFrameMerge size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameRoots{}), uintptr(24); got != want {
		t.Fatalf("ctrlFrameRoots size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameEH{}), uintptr(32); got != want {
		t.Fatalf("ctrlFrameEH size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ehCatchClause{}), uintptr(20); got != want {
		t.Fatalf("ehCatchClause size = %d, want %d", got, want)
	}
}

func TestControlGCRootSegmentsShareBackingArm64(t *testing.T) {
	var f fn
	fr := ctrlFrame{height: 2, paramN: 2, resultN: 2}
	flags := f.ensureFrameGCRootFlags(&fr)
	flags[1], flags[2] = true, true
	fr.set(ctrlHasBaseGCRoots, true)
	fr.set(ctrlHasParamGCRoots, true)
	f.setFrameResultGCRoot(&fr, 1)

	base, params, results := f.frameBaseGCRoots(&fr), f.frameParamGCRoots(&fr), f.frameResultGCRoots(&fr)
	if len(base) != 2 || base[0] || !base[1] {
		t.Fatalf("base roots = %v, want [false true]", base)
	}
	if len(params) != 2 || !params[0] || params[1] {
		t.Fatalf("parameter roots = %v, want [true false]", params)
	}
	if len(results) != 2 || results[0] || !results[1] {
		t.Fatalf("result roots = %v, want [false true]", results)
	}
}

func TestCaptureControlGCRootSegmentsArm64(t *testing.T) {
	f := fn{s: newStack(), gcFrameRoots: &shared.GCFrameRootPlan{Candidate: true}}
	base := f.s.pushValue(storage{})
	base.st.setGCRoot(true)
	param := f.s.pushValue(storage{})
	param.st.setGCRoot(true)
	fr := ctrlFrame{height: 1, paramN: 1, resultN: 1}
	f.captureGCFrameShape(&fr)

	if got := f.frameBaseGCRoots(&fr); len(got) != 1 || !got[0] {
		t.Fatalf("base roots = %v, want [true]", got)
	}
	if got := f.frameParamGCRoots(&fr); len(got) != 1 || !got[0] {
		t.Fatalf("parameter roots = %v, want [true]", got)
	}
	if got := f.frameResultGCRoots(&fr); got != nil {
		t.Fatalf("result roots = %v, want nil", got)
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

func TestFrameEndSitesInlinePairArm64(t *testing.T) {
	var f fn
	fr := ctrlFrame{kind: cfIf, controlSite: 99}
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
	if fr.controlSite != 99 {
		t.Fatalf("if false-edge site = %d, want 99", fr.controlSite)
	}
}

func TestBoundedPinCandidateOrderingArm64(t *testing.T) {
	var storage [3]gpCand
	top := storage[:0]
	for _, candidate := range []gpCand{
		{global: true, idx: 0, score: 5},
		{idx: 0, score: 5},
		{global: true, idx: 1, score: 9},
		{idx: 2, score: 9},
		{idx: 1, score: 1},
	} {
		top = insertGPCandidate(top, candidate, len(storage))
	}
	want := []gpCand{{idx: 2, score: 9}, {global: true, idx: 1, score: 9}, {idx: 0, score: 5}}
	for i := range want {
		if top[i] != want[i] {
			t.Fatalf("GP candidate %d = %+v, want %+v", i, top[i], want[i])
		}
	}

	scores := []uint32{3, 9, 9, 1}
	var localsStorage [2]uint16
	locals := localsStorage[:0]
	for _, local := range []uint16{3, 2, 0, 1} {
		locals = insertLocalCandidate(locals, local, scores, len(localsStorage))
	}
	if len(locals) != 2 || locals[0] != 1 || locals[1] != 2 {
		t.Fatalf("local candidates = %v, want [1 2]", locals)
	}
}

func TestIntrusiveReturnPatchChainArm64(t *testing.T) {
	a := &a64.Asm{}
	f := fn{a: a, sc: &scratch{asm: a}}
	sites := make([]int, 3)
	for i := range sites {
		sites[i] = a.Branch()
		f.appendReturnSite(sites[i])
	}
	target := a.Len()
	f.patchReturnSites()
	for _, site := range sites {
		got, ok := branchTarget(site, rdWord(a.B, site))
		if !ok || got != target {
			t.Fatalf("return at %d targets %d/%v, want %d/true", site, got, ok, target)
		}
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

func TestConvergeEdgeWithoutPinnedLocalsSkipsSnapshotArm64(t *testing.T) {
	f := fn{
		usesCalls: true,
		nLocals:   8,
		locals:    make([]localDef, 8),
	}
	var target packedLocStates
	f.convergeEdgeTo(&target)
	if target != nil {
		t.Fatalf("merge state = %v, want nil", target)
	}
	if len(f.lsPool) != 0 || f.lsPoolBytes != 0 {
		t.Fatalf("local-state pool = %d buffers/%d bytes, want empty", len(f.lsPool), f.lsPoolBytes)
	}
}

func TestPushCtrlReusesMergeSlotAtDepth(t *testing.T) {
	f := fn{ctrl: make([]ctrlFrame, 0, 1)}
	first := ctrlFrame{height: 1}
	f.ensureCtrlMerge(&first).branchState = make(packedLocStates, 1)
	first.set(ctrlHasBaseGCRoots, true)
	f.ensureCtrlRoots(&first).flags = []bool{true}
	f.pushCtrl(&first)
	f.releaseCtrlMerge(&f.ctrl[0])
	f.ctrl = f.ctrl[:0]

	next := ctrlFrame{height: 2}
	f.ensureCtrlMerge(&next).branchState = make(packedLocStates, 2)
	next.set(ctrlHasBaseGCRoots, true)
	f.ensureCtrlRoots(&next).flags = []bool{false, true}
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
	roots := []*elem{testElem(ekValue), testElem(ekDeferred), testElem(ekValue)}
	if got := gcRootFlags(roots); got != nil {
		t.Fatalf("all-false roots = %v, want nil", got)
	}
	roots[1].setElemKind(ekValue)
	roots[1].st.setGCRoot(true)
	got := gcRootFlags(roots)
	if len(got) != len(roots) || got[0] || !got[1] || got[2] {
		t.Fatalf("roots = %v, want [false true false]", got)
	}
}

func TestLocStatePoolRetentionIsBoundedArm64(t *testing.T) {
	f := fn{nLocals: 4}
	for range maxRetainedLocStateBufs + 3 {
		f.freeLocStateBuf(make(packedLocStates, 1))
	}
	if got := len(f.lsPool); got != maxRetainedLocStateBufs {
		t.Fatalf("retained local-state buffers = %d, want %d", got, maxRetainedLocStateBufs)
	}
	if got := f.lsPoolBytes; got != maxRetainedLocStateBufs {
		t.Fatalf("retained local-state bytes = %d, want %d", got, maxRetainedLocStateBufs)
	}
	if got := cap(f.lsPool); got != maxRetainedLocStateBufs {
		t.Fatalf("retained local-state header capacity = %d, want %d", got, maxRetainedLocStateBufs)
	}

	f.lsPool = nil
	f.lsPoolBytes = 0
	f.freeLocStateBuf(make(packedLocStates, 1, maxRetainedLocStateBytes))
	f.freeLocStateBuf(make(packedLocStates, 1))
	if got := len(f.lsPool); got != 1 {
		t.Fatalf("payload-bounded local-state buffers = %d, want 1", got)
	}
	if got := f.lsPoolBytes; got != maxRetainedLocStateBytes {
		t.Fatalf("payload-bounded local-state bytes = %d, want %d", got, maxRetainedLocStateBytes)
	}
	_ = f.newLocStateBuf()
	if f.lsPoolBytes != 0 {
		t.Fatalf("local-state bytes after reuse = %d, want 0", f.lsPoolBytes)
	}

	f.nLocals = 8
	f.lsPool = []packedLocStates{make(packedLocStates, 1)}
	f.lsPoolBytes = 1
	_ = f.newLocStateBuf()
	if f.lsPoolBytes != 0 {
		t.Fatalf("local-state bytes after undersized eviction = %d, want 0", f.lsPoolBytes)
	}
}

func TestEndSitePoolRetentionIsBoundedArm64(t *testing.T) {
	var f fn
	for range maxRetainedEndsBufs + 3 {
		f.freeEndsBuf(make([]uint32, 0, 1))
	}
	if got := len(f.endsPool); got != maxRetainedEndsBufs {
		t.Fatalf("retained end-site buffers = %d, want %d", got, maxRetainedEndsBufs)
	}

	f.endsPool = nil
	f.freeEndsBuf(make([]uint32, 0, maxRetainedEndsBufSites+1))
	if f.endsPool != nil {
		t.Fatal("oversized end-site buffer was retained")
	}
}

func TestReserveLocalScratchArm64(t *testing.T) {
	sc := &scratch{}
	sc.reserveLocalScratch(7)
	if cap(sc.fnState.localType) != 7 || cap(sc.fnState.localSlot) != 7 || cap(sc.fnState.locals) != 7 {
		t.Fatalf("local scratch capacities = %d/%d/%d, want 7/7/7", cap(sc.fnState.localType), cap(sc.fnState.localSlot), cap(sc.fnState.locals))
	}
}
