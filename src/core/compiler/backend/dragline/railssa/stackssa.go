package railssa

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const maxStackSSACells = 1 << 20

type FlowValueID uint32

type FlowValueKind uint8

const (
	FlowValueInvalid FlowValueKind = iota
	FlowValueInitialLocal
	FlowValueInstruction
	FlowValueBlockParam
)

type FlowValue struct {
	Kind  FlowValueKind
	Type  wasm.ValType
	Block BlockID
	Instr uint32
	Local uint32
	Slot  uint32
}

type StackParam struct {
	Block BlockID
	Slot  uint32
	Value FlowValueID
}

type StackEdgeArgument struct {
	Edge     uint32
	Param    FlowValueID
	Argument FlowValueID
}

// ValueFlow resolves the operand stack and local aliases into typed SSA values.
// EntryStacks and ExitStacks use block-major storage with MaxStack stride.
type ValueFlow struct {
	MaxStack   uint32
	ParamCount uint16

	Values   []FlowValue
	Params   []StackParam
	EdgeArgs []StackEdgeArgument

	EntryStacks []FlowValueID
	ExitStacks  []FlowValueID
	EntryDepths []uint32
	ExitDepths  []uint32

	InstructionValues     []FlowValueID
	LocalDefinitionValues []FlowValueID
	Reachable             []bool

	phi    []FlowValueID
	ready  []bool
	before []FlowValueID
	merge  []FlowValueID
	stack  []FlowValueID
}

func (v *ValueFlow) entry(block BlockID) []FlowValueID {
	start := uint64(block) * uint64(v.MaxStack)
	return v.EntryStacks[start : start+uint64(v.MaxStack)]
}

func (v *ValueFlow) exit(block BlockID) []FlowValueID {
	start := uint64(block) * uint64(v.MaxStack)
	return v.ExitStacks[start : start+uint64(v.MaxStack)]
}

// BlockEntry returns the immutable operand-stack values entering block.
func (v *ValueFlow) BlockEntry(block BlockID) []FlowValueID {
	return v.entry(block)[:v.EntryDepths[block]]
}

