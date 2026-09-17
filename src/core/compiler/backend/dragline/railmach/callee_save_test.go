package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

func TestCalleeSaveUseChainAcceptsBoundedColdLinearRegion(t *testing.T) {
	f := &Func{
		Blocks: make([]Block, 5),
		Edges: []Edge{
			{From: 0, To: 1},
			{From: 1, To: 2},
			{From: 2, To: 3},
			{From: 3, To: 4},
		},
	}
	for index := range f.Blocks {
		f.Blocks[index].Region = railssa.NoRegion
	}
	cold := []bool{false, true, true, true, false}
	use := calleeSaveUse{present: true, eligible: true, count: 2}
	use.blocks[0], use.last[0] = 1, 7
	use.blocks[1], use.last[1] = 3, 19
	entry, exit, last, ok := calleeSaveUseChain(f, use, cold, nil)
	if !ok || entry != 1 || exit != 3 || last != 19 {
		t.Fatalf("cold chain = %d..%d last %d, %v", entry, exit, last, ok)
	}
	if !verifyCalleeSaveChain(f, entry, exit, cold, nil) {
		t.Fatal("accepted cold chain failed independent verification")
	}

	// A side entry could bypass the save at block 1.
	f.Edges = append(f.Edges, Edge{From: 0, To: 2})
	if _, _, _, ok := calleeSaveUseChain(f, use, cold, nil); ok {
		t.Fatal("cold chain with a side entry was accepted")
	}
	f.Edges = f.Edges[:len(f.Edges)-1]

	// A side exit could leave without executing the restore in block 3.
	f.Edges = append(f.Edges, Edge{From: 2, To: 4})
	if _, _, _, ok := calleeSaveUseChain(f, use, cold, nil); ok {
		t.Fatal("cold chain with a side exit was accepted")
	}

	// Loop headers are never eligible even when profile-cold.
	f.Edges = f.Edges[:len(f.Edges)-1]
	f.Blocks[2].Flags = railssa.BlockLoopHeader
	if _, _, _, ok := calleeSaveUseChain(f, use, cold, nil); ok {
		t.Fatal("cold chain through a loop header was accepted")
	}
}
