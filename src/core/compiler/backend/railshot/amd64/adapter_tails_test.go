//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestShareAdapterTailsRemapsModuleOffsetsAMD64(t *testing.T) {
	template := []byte{
		0xe8, 0, 0, 0, 0, // CALL internal
		0x59, 0x48, 0x89, 0x01, 0x48, 0x89, 0x51, 0x08, 0xc3, // tail
		0x90, 0x90, 0x90, 0x90, 0x90, // internal body
	}
	code := append(append(append([]byte(nil), template...), template...), template...)
	entry := []int{0, 19, 38}
	internal := []int{14, 33, 52}
	relocs := [][]callReloc{{{at: 15}}, {{at: 15}}, {{at: 15}}}
	infos := []adapterTailInfo{
		{function: 0, returnOff: 5, endOff: 14},
		{function: 1, returnOff: 5, endOff: 14},
		{function: 2, returnOff: 5, endOff: 14},
	}
	roots := testGCModuleRootPlansAMD64(t,
		testGCPlanWithCallsites(t, 0, [2]uint32{15, 0}),
		testGCPlanWithCallsites(t, 0, [2]uint32{15, 0}),
		testGCPlanWithCallsites(t, 0, [2]uint32{15, 0}),
	)
	literalWords := []uint64{0, uint64(15) << 32, 0, uint64(15) << 32, 0, uint64(15) << 32}
	literalOffsets := []uint32{0, 2, 4, 6}

	got, islandBytes, err := shareAdapterTailsAMD64(code, entry, internal, relocs, literalWords, literalOffsets, infos, roots, nil)
	if err != nil {
		t.Fatal(err)
	}
	if islandBytes != 9 || len(got) != 54 {
		t.Fatalf("shared result = %d bytes with %d-byte island, want 54 and 9", len(got), islandBytes)
	}
	if want := []int{0, 15, 30}; !equalIntsAMD64(entry, want) {
		t.Fatalf("entries = %v, want %v", entry, want)
	}
	if want := []int{10, 25, 40}; !equalIntsAMD64(internal, want) {
		t.Fatalf("internal entries = %v, want %v", internal, want)
	}
	for i := range relocs {
		if relocs[i][0].at != 11 || testGCCallsiteReturn(t, roots.Function(i), 0) != 11 {
			t.Fatalf("function %d offsets: reloc=%d callsite=%d, want 11,11", i, relocs[i][0].at, testGCCallsiteReturn(t, roots.Function(i), 0))
		}
		if site := uint32(literalWords[2*i+1] >> 32); site != 11 {
			t.Fatalf("function %d literal site = %d, want 11", i, site)
		}
		if callDisp := int32(binary.LittleEndian.Uint32(got[entry[i]+1:])); callDisp != 5 {
			t.Fatalf("function %d adapter call displacement = %d, want 5", i, callDisp)
		}
	}
	for i, want := range []int32{35, 20, 5} {
		if gotDisp := int32(binary.LittleEndian.Uint32(got[entry[i]+6:])); gotDisp != want {
			t.Fatalf("function %d shared-tail displacement = %d, want %d", i, gotDisp, want)
		}
	}
	if !bytes.Equal(got[len(got)-islandBytes:], template[5:14]) {
		t.Fatal("shared tail differs from exact template")
	}
}

func testGCModuleRootPlansAMD64(t testing.TB, plans ...*shared.GCFrameRootPlan) *shared.GCModuleFrameRootPlan {
	t.Helper()
	module := shared.NewGCModuleFrameRootPlan(len(plans))
	count := 0
	for function, plan := range plans {
		if plan != nil {
			if !module.MarkFunction(function) {
				t.Fatalf("mark function root plan %d", function)
			}
			count++
		}
	}
	if !module.ReserveFunctions(count) {
		t.Fatalf("reserve %d function root plans", count)
	}
	for function, plan := range plans {
		if plan != nil {
			dst, ok := module.BeginFunction(function)
			if !ok {
				t.Fatalf("begin function root plan %d", function)
			}
			*dst = *plan
		}
	}
	return module
}

func equalIntsAMD64(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAdapterTailInfoRejectsEmbeddedReturnPCAMD64(t *testing.T) {
	f := fn{adapterReturnOff: 5, adapterEndOff: 20, adapterReturnReferenced: true}
	if info := f.adapterTailInfo(); info.returnOff != 0 || info.endOff != 0 {
		t.Fatalf("referenced adapter return admitted for sharing: %#v", info)
	}
}

func TestAdapterTailIslandRangeAMD64(t *testing.T) {
	if !adapterTailIslandInRangeAMD64((1<<31)-17, 16) {
		t.Fatal("exact conservative island limit rejected")
	}
	if adapterTailIslandInRangeAMD64((1<<31)-16, 16) {
		t.Fatal("out-of-range island admitted")
	}
}