func BuildValueFlow(f *StackFunc, cfg *CFG, locals *LocalSSA, reuse *ValueFlow) (*ValueFlow, error) {
	if f == nil || cfg == nil || locals == nil || len(cfg.EdgeStacks) != len(cfg.Edges) {
		return nil, fmt.Errorf("railssa: value flow requires function, CFG, locals, and edge stacks")
	}
	stride := int(f.MaxStack)
	cells := uint64(stride) * uint64(len(cfg.Blocks))
	if cells > maxStackSSACells {
		return nil, fmt.Errorf("railssa: value flow requires %d cells, exceeds %d", cells, maxStackSSACells)
	}
	if reuse == nil {
		reuse = new(ValueFlow)
	}
	values := reuse.Values[:0]
	if reserve := knownValueCapacity(f, locals); cap(values) < reserve {
		values = make([]FlowValue, 0, reserve)
	}
	params := reuse.Params[:0]
	edgeArgs := reuse.EdgeArgs[:0]
	entryStacks := resizeClear(reuse.EntryStacks, int(cells))
	exitStacks := resizeClear(reuse.ExitStacks, int(cells))
	entryDepths := resizeClear(reuse.EntryDepths, len(cfg.Blocks))
	exitDepths := resizeClear(reuse.ExitDepths, len(cfg.Blocks))
	instructionValues := resizeClear(reuse.InstructionValues, len(f.Instrs))
	localDefinitionValues := resizeClear(reuse.LocalDefinitionValues, len(locals.Definitions))
	reachable := resizeClear(reuse.Reachable, len(cfg.Blocks))
	phi := resizeClear(reuse.phi, int(cells))
	ready := resizeClear(reuse.ready, len(cfg.Blocks))
	before := resizeClear(reuse.before, stride)
	merge := resizeClear(reuse.merge, stride)
	stack := reuse.stack[:0]
	if cap(stack) < stride {
		stack = make([]FlowValueID, 0, stride)
	}
	*reuse = ValueFlow{
		MaxStack: uint32(stride), ParamCount: uint16(len(f.Params)), Values: values, Params: params, EdgeArgs: edgeArgs,
		EntryStacks: entryStacks, ExitStacks: exitStacks, EntryDepths: entryDepths, ExitDepths: exitDepths,
		InstructionValues: instructionValues, LocalDefinitionValues: localDefinitionValues,
		Reachable: reachable,
		phi:       phi, ready: ready, before: before, merge: merge, stack: stack,
	}
	flow := reuse
	flow.Values = append(flow.Values, FlowValue{})
	copy(flow.Reachable, locals.Reachable)
	for definitionID, definition := range locals.Definitions {
		switch definition.Kind {
		case DefinitionInitial:
			flow.LocalDefinitionValues[definitionID] = flow.addValue(FlowValue{Kind: FlowValueInitialLocal, Type: definition.Type, Local: definition.Local, Instr: ^uint32(0)})
		case DefinitionBlockParam:
			flow.LocalDefinitionValues[definitionID] = flow.addValue(FlowValue{Kind: FlowValueBlockParam, Type: definition.Type, Block: definition.Block, Local: definition.Local, Instr: ^uint32(0)})
		}
	}

	flow.ready[0] = true
	for pass := 0; pass <= len(cfg.Blocks)*2; pass++ {
		changed := false
		for blockIndex := range cfg.Blocks {
			block := BlockID(blockIndex)
			if !flow.Reachable[block] {
				continue
			}
			if block != 0 {
				depth, merged, ok, err := mergeBlockStack(flow, cfg, flow.ready, block, flow.phi, flow.merge)
				if err != nil {
					return nil, err
				}
				if !ok {
					continue
				}
				if flow.EntryDepths[block] != depth || !equalFlowValues(flow.entry(block)[:depth], merged) {
					flow.EntryDepths[block] = depth
					copy(flow.entry(block), merged)
					changed = true
				}
			}
			entryDepth := flow.EntryDepths[block]
			flow.stack = flow.stack[:entryDepth]
			copy(flow.stack, flow.entry(block)[:entryDepth])
			if err := flow.simulateBlock(f, cfg, locals, block, &flow.stack); err != nil {
				return nil, err
			}
			copy(flow.before, flow.exit(block))
			if flow.ExitDepths[block] != uint32(len(flow.stack)) || !equalFlowValues(flow.before[:flow.ExitDepths[block]], flow.stack) || !flow.ready[block] {
				flow.ExitDepths[block] = uint32(len(flow.stack))
				clear(flow.exit(block))
				copy(flow.exit(block), flow.stack)
				changed = true
			}
			flow.ready[block] = true
		}
		if !changed {
			break
		}
		if pass == len(cfg.Blocks)*2 {
			return nil, fmt.Errorf("railssa: value flow did not converge after %d passes", pass+1)
		}
	}
	for blockIndex := range cfg.Blocks {
		for slot := 0; slot < stride; slot++ {
			value := flow.phi[blockIndex*stride+slot]
			if value != 0 {
				flow.Params = append(flow.Params, StackParam{Block: BlockID(blockIndex), Slot: uint32(slot), Value: value})
			}
		}
	}
	for edgeIndex, edge := range cfg.Edges {
		if !flow.Reachable[edge.From] || !flow.Reachable[edge.To] {
			continue
		}
		for _, param := range flow.Params {
			if param.Block != edge.To {
				continue
			}
			argument, err := transferredValue(flow, cfg.EdgeStacks[edgeIndex], edge.From, param.Slot)
			if err != nil {
				return nil, fmt.Errorf("railssa: edge %d stack argument: %w", edgeIndex, err)
			}
			flow.EdgeArgs = append(flow.EdgeArgs, StackEdgeArgument{Edge: uint32(edgeIndex), Param: param.Value, Argument: argument})
		}
	}
	// Local SSA and operand-stack SSA share the same FlowValue namespace. Carry
	// demanded local joins on the same edge-argument slab so later consumers do
	// not need a second phi or environment representation.
	for _, argument := range locals.EdgeArgs {
		param := flow.LocalDefinitionValues[argument.Param]
		value := flow.LocalDefinitionValues[argument.Argument]
		if param == 0 || value == 0 {
			return nil, fmt.Errorf("railssa: local edge %d has unresolved value", argument.Edge)
		}
		flow.EdgeArgs = append(flow.EdgeArgs, StackEdgeArgument{Edge: argument.Edge, Param: param, Argument: value})
	}
	if err := VerifyValueFlow(f, cfg, locals, flow); err != nil {
		return nil, err
	}
	return flow, nil
}

// knownValueCapacity counts values that construction creates independently of
// operand-stack joins. It deliberately excludes local.set definitions, which
// alias existing values, and stack-phi cells, which remain demand allocated.
// Thus large NOP or local-assignment bodies do not reserve one wide FlowValue
// per instruction.
func knownValueCapacity(f *StackFunc, locals *LocalSSA) int {
	count := 1 // ID zero is invalid.
	for _, definition := range locals.Definitions {
		if definition.Kind == DefinitionInitial || definition.Kind == DefinitionBlockParam {
			count++
		}
	}
	for source, instruction := range f.Instrs {
		if instruction.Kind == wasm.InstrCall || instruction.Kind == wasm.InstrCallIndirect {
			count += int(f.InstructionResultCount(uint32(source), instruction))
		} else if instruction.Kind == wasm.InstrBrOnCast || instruction.Kind == wasm.InstrBrOnCastFail {
			count++
		} else if stackInstructionHasValueResult(instruction) {
			count++
		}
	}
	return count
}

func (v *ValueFlow) addValue(value FlowValue) FlowValueID {
	id := FlowValueID(len(v.Values))
	v.Values = append(v.Values, value)
	return id
}

