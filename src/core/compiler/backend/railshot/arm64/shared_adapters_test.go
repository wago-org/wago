//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestCompactSharedAdaptersRemapsCallsAndGCReturnsArm64(t *testing.T) {
	build := func(target int) []byte {
		a := &a64.Asm{}
		for i := 0; i < 4; i++ {
			a.MovReg64(X4, X5)
		}
		call := a.Bl()
		a.Ret()
		a.Ret() // internal body
		if !a.PatchBranch26(call, target) {
			t.Fatal("synthetic adapter call is out of range")
		}
		return a.B
	}
	first := build(24)
	second := build(0)
	code := append(append([]byte(nil), first...), second...)
	entry := []int{0, len(first)}
	internal := []int{24, len(first) + 24}
	relocs := [][]callReloc{{{at: 24}}, {{at: 24}}}
	infos := []sharedAdapterInfo{
		{function: 0, callOff: 16, endOff: 24},
		{function: 1, callOff: 16, endOff: 24},
	}
	roots := &shared.GCModuleFrameRootPlan{Functions: []*shared.GCFrameRootPlan{
		testGCPlanWithCallsites(t, 20, [2]uint32{24, 0}),
		testGCPlanWithCallsites(t, 20, [2]uint32{24, 0}),
	}}
	stats := &ModuleStats{Funcs: []*CodegenStats{
		{CodeBytes: len(first), NativeSize: NativeFunctionSizeReport{TotalBytes: len(first), HostAdapterBytes: 24, InternalFunctionBytes: 4}},
		{CodeBytes: len(second), NativeSize: NativeFunctionSizeReport{TotalBytes: len(second), HostAdapterBytes: 24, InternalFunctionBytes: 4}},
	}}

	got, sharedBytes, err := shareAdapters(code, entry, internal, relocs, infos, roots, stats)
	if err != nil {
		t.Fatal(err)
	}
	if sharedBytes != 24 || len(got) != 48 {
		t.Fatalf("shared adapter bytes/code = %d/%d, want 24/48", sharedBytes, len(got))
	}
	if entry[0] != 0 || entry[1] != 12 || internal[0] != 8 || internal[1] != 20 {
		t.Fatalf("entry/internal remap = %v/%v, want [0 12]/[8 20]", entry, internal)
	}
	if relocs[0][0].at != 8 || relocs[1][0].at != 8 || testGCCallsiteReturn(t, roots.Functions[0], 0) != 8 || testGCCallsiteReturn(t, roots.Functions[1], 0) != 8 {
		t.Fatalf("internal metadata remap = relocs %v/%v callsites %v/%v", relocs[0], relocs[1], roots.Functions[0].CallsiteData, roots.Functions[1].CallsiteData)
	}
	if roots.Functions[0].AdapterReturnOffset != 44 || roots.Functions[1].AdapterReturnOffset != 32 {
		t.Fatalf("shared adapter return offsets = %d/%d, want 44/32", roots.Functions[0].AdapterReturnOffset, roots.Functions[1].AdapterReturnOffset)
	}
	if word := binary.LittleEndian.Uint32(got[40:]); word != 0xd63f0220 {
		t.Fatalf("shared call word = %#x, want BLR X17", word)
	}
	for i := range stats.Funcs {
		if stats.Funcs[i].NativeSize.HostAdapterBytes != sharedAdapterThunkBytes || stats.Funcs[i].CodeBytes != 12 {
			t.Fatalf("function %d stats = %#v", i, stats.Funcs[i])
		}
	}
}
