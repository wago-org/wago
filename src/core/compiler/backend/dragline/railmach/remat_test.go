package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

func TestPriceAffineRematerializationUsesSelectedTargetCost(t *testing.T) {
	f := &Func{Target: TargetARM64, VRegs: make([]VRegData, 3), Insts: []Inst{{Result: 2}}}
	f.VRegs[1] = VRegData{Bank: BankGPR, Type: TypeI64}
	f.VRegs[2] = VRegData{Bank: BankGPR, Type: TypeI64, Def: 3}
	selection := &SelectionPlan{Selections: []Selection{{Cost: SelectCost{Bytes: 4, Latency: 1}}}}
	pressure := &railssa.PressurePlan{Remats: []railssa.RematRecipe{{Value: 2, Base: 1, Aux: 7, Kind: railssa.RematAffine}}}
	plan, err := PriceAffineRematerialization(f, selection, pressure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Decisions) != 1 || !plan.Decisions[0].Profitable || plan.Decisions[0].RecipeCost != 8 {
		t.Fatalf("decisions = %#v", plan.Decisions)
	}
}
