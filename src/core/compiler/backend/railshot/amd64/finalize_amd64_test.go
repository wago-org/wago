//go:build amd64

package amd64

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	amd64enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestIdentityFinalizerPreservesBytesAndMetadata(t *testing.T) {
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = false
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})

	code := []byte{0x48, 0x81, 0xec, 0, 0, 0, 0, 0xe8, 0, 0, 0, 0, 0xc3}
	original := append([]byte(nil), code...)
	plan := &shared.GCFrameRootPlan{
		AdapterReturnOffset: 12,
		Callsites:           []shared.GCFrameCallsitePlan{{ReturnOffset: 12}},
	}
	f := fn{
		a:                &amd64enc.Asm{B: code},
		relocs:           []callReloc{{at: 8}},
		adapterReturnOff: 12,
		gcFrameRoots:     plan,
	}

	internal, err := f.finalizeNativeCode(7)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.a.B, original) {
		t.Fatalf("identity finalizer changed bytes: %x != %x", f.a.B, original)
	}
	if internal != 7 || f.relocs[0].at != 8 || f.adapterReturnOff != 12 ||
		plan.AdapterReturnOffset != 12 || plan.Callsites[0].ReturnOffset != 12 {
		t.Fatalf("metadata changed: internal=%d reloc=%d adapter=%d gc-adapter=%d gc-call=%d",
			internal, f.relocs[0].at, f.adapterReturnOff,
			plan.AdapterReturnOffset, plan.Callsites[0].ReturnOffset)
	}
}

func TestSizeCompactsBoundedLoopFrameReservationsAMD64(t *testing.T) {
	oldEnabled, oldDisabled, oldLoops := nativeCompactionEnabled, nativeCompactionDisabled, loopCompactionEnabled
	nativeCompactionEnabled, nativeCompactionDisabled, loopCompactionEnabled = false, false, true
	t.Cleanup(func() {
		nativeCompactionEnabled, nativeCompactionDisabled, loopCompactionEnabled = oldEnabled, oldDisabled, oldLoops
	})

	m := modFuncs(t, funcDef{nil, nil, []byte{0x00, 0x03, 0x40, 0x0b, 0x0b}})
	objective := OptimizeSize
	stats := &ModuleStats{}
	compact, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 1, Stats: stats})
	if err != nil {
		t.Fatal(err)
	}
	if compact.CodeImage != nil {
		defer compact.CodeImage.Close()
	}
	if got := stats.NativeSize.DeadFrameReservationBytes; got != 0 {
		t.Fatalf("Size loop dead frame bytes = %d, want 0", got)
	}

	loopCompactionEnabled = false
	reservedStats := &ModuleStats{}
	reserved, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 1, Stats: reservedStats})
	if err != nil {
		t.Fatal(err)
	}
	if reserved.CodeImage != nil {
		defer reserved.CodeImage.Close()
	}
	if got := reservedStats.NativeSize.DeadFrameReservationBytes; got == 0 {
		t.Fatal("rollback loop retained no dead frame reservation; test cannot detect compaction")
	}
	if len(compact.Code) >= len(reserved.Code) {
		t.Fatalf("compacted loop code = %d bytes, rollback = %d", len(compact.Code), len(reserved.Code))
	}

	loopCompactionEnabled = true
	parallel, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if parallel.CodeImage != nil {
		defer parallel.CodeImage.Close()
	}
	if !bytes.Equal(compact.Code, parallel.Code) || !reflect.DeepEqual(compact.Entry, parallel.Entry) || !reflect.DeepEqual(compact.InternalEntry, parallel.InternalEntry) {
		t.Fatal("serial and parallel loop compaction differ")
	}

	f := fn{
		a:       &amd64enc.Asm{B: make([]byte, maxAMD64LoopCompactionBytes+1)},
		hasLoop: true,
		policy:  shared.CodegenPolicyForObjective(currentCodegenPolicy().Selection, OptimizeSize),
	}
	result, _, _, err := f.finalizeFrameAdjustments()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Code) != len(f.a.B) {
		t.Fatal("oversized loop function unexpectedly compacted")
	}
}

