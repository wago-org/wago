package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestARM64RailMachV128LocationsPreserveAllBytes(t *testing.T) {
	plan := &nativeBackendPlan{Machine: &railmach.Func{VRegs: []railmach.VRegData{{}, {Type: railmach.TypeV128, Bank: railmach.BankFPR}}}}
	var a arm64.Asm
	if err := arm64RailMachWriteLocation(&a, plan, 1, railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if err := arm64RailMachWriteLocation(&a, plan, 1, railmach.Location{Kind: railmach.LocationSpill, Bank: railmach.BankFPR, Index: 2}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := arm64RailMachReadLocation(&a, plan, 1, railmach.Location{Kind: railmach.LocationSpill, Bank: railmach.BankFPR, Index: 2}, 2, 0); err != nil {
		t.Fatal(err)
	}
	if got := a.Len(); got != 12 {
		t.Fatalf("vector move/store/load bytes = %d, want 12", got)
	}
}