func mergeBlockStack(flow *ValueFlow, cfg *CFG, ready []bool, block BlockID, phi, scratch []FlowValueID) (uint32, []FlowValueID, bool, error) {
	var merged []FlowValueID
	depth := uint32(0)
	for edgeIndex, edge := range cfg.Edges {
		if edge.To != block || !flow.Reachable[edge.From] || !ready[edge.From] {
			continue
		}
		candidateDepth, err := transferredDepth(flow, cfg.EdgeStacks[edgeIndex], edge.From)
		if err != nil {
			return 0, nil, false, fmt.Errorf("railssa: edge %d into block %d: %w", edgeIndex, block, err)
		}
		if merged == nil {
			depth = candidateDepth
			merged = scratch[:depth]
			for slot := uint32(0); slot < depth; slot++ {
				merged[slot], err = transferredValue(flow, cfg.EdgeStacks[edgeIndex], edge.From, slot)
				if err != nil {
					return 0, nil, false, err
				}
				typ, refined := transferredType(flow, cfg, uint32(edgeIndex), edge.From, slot, merged[slot])
				if refined {
					cell := int(block)*int(flow.MaxStack) + int(slot)
					if phi[cell] == 0 {
						phi[cell] = flow.addValue(FlowValue{Kind: FlowValueBlockParam, Type: typ, Block: block, Slot: slot, Instr: ^uint32(0), Local: ^uint32(0)})
					}
					merged[slot] = phi[cell]
				}
			}
			continue
		}
		if candidateDepth != depth {
			return 0, nil, false, fmt.Errorf("railssa: block %d incoming stack depths %d and %d differ", block, depth, candidateDepth)
		}
		for slot := uint32(0); slot < depth; slot++ {
			value, err := transferredValue(flow, cfg.EdgeStacks[edgeIndex], edge.From, slot)
			if err != nil {
				return 0, nil, false, err
			}
			typ, _ := transferredType(flow, cfg, uint32(edgeIndex), edge.From, slot, value)
			if flow.Values[merged[slot]].Type != typ {
				return 0, nil, false, fmt.Errorf("railssa: block %d slot %d incoming types differ", block, slot)
			}
			if value != merged[slot] {
				cell := int(block)*int(flow.MaxStack) + int(slot)
				if phi[cell] == 0 {
					phi[cell] = flow.addValue(FlowValue{Kind: FlowValueBlockParam, Type: flow.Values[value].Type, Block: block, Slot: slot, Instr: ^uint32(0), Local: ^uint32(0)})
				}
				merged[slot] = phi[cell]
			}
		}
	}
	if merged == nil {
		return 0, nil, false, nil
	}
	// Once a slot became a parameter, it remains one even if the first ready
	// predecessor in a later pass happens to carry a different concrete value.
	for slot := uint32(0); slot < depth; slot++ {
		cell := int(block)*int(flow.MaxStack) + int(slot)
		if phi[cell] != 0 {
			merged[slot] = phi[cell]
		}
	}
	return depth, merged, true, nil
}

func transferredType(flow *ValueFlow, cfg *CFG, edge uint32, from BlockID, slot uint32, value FlowValueID) (wasm.ValType, bool) {
	for _, refinement := range cfg.Refinements {
		if refinement.Edge != edge {
			continue
		}
		refinedSlot := refinement.Slot
		if refinedSlot == ^uint32(0) {
			depth, err := transferredDepth(flow, cfg.EdgeStacks[edge], from)
			if err != nil || depth == 0 {
				break
			}
			refinedSlot = depth - 1
		}
		if refinedSlot == slot {
			return refinement.Type, true
		}
	}
	return flow.Values[value].Type, false
}

func transferredDepth(flow *ValueFlow, transfer EdgeStack, from BlockID) (uint32, error) {
	depth := flow.ExitDepths[from]
	if transfer.CarriesAll() {
		return depth, nil
	}
	want := transfer.PrefixDepth() + uint32(transfer.ResultArity())
	if transfer.PrefixDepth() > depth || uint32(transfer.ResultArity()) > depth-transfer.PrefixDepth() {
		return 0, fmt.Errorf("source depth %d cannot supply prefix %d plus %d results", depth, transfer.PrefixDepth(), transfer.ResultArity())
	}
	return want, nil
}

func transferredValue(flow *ValueFlow, transfer EdgeStack, from BlockID, slot uint32) (FlowValueID, error) {
	depth, err := transferredDepth(flow, transfer, from)
	if err != nil {
		return 0, err
	}
	if slot >= depth {
		return 0, fmt.Errorf("slot %d exceeds transferred depth %d", slot, depth)
	}
	source := flow.exit(from)
	if transfer.CarriesAll() || slot < transfer.PrefixDepth() {
		return source[slot], nil
	}
	resultSlot := slot - transfer.PrefixDepth()
	return source[flow.ExitDepths[from]-uint32(transfer.ResultArity())+resultSlot], nil
}

func (v *ValueFlow) simulateBlock(f *StackFunc, cfg *CFG, locals *LocalSSA, block BlockID, stack *[]FlowValueID) error {
	record := cfg.Blocks[block]
	for instructionIndex := record.InstStart; instructionIndex < record.InstStart+record.InstCount; instructionIndex++ {
		instruction := f.Instrs[instructionIndex]
		if err := v.applyInstruction(f, locals, block, instructionIndex, instruction, stack); err != nil {
			return fmt.Errorf("railssa: block %d instruction %d %s: %w", block, instructionIndex, instruction.Kind, err)
		}
	}
	return nil
}