func TestIdentityFinalizerCompileParity(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x01, 0x0b})
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeCompactionEnabled = false

	nativeFinalizerEnabled = false
	without, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	defer without.CodeImage.Close()

	nativeFinalizerEnabled = true
	with, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	defer with.CodeImage.Close()

	if !bytes.Equal(without.Code, with.Code) {
		t.Fatalf("identity finalizer changed module bytes")
	}
	if !reflect.DeepEqual(without.Entry, with.Entry) ||
		!reflect.DeepEqual(without.InternalEntry, with.InternalEntry) {
		t.Fatalf("identity finalizer changed entries: entry %v/%v internal %v/%v",
			without.Entry, with.Entry, without.InternalEntry, with.InternalEntry)
	}
}

func TestFinalizerCompactsSmallFrameAdjustments(t *testing.T) {
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = true

	a := &amd64enc.Asm{}
	subSite := a.Len() + 3
	a.SubRsp(24)
	a.B = append(a.B, 0x90)
	addSite := a.Len() + 3
	a.AddRsp(24)
	sc := &scratch{}
	f := fn{a: a, sc: sc, subRspAt: subSite, addRspAt: addSite}

	if _, err := f.finalizeNativeCode(0); err != nil {
		t.Fatal(err)
	}
	if got, want := len(f.a.B), 9; got != want {
		t.Fatalf("compacted code length = %d, want %d", got, want)
	}
	want := []byte{0x48, 0x83, 0xec, 24, 0x90, 0x48, 0x83, 0xc4, 24}
	if !bytes.Equal(f.a.B, want) {
		t.Fatalf("compacted frame bytes = %x, want %x", f.a.B, want)
	}
}

func TestFinalizerCompactsBoundedSubsetOfBranchHoles(t *testing.T) {
	oldEnabled, oldCompact, oldDisabled := nativeFinalizerEnabled, nativeCompactionEnabled, nativeCompactionDisabled
	oldPartial := partialHoleCompactionEnabled
	nativeFinalizerEnabled, nativeCompactionEnabled, nativeCompactionDisabled, partialHoleCompactionEnabled = true, true, false, true
	t.Cleanup(func() {
		nativeFinalizerEnabled, nativeCompactionEnabled, nativeCompactionDisabled = oldEnabled, oldCompact, oldDisabled
		partialHoleCompactionEnabled = oldPartial
	})

	a := &amd64enc.Asm{}
	subSite := a.Len() + 3
	a.SubRsp(24)
	sc := &scratch{}
	for range shared.MaxOffsetMapDeletions + 4 {
		over := a.Len()
		a.B = append(a.B, 0x90, 0x90, 0x90, 0x90, 0x0f, 0x1f, 0x44, 0x00, 0x00)
		sc.brFoldSites = append(sc.brFoldSites, over)
	}
	addSite := a.Len() + 3
	a.AddRsp(24)
	f := fn{
		a:        a,
		sc:       sc,
		policy:   shared.CodegenPolicyForObjective(optimization.Selection{}, shared.OptimizeSize),
		subRspAt: subSite,
		addRspAt: addSite,
	}
	oldLen := len(a.B)
	if _, err := f.finalizeNativeCode(0); err != nil {
		t.Fatal(err)
	}
	const frameBytes = 6
	const admittedHoleBytes = (shared.MaxOffsetMapDeletions - 2) * 5
	if got, want := len(f.a.B), oldLen-frameBytes-admittedHoleBytes; got != want {
		t.Fatalf("partially compacted code = %d bytes, want %d", got, want)
	}

	partialHoleCompactionEnabled = false
	a2 := &amd64enc.Asm{}
	sub2 := a2.Len() + 3
	a2.SubRsp(24)
	sc2 := &scratch{}
	for range 10 {
		over := a2.Len()
		a2.B = append(a2.B, 0x90, 0x90, 0x90, 0x90, 0x0f, 0x1f, 0x44, 0x00, 0x00)
		sc2.brFoldSites = append(sc2.brFoldSites, over)
	}
	add2 := a2.Len() + 3
	a2.AddRsp(24)
	f2 := fn{a: a2, sc: sc2, subRspAt: sub2, addRspAt: add2}
	oldLen2 := len(a2.B)
	if _, err := f2.finalizeNativeCode(0); err != nil {
		t.Fatal(err)
	}
	if len(f2.a.B) != oldLen2 {
		t.Fatal("all-or-nothing rollback unexpectedly compacted over-budget holes")
	}
}

