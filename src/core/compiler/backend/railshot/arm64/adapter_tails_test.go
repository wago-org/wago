//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestShareAdapterTailsRemapsModuleOffsetsArm64(t *testing.T) {
	const (
		bl  = uint32(0x94000000)
		adr = uint32(0x10000000)
		ret = uint32(0xd65f03c0)
	)
	words := []uint32{
		bl, 0xa9400fe2, 0xf9000060, ret, adr, ret,
		bl, 0xa9400fe2, 0xf9000060, ret, adr, ret,
	}
	code := make([]byte, 4*len(words))
	for i, word := range words {
		binary.LittleEndian.PutUint32(code[4*i:], word)
	}
	entry := []int{0, 24}
	internal := []int{16, 40}
	relocs := [][]callReloc{{{at: 20}}, {{at: 20}}}
	infos := []adapterTailInfo{
		{function: 0, returnOff: 4, endOff: 16},
		{function: 1, returnOff: 4, endOff: 16},
	}
	roots := testGCModuleRootPlansARM64(t,
		testGCPlanWithCallsites(t, 0, [2]uint32{20, 0}),
		testGCPlanWithCallsites(t, 0, [2]uint32{20, 0}),
	)

	got, islandBytes, err := shareAdapterTails(code, entry, internal, relocs, infos, roots, nil)
	if err != nil {
		t.Fatal(err)
	}
	if islandBytes != 12 || len(got) != 44 {
		t.Fatalf("shared result = %d bytes with %d-byte island, want 44 and 12", len(got), islandBytes)
	}
	if want := []int{0, 16}; entry[0] != want[0] || entry[1] != want[1] {
		t.Fatalf("entries = %v, want %v", entry, want)
	}
	if want := []int{8, 24}; internal[0] != want[0] || internal[1] != want[1] {
		t.Fatalf("internal entries = %v, want %v", internal, want)
	}
	for i := range relocs {
		if relocs[i][0].at != 12 || testGCCallsiteReturn(t, roots.Function(i), 0) != 12 {
			t.Fatalf("function %d offsets: reloc=%d callsite=%d, want 12,12", i, relocs[i][0].at, testGCCallsiteReturn(t, roots.Function(i), 0))
		}
	}
	if word := binary.LittleEndian.Uint32(got[0:]); word != 0x94000002 {
		t.Fatalf("first adapter BL = %#x, want %#x", word, uint32(0x94000002))
	}
	if word := binary.LittleEndian.Uint32(got[4:]); word != 0x14000007 {
		t.Fatalf("first shared-tail B = %#x, want %#x", word, uint32(0x14000007))
	}
	if word := binary.LittleEndian.Uint32(got[16:]); word != 0x94000002 {
		t.Fatalf("second adapter BL = %#x, want %#x", word, uint32(0x94000002))
	}
	if word := binary.LittleEndian.Uint32(got[20:]); word != 0x14000003 {
		t.Fatalf("second shared-tail B = %#x, want %#x", word, uint32(0x14000003))
	}
	for i, want := range []uint32{0xa9400fe2, 0xf9000060, ret} {
		if gotWord := binary.LittleEndian.Uint32(got[32+4*i:]); gotWord != want {
			t.Fatalf("shared tail word %d = %#x, want %#x", i, gotWord, want)
		}
	}
}

func testGCModuleRootPlansARM64(t testing.TB, plans ...*shared.GCFrameRootPlan) *shared.GCModuleFrameRootPlan {
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

func TestAdapterTailInfoRejectsEmbeddedReturnPCArm64(t *testing.T) {
	f := fn{adapterReturnOff: 8, adapterEndOff: 20, adapterReturnReferenced: true}
	if info := f.adapterTailInfo(); info.returnOff != 0 || info.endOff != 0 {
		t.Fatalf("referenced adapter return admitted for sharing: %#v", info)
	}
}

func TestAdapterTailSharingRejectsPCRelativeCodeArm64(t *testing.T) {
	const (
		bl  = uint32(0x94000000)
		ret = uint32(0xd65f03c0)
	)
	words := []uint32{bl, bl, ret, bl, bl, ret, bl, bl, ret}
	code := make([]byte, 4*len(words))
	for i, word := range words {
		binary.LittleEndian.PutUint32(code[4*i:], word)
	}
	entry := []int{0, 12, 24}
	infos := []adapterTailInfo{{function: 0, returnOff: 4, endOff: 12}, {function: 1, returnOff: 4, endOff: 12}, {function: 2, returnOff: 4, endOff: 12}}
	groups, admitted, sharedBytes := planSharedAdapterTails(code, entry, infos)
	if groups != nil || admitted != nil || sharedBytes != 0 {
		t.Fatalf("PC-relative tails admitted: groups=%v functions=%v bytes=%d", groups, admitted, sharedBytes)
	}
}

func TestAdapterTailPositionIndependentRejectsLiteralLoadArm64(t *testing.T) {
	code := make([]byte, 4)
	binary.LittleEndian.PutUint32(code, 0x58000000) // LDR X0, literal
	if adapterTailPositionIndependent(code) {
		t.Fatal("literal load admitted into movable adapter tail")
	}
}

func TestAdapterTailIslandRangeArm64(t *testing.T) {
	if !adapterTailIslandInRange((1<<27)-16, 16) {
		t.Fatal("exact conservative island limit rejected")
	}
	if adapterTailIslandInRange((1<<27)-15, 16) {
		t.Fatal("out-of-range island admitted")
	}
}
