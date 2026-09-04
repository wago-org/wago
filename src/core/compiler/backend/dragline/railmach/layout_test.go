package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

func TestBlockLayoutJoinsHotChainsAndSeparatesColdBlocks(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		VRegs:  []VRegData{{}},
		Blocks: []Block{{Weight: 10}, {Weight: 8}, {Weight: 0}, {Weight: 7}},
		Edges: []Edge{
			{From: 0, To: 1, Kind: railssa.EdgeTrue},
			{From: 0, To: 2, Kind: railssa.EdgeFalse},
			{From: 1, To: 3, Kind: railssa.EdgeFallthrough},
			{From: 2, To: 3, Kind: railssa.EdgeBranch},
		},
	}
	layout, err := BuildBlockLayout(f, []uint64{100, 1, 90, 1}, []uint32{4, 8, 12, 16}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []railssa.BlockID{0, 1, 3, 2}
	if len(layout.Order) != len(want) {
		t.Fatalf("order = %v", layout.Order)
	}
	for index := range want {
		if layout.Order[index] != want[index] {
			t.Fatalf("order = %v, want %v", layout.Order, want)
		}
	}
	if layout.Score != 190 || layout.ColdStart != 3 || layout.HotBytes != 28 {
		t.Fatalf("layout = %#v", layout)
	}
}

func TestBlockLayoutRejectsMismatchedInputsAndCorruption(t *testing.T) {
	f := &Func{Target: TargetAMD64, VRegs: []VRegData{{}}, Blocks: []Block{{Weight: 1}}}
	if _, err := BuildBlockLayout(f, []uint64{1}, []uint32{4}, nil); err == nil {
		t.Fatal("mismatched edge weights accepted")
	}
	if err := VerifyBlockLayout(f, &BlockLayout{Order: []railssa.BlockID{0}, Position: []uint32{1}, ColdStart: 1}); err == nil {
		t.Fatal("corrupt inverse layout accepted")
	}
}
