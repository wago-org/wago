package railmach

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railspec"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type OperandForm uint8

const (
	FormInvalid OperandForm = iota
	FormRegister
	FormImmediate
	FormAddress
	FormFixedRegister
	FormFlags
)

type SelectCost struct {
	PeakGPR      uint8
	PeakVector   uint8
	FixedNeed    uint8
	Moves        uint8
	Latency      uint16
	ResourceCost uint16
	Bytes        uint16
}

type Selection struct {
	FormStart  uint32
	Rule       railspec.RuleID
	FormCount  uint16
	ResultForm OperandForm
	Cost       SelectCost
}

type CombinationKind uint8

const (
	CombineInvalid CombinationKind = iota
	CombineCompareBranch
	CombineImmediate
	CombineAddress
)

type Combination struct {
	Producer uint32
	Consumer uint32
	Kind     CombinationKind
	_        [3]byte
}

type AddressFold struct {
	Producer       uint32
	Consumer       uint32
	Base           VReg
	OriginalOffset uint32
	AddedOffset    uint32
}

type SelectionPlan struct {
	Selections   []Selection
	Forms        []OperandForm
	Combinations []Combination
	AddressFolds []AddressFold

	useCount       []uint32
	soleConsumer   []uint32
	verifyUseCount []uint32
}

func (p *SelectionPlan) OperandForms(id uint32) []OperandForm {
	selection := p.Selections[id]
	return p.Forms[selection.FormStart : selection.FormStart+uint32(selection.FormCount)]
}

// SelectOrder performs consumer-aware core instruction selection without
// creating a third IR. The generated RailSpec table supplies legality and cost;
// the plan retains operand/result forms and shallow producer combinations.
func SelectOrder(target Target, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, simplified *railssa.SimplifyResult, reuse *SelectionPlan) (*SelectionPlan, error) {
	model, err := railspec.GenericCostModel(targetRuleMask(target))
	if err != nil {
		return nil, err
	}
	return SelectOrderWithCostModel(target, flow, semantic, simplified, model, reuse)
}