func (v *ValueFlow) applyInstruction(f *StackFunc, locals *LocalSSA, block BlockID, instructionIndex uint32, instruction StackInstr, stack *[]FlowValueID) error {
	kind := instruction.Kind
	pushResultAt := func(typ wasm.ValType, ordinal uint32) error {
		value := v.InstructionValues[instructionIndex]
		if value == 0 {
			if ordinal != 0 {
				return fmt.Errorf("instruction result %d has no first value", ordinal)
			}
			value = v.addValue(FlowValue{Kind: FlowValueInstruction, Type: typ, Block: block, Instr: instructionIndex, Local: ^uint32(0)})
			v.InstructionValues[instructionIndex] = value
		} else {
			value += FlowValueID(ordinal)
			if int(value) == len(v.Values) {
				v.addValue(FlowValue{Kind: FlowValueInstruction, Type: typ, Block: block, Instr: instructionIndex, Local: ^uint32(0), Slot: ordinal})
			}
		}
		if int(value) >= len(v.Values) {
			return fmt.Errorf("instruction result %d value is unavailable", ordinal)
		}
		if v.Values[value].Type != typ || v.Values[value].Instr != instructionIndex {
			return fmt.Errorf("instruction result type changed from %s to %s", v.Values[value].Type, typ)
		}
		return pushFlow(stack, value, v.MaxStack)
	}
	pushResult := func(typ wasm.ValType) error { return pushResultAt(typ, 0) }
	pop := func(want wasm.ValType) (FlowValueID, error) { return popFlow(stack, v.Values, want) }
	switch {
	case kind == wasm.InstrInvalid || kind == wasm.InstrNop || kind == wasm.InstrBlock || kind == wasm.InstrLoop || instruction.IsElse() || kind == wasm.InstrBr || kind == wasm.InstrReturn || kind == wasm.InstrUnreachable:
		return nil
	case kind == wasm.InstrIf || kind == wasm.InstrBrIf || kind == wasm.InstrBrTable:
		_, err := pop(wasm.I32)
		return err
	case kind == wasm.InstrBrOnCast || kind == wasm.InstrBrOnCastFail:
		if len(*stack) == 0 || v.Values[(*stack)[len(*stack)-1]].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("branch cast reference operand is unavailable")
		}
		value := v.InstructionValues[instructionIndex]
		if value == 0 {
			value = v.addValue(FlowValue{Kind: FlowValueInstruction, Type: wasm.I32, Block: block, Instr: instructionIndex, Local: ^uint32(0)})
			v.InstructionValues[instructionIndex] = value
		}
		if v.Values[value].Type != wasm.I32 {
			return fmt.Errorf("branch cast condition type is %s", v.Values[value].Type)
		}
		return nil
	case kind == wasm.InstrDrop:
		_, err := popFlow(stack, v.Values, wasm.ValType{})
		return err
	case kind == wasm.InstrLocalGet:
		definition := locals.InstructionValues[instructionIndex]
		if int(definition) >= len(v.LocalDefinitionValues) || definition == 0 {
			return fmt.Errorf("local.get has no environment definition")
		}
		value := v.LocalDefinitionValues[definition]
		if value == 0 {
			return fmt.Errorf("local.get definition %d has no SSA value", definition)
		}
		v.InstructionValues[instructionIndex] = value
		return pushFlow(stack, value, v.MaxStack)
	case kind == wasm.InstrLocalSet:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if !flowTypeAssignable(f.Module, v.Values[value].Type, f.Locals[instruction.U32()]) {
			return fmt.Errorf("local.set operand type %s, want %s", v.Values[value].Type, f.Locals[instruction.U32()])
		}
		v.LocalDefinitionValues[locals.InstructionValues[instructionIndex]] = value
		return nil
	case kind == wasm.InstrLocalTee:
		if len(*stack) == 0 {
			return fmt.Errorf("operand stack underflow")
		}
		value := (*stack)[len(*stack)-1]
		if !flowTypeAssignable(f.Module, v.Values[value].Type, f.Locals[instruction.U32()]) {
			return fmt.Errorf("local.tee operand type %s, want %s", v.Values[value].Type, f.Locals[instruction.U32()])
		}
		v.LocalDefinitionValues[locals.InstructionValues[instructionIndex]] = value
		return nil
	case kind == wasm.InstrGlobalGet:
		return pushResult(f.Globals[instruction.U32()])
	case kind == wasm.InstrGlobalSet:
		_, err := pop(f.Globals[instruction.U32()])
		return err
	case kind == wasm.InstrI32Const:
		return pushResult(wasm.I32)
	case kind == wasm.InstrI64Const:
		return pushResult(wasm.I64)
	case kind == wasm.InstrF32Const:
		return pushResult(wasm.F32)
	case kind == wasm.InstrF64Const:
		return pushResult(wasm.F64)
	case kind == wasm.InstrRefNull || kind == wasm.InstrRefFunc:
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef {
			return fmt.Errorf("%s result type is unavailable", kind)
		}
		return pushResult(typ)
	case kind == wasm.InstrRefIsNull:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("ref.is_null operand is %s", v.Values[value].Type)
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrRefEq:
		for range 2 {
			value, err := popFlow(stack, v.Values, wasm.ValType{})
			if err != nil {
				return err
			}
			if v.Values[value].Type.Kind() != wasm.ValRef {
				return fmt.Errorf("ref.eq operand is %s", v.Values[value].Type)
			}
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrRefAsNonNull:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		typ := v.Values[value].Type
		if typ.Kind() != wasm.ValRef {
			return fmt.Errorf("ref.as_non_null operand is %s", typ)
		}
		return pushResult(wasm.RefVal(typ.Ref().WithNullable(false)))
	case kind == wasm.InstrRefI31:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef {
			return fmt.Errorf("ref.i31 result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrI31GetS || kind == wasm.InstrI31GetU:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("%s operand is %s", kind, v.Values[value].Type)
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrStructNewDefault:
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			return fmt.Errorf("struct.new_default result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrStructNew:
		for fieldIndex := instruction.Params(); fieldIndex > 0; fieldIndex-- {
			field, ok := f.Module.StructField(instruction.U32(), fieldIndex-1)
			if !ok {
				return fmt.Errorf("struct.new field %d:%d is unavailable", instruction.U32(), fieldIndex-1)
			}
			value, err := popFlow(stack, v.Values, wasm.ValType{})
			if err != nil {
				return err
			}
			want := field.Storage().Val()
			if field.Storage().Packed() {
				want = wasm.I32
			}
			got := v.Values[value].Type
			if want.Kind() == wasm.ValRef {
				if got.Kind() != wasm.ValRef || !f.Module.ReferenceTypeSubtype(got.Ref(), want.Ref()) {
					return fmt.Errorf("struct.new field %d value is %s, want subtype of %s", fieldIndex-1, got, want)
				}
			} else if got != want {
				return fmt.Errorf("struct.new field %d value is %s, want %s", fieldIndex-1, got, want)
			}
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			return fmt.Errorf("struct.new result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrArrayNewDefault:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			return fmt.Errorf("array.new_default result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrArrayNew:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		field, ok := f.Module.ArrayField(instruction.U32())
		if !ok {
			return fmt.Errorf("array.new type %d is unavailable", instruction.U32())
		}
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		want := field.Storage().Val()
		if field.Storage().Packed() {
			want = wasm.I32
		}
		got := v.Values[value].Type
		if want.Kind() == wasm.ValRef {
			if got.Kind() != wasm.ValRef || !f.Module.ReferenceTypeSubtype(got.Ref(), want.Ref()) {
				return fmt.Errorf("array.new initializer is %s, want subtype of %s", got, want)
			}
		} else if got != want {
			return fmt.Errorf("array.new initializer is %s, want %s", got, want)
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			return fmt.Errorf("array.new result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrArrayNewFixed:
		field, ok := f.Module.ArrayField(instruction.U32())
		if !ok {
			return fmt.Errorf("array.new_fixed type %d is unavailable", instruction.U32())
		}
		want := field.Storage().Val()
		if field.Storage().Packed() {
			want = wasm.I32
		}
		for elementIndex := instruction.Params(); elementIndex > 0; elementIndex-- {
			value, err := popFlow(stack, v.Values, wasm.ValType{})
			if err != nil {
				return err
			}
			got := v.Values[value].Type
			if want.Kind() == wasm.ValRef {
				if got.Kind() != wasm.ValRef || !f.Module.ReferenceTypeSubtype(got.Ref(), want.Ref()) {
					return fmt.Errorf("array.new_fixed element %d is %s, want subtype of %s", elementIndex-1, got, want)
				}
			} else if got != want {
				return fmt.Errorf("array.new_fixed element %d is %s, want %s", elementIndex-1, got, want)
			}
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			return fmt.Errorf("array.new_fixed result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrArrayNewData || kind == wasm.InstrArrayNewElem:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			return fmt.Errorf("%s result type is unavailable", kind)
		}
		return pushResult(typ)
	case kind == wasm.InstrArrayLen:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("array.len operand is %s", v.Values[value].Type)
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrAnyConvertExtern || kind == wasm.InstrExternConvertAny:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("%s operand is %s", kind, v.Values[value].Type)
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef {
			return fmt.Errorf("%s result type is unavailable", kind)
		}
		return pushResult(typ)
	case kind == wasm.InstrRefTest:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("ref.test operand is %s", v.Values[value].Type)
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrRefCast:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("ref.cast operand is %s", v.Values[value].Type)
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok || typ.Kind() != wasm.ValRef {
			return fmt.Errorf("ref.cast result type is unavailable")
		}
		return pushResult(typ)
	case kind == wasm.InstrArrayGet || kind == wasm.InstrArrayGetS || kind == wasm.InstrArrayGetU:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		object, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[object].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("%s object is %s", kind, v.Values[object].Type)
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok {
			return fmt.Errorf("%s result type is unavailable", kind)
		}
		return pushResult(typ)
	case kind == wasm.InstrArraySet:
		field, ok := f.Module.ArrayField(instruction.U32())
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("array.set type %d is unavailable or immutable", instruction.U32())
		}
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		want := field.Storage().Val()
		if field.Storage().Packed() {
			want = wasm.I32
		}
		got := v.Values[value].Type
		if want.Kind() == wasm.ValRef {
			if got.Kind() != wasm.ValRef || !f.Module.ReferenceTypeSubtype(got.Ref(), want.Ref()) {
				return fmt.Errorf("array.set value is %s, want subtype of %s", got, want)
			}
		} else if got != want {
			return fmt.Errorf("array.set value is %s, want %s", got, want)
		}
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		object, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[object].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("array.set object is %s", v.Values[object].Type)
		}
		return nil
	case kind == wasm.InstrArrayFill:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		field, ok := f.Module.ArrayField(instruction.U32())
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("array.fill type %d is unavailable or immutable", instruction.U32())
		}
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		want := field.Storage().Val()
		if field.Storage().Packed() {
			want = wasm.I32
		}
		got := v.Values[value].Type
		if !flowTypeAssignable(f.Module, got, want) {
			return fmt.Errorf("array.fill value is %s, want %s", got, want)
		}
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		object, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[object].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("array.fill object is %s", v.Values[object].Type)
		}
		return nil
	case kind == wasm.InstrArrayCopy:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		source, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[source].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("array.copy source is %s", v.Values[source].Type)
		}
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		destination, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[destination].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("array.copy destination is %s", v.Values[destination].Type)
		}
		return nil
	case kind == wasm.InstrArrayInitData || kind == wasm.InstrArrayInitElem:
		for range 3 {
			if _, err := pop(wasm.I32); err != nil {
				return err
			}
		}
		object, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[object].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("%s object is %s", kind, v.Values[object].Type)
		}
		return nil
	case kind == wasm.InstrStructGet || kind == wasm.InstrStructGetS || kind == wasm.InstrStructGetU:
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[value].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("%s operand is %s", kind, v.Values[value].Type)
		}
		typ, ok := f.InstructionResultType(instructionIndex, instruction, 0)
		if !ok {
			return fmt.Errorf("%s result type is unavailable", kind)
		}
		return pushResult(typ)
	case kind == wasm.InstrStructSet:
		typeIndex, fieldIndex := uint32(instruction.U64()>>32), instruction.U32()
		field, ok := f.Module.StructField(typeIndex, fieldIndex)
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("struct.set field %d:%d is unavailable or immutable", typeIndex, fieldIndex)
		}
		value, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		want := field.Storage().Val()
		if field.Storage().Packed() {
			want = wasm.I32
		}
		got := v.Values[value].Type
		if want.Kind() == wasm.ValRef {
			if got.Kind() != wasm.ValRef || !f.Module.ReferenceTypeSubtype(got.Ref(), want.Ref()) {
				return fmt.Errorf("struct.set value is %s, want subtype of %s", got, want)
			}
		} else if got != want {
			return fmt.Errorf("struct.set value is %s, want %s", got, want)
		}
		object, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		if v.Values[object].Type.Kind() != wasm.ValRef {
			return fmt.Errorf("struct.set object is %s", v.Values[object].Type)
		}
		return nil
	case kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		return pushResult(instruction.ValueType())
	case kind >= wasm.InstrI32Store && kind <= wasm.InstrI64Store32:
		valueType := storeValueType(kind)
		if _, err := pop(valueType); err != nil {
			return err
		}
		_, err := pop(wasm.I32)
		return err
	case kind == wasm.InstrMemorySize:
		return pushResult(wasm.I32)
	case kind == wasm.InstrMemoryGrow:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrMemoryCopy || kind == wasm.InstrMemoryFill:
		for range 3 {
			if _, err := pop(wasm.I32); err != nil {
				return err
			}
		}
		return nil
	case kind == wasm.InstrDataDrop || kind == wasm.InstrElemDrop:
		return nil
	case kind == wasm.InstrCall || kind == wasm.InstrCallIndirect:
		if kind == wasm.InstrCallIndirect {
			if _, err := pop(wasm.I32); err != nil {
				return err
			}
		}
		for range instruction.Params() {
			if _, err := popFlow(stack, v.Values, wasm.ValType{}); err != nil {
				return err
			}
		}
		for result := uint32(0); result < f.InstructionResultCount(instructionIndex, instruction); result++ {
			typ, ok := f.InstructionResultType(instructionIndex, instruction, result)
			if !ok {
				return fmt.Errorf("call result %d type is unavailable", result)
			}
			if err := pushResultAt(typ, result); err != nil {
				return err
			}
		}
		return nil
	case kind == wasm.InstrSelect:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		right, err := popFlow(stack, v.Values, wasm.ValType{})
		if err != nil {
			return err
		}
		left, err := popFlow(stack, v.Values, v.Values[right].Type)
		if err != nil {
			return err
		}
		_ = left
		return pushResult(v.Values[right].Type)
	case wasm.IsSIMDValidationInstructionKind(kind):
		d, ok := f.SIMDImmediateAt(instructionIndex)
		if !ok {
			return fmt.Errorf("SIMD descriptor is unavailable")
		}
		popType := func(types ...wasm.ValType) error {
			for _, typ := range types {
				if _, err := pop(typ); err != nil {
					return err
				}
			}
			return nil
		}
		switch d.Class {
		case wasm.SIMDEffectLoad:
			if err := popType(wasm.I32); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectStore, wasm.SIMDEffectStoreLane:
			return popType(wasm.V128, wasm.I32)
		case wasm.SIMDEffectLoadLane:
			if err := popType(wasm.V128, wasm.I32); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectSplat:
			if err := popType(d.Scalar); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectExtract:
			if err := popType(wasm.V128); err != nil {
				return err
			}
			return pushResult(d.Scalar)
		case wasm.SIMDEffectReplace:
			if err := popType(d.Scalar, wasm.V128); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectShift:
			if err := popType(wasm.I32, wasm.V128); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectUnary:
			if err := popType(wasm.V128); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectBinary:
			if err := popType(wasm.V128, wasm.V128); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectTernary, wasm.SIMDEffectBitselect:
			if err := popType(wasm.V128, wasm.V128, wasm.V128); err != nil {
				return err
			}
			return pushResult(wasm.V128)
		case wasm.SIMDEffectReduceI32:
			if err := popType(wasm.V128); err != nil {
				return err
			}
			return pushResult(wasm.I32)
		case wasm.SIMDEffectConst:
			return pushResult(wasm.V128)
		default:
			return fmt.Errorf("SIMD effect is not classified")
		}
	case scalarBinaryKind(kind):
		typ := binaryValueType(kind)
		if _, err := pop(typ); err != nil {
			return err
		}
		if _, err := pop(typ); err != nil {
			return err
		}
		return pushResult(typ)
	case scalarComparisonKind(kind):
		typ := comparisonOperandType(kind)
		if _, err := pop(typ); err != nil {
			return err
		}
		if _, err := pop(typ); err != nil {
			return err
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrI32Eqz:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		return pushResult(wasm.I32)
	case kind == wasm.InstrI64Eqz:
		if _, err := pop(wasm.I64); err != nil {
			return err
		}
		return pushResult(wasm.I32)
	case kind >= wasm.InstrI32Clz && kind <= wasm.InstrI32Popcnt:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		return pushResult(wasm.I32)
	case kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Popcnt:
		if _, err := pop(wasm.I64); err != nil {
			return err
		}
		return pushResult(wasm.I64)
	case kind >= wasm.InstrI32Extend8S && kind <= wasm.InstrI32Extend16S:
		if _, err := pop(wasm.I32); err != nil {
			return err
		}
		return pushResult(wasm.I32)
	case kind >= wasm.InstrI64Extend8S && kind <= wasm.InstrI64Extend32S:
		if _, err := pop(wasm.I64); err != nil {
			return err
		}
		return pushResult(wasm.I64)
	case kind >= wasm.InstrF32Abs && kind <= wasm.InstrF32Sqrt:
		if _, err := pop(wasm.F32); err != nil {
			return err
		}
		return pushResult(wasm.F32)
	case kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Sqrt:
		if _, err := pop(wasm.F64); err != nil {
			return err
		}
		return pushResult(wasm.F64)
	default:
		input, output, ok := conversionTypes(kind)
		if !ok {
			return fmt.Errorf("instruction is not classified")
		}
		if _, err := pop(input); err != nil {
			return err
		}
		return pushResult(output)
	}
}

