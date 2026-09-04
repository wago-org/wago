//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestCompactSharedAdaptersRemapsCallsLiteralsAndGCReturnsAMD64(t *testing.T) {
	before := stackDeltaAdapterThunkEnabled
	stackDeltaAdapterThunkEnabled = true
	t.Cleanup(func() { stackDeltaAdapterThunkEnabled = before })
	build := func(target int) []byte {
		a := &encoder.Asm{}
		for i := 0; i < 8; i++ {
			a.MovReg64(RAX, RCX)
		}
		adapterCall := a.CallRel32()
		a.Ret()
		internalCall := a.CallRel32()
		a.PatchRel32(adapterCall, target)
		a.PatchRel32(internalCall, 0)
		return a.B
	}
	first := build(30)
	second := build(0)
	code := append(append([]byte(nil), first...), second...)
	entry := []int{0, len(first)}
	internal := []int{30, len(first) + 30}
	relocs := [][]callReloc{{{at: 31}}, {{at: 31}}}
	infos := []sharedAdapterInfo{
		{function: 0, dispOff: 25, endOff: 30},
		{function: 1, dispOff: 25, endOff: 30},
	}
	literalWords := []uint64{
		1, 1, 2, 16, uint64(31) << 32,
		1, 1, 2, 16, uint64(31) << 32,
	}
	literalOffsets := []uint32{0, 5, 10}
	roots := &shared.GCModuleFrameRootPlan{Functions: []*shared.GCFrameRootPlan{
		testGCPlanWithCallsites(t, 29, [2]uint32{30, 0}),
		testGCPlanWithCallsites(t, 29, [2]uint32{30, 0}),
	}}
	stats := &ModuleStats{Funcs: []*CodegenStats{
		{CodeBytes: len(first), NativeSize: NativeFunctionSizeReport{TotalBytes: len(first), HostAdapterBytes: 30, InternalFunctionBytes: 5}},
		{CodeBytes: len(second), NativeSize: NativeFunctionSizeReport{TotalBytes: len(second), HostAdapterBytes: 30, InternalFunctionBytes: 5}},
	}}

	got, sharedBytes, err := shareAdaptersAMD64(code, entry, internal, relocs, literalWords, literalOffsets, infos, roots, stats)
	if err != nil {
		t.Fatal(err)
	}
	if sharedBytes != 27 || len(got) != 61 {
		t.Fatalf("shared adapter bytes/code = %d/%d, want 27/61", sharedBytes, len(got))
	}
	if entry[0] != 0 || entry[1] != 17 || internal[0] != 12 || internal[1] != 29 {
		t.Fatalf("entry/internal remap = %v/%v, want [0 17]/[12 29]", entry, internal)
	}
	if relocs[0][0].at != 13 || relocs[1][0].at != 13 || uint32(literalWords[4]>>32) != 13 || uint32(literalWords[9]>>32) != 13 {
		t.Fatalf("call/literal remap = relocs %v/%v literals %d/%d", relocs[0], relocs[1], literalWords[4]>>32, literalWords[9]>>32)
	}
	if testGCCallsiteReturn(t, roots.Functions[0], 0) != 12 || testGCCallsiteReturn(t, roots.Functions[1], 0) != 12 {
		t.Fatalf("GC callsite remap = %v/%v", roots.Functions[0].CallsiteData, roots.Functions[1].CallsiteData)
	}
	if roots.Functions[0].AdapterReturnOffset != 60 || roots.Functions[1].AdapterReturnOffset != 43 {
		t.Fatalf("shared adapter return offsets = %d/%d, want 60/43", roots.Functions[0].AdapterReturnOffset, roots.Functions[1].AdapterReturnOffset)
	}
	if got[58] != 0xff || got[59] != 0xd5 {
		t.Fatalf("shared call bytes = % x, want ff d5", got[58:60])
	}
	for i := range stats.Funcs {
		if stats.Funcs[i].NativeSize.HostAdapterBytes != legacySharedAdapterThunkBytesAMD64 || stats.Funcs[i].CodeBytes != 17 {
			t.Fatalf("function %d stats = %#v", i, stats.Funcs[i])
		}
	}
}