func TestFinalizerDeletesBranchFoldHole(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := mod1(t, i32, i32, []byte{
		0x00,       // no locals
		0x02, 0x40, // block void
		0x20, 0x00, 0x41, 0x05, 0x48, // local.get x; i32.const 5; i32.lt_s
		0x0d, 0x00, // br_if 0
		0x41, 0x0a, 0x0f, // return 10
		0x0b,
		0x41, 0x14, // return 20 through function end
		0x0b,
	})
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeFinalizerEnabled = true

	compile := func(compact bool) (*amd64enc.CompiledModule, *ModuleStats) {
		nativeCompactionEnabled = compact
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		return cm, stats
	}
	without, withoutStats := compile(false)
	defer without.CodeImage.Close()
	with, withStats := compile(true)
	defer with.CodeImage.Close()

	if got := withoutStats.Funcs[0].NativeSize.BranchFoldHoleBytes; got != 5 {
		t.Fatalf("uncompacted branch hole = %d, want 5", got)
	}
	if got := withStats.Funcs[0].NativeSize.BranchFoldHoleBytes; got != 0 {
		t.Fatalf("compacted branch hole = %d, want 0", got)
	}
	if got, wantMax := len(with.Code), len(without.Code)-5; got > wantMax {
		t.Fatalf("compacted length = %d, want <= %d", got, wantMax)
	}
}

func TestFinalizerRelaxesShortBranches(t *testing.T) {
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = true

	finalize := func(t *testing.T, emit func(*amd64enc.Asm)) []byte {
		t.Helper()
		a := &amd64enc.Asm{Rel32SiteLimit: maxAMD64FinalizerRel32Sites}
		subSite := a.Len() + 3
		a.SubRsp(0)
		emit(a)
		addSite := a.Len() + 3
		a.AddRsp(0)
		f := fn{a: a, sc: &scratch{}, subRspAt: subSite, addRspAt: addSite, frameElided: true}
		if _, err := f.finalizeNativeCode(0); err != nil {
			t.Fatal(err)
		}
		return f.a.B
	}

	t.Run("rel8 boundaries", func(t *testing.T) {
		for _, test := range []struct {
			name        string
			conditional bool
			distance    int
			short       bool
		}{
			{"jmp +127", false, 127, true},
			{"jmp +128", false, 128, false},
			{"jcc +127", true, 127, true},
			{"jcc +128", true, 128, false},
		} {
			t.Run(test.name, func(t *testing.T) {
				code := finalize(t, func(a *amd64enc.Asm) {
					var site int
					if test.conditional {
						site = a.JccPlaceholder(amd64enc.CondE)
					} else {
						site = a.JmpPlaceholder()
					}
					a.B = append(a.B, make([]byte, test.distance)...)
					a.PatchRel32(site, a.Len())
				})
				if test.short {
					wantOpcode := byte(0xeb)
					if test.conditional {
						wantOpcode = 0x74
					}
					if code[0] != wantOpcode || code[1] != 127 {
						t.Fatalf("short branch = %x %d, want %x 127", code[0], code[1], wantOpcode)
					}
				} else if test.conditional && !bytes.Equal(code[:2], []byte{0x0f, 0x84}) {
					t.Fatalf("long jcc prefix = %x, want 0f84", code[:2])
				} else if !test.conditional && code[0] != 0xe9 {
					t.Fatalf("long jmp opcode = %x, want e9", code[0])
				}
			})
		}
	})

	t.Run("cascading", func(t *testing.T) {
		code := finalize(t, func(a *amd64enc.Asm) {
			outer := a.JmpPlaceholder()
			inner := a.JmpPlaceholder()
			a.B = append(a.B, make([]byte, 125)...)
			target := a.Len()
			a.PatchRel32(outer, target)
			a.PatchRel32(inner, target)
		})
		if len(code) != 2+2+125 || !bytes.Equal(code[:4], []byte{0xeb, 127, 0xeb, 125}) {
			t.Fatalf("cascading branches = %x len=%d", code[:4], len(code))
		}
	})

	t.Run("branch to next", func(t *testing.T) {
		for _, conditional := range []bool{false, true} {
			code := finalize(t, func(a *amd64enc.Asm) {
				var site int
				if conditional {
					site = a.JccPlaceholder(amd64enc.CondE)
				} else {
					site = a.JmpPlaceholder()
				}
				a.PatchRel32(site, a.Len())
				a.B = append(a.B, 0x90)
			})
			if !bytes.Equal(code, []byte{0x90}) {
				t.Fatalf("conditional=%v: compacted bytes = %x, want 90", conditional, code)
			}
		}
	})
}