func pushFlow(stack *[]FlowValueID, value FlowValueID, max uint32) error {
	if uint32(len(*stack)) >= max {
		return fmt.Errorf("operand stack exceeds maximum %d", max)
	}
	*stack = append(*stack, value)
	return nil
}

func popFlow(stack *[]FlowValueID, values []FlowValue, want wasm.ValType) (FlowValueID, error) {
	if len(*stack) == 0 {
		return 0, fmt.Errorf("operand stack underflow")
	}
	index := len(*stack) - 1
	value := (*stack)[index]
	*stack = (*stack)[:index]
	if want != (wasm.ValType{}) && values[value].Type != want {
		return 0, fmt.Errorf("operand type %s, want %s", values[value].Type, want)
	}
	return value, nil
}

func flowTypeAssignable(m *wasm.Module, got, want wasm.ValType) bool {
	if got == want {
		return true
	}
	return m != nil && got.Kind() == wasm.ValRef && want.Kind() == wasm.ValRef && m.ReferenceTypeSubtype(got.Ref(), want.Ref())
}

func peekFlow(stack []FlowValueID, values []FlowValue, want wasm.ValType) (FlowValueID, error) {
	if len(stack) == 0 {
		return 0, fmt.Errorf("operand stack underflow")
	}
	value := stack[len(stack)-1]
	if values[value].Type != want {
		return 0, fmt.Errorf("operand type %s, want %s", values[value].Type, want)
	}
	return value, nil
}

