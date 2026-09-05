//go:build arm64

package arm64

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestFinalizerRejectsFragmentOffsetOverflowArm64(t *testing.T) {
	before := nativeFinalizerEnabled
	nativeFinalizerEnabled = true
	t.Cleanup(func() { nativeFinalizerEnabled = before })
	f := fn{sc: &scratch{fragmentOverflow: true}}
	if _, err := f.finalizeNativeCode(0); err == nil {
		t.Fatal("finalizer accepted an overflowing compact fragment offset")
	}
}

func TestIdentityFinalizerRemapsAllArm64Metadata(t *testing.T) {
	before := nativeFinalizerEnabled
	beforeValidate := nativeFinalizerValidate
	beforeCompact := nativeCompactionEnabled
	nativeFinalizerEnabled = true
	nativeFinalizerValidate = true
	nativeCompactionEnabled = false
	t.Cleanup(func() {
		nativeFinalizerEnabled = before
		nativeFinalizerValidate = beforeValidate
		nativeCompactionEnabled = beforeCompact
	})

	plan := testGCPlanWithCallsites(t, 12, [2]uint32{16, 0}, [2]uint32{24, 64})
	sc := &scratch{asm: &a64.Asm{B: make([]byte, 32)}, finalizerMarkers: map[int]bool{finalizerMarkerKey(28, markerDeadHole): true}}
	f := fn{
		a:                sc.asm,
		sc:               sc,
		relocs:           []callReloc{{at: 4}, {at: 20}},
		adapterReturnOff: 12,
		gcFrameRoots:     plan,
		subRspAt:         0,
		addRspAt:         20,
	}

	internal, err := f.finalizeNativeCode(8)
	if err != nil {
		t.Fatal(err)
	}
	if internal != 8 || f.adapterReturnOff != 12 || plan.AdapterReturnOffset != 12 {
		t.Fatalf("entry/adapter offsets changed: internal=%d adapter=%d plan=%d", internal, f.adapterReturnOff, plan.AdapterReturnOffset)
	}
	if f.relocs[0].at != 4 || f.relocs[1].at != 20 || testGCCallsiteReturn(t, plan, 0) != 16 || testGCCallsiteReturn(t, plan, 1) != 24 {
		t.Fatalf("relocation/callsite offsets changed: relocs=%#v callsite-data=%#v", f.relocs, plan.CallsiteData)
	}
}

func TestCompileIdentityFinalizerByteParityArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)

	before := nativeFinalizerEnabled
	beforeValidate := nativeFinalizerValidate
	beforeCompact := nativeCompactionEnabled
	nativeFinalizerValidate = true
	nativeCompactionEnabled = false
	t.Cleanup(func() {
		nativeFinalizerEnabled = before
		nativeFinalizerValidate = beforeValidate
		nativeCompactionEnabled = beforeCompact
	})
	compile := func(enabled bool) ([]byte, []int, []int, []uint64) {
		nativeFinalizerEnabled = enabled
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 2})
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), cm.Code...), slices.Clone(cm.Entry), slices.Clone(cm.InternalEntry), slices.Clone(cm.DirectPrepared)
	}

	withoutCode, withoutEntry, withoutInternal, withoutPrepared := compile(false)
	withCode, withEntry, withInternal, withPrepared := compile(true)
	if !bytes.Equal(withCode, withoutCode) || !slices.Equal(withEntry, withoutEntry) || !slices.Equal(withInternal, withoutInternal) || !slices.Equal(withPrepared, withoutPrepared) {
		t.Fatalf("identity finalizer changed module output:\ncode equal=%v\nentry %v / %v\ninternal %v / %v\nprepared %v / %v",
			bytes.Equal(withCode, withoutCode), withEntry, withoutEntry, withInternal, withoutInternal, withPrepared, withoutPrepared)
	}
}

