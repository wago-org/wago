//go:build amd64

package amd64

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestCtrlFrameSize(t *testing.T) {
	if got, want := unsafe.Sizeof(ctrlFrame{}), uintptr(72); got != want {
		t.Fatalf("ctrlFrame size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameMerge{}), uintptr(88); got != want {
		t.Fatalf("ctrlFrameMerge size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameRoots{}), uintptr(72); got != want {
		t.Fatalf("ctrlFrameRoots size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameEH{}), uintptr(48); got != want {
		t.Fatalf("ctrlFrameEH size = %d, want %d", got, want)
	}
}

func TestControlGCRootSidecarAMD64(t *testing.T) {
	var f fn
	fr := ctrlFrame{}
	f.ensureCtrlRoots(&fr).baseGCRoots = []bool{true}
	if got := f.frameBaseGCRoots(&fr); len(got) != 1 || !got[0] {
		t.Fatalf("root-only sidecar = %v, want [true]", got)
	}
}

func TestScalarBlockResultUsesInlineFrameTypeAMD64(t *testing.T) {
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

func TestControlBaseTypeArenaAMD64(t *testing.T) {
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

func TestControlBaseTypeArenaColdFallbackAMD64(t *testing.T) {
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

func TestControlBaseTypeArenaRejectsOutOfOrderReleaseAMD64(t *testing.T) {
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

func TestFrameEndSitesInlinePairAMD64(t *testing.T) {
	var f fn
	fr := ctrlFrame{kind: cfIf, controlSite: 99}
	f.frameAddEnd(&fr, 4)
	f.frameAddEnd(&fr, 12)
	f.frameAddEnd(&fr, 20)
	first, second, overflow := f.frameEndSites(&fr)
	if first != 5 {
		t.Fatalf("first packed end site = %d, want 5", first)
	}
	if second != 13 {
		t.Fatalf("second packed end site = %d, want 13", second)
	}
	if len(overflow) != 1 || overflow[0] != 21 {
		t.Fatalf("overflow packed end sites = %v, want [21]", overflow)
	}
	if fr.controlSite != 99 {
		t.Fatalf("if false-edge site = %d, want 99", fr.controlSite)
	}
}

func TestBoundedPinCandidateOrderingAMD64(t *testing.T) {
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

func TestIntrusiveReturnPatchChainAMD64(t *testing.T) {
	a := &x86.Asm{}
	f := fn{a: a, sc: &scratch{asm: a}}
	sites := make([]int, 3)
	for i := range sites {
		sites[i] = a.JmpPlaceholder()
		f.appendReturnSite(sites[i])
	}
	target := a.Len()
	f.patchReturnSites()
	for _, site := range sites {
		displacement := int(int32(binary.LittleEndian.Uint32(a.B[site : site+4])))
		if got := site + 4 + displacement; got != target {
			t.Fatalf("return at %d targets %d, want %d", site, got, target)
		}
	}
}

func TestPushCtrlReusesMergeSlotAtDepth(t *testing.T) {
	f := fn{ctrl: make([]ctrlFrame, 0, 1)}
	first := ctrlFrame{}
	f.ensureCtrlMerge(&first).branchState = make([]locState, 1)
	f.ensureCtrlRoots(&first).baseGCRoots = []bool{true}
	f.pushCtrl(&first)
	f.releaseCtrlMerge(&f.ctrl[0])
	f.ctrl = f.ctrl[:0]

	next := ctrlFrame{}
	f.ensureCtrlMerge(&next).branchState = make([]locState, 2)
	f.ensureCtrlRoots(&next).baseGCRoots = []bool{false, true}
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

func TestLocStatePoolRetentionIsBoundedAMD64(t *testing.T) {
	f := fn{pinnedLocals: []int{0}}
	for range maxRetainedLocStateBufs + 3 {
		f.freeLocStateBuf(make([]locState, 1))
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
	f.freeLocStateBuf(make([]locState, 1, maxRetainedLocStateBytes))
	f.freeLocStateBuf(make([]locState, 1))
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

	f.pinnedLocals = []int{0, 1}
	f.lsPool = [][]locState{make([]locState, 1)}
	f.lsPoolBytes = 1
	_ = f.newLocStateBuf()
	if f.lsPoolBytes != 0 {
		t.Fatalf("local-state bytes after undersized eviction = %d, want 0", f.lsPoolBytes)
	}
}

func TestEndSitePoolRetentionIsBoundedAMD64(t *testing.T) {
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

func TestReserveLocalScratchAMD64(t *testing.T) {
	sc := &scratch{}
	sc.reserveLocalScratch(7)
	if cap(sc.fnState.localType) != 7 || cap(sc.fnState.localSlot) != 7 || cap(sc.fnState.locals) != 7 {
		t.Fatalf("local scratch capacities = %d/%d/%d, want 7/7/7", cap(sc.fnState.localType), cap(sc.fnState.localSlot), cap(sc.fnState.locals))
	}
}