func binaryValueType(kind wasm.InstrKind) wasm.ValType {
	switch {
	case kind >= wasm.InstrI32Add && kind <= wasm.InstrI32Rotr:
		return wasm.I32
	case kind >= wasm.InstrI64Add && kind <= wasm.InstrI64Rotr:
		return wasm.I64
	case kind >= wasm.InstrF32Add && kind <= wasm.InstrF32Copysign:
		return wasm.F32
	default:
		return wasm.F64
	}
}

func comparisonOperandType(kind wasm.InstrKind) wasm.ValType {
	switch {
	case kind >= wasm.InstrI32Eq && kind <= wasm.InstrI32GeU:
		return wasm.I32
	case kind >= wasm.InstrI64Eq && kind <= wasm.InstrI64GeU:
		return wasm.I64
	case kind >= wasm.InstrF32Eq && kind <= wasm.InstrF32Ge:
		return wasm.F32
	default:
		return wasm.F64
	}
}

func storeValueType(kind wasm.InstrKind) wasm.ValType {
	switch kind {
	case wasm.InstrI32Store, wasm.InstrI32Store8, wasm.InstrI32Store16:
		return wasm.I32
	case wasm.InstrI64Store, wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32:
		return wasm.I64
	case wasm.InstrF32Store:
		return wasm.F32
	default:
		return wasm.F64
	}
}