func TestRemapPCRelativeWordArm64(t *testing.T) {
	offsets, err := shared.NewOffsetMap(32, []shared.DeletedRange{{Off: 8, Len: 4}})
	if err != nil {
		t.Fatal(err)
	}

	encodeADR := func(delta int) uint32 {
		imm := uint32(delta) & 0x1FFFFF
		return 0x10000000 | (imm&3)<<29 | ((imm>>2)&0x7FFFF)<<5
	}
	tests := []struct {
		name       string
		oldPC      int
		word       uint32
		wantTarget int
		adr        bool
	}{
		{name: "B forward", oldPC: 0, word: 0x14000000 | 5, wantTarget: 16},
		{name: "BL forward", oldPC: 0, word: 0x94000000 | 5, wantTarget: 16},
		{name: "B.cond backward", oldPC: 16, word: 0x54000000 | (uint32(0x7FFFC) << 5), wantTarget: 0},
		{name: "CBZ backward", oldPC: 16, word: 0x34000000 | (uint32(0x7FFFC) << 5), wantTarget: 0},
		{name: "TBZ backward", oldPC: 16, word: 0x36000000 | (uint32(0x3FFC) << 5), wantTarget: 0},
		{name: "ADR forward", oldPC: 0, word: encodeADR(20), wantTarget: 16, adr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newPC, ok := offsets.Map(test.oldPC)
			if !ok {
				t.Fatalf("old PC %d was deleted", test.oldPC)
			}
			got, err := remapPCRelativeWord(test.word, test.oldPC, newPC, &offsets)
			if err != nil {
				t.Fatal(err)
			}
			var target int
			if test.adr {
				target, ok = adrTarget(newPC, got)
			} else {
				target, ok = branchTarget(newPC, got)
			}
			if !ok || target != test.wantTarget {
				t.Fatalf("remapped target = %d, %v; want %d", target, ok, test.wantTarget)
			}
		})
	}
}

func TestFinalizePeepholesRecordsBranchToNextArm64(t *testing.T) {
	beforeFinalizer, beforeCompaction := nativeFinalizerEnabled, nativeCompactionEnabled
	nativeFinalizerEnabled, nativeCompactionEnabled = true, true
	t.Cleanup(func() { nativeFinalizerEnabled, nativeCompactionEnabled = beforeFinalizer, beforeCompaction })

	code := make([]byte, 24)
	wrWord(code, 0, 0x14000001)  // B +4: removable.
	wrWord(code, 4, 0x94000001)  // BL +4: writes LR, retain.
	wrWord(code, 8, 0x54000020)  // B.cond +4: removable.
	wrWord(code, 12, 0x34000020) // CBZ +4: removable.
	wrWord(code, 16, 0x36000020) // TBZ +4: removable.
	wrWord(code, 20, nopWord)

	sc := &scratch{asm: &a64.Asm{B: code}}
	f := fn{a: sc.asm, sc: sc}
	f.finalizePeepholes()
	for _, pc := range []int{0, 8, 12, 16} {
		if !slices.Contains(sc.branchNextSites[:sc.branchNextN], pc) {
			t.Errorf("branch at %d was not recorded", pc)
		}
	}
	if slices.Contains(sc.branchNextSites[:sc.branchNextN], 4) {
		t.Error("BL-to-next was incorrectly recorded")
	}
}

func TestFinalizePeepholesSkipOpaqueFragmentsArm64(t *testing.T) {
	beforeFinalizer, beforeCompaction := nativeFinalizerEnabled, nativeCompactionEnabled
	nativeFinalizerEnabled, nativeCompactionEnabled = true, true
	t.Cleanup(func() { nativeFinalizerEnabled, nativeCompactionEnabled = beforeFinalizer, beforeCompaction })

	a := &a64.Asm{}
	a.Store64(a64.X0, a64.SP, 0)
	a.Load64(a64.X0, a64.SP, 0)
	want := bytes.Clone(a.B)
	sc := &scratch{asm: a, finalFragments: []finalizerFragment{{start: 0, end: uint32(len(a.B)), kind: fragmentPlugin}}}
	f := fn{a: a, sc: sc, opaqueFragments: true}
	f.finalizePeepholes()
	if !bytes.Equal(a.B, want) {
		t.Fatalf("opaque fragment rewritten: got %x, want %x", a.B, want)
	}
}

func TestBranchTargetBitsArm64(t *testing.T) {
	targets := make([]uint64, 65)
	const n = 65 * 64 * 4
	for _, off := range []int{0, 252, 256, n - 4} {
		branchTargetAdd(targets, off, n)
		if !branchTargeted(targets, off) {
			t.Fatalf("target %d was not retained", off)
		}
	}
	for _, off := range []int{-4, 2, n, n + 4} {
		branchTargetAdd(targets, off, n)
		if branchTargeted(targets, off) {
			t.Fatalf("invalid target %d was retained", off)
		}
	}
}

func TestFinalizePeepholesDoesNotRetainGiantBranchTargetBitsArm64(t *testing.T) {
	beforeFinalizer, beforeCompaction := nativeFinalizerEnabled, nativeCompactionEnabled
	nativeFinalizerEnabled, nativeCompactionEnabled = true, true
	t.Cleanup(func() { nativeFinalizerEnabled, nativeCompactionEnabled = beforeFinalizer, beforeCompaction })

	giant := make([]byte, len((scratch{}).branchTargetInline)*64*4+4)
	wrWord(giant, 0, 0x14000001)
	sc := &scratch{asm: &a64.Asm{B: giant}}
	f := fn{a: sc.asm, sc: sc}
	f.finalizePeepholes()
	if len(sc.branchTargets) <= len(sc.branchTargetInline) {
		t.Fatalf("giant target words = %d, want overflow", len(sc.branchTargets))
	}

	sc.asm.B = make([]byte, 8)
	f.finalizePeepholes()
	if len(sc.branchTargets) != 1 || &sc.branchTargets[0] != &sc.branchTargetInline[0] {
		t.Fatal("small successor retained giant branch-target backing")
	}
}

