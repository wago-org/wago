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

	plan := &shared.GCFrameRootPlan{
		AdapterReturnOffset: 12,
		Callsites: []shared.GCFrameCallsitePlan{
			{ReturnOffset: 16},
			{ReturnOffset: 24, StackAdjust: 64},
		},
	}
	sc := &scratch{asm: &a64.Asm{B: make([]byte, 32)}, branchTargets: map[int]bool{finalizerMarkerKey(28, markerDeadHole): true}}
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
	if f.relocs[0].at != 4 || f.relocs[1].at != 20 || plan.Callsites[0].ReturnOffset != 16 || plan.Callsites[1].ReturnOffset != 24 {
		t.Fatalf("relocation/callsite offsets changed: relocs=%#v callsites=%#v", f.relocs, plan.Callsites)
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
		if !sc.branchTargets[finalizerMarkerKey(pc, markerBranchNext)] {
			t.Errorf("branch at %d was not recorded", pc)
		}
	}
	if sc.branchTargets[finalizerMarkerKey(4, markerBranchNext)] {
		t.Error("BL-to-next was incorrectly recorded")
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
		f := fn{a: &a64.Asm{B: code}, sc: &scratch{branchTargets: map[int]bool{20: true}}}
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
		f := fn{a: &a64.Asm{B: code}, sc: &scratch{branchTargets: markers}}
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