func conversionTypes(kind wasm.InstrKind) (wasm.ValType, wasm.ValType, bool) {
	switch kind {
	case wasm.InstrI32WrapI64:
		return wasm.I64, wasm.I32, true
	case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U, wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U:
		return wasm.F32, wasm.I32, true
	case wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U:
		return wasm.F64, wasm.I32, true
	case wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U:
		return wasm.I32, wasm.I64, true
	case wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U, wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U:
		return wasm.F32, wasm.I64, true
	case wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		return wasm.F64, wasm.I64, true
	case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32ReinterpretI32:
		return wasm.I32, wasm.F32, true
	case wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U:
		return wasm.I64, wasm.F32, true
	case wasm.InstrF32DemoteF64:
		return wasm.F64, wasm.F32, true
	case wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U:
		return wasm.I32, wasm.F64, true
	case wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U, wasm.InstrF64ReinterpretI64:
		return wasm.I64, wasm.F64, true
	case wasm.InstrF64PromoteF32:
		return wasm.F32, wasm.F64, true
	case wasm.InstrI32ReinterpretF32:
		return wasm.F32, wasm.I32, true
	case wasm.InstrI64ReinterpretF64:
		return wasm.F64, wasm.I64, true
	default:
		return wasm.ValType{}, wasm.ValType{}, false
	}
}