func TestFinalizerCandidateInventoryIsBoundedArm64(t *testing.T) {
	beforeFinalizer, beforeCompaction := nativeFinalizerEnabled, nativeCompactionEnabled
	beforeDisabled := nativeCompactionDisabled
	nativeFinalizerEnabled, nativeCompactionEnabled, nativeCompactionDisabled = true, true, false
	t.Cleanup(func() {
		nativeFinalizerEnabled, nativeCompactionEnabled = beforeFinalizer, beforeCompaction
		nativeCompactionDisabled = beforeDisabled
	})

	sc := &scratch{}
	f := fn{sc: sc}
	for off := 4 * (maxFinalizerDeletions + 3); off >= 0; off -= 4 {
		f.recordBranchNext(off)
	}
	got := append([]int(nil), sc.branchNextSites[:sc.branchNextN]...)
	slices.Sort(got)
	want := make([]int, maxFinalizerDeletions)
	for i := range want {
		want[i] = i * 4
	}
	if !slices.Equal(got, want) {
		t.Fatalf("bounded branch candidates = %v, want earliest %v", got, want)
	}
	for off := 0; off <= 4*maxFinalizerDeletions; off += 4 {
		f.recordDeadHole(off)
	}
	if sc.deadHoleN != maxFinalizerDeletions || !sc.deadHoleOverflow {
		t.Fatalf("dead-hole inventory = %d overflow=%v, want %d and overflow", sc.deadHoleN, sc.deadHoleOverflow, maxFinalizerDeletions)
	}
}

func TestSizeCompactsLoopFrameReservationsArm64(t *testing.T) {
	beforeEnabled, beforeDisabled := nativeCompactionEnabled, nativeCompactionDisabled
	beforeLoops := loopCompactionEnabled
	nativeCompactionEnabled, nativeCompactionDisabled, loopCompactionEnabled = false, false, true
	t.Cleanup(func() {
		nativeCompactionEnabled, nativeCompactionDisabled = beforeEnabled, beforeDisabled
		loopCompactionEnabled = beforeLoops
	})

	m := modFuncs(t, funcDef{nil, nil, []byte{0x00, 0x03, 0x40, 0x0b, 0x0b}})
	stats := &ModuleStats{}
	compact, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Workers: 1, Stats: stats})
	if err != nil {
		t.Fatal(err)
	}
	if got := stats.NativeSize.DeadFrameReservationBytes; got != 0 {
		t.Fatalf("Size loop dead frame bytes = %d, want 0", got)
	}

	loopCompactionEnabled = false
	reservedStats := &ModuleStats{}
	reserved, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Workers: 1, Stats: reservedStats})
	if err != nil {
		t.Fatal(err)
	}
	if got := reservedStats.NativeSize.DeadFrameReservationBytes; got == 0 {
		t.Fatal("rollback loop retained no dead frame reservation; test cannot detect compaction")
	}
	if len(compact.Code) >= len(reserved.Code) {
		t.Fatalf("compacted loop code = %d bytes, rollback = %d", len(compact.Code), len(reserved.Code))
	}
	loopCompactionEnabled = true
	parallel, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Code, parallel.Code) || !slices.Equal(compact.Entry, parallel.Entry) || !slices.Equal(compact.InternalEntry, parallel.InternalEntry) {
		t.Fatal("serial and parallel loop compaction differ")
	}

	execModule := modFuncs(t, funcDef{nil, []wasm.ValType{wasm.I64}, []byte{
		0x00,       // locals
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x0c, 0x01, // br 1: leave block
		0x0b,       // end loop
		0x0b,       // end block
		0x42, 0x2a, // i64.const 42
		0x0b,
	}})
	if got, err := runArm64WrapperWithOptions(t, execModule, CompileOptions{CompactNative: true}); err != nil || got != 42 {
		t.Fatalf("compacted loop execution = %d, %v; want 42", got, err)
	}
}

