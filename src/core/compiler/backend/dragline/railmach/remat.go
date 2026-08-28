package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

type RematDecision struct {
	Value      VReg
	Base       VReg
	Aux        uint64
	RecipeCost uint16
	SpillCost  uint16
	Profitable bool
	_          [3]byte
}

type RematPlan struct {
	Decisions []RematDecision
}

// PriceAffineRematerialization is the bounded target-cost experiment from the
// pressure plan. It records decisions but does not alter allocation until the
// finalizers can realize every accepted recipe.
func PriceAffineRematerialization(f *Func, selection *SelectionPlan, pressure *railssa.PressurePlan, reuse *RematPlan) (*RematPlan, error) {
	if f == nil || selection == nil || pressure == nil || len(selection.Selections) != len(f.Insts) {
		return nil, fmt.Errorf("railmach: affine rematerialization requires machine, selection, and pressure plans")
	}
	if reuse == nil {
		reuse = new(RematPlan)
	}
	decisions := reuse.Decisions[:0]
	*reuse = RematPlan{Decisions: decisions}
	for _, recipe := range pressure.Remats {
		if recipe.Kind != railssa.RematAffine {
			continue
		}
		if recipe.Value == 0 || recipe.Base == 0 || int(recipe.Value) >= len(f.VRegs) || int(recipe.Base) >= len(f.VRegs) {
			return nil, fmt.Errorf("railmach: invalid affine recipe %#v", recipe)
		}
		definition := f.VRegs[recipe.Value].Def / 6
		if int(definition) >= len(selection.Selections) {
			return nil, fmt.Errorf("railmach: affine recipe definition exceeds selection")
		}
		cost := selection.Selections[definition].Cost
		recipeCost := cost.Bytes + cost.Latency*4
		spillCost := uint16(20) // one target load: four bytes plus modeled latency four
		reuse.Decisions = append(reuse.Decisions, RematDecision{
			Value: VReg(recipe.Value), Base: VReg(recipe.Base), Aux: recipe.Aux,
			RecipeCost: recipeCost, SpillCost: spillCost, Profitable: recipeCost < spillCost,
		})
	}
	if err := VerifyRematPlan(f, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func VerifyRematPlan(f *Func, plan *RematPlan) error {
	if f == nil || plan == nil {
		return fmt.Errorf("railmach: malformed rematerialization plan")
	}
	for _, decision := range plan.Decisions {
		if decision.Value == 0 || decision.Base == 0 || int(decision.Value) >= len(f.VRegs) || int(decision.Base) >= len(f.VRegs) || decision.RecipeCost == 0 || decision.SpillCost == 0 || decision.Profitable != (decision.RecipeCost < decision.SpillCost) {
			return fmt.Errorf("railmach: invalid rematerialization decision %#v", decision)
		}
	}
	return nil
}
