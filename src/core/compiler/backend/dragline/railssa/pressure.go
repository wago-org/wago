package railssa

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type BlockPressure struct {
	PeakGPR uint16
	PeakFPR uint16
	_       uint32
}

type SinkMove struct {
	Instruction uint32
	Before      uint32
	Block       BlockID
}

type RematKind uint8

const (
	RematInvalid RematKind = iota
	RematConstant
	RematExtend
	RematAffine
)

type RematRecipe struct {
	Value FlowValueID
	Base  FlowValueID
	Aux   uint64
	Kind  RematKind
	_     [7]byte
}

type Induction struct {
	Value FlowValueID
	Base  FlowValueID
	Step  int64
	Block BlockID
}

type LICMMove struct {
	Instruction uint32
	Preheader   BlockID
	Loop        BlockID
}

type ColdUse struct {
	Value       FlowValueID
	Instruction uint32
	HotWeight   uint32
	ColdWeight  uint32
}

type PressurePlan struct {
	Blocks      []BlockPressure
	Sinks       []SinkMove
	Remats      []RematRecipe
	Inductions  []Induction
	LICM        []LICMMove
	ColdUses    []ColdUse
	ReducedArgs uint32

	definition           []uint32
	lastUse              []uint32
	useCount             []uint8
	directUseCount       []uint8
	directUseBlock       []BlockID
	directUseInstruction []uint32
	valueBlock           []BlockID
	gprDelta             []int32
	fprDelta             []int32
	positionBlock        []BlockID
	rematerializable     []bool
	maxUseWeight         []uint32
}

// Pressure shaping only distinguishes unused, single-use, and multi-use
// values. Saturating at two keeps that exact decision domain while bounding
// the counter to one byte even for adversarially large use lists.
func saturatingUseCount(count uint8) uint8 {
	if count < 2 {
		return count + 1
	}
	return count
}