func TestLoopCompactionHasFixedFunctionSizeBoundArm64(t *testing.T) {
	beforeLimit := arm64LoopCompactionLimit
	arm64LoopCompactionLimit = 16 << 10
	t.Cleanup(func() { arm64LoopCompactionLimit = beforeLimit })
	stats := &CodegenStats{}
	f := fn{
		a:       &a64.Asm{B: make([]byte, arm64LoopCompactionLimit+4)},
		sc:      &scratch{},
		hasLoop: true,
		policy:  shared.CompactCodegenPolicy(currentCodegenPolicy().Selection),
		stats:   stats,
	}
	var storage [maxFinalizerDeletions]shared.DeletedRange
	if _, _, ok := f.buildCompactionPlan(storage[:0]); ok {
		t.Fatal("oversized loop function unexpectedly admitted to compaction")
	}
	if got, want := stats.FinalizerFallback, "loop-function-size"; got != want {
		t.Fatalf("finalizer fallback = %q, want %q", got, want)
	}
}

func TestLoopCompactionLimitRespectsArchitectureAndPolicyBoundsArm64(t *testing.T) {
	beforeLimit := arm64LoopCompactionLimit
	t.Cleanup(func() { arm64LoopCompactionLimit = beforeLimit })
	policy := shared.CompactCodegenPolicy(currentCodegenPolicy().Selection)
	f := fn{
		a:       &a64.Asm{B: make([]byte, 20<<10)},
		sc:      &scratch{},
		hasLoop: true,
		policy:  policy,
		stats:   &CodegenStats{},
	}
	var storage [maxFinalizerDeletions]shared.DeletedRange
	arm64LoopCompactionLimit = 16 << 10
	if _, _, ok := f.buildCompactionPlan(storage[:0]); ok {
		t.Fatal("16 KiB architecture bound admitted 20 KiB function")
	}
	arm64LoopCompactionLimit = 32 << 10
	f.stats.FinalizerFallback = ""
	if _, _, ok := f.buildCompactionPlan(storage[:0]); !ok {
		t.Fatalf("32 KiB architecture bound rejected 20 KiB function: %s", f.stats.FinalizerFallback)
	}
	f.policy.MaxLoopCompactionBytes = 16 << 10
	if _, _, ok := f.buildCompactionPlan(storage[:0]); ok {
		t.Fatal("16 KiB immutable policy bound admitted 20 KiB function")
	}
}

func TestCompactNativeCodeRemapsBranchesAndJumpTableArm64(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		code := make([]byte, 24)
		wrWord(code, 0, 0x14000000|5) // B from 0 to 20.
		for pc := 4; pc < len(code); pc += 4 {
			wrWord(code, pc, nopWord)
		}
		deletions := []shared.DeletedRange{{Off: 8, Len: 4}}
		offsets, err := shared.NewOffsetMap(len(code), deletions)
		if err != nil {
			t.Fatal(err)
		}
		f := fn{a: &a64.Asm{B: code}, sc: &scratch{hasBranchTargets: true}}
		got, err := f.compactNativeCode(&offsets, deletions)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 20 {
			t.Fatalf("compacted length = %d, want 20", len(got))
		}
		if target, ok := branchTarget(0, rdWord(got, 0)); !ok || target != 16 {
			t.Fatalf("branch target = %d, %v; want 16", target, ok)
		}
	})

	t.Run("jump table", func(t *testing.T) {
		code := make([]byte, 28)
		wrWord(code, 0, 0x10000000|(uint32(2)<<5)) // ADR from 0 to table at 8.
		wrWord(code, 4, nopWord)
		wrWord(code, 8, uint32(12))  // Table base 8 to target 20.
		wrWord(code, 12, uint32(16)) // Table base 8 to target 24.
		for pc := 16; pc < len(code); pc += 4 {
			wrWord(code, pc, nopWord)
		}
		deletions := []shared.DeletedRange{{Off: 16, Len: 4}}
		offsets, err := shared.NewOffsetMap(len(code), deletions)
		if err != nil {
			t.Fatal(err)
		}
		markers := map[int]bool{
			finalizerMarkerKey(8, markerJumpDataStart): true,
			finalizerMarkerKey(16, markerJumpDataEnd):  true,
		}
		f := fn{a: &a64.Asm{B: code}, sc: &scratch{
			finalizerMarkers: markers,
			finalFragments:   []finalizerFragment{{start: 8, end: 16, kind: fragmentJumpData}},
		}}
		got, err := f.compactNativeCode(&offsets, deletions)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 24 {
			t.Fatalf("compacted length = %d, want 24", len(got))
		}
		if target, ok := adrTarget(0, rdWord(got, 0)); !ok || target != 8 {
			t.Fatalf("ADR target = %d, %v; want 8", target, ok)
		}
		if got := int(int32(rdWord(got, 8))); got != 8 {
			t.Fatalf("first table delta = %d, want 8", got)
		}
		if got := int(int32(rdWord(got, 12))); got != 12 {
			t.Fatalf("second table delta = %d, want 12", got)
		}
	})
}
