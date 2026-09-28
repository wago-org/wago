//go:build amd64

package amd64

import (
	"encoding/binary"
	"strconv"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestCtrlFrameSize(t *testing.T) {
	if got, want := unsafe.Sizeof(ctrlFrame{}), uintptr(80); got != want {
		t.Fatalf("ctrlFrame size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameMerge{}), uintptr(104); got != want {
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

func TestControlGCRootSidecarAMD64(t *testing.T) {
	var f fn
	fr := ctrlFrame{height: 1}
	fr.set(ctrlHasBaseGCRoots, true)
	f.ensureCtrlRoots(&fr).flags = []bool{true}
	if got := f.frameBaseGCRoots(&fr); len(got) != 1 || !got[0] {
		t.Fatalf("root-only sidecar = %v, want [true]", got)
	}
}

func TestControlGCRootSegmentsShareBackingAMD64(t *testing.T) {
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

func TestCaptureControlGCRootSegmentsAMD64(t *testing.T) {
	f := fn{s: newStack()}
	base := f.s.pushValue(storage{})
	f.setStackGCRoot(base, true)
	param := f.s.pushValue(storage{})
	f.setStackGCRoot(param, true)
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

func TestCaptureControlGCRootShapeSkipsScalarStackAMD64(t *testing.T) {
	f := fn{s: newStackWithCap(128)}
	for range 128 {
		f.pushValue(storage{kind: stConst, typ: mtI64})
	}
	var fr ctrlFrame
	f.captureGCFrameShape(&fr)
	if f.tmpRoots != nil {
		t.Fatalf("scalar-only capture walked the operand stack: roots=%d", len(f.tmpRoots))
	}
	if fr.has(ctrlHasBaseGCRoots) || fr.has(ctrlHasParamGCRoots) {
		t.Fatal("scalar-only capture recorded GC roots")
	}
}

func TestOpEndReusesCanonicalFallthroughStackAMD64(t *testing.T) {
	const height = 128
	a := &x86.Asm{}
	f := fn{
		a:    a,
		s:    newStackWithCap(height + 8),
		ctrl: []ctrlFrame{{kind: cfBlock, height: height, controlSite: -1}},
	}
	types := make([]machineType, height)
	for i := range types {
		types[i] = mtI64
	}
	f.setDepthTypesWithGCRoots(types, nil)
	last := f.s.back()
	codeLen := a.Len()
	if !f.s.canonicalSlots {
		t.Fatal("setDepthTypes did not mark the slot image canonical")
	}
	if err := f.opEnd(); err != nil {
		t.Fatal(err)
	}
	if f.s.back() != last {
		t.Fatal("opEnd rebuilt an unchanged canonical fallthrough stack")
	}
	if !f.s.canonicalSlots || f.depth() != height {
		t.Fatalf("post-end stack canonical=%v depth=%d, want canonical depth %d", f.s.canonicalSlots, f.depth(), height)
	}
	if got := a.Len(); got != codeLen {
		t.Fatalf("opEnd emitted %d bytes for an empty fallthrough block, want 0", got-codeLen)
	}
}

func BenchmarkCanonicalControlBoundaryAMD64(b *testing.B) {
	for _, height := range []int{64, 256, 1024, 4096} {
		b.Run("Fast/H"+strconv.Itoa(height), func(b *testing.B) {
			f := fn{s: newStackWithCap(height + 8)}
			types := make([]machineType, height)
			for i := range types {
				types[i] = mtI64
			}
			f.setDepthTypesWithGCRoots(types, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				f.flush()
			}
		})
		b.Run("ScanOnly/H"+strconv.Itoa(height), func(b *testing.B) {
			f := fn{s: newStackWithCap(height + 8)}
			types := make([]machineType, height)
			for i := range types {
				types[i] = mtI64
			}
			f.setDepthTypesWithGCRoots(types, nil)
			roots := f.rootsBottomToTop()
			if !canonicalSlotLayout(roots) {
				b.Fatal("benchmark stack is not canonical")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				roots = f.rootsBottomToTop()
				if !canonicalSlotLayout(roots) {
					b.Fatal("benchmark stack lost canonical layout")
				}
			}
		})
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
	f := fn{s: newStack()}
	f.pushValue(storage{kind: stConst, typ: mtI32})
	f.pushValue(storage{kind: stConst, typ: mtV128})
	outer := ctrlFrame{}
	f.setFrameBaseTypePrefix(&outer, f.depth())
	f.pushValue(storage{kind: stConst, typ: mtF64})
	inner := ctrlFrame{}
	f.setFrameBaseTypePrefix(&inner, f.depth())
	f.releaseFrameBaseTypes(&ctrlFrame{}) // unreachable frames never acquire arena storage

	if got := f.frameBaseTypes(&outer); len(got) != 2 || got[0] != mtI32 || got[1] != mtV128 {
		t.Fatalf("outer base types = %v, want [i32 v128]", got)
	}
	if got := f.frameBaseTypes(&inner); len(got) != 3 || got[0] != mtI32 || got[1] != mtV128 || got[2] != mtF64 {
		t.Fatalf("inner base types = %v, want [i32 v128 f64]", got)
	}

	f.releaseFrameBaseTypes(&inner)
	f.releaseFrameBaseTypes(&outer)
	if f.controlBaseTypeN != 0 {
		t.Fatalf("released arena length = %d, want 0", f.controlBaseTypeN)
	}
	reused := ctrlFrame{}
	f.setDepthTypesWithGCRoots(nil, nil)
	f.pushValue(storage{kind: stConst, typ: mtI64})
	f.pushValue(storage{kind: stConst, typ: mtF32})
	f.setFrameBaseTypePrefix(&reused, f.depth())
	if got := f.frameBaseTypes(&reused); len(got) != 2 || got[0] != mtI64 || got[1] != mtF32 {
		t.Fatalf("reused base types = %v, want [i64 f32]", got)
	}
}

func TestControlBaseTypeArenaColdFallbackAMD64(t *testing.T) {
	f := fn{s: newStack(), controlBaseTypeN: uint8(maxScratchFunctionResults)}
	f.pushValue(storage{kind: stConst, typ: mtI32})
	f.pushValue(storage{kind: stConst, typ: mtV128})
	f.pushValue(storage{kind: stConst, typ: mtF64})
	fr := ctrlFrame{height: 3, resultN: 1, res0: mtI64}
	f.setFrameBaseTypePrefix(&fr, 3)
	if !fr.has(ctrlColdBaseTypes) {
		t.Fatal("overflow frame did not use a cold prefix handle")
	}
	if got := f.ctrlMerge(&fr).baseTypeTop; got != f.s.back() {
		t.Fatal("cold prefix did not retain the existing operand root")
	}
	// A flush replaces the live operand nodes. The captured control prefix must
	// still describe the original immutable base.
	f.setDepthTypesWithGCRoots([]machineType{mtI32}, nil)
	if got := f.frameBaseTypes(&fr); len(got) != 3 || got[0] != mtI32 || got[1] != mtV128 || got[2] != mtF64 {
		t.Fatalf("cold base types = %v, want [i32 v128 f64]", got)
	}
	if got := fr.appendResultTypes(nil); len(got) != 1 || got[0] != mtI64 {
		t.Fatalf("cold result types = %v, want [i64]", got)
	}
	f.releaseFrameBaseTypes(&fr)
	if f.controlBaseTypeN != uint8(maxScratchFunctionResults) {
		t.Fatalf("cold release changed arena length to %d", f.controlBaseTypeN)
	}
	f.releaseCtrlMerge(&fr)
}

func TestLogicalOperandDepthCounterAMD64(t *testing.T) {
	f := fn{s: newStack()}
	left := f.pushValue(storage{kind: stConst, typ: mtI64})
	right := f.pushValue(storage{kind: stConst, typ: mtI32})
	if got := f.depth(); got != 2 {
		t.Fatalf("value depth = %d, want 2", got)
	}
	f.erase(left) // optimizer removes a lower operand while the right stays live
	if got := f.depth(); got != 1 {
		t.Fatalf("depth after lower-operand removal = %d, want 1", got)
	}
	f.erase(right)
	if got := f.depth(); got != 0 {
		t.Fatalf("depth after removing both values = %d, want 0", got)
	}

	left = f.pushValue(storage{kind: stConst, typ: mtI64})
	right = f.pushValue(storage{kind: stConst, typ: mtI32})
	node := f.s.alloc()
	node.setElemKind(ekDeferred)
	node.setValueType(mtI64)
	node.arg0, node.arg1 = left, right
	f.s.pushDeferred(node)
	if got := f.depth(); got != 1 {
		t.Fatalf("binary expression depth = %d, want 1", got)
	}
	if got := len(f.rootsBottomToTop()); got != 1 {
		t.Fatalf("binary expression roots = %d, want 1", got)
	}
}

func BenchmarkControlBasePrefixCaptureAMD64(b *testing.B) {
	for _, height := range []int{256, 1024, 4096} {
		for _, frames := range []int{1, 8, 32} {
			name := "H" + strconv.Itoa(height) + "/D" + strconv.Itoa(frames)
			b.Run("Handle/"+name, func(b *testing.B) {
				f := fn{s: newStackWithCap(height + 8), controlBaseTypeN: uint8(maxScratchFunctionResults)}
				for i := 0; i < height; i++ {
					f.pushValue(storage{kind: stConst, typ: machineType(1 + i%5)})
				}
				var fr ctrlFrame
				f.setFrameBaseTypePrefix(&fr, height) // reserve the cold-prefix sidecar before timing
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for j := 0; j < frames; j++ {
						f.setFrameBaseTypePrefix(&fr, height)
					}
				}
				b.ReportMetric(float64(frames), "frames/op")
			})
			b.Run("Expanded/"+name, func(b *testing.B) {
				f := fn{s: newStackWithCap(height + 8)}
				for i := 0; i < height; i++ {
					f.pushValue(storage{kind: stConst, typ: machineType(1 + i%5)})
				}
				cache := make(map[uint64][][]machineType)
				legacyCapture := func() {
					types := f.currentLogicalTypes()[:height]
					hash := uint64(len(types)) ^ 1469598103934665603
					for _, typ := range types {
						hash ^= uint64(typ)
						hash *= 1099511628211
					}
					for _, prior := range cache[hash] {
						if len(prior) != len(types) {
							continue
						}
						equal := true
						for i := range types {
							if prior[i] != types[i] {
								equal = false
								break
							}
						}
						if equal {
							return
						}
					}
					cache[hash] = append(cache[hash], append([]machineType(nil), types...))
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for j := 0; j < frames; j++ {
						legacyCapture()
					}
				}
				b.ReportMetric(float64(frames), "frames/op")
			})
		}
	}
}

func TestControlBaseTypeArenaRejectsOutOfOrderReleaseAMD64(t *testing.T) {
	f := fn{s: newStack()}
	f.pushValue(storage{kind: stConst, typ: mtI32})
	outer := ctrlFrame{}
	inner := ctrlFrame{}
	f.setFrameBaseTypePrefix(&outer, 1)
	f.pushValue(storage{kind: stConst, typ: mtI64})
	f.setFrameBaseTypePrefix(&inner, 2)
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
	first := ctrlFrame{height: 1}
	f.ensureCtrlMerge(&first).branchState = make([]locState, 1)
	first.set(ctrlHasBaseGCRoots, true)
	f.ensureCtrlRoots(&first).flags = []bool{true}
	f.pushCtrl(&first)
	f.releaseCtrlMerge(&f.ctrl[0])
	f.ctrl = f.ctrl[:0]

	next := ctrlFrame{height: 2}
	f.ensureCtrlMerge(&next).branchState = make([]locState, 2)
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
	roots := []*elem{testValueElem(storage{}), testDeferredElem(opNone, mtNone, nil, nil), testValueElem(storage{})}
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