func VerifyValueFlow(f *StackFunc, cfg *CFG, locals *LocalSSA, flow *ValueFlow) error {
	if f == nil || cfg == nil || locals == nil || flow == nil || len(flow.Values) == 0 || flow.Values[0].Kind != FlowValueInvalid {
		return fmt.Errorf("railssa: malformed value flow header")
	}
	if len(flow.EntryDepths) != len(cfg.Blocks) || len(flow.ExitDepths) != len(cfg.Blocks) || len(flow.InstructionValues) != len(f.Instrs) {
		return fmt.Errorf("railssa: malformed value flow dense storage")
	}
	for block := range cfg.Blocks {
		if flow.EntryDepths[block] > flow.MaxStack || flow.ExitDepths[block] > flow.MaxStack {
			return fmt.Errorf("railssa: block %d exceeds stack maximum", block)
		}
	}
	for _, param := range flow.Params {
		if int(param.Block) >= len(cfg.Blocks) || param.Slot >= flow.EntryDepths[param.Block] || int(param.Value) >= len(flow.Values) {
			return fmt.Errorf("railssa: invalid stack parameter %#v", param)
		}
		value := flow.Values[param.Value]
		if value.Kind != FlowValueBlockParam || value.Block != param.Block || value.Slot != param.Slot || flow.entry(param.Block)[param.Slot] != param.Value {
			return fmt.Errorf("railssa: inconsistent stack parameter %#v", param)
		}
	}
	for instruction, value := range flow.InstructionValues {
		if f.Instrs[instruction].Kind != wasm.InstrLocalGet || value == 0 {
			continue
		}
		if int(value) >= len(flow.Values) || !flowTypeAssignable(f.Module, flow.Values[value].Type, f.Locals[f.Instrs[instruction].U32()]) {
			return fmt.Errorf("railssa: local.get %d has invalid flow value %d", instruction, value)
		}
	}
	for _, argument := range flow.EdgeArgs {
		if int(argument.Edge) >= len(cfg.Edges) || int(argument.Param) >= len(flow.Values) || int(argument.Argument) >= len(flow.Values) {
			return fmt.Errorf("railssa: invalid stack edge argument %#v", argument)
		}
		param, value := flow.Values[argument.Param], flow.Values[argument.Argument]
		typ, _ := transferredType(flow, cfg, argument.Edge, cfg.Edges[argument.Edge].From, param.Slot, argument.Argument)
		if param.Kind != FlowValueBlockParam || param.Type != typ || value.Type.Kind() != typ.Kind() || cfg.Edges[argument.Edge].To != param.Block {
			return fmt.Errorf("railssa: inconsistent stack edge argument %#v", argument)
		}
	}
	exit := BlockID(len(cfg.Blocks) - 1)
	if flow.Reachable[exit] && flow.EntryDepths[exit] != uint32(len(f.Results)) {
		return fmt.Errorf("railssa: function exit depth %d, want %d", flow.EntryDepths[exit], len(f.Results))
	}
	return nil
}

func equalFlowValues(a, b []FlowValueID) bool {
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