// SelectOrderWithCostModel performs selection using one validated CPU policy.
// This keeps target costs explicit and replayable instead of reading host state
// from instruction selection.
func SelectOrderWithCostModel(target Target, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, simplified *railssa.SimplifyResult, model railspec.CostModel, reuse *SelectionPlan) (*SelectionPlan, error) {
	if flow == nil || semantic == nil || !simplified.HasIntegerFactDomain(len(flow.Values)) {
		return nil, fmt.Errorf("railmach: selection requires semantic facts")
	}
	targetMask := targetRuleMask(target)
	if targetMask == 0 {
		return nil, fmt.Errorf("railmach: selection target %s is unavailable", target)
	}
	if model.Target != targetMask {
		return nil, fmt.Errorf("railmach: cost model target %#x does not match %s", uint8(model.Target), target)
	}
	if err := model.Validate(); err != nil {
		return nil, err
	}
	if reuse == nil {
		reuse = new(SelectionPlan)
	}
	selections := resize(reuse.Selections, len(semantic.Insts))
	forms := reuse.Forms[:0]
	combinations := reuse.Combinations[:0]
	addressFolds := reuse.AddressFolds[:0]
	useCount := resize(reuse.useCount, len(flow.Values))
	soleConsumer := resize(reuse.soleConsumer, len(flow.Values))
	verifyUseCount := reuse.verifyUseCount
	*reuse = SelectionPlan{Selections: selections, Forms: forms, Combinations: combinations, AddressFolds: addressFolds, useCount: useCount, soleConsumer: soleConsumer, verifyUseCount: verifyUseCount}
	for instructionID := range semantic.Insts {
		for _, value := range semantic.Operands(uint32(instructionID)) {
			if int(value) < len(useCount) {
				useCount[value]++
				soleConsumer[value] = uint32(instructionID)
			}
		}
	}
	for id, instruction := range semantic.Insts {
		args := semantic.Operands(uint32(id))
		rhsKnown, rhs := false, uint64(0)
		if len(args) >= 2 {
			value := args[len(args)-1]
			for simplified.Aliases[value] != value {
				value = simplified.Aliases[value]
			}
			fact := simplified.IntegerFactAt(value)
			rhsKnown, rhs = fact.Known, fact.Min
		}
		feedsBranch := false
		branchConsumer := ^uint32(0)
		if instruction.Result != 0 && int(instruction.Result) < len(useCount) && useCount[instruction.Result] == 1 {
			consumer := soleConsumer[instruction.Result]
			next := semantic.Insts[consumer]
			nextArgs := semantic.Operands(consumer)
			feedsBranch = (next.Op == wasm.InstrIf || next.Op == wasm.InstrBrIf) && len(nextArgs) == 1 && nextArgs[0] == instruction.Result
			if feedsBranch {
				branchConsumer = consumer
			}
		}
		ruleID := railspec.SelectRule(targetMask, instruction.Op, rhsKnown, rhs, feedsBranch)
		if !railspec.VerifyRule(ruleID, targetMask) {
			return nil, fmt.Errorf("railmach: instruction %d selected illegal rule %d", id, ruleID)
		}
		cost, ok := model.PriorityCost(ruleID)
		if !ok {
			return nil, fmt.Errorf("railmach: instruction %d has no cost for rule %d", id, ruleID)
		}
		selection := &reuse.Selections[id]
		selection.FormStart = uint32(len(reuse.Forms))
		selection.FormCount = uint16(len(args))
		selection.Rule = ruleID
		selection.ResultForm = FormRegister
		selection.Cost = SelectCost{Latency: cost.Latency, ResourceCost: cost.Uops, Bytes: cost.NativeBytes}
		for range args {
			reuse.Forms = append(reuse.Forms, FormRegister)
		}
		switch ruleID {
		case railspec.RuleAMD64Imm32, railspec.RuleARM64Imm12:
			reuse.Forms[len(reuse.Forms)-1] = FormImmediate
			reuse.Combinations = append(reuse.Combinations, Combination{Producer: producerInstruction(flow, semantic, args[len(args)-1]), Consumer: uint32(id), Kind: CombineImmediate})
		case railspec.RuleAMD64ShiftCL:
			if len(args) >= 2 {
				reuse.Forms[len(reuse.Forms)-1] = FormFixedRegister
				selection.Cost.FixedNeed++
			}
		case railspec.RuleAMD64DivFixed:
			if len(args) >= 1 {
				reuse.Forms[selection.FormStart] = FormFixedRegister
				selection.Cost.FixedNeed++
			}
		case railspec.RuleFoldedMemoryAddress:
			if len(args) >= 1 {
				reuse.Forms[selection.FormStart] = FormAddress
				reuse.Combinations = append(reuse.Combinations, Combination{Producer: producerInstruction(flow, semantic, args[0]), Consumer: uint32(id), Kind: CombineAddress})
			}
		case railspec.RuleCompareBranchFlags:
			selection.ResultForm = FormFlags
			reuse.Combinations = append(reuse.Combinations, Combination{Producer: uint32(id), Consumer: branchConsumer, Kind: CombineCompareBranch})
		}
	}
	if err := verifySelectionReusingScratch(target, flow, semantic, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

// ApplyAddressFolding commits producer-linked i32 address additions only when
// SparseSimplify proves the addition cannot wrap. The memory immediate absorbs
// the constant, the consumer directly uses the base, and the now-dead add is
// retained by identity but emits zero bytes.
func ApplyAddressFolding(f *Func, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, simplified *railssa.SimplifyResult, plan *SelectionPlan) (uint32, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	if err := verifySelectionReusingScratch(f.Target, flow, semantic, plan); err != nil {
		return 0, err
	}
	// Selection verification has just populated this same vreg-sized scratch.
	// Its counts are no longer needed, so reuse the slab for post-selection
	// machine uses instead of allocating a second table.
	uses := resize(plan.verifyUseCount, len(f.VRegs))
	plan.verifyUseCount = uses
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	var committed uint32
	for _, combination := range plan.Combinations {
		if combination.Kind != CombineAddress || combination.Producer == ^uint32(0) || int(combination.Producer) >= len(f.Insts) || int(combination.Consumer) >= len(f.Insts) {
			continue
		}
		producer, consumer := &f.Insts[combination.Producer], &f.Insts[combination.Consumer]
		if producer.Op != wasm.InstrI32Add || producer.Result == 0 || uses[producer.Result] != 1 {
			continue
		}
		if _, _, _, memory := nativeMemorySelection(consumer.Op); !memory {
			continue
		}
		producerOperands, consumerOperands := f.InstructionOperands(combination.Producer), f.InstructionOperands(combination.Consumer)
		if len(producerOperands) != 2 || len(consumerOperands) == 0 || consumerOperands[0].Reg != producer.Result {
			continue
		}
		constantData := f.VRegs[producerOperands[1].Reg]
		if constantData.Flags&VRegRematerializable == 0 || int(constantData.Def/6) >= len(f.Insts) {
			continue
		}
		constant := f.Insts[constantData.Def/6]
		if constant.Op != wasm.InstrI32Const {
			continue
		}
		added := uint32(constant.Aux)
		base := producerOperands[0].Reg
		fact := simplified.IntegerFactAt(railssa.FlowValueID(base))
		original := uint32(consumer.Aux)
		if !fact.RangeKnown || fact.Max > uint64(math.MaxUint32-added) || uint64(original)+uint64(added) > math.MaxUint32 {
			continue
		}
		plan.AddressFolds = append(plan.AddressFolds, AddressFold{
			Producer: combination.Producer, Consumer: combination.Consumer, Base: base,
			OriginalOffset: original, AddedOffset: added,
		})
		consumerOperands[0].Reg = base
		consumerOperands[0].Bank = f.VRegs[base].Bank
		consumer.Aux = uint64(original + added)
		f.VRegs[producer.Result].Flags |= VRegElided
		committed++
	}
	if err := VerifyAddressFolds(f, flow, semantic, simplified, plan); err != nil {
		return 0, err
	}
	return committed, nil
}

func VerifyAddressFolds(f *Func, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, simplified *railssa.SimplifyResult, plan *SelectionPlan) error {
	for index, fold := range plan.AddressFolds {
		if int(fold.Producer) >= len(f.Insts) || int(fold.Consumer) >= len(f.Insts) || fold.Base == 0 || int(fold.Base) >= len(f.VRegs) {
			return fmt.Errorf("railmach: address fold %d has invalid identity", index)
		}
		producer, consumer := f.Insts[fold.Producer], f.Insts[fold.Consumer]
		originalProducer, originalConsumer := semantic.Insts[fold.Producer], semantic.Insts[fold.Consumer]
		fact := simplified.IntegerFactAt(railssa.FlowValueID(fold.Base))
		if producer.Op != wasm.InstrI32Add || originalProducer.Op != wasm.InstrI32Add || producer.Result == 0 ||
			originalConsumer.Aux != uint64(fold.OriginalOffset) || consumer.Aux != uint64(fold.OriginalOffset+fold.AddedOffset) ||
			!fact.RangeKnown || fact.Max > uint64(math.MaxUint32-fold.AddedOffset) {
			return fmt.Errorf("railmach: address fold %d failed independent replay", index)
		}
		operands := f.InstructionOperands(fold.Consumer)
		if len(operands) == 0 || operands[0].Reg != fold.Base || f.VRegs[producer.Result].Flags&VRegElided == 0 {
			return fmt.Errorf("railmach: address fold %d was not committed", index)
		}
	}
	_ = flow
	return nil
}

func nativeMemorySelection(kind wasm.InstrKind) (size int, signed, store, ok bool) {
	if kind < wasm.InstrI32Load || kind > wasm.InstrI64Store32 {
		return 0, false, false, false
	}
	return 1, false, kind >= wasm.InstrI32Store, true
}

func producerInstruction(flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, value railssa.FlowValueID) uint32 {
	definition := flow.Values[value]
	if definition.Kind != railssa.FlowValueInstruction {
		return ^uint32(0)
	}
	id := semantic.InstructionMap[definition.Instr]
	if id == 0 {
		return ^uint32(0)
	}
	return id - 1
}

func targetRuleMask(target Target) railspec.TargetMask {
	switch target {
	case TargetAMD64:
		return railspec.TargetAMD64
	case TargetARM64:
		return railspec.TargetARM64
	default:
		return 0
	}
}

func VerifySelection(target Target, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, plan *SelectionPlan) error {
	if flow == nil || semantic == nil {
		return fmt.Errorf("railmach: malformed selection flow")
	}
	return verifySelection(target, flow, semantic, plan, make([]uint32, len(flow.Values)))
}

func verifySelectionReusingScratch(target Target, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, plan *SelectionPlan) error {
	if plan == nil || flow == nil || semantic == nil {
		return fmt.Errorf("railmach: malformed selection plan")
	}
	useCount := resize(plan.verifyUseCount, len(flow.Values))
	plan.verifyUseCount = useCount
	return verifySelection(target, flow, semantic, plan, useCount)
}

func verifySelection(target Target, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, plan *SelectionPlan, useCount []uint32) error {
	if plan == nil || len(plan.Selections) != len(semantic.Insts) {
		return fmt.Errorf("railmach: malformed selection plan")
	}
	targetMask := targetRuleMask(target)
	expectedStart := uint32(0)
	for id, selection := range plan.Selections {
		if selection.FormStart != expectedStart || uint64(selection.FormStart)+uint64(selection.FormCount) > uint64(len(plan.Forms)) || !railspec.VerifyRule(selection.Rule, targetMask) || selection.ResultForm == FormInvalid {
			return fmt.Errorf("railmach: selection %d is invalid", id)
		}
		expectedStart += uint32(selection.FormCount)
		if int(selection.FormCount) != len(semantic.Operands(uint32(id))) {
			return fmt.Errorf("railmach: selection %d form arity mismatch", id)
		}
		for _, form := range plan.OperandForms(uint32(id)) {
			if form == FormInvalid {
				return fmt.Errorf("railmach: selection %d has invalid operand form", id)
			}
		}
	}
	if expectedStart != uint32(len(plan.Forms)) {
		return fmt.Errorf("railmach: selection forms are not dense")
	}
	for instructionID := range semantic.Insts {
		for _, value := range semantic.Operands(uint32(instructionID)) {
			if int(value) < len(useCount) {
				useCount[value]++
			}
		}
	}
	for id, combination := range plan.Combinations {
		if combination.Consumer >= uint32(len(semantic.Insts)) || combination.Kind == CombineInvalid || combination.Producer != ^uint32(0) && combination.Producer >= uint32(len(semantic.Insts)) {
			return fmt.Errorf("railmach: combination %d is invalid", id)
		}
		if combination.Kind == CombineCompareBranch {
			producer, consumer := semantic.Insts[combination.Producer], semantic.Insts[combination.Consumer]
			operands := semantic.Operands(combination.Consumer)
			if producer.Result == 0 || int(producer.Result) >= len(useCount) || useCount[producer.Result] != 1 || (consumer.Op != wasm.InstrIf && consumer.Op != wasm.InstrBrIf) || len(operands) != 1 || operands[0] != producer.Result {
				return fmt.Errorf("railmach: compare/branch combination %d failed sole-use replay", id)
			}
		}
	}
	return nil
}
