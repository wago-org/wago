package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestAMD64RailMachV128LocationsPreserveAllBytes(t *testing.T) {
	plan := &nativeBackendPlan{Machine: &railmach.Func{VRegs: []railmach.VRegData{{}, {Type: railmach.TypeV128, Bank: railmach.BankFPR}}}}
	var a amd64.Asm
	if err := amd64RailMachWriteLocation(&a, plan, 1, railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 1}, 0); err != nil {
		t.Fatal(err)
	}
	moveBytes := a.Len()
	if err := amd64RailMachWriteLocation(&a, plan, 1, railmach.Location{Kind: railmach.LocationSpill, Bank: railmach.BankFPR, Index: 2}, 1); err != nil {
		t.Fatal(err)
	}
	storeBytes := a.Len()
	if _, err := amd64RailMachReadLocation(&a, plan, 1, railmach.Location{Kind: railmach.LocationSpill, Bank: railmach.BankFPR, Index: 2}, 2, 0); err != nil {
		t.Fatal(err)
	}
	if moveBytes == 0 || storeBytes == moveBytes || a.Len() == storeBytes {
		t.Fatalf("vector move/store/load byte boundaries = %d/%d/%d", moveBytes, storeBytes, a.Len())
	}
}