// PressureShape derives bounded, source-stable pressure transformations. The
// plan is separate from the immutable semantic graph until selection can price
// and commit it with target costs.
func PressureShape(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, reuse *PressurePlan) (*PressurePlan, error) {
	if err := VerifySimplify(f, cfg, flow, semantic, metadata, simplified); err != nil {
		return nil, err
	}
	if reuse == nil {
		reuse = new(PressurePlan)
	}
	blocks := resizeClear(reuse.Blocks, len(cfg.Blocks))
	sinks := reuse.Sinks[:0]
	remats := reuse.Remats[:0]
	rematCount := 0
	for semanticID, instruction := range semantic.Insts {
		if _, ok := pressureRematRecipe(f, semantic, simplified, uint32(semanticID), instruction); ok {
			rematCount++
		}
	}
	if cap(remats) < rematCount {
		remats = make([]RematRecipe, 0, rematCount)
	}
	inductions := reuse.Inductions[:0]
	licm := reuse.LICM[:0]
	coldUses := reuse.ColdUses[:0]
	definition := resizeClear(reuse.definition, len(flow.Values))
	lastUse := resizeClear(reuse.lastUse, len(flow.Values))
	useCount := resizeClear(reuse.useCount, len(flow.Values))
	directUseCount := resizeClear(reuse.directUseCount, len(flow.Values))
	directUseBlock := resizeClear(reuse.directUseBlock, len(flow.Values))
	directUseInstruction := resizeClear(reuse.directUseInstruction, len(flow.Values))
	valueBlock := resizeClear(reuse.valueBlock, len(flow.Values))
	gprDelta := resizeClear(reuse.gprDelta, len(semantic.Insts)+1)
	fprDelta := resizeClear(reuse.fprDelta, len(semantic.Insts)+1)
	positionBlock := resizeClear(reuse.positionBlock, len(semantic.Insts))
	rematerializable := resizeClear(reuse.rematerializable, len(flow.Values))
	maxUseWeight := resizeClear(reuse.maxUseWeight, len(flow.Values))
	*reuse = PressurePlan{
		Blocks: blocks, Sinks: sinks, Remats: remats, Inductions: inductions, LICM: licm, ColdUses: coldUses, ReducedArgs: simplified.Metrics.TrivialArguments,
		definition: definition, lastUse: lastUse, useCount: useCount,
		directUseCount: directUseCount, directUseBlock: directUseBlock, directUseInstruction: directUseInstruction, valueBlock: valueBlock, gprDelta: gprDelta,
		fprDelta: fprDelta, positionBlock: positionBlock, rematerializable: rematerializable, maxUseWeight: maxUseWeight,
	}
	for block, record := range semantic.Blocks {
		for semanticID := record.InstStart; semanticID < record.InstStart+record.InstCount; semanticID++ {
			instruction := semantic.Insts[semanticID]
			for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
				result := instruction.Result + FlowValueID(ordinal)
				definition[result] = semanticID
				lastUse[result] = semanticID
				valueBlock[result] = BlockID(block)
			}
			for _, argument := range semantic.Operands(semanticID) {
				directUseCount[argument] = saturatingUseCount(directUseCount[argument])
				directUseBlock[argument], directUseInstruction[argument] = BlockID(block), semanticID
				argument = resolveAlias(simplified.Aliases, argument)
				useCount[argument] = saturatingUseCount(useCount[argument])
				if semanticID >= lastUse[argument] {
					lastUse[argument] = semanticID
				}
			}
		}
	}
	for _, edge := range flow.EdgeArgs {
		argument := resolveAlias(simplified.Aliases, edge.Argument)
		useCount[argument] = saturatingUseCount(useCount[argument])
		from := cfg.Edges[edge.Edge].From
		record := semantic.Blocks[from]
		if record.InstCount != 0 {
			position := record.InstStart + record.InstCount - 1
			lastUse[argument] = max(lastUse[argument], position)
		}
	}
	exit := BlockID(len(cfg.Blocks) - 1)
	for _, result := range flow.BlockEntry(exit) {
		result = resolveAlias(simplified.Aliases, result)
		useCount[result] = saturatingUseCount(useCount[result])
		lastUse[result] = uint32(len(semantic.Insts))
	}

	for block, record := range semantic.Blocks {
		for position := record.InstStart; position < record.InstStart+record.InstCount; position++ {
			reuse.positionBlock[position] = BlockID(block)
		}
	}
	for value := 1; value < len(flow.Values); value++ {
		if useCount[value] == 0 || definition[value] > lastUse[value] {
			continue
		}
		start, end := definition[value], lastUse[value]+1
		var delta []int32
		switch flow.Values[value].Type {
		case wasm.I32, wasm.I64:
			delta = reuse.gprDelta
		case wasm.F32, wasm.F64:
			delta = reuse.fprDelta
		default:
			continue
		}
		delta[start]++
		if end < uint32(len(delta)) {
			delta[end]--
		}
	}
	gpr, fpr := int32(0), int32(0)
	for position := range semantic.Insts {
		gpr += reuse.gprDelta[position]
		fpr += reuse.fprDelta[position]
		if gpr > 1<<16-1 || fpr > 1<<16-1 {
			required := max(gpr, fpr)
			return nil, &BudgetError{Resource: fmt.Sprintf("instruction %d pressure", position), Required: uint64(required), Limit: 1<<16 - 1}
		}
		pressure := &reuse.Blocks[reuse.positionBlock[position]]
		pressure.PeakGPR = max(pressure.PeakGPR, uint16(gpr))
		pressure.PeakFPR = max(pressure.PeakFPR, uint16(fpr))
	}

	for semanticID, instruction := range semantic.Insts {
		result := instruction.Result
		if result == 0 {
			continue
		}
		if recipe, ok := pressureRematRecipe(f, semantic, simplified, uint32(semanticID), instruction); ok {
			reuse.Remats = append(reuse.Remats, recipe)
		}
		meta := metadata.Instructions[instruction.Source]
		if directUseCount[result] == 1 && useCount[result] == directUseCount[result] && directUseBlock[result] == valueBlock[result] && directUseInstruction[result] > uint32(semanticID)+1 && meta.Reads == 0 && meta.Writes == 0 && meta.Flags == 0 && cheapSinkOp(instruction.Op) {
			reuse.Sinks = append(reuse.Sinks, SinkMove{Instruction: uint32(semanticID), Before: directUseInstruction[result], Block: valueBlock[result]})
		}
		block := valueBlock[result]
		if cfg.Blocks[block].Flags&BlockLoopHeader != 0 && (instruction.Op == wasm.InstrI32Add || instruction.Op == wasm.InstrI32Sub || instruction.Op == wasm.InstrI64Add || instruction.Op == wasm.InstrI64Sub) {
			args := semantic.Operands(uint32(semanticID))
			left, right := resolveAlias(simplified.Aliases, args[0]), resolveAlias(simplified.Aliases, args[1])
			if rightFact := simplified.IntegerFactAt(right); flow.Values[left].Kind == FlowValueBlockParam && rightFact.Known {
				step := int64(rightFact.Min)
				if instruction.Op == wasm.InstrI32Sub || instruction.Op == wasm.InstrI64Sub {
					step = -step
				}
				reuse.Inductions = append(reuse.Inductions, Induction{Value: result, Base: left, Step: step, Block: block})
			}
		}
	}
	for _, recipe := range reuse.Remats {
		rematerializable[recipe.Value] = true
	}
	for blockID, block := range semantic.Blocks {
		weight := cfg.Blocks[blockID].Weight
		for instruction := block.InstStart; instruction < block.InstStart+block.InstCount; instruction++ {
			for _, argument := range semantic.Operands(instruction) {
				argument = resolveAlias(simplified.Aliases, argument)
				maxUseWeight[argument] = max(maxUseWeight[argument], weight)
			}
		}
	}
	for blockID, block := range semantic.Blocks {
		weight := cfg.Blocks[blockID].Weight
		for instruction := block.InstStart; instruction < block.InstStart+block.InstCount; instruction++ {
			for _, argument := range semantic.Operands(instruction) {
				argument = resolveAlias(simplified.Aliases, argument)
				if rematerializable[argument] && planDifferentBlock(reuse.valueBlock[argument], BlockID(blockID)) && uint64(weight)*4 <= uint64(maxUseWeight[argument]) {
					reuse.ColdUses = append(reuse.ColdUses, ColdUse{Value: argument, Instruction: instruction, HotWeight: maxUseWeight[argument], ColdWeight: weight})
				}
			}
		}
	}
	planPressureLICM(f, cfg, flow, semantic, metadata, simplified, reuse)
	if err := VerifyPressurePlan(flow, semantic, metadata, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func pressureRematRecipe(f *StackFunc, semantic *SemanticFunc, simplified *SimplifyResult, semanticID uint32, instruction SemanticInst) (RematRecipe, bool) {
	if instruction.Result == 0 {
		return RematRecipe{}, false
	}
	source := f.Instrs[instruction.Source]
	switch source.Kind {
	case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const:
		return RematRecipe{Value: instruction.Result, Aux: source.U64(), Kind: RematConstant}, true
	case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U,
		wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		args := semantic.Operands(semanticID)
		return RematRecipe{Value: instruction.Result, Base: resolveAlias(simplified.Aliases, args[0]), Kind: RematExtend}, true
	case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI64Add, wasm.InstrI64Sub:
		args := semantic.Operands(semanticID)
		left, right := resolveAlias(simplified.Aliases, args[0]), resolveAlias(simplified.Aliases, args[1])
		leftFact, rightFact := simplified.IntegerFactAt(left), simplified.IntegerFactAt(right)
		if !leftFact.Known && rightFact.Known {
			step := rightFact.Min
			if instruction.Op == wasm.InstrI32Sub || instruction.Op == wasm.InstrI64Sub {
				step = uint64(-int64(step))
			}
			return RematRecipe{Value: instruction.Result, Base: left, Aux: step, Kind: RematAffine}, true
		}
	}
	return RematRecipe{}, false
}

func planDifferentBlock(a, b BlockID) bool { return a != b }

func planPressureLICM(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, plan *PressurePlan) {
	for loopID, loop := range cfg.Blocks {
		if loop.Flags&BlockLoopHeader == 0 {
			continue
		}
		preheader, ok := loopPreheader(f, cfg, BlockID(loopID), loop.Region)
		if !ok {
			continue
		}
		semanticLoop := semantic.Blocks[loopID]
		for instructionID := semanticLoop.InstStart; instructionID < semanticLoop.InstStart+semanticLoop.InstCount; instructionID++ {
			instruction := semantic.Insts[instructionID]
			meta := metadata.Instructions[instruction.Source]
			if instruction.Result == 0 || !licmPureOp(instruction.Op) || meta.Reads != 0 || meta.Writes != 0 || meta.Flags != 0 || meta.Traps != 0 || meta.Obligations != 0 {
				continue
			}
			invariant := true
			for _, argument := range semantic.Operands(instructionID) {
				argument = resolveAlias(simplified.Aliases, argument)
				if blockInRegion(cfg.Blocks[plan.valueBlock[argument]].Region, loop.Region, f) {
					invariant = false
					break
				}
			}
			if !invariant || !allValueUsesInLoop(f, cfg, semantic, instruction.Result, loop.Region) {
				continue
			}
			bankPeak, loopPeak := &plan.Blocks[preheader].PeakGPR, plan.Blocks[loopID].PeakGPR
			if flow.Values[instruction.Result].Type == wasm.F32 || flow.Values[instruction.Result].Type == wasm.F64 {
				bankPeak, loopPeak = &plan.Blocks[preheader].PeakFPR, plan.Blocks[loopID].PeakFPR
			}
			if uint32(*bankPeak)+1 > uint32(loopPeak) {
				continue
			}
			plan.LICM = append(plan.LICM, LICMMove{Instruction: instructionID, Preheader: preheader, Loop: BlockID(loopID)})
		}
	}
}

func loopPreheader(f *StackFunc, cfg *CFG, loop BlockID, region RegionID) (BlockID, bool) {
	record := cfg.Blocks[loop]
	var preheader BlockID
	found := false
	for _, pred := range cfg.Preds[record.PredStart : record.PredStart+uint32(record.PredCount)] {
		if blockInRegion(cfg.Blocks[pred].Region, region, f) {
			continue
		}
		if found {
			return 0, false
		}
		preheader, found = pred, true
	}
	return preheader, found
}

func blockInRegion(candidate, region RegionID, f *StackFunc) bool {
	if candidate == region {
		return true
	}
	if f == nil {
		return false
	}
	for candidate != NoRegion {
		candidate = f.Regions[candidate].Parent
		if candidate == region {
			return true
		}
	}
	return false
}

func allValueUsesInLoop(f *StackFunc, cfg *CFG, semantic *SemanticFunc, value FlowValueID, region RegionID) bool {
	used := false
	for blockID, block := range semantic.Blocks {
		for instruction := block.InstStart; instruction < block.InstStart+block.InstCount; instruction++ {
			for _, argument := range semantic.Operands(instruction) {
				if argument == value {
					used = true
					if !blockInRegion(cfg.Blocks[blockID].Region, region, f) {
						return false
					}
				}
			}
		}
	}
	return used
}

func licmPureOp(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Add || kind == wasm.InstrI32Sub || kind == wasm.InstrI32Mul ||
		kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub || kind == wasm.InstrI64Mul ||
		kind == wasm.InstrI32And || kind == wasm.InstrI32Or || kind == wasm.InstrI32Xor ||
		kind == wasm.InstrI64And || kind == wasm.InstrI64Or || kind == wasm.InstrI64Xor
}

func cheapSinkOp(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Const || kind == wasm.InstrI64Const ||
		kind == wasm.InstrI32Add || kind == wasm.InstrI32Sub || kind == wasm.InstrI32And || kind == wasm.InstrI32Or || kind == wasm.InstrI32Xor ||
		kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub || kind == wasm.InstrI64And || kind == wasm.InstrI64Or || kind == wasm.InstrI64Xor
}

func VerifyPressurePlan(flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, plan *PressurePlan) error {
	if plan == nil || len(plan.Blocks) != len(flow.Reachable) {
		return fmt.Errorf("railssa: malformed pressure plan")
	}
	for _, sink := range plan.Sinks {
		if sink.Instruction >= uint32(len(semantic.Insts)) || sink.Before >= uint32(len(semantic.Insts)) || sink.Before <= sink.Instruction || metadata.Instructions[semantic.Insts[sink.Instruction].Source].Flags != 0 {
			return fmt.Errorf("railssa: invalid sink move %#v", sink)
		}
	}
	for _, recipe := range plan.Remats {
		if recipe.Value == 0 || int(recipe.Value) >= len(flow.Values) || recipe.Kind == RematInvalid {
			return fmt.Errorf("railssa: invalid rematerialization %#v", recipe)
		}
	}
	for _, induction := range plan.Inductions {
		if induction.Value == 0 || induction.Base == 0 || int(induction.Value) >= len(flow.Values) || int(induction.Base) >= len(flow.Values) {
			return fmt.Errorf("railssa: invalid induction %#v", induction)
		}
	}
	for _, move := range plan.LICM {
		if move.Instruction >= uint32(len(semantic.Insts)) || int(move.Preheader) >= len(plan.Blocks) || int(move.Loop) >= len(plan.Blocks) || move.Preheader == move.Loop || !licmPureOp(semantic.Insts[move.Instruction].Op) {
			return fmt.Errorf("railssa: invalid LICM move %#v", move)
		}
	}
	for _, use := range plan.ColdUses {
		if use.Value == 0 || int(use.Value) >= len(flow.Values) || use.Instruction >= uint32(len(semantic.Insts)) || use.ColdWeight == 0 || uint64(use.ColdWeight)*4 > uint64(use.HotWeight) {
			return fmt.Errorf("railssa: invalid cold use %#v", use)
		}
	}
	return nil
}
