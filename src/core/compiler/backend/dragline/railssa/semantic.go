package railssa

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const maxSemanticArgs = 1 << 20

// SemanticRange indexes a contiguous run in SemanticFunc.Args.
type SemanticRange struct {
	Start uint32
	Len   uint16
}

// SemanticInst is one source-ordered, non-administrative Wasm operation.
// Its block is implicit in SemanticFunc.Blocks, and variable operands live in
// the shared Args slab. The record is deliberately 24 bytes on 64-bit hosts.
type SemanticInst struct {
	Aux      uint64
	ArgStart uint32
	Result   FlowValueID
	Source   uint32
	ArgLen   uint16
	Op       wasm.InstrKind
}

// ResultCount returns the number of consecutive FlowValue definitions. Call
// arity is encoded in Aux's high word after semantic operands have captured the
// parameter count; all other value-producing instructions remain scalar.
func (i SemanticInst) ResultCount() uint32 {
	if i.Result == 0 {
		return 0
	}
	if i.Op == wasm.InstrCall || i.Op == wasm.InstrCallIndirect {
		return uint32(i.Aux >> 32)
	}
	return 1
}

// SemanticBlock maps one CFG block to its contiguous semantic instructions.
type SemanticBlock struct {
	InstStart uint32
	InstCount uint32
}

// SemanticFunc is the dense semantic-operation portion of RailSSA. Locals,
// drops, and structured delimiters do not become instructions.
type SemanticFunc struct {
	Insts  []SemanticInst
	Args   []FlowValueID
	Blocks []SemanticBlock

	// InstructionMap stores semantic instruction ID + 1, or zero when the
	// original StackInstr is administrative.
	InstructionMap []uint32
	stack          []FlowValueID
}

func (s *SemanticFunc) Operands(id uint32) []FlowValueID {
	instruction := s.Insts[id]
	start := int(instruction.ArgStart)
	return s.Args[start : start+int(instruction.ArgLen)]
}

// BuildSemanticFunc converts stabilized operand-stack value flow into compact
// source-ordered operations. It replays each block once and allocates no
// per-instruction operand slices.
func BuildSemanticFunc(f *StackFunc, cfg *CFG, flow *ValueFlow, reuse *SemanticFunc) (*SemanticFunc, error) {
	if f == nil || cfg == nil || flow == nil || len(flow.Reachable) != len(cfg.Blocks) {
		return nil, fmt.Errorf("railssa: semantic build requires function, CFG, and value flow")
	}
	if reuse == nil {
		reuse = new(SemanticFunc)
	}
	emitCount, argCount := 0, 0
	for blockIndex, record := range cfg.Blocks {
		block := BlockID(blockIndex)
		if !flow.Reachable[block] || record.Flags&BlockExit != 0 {
			continue
		}
		for source := record.InstStart; source < record.InstStart+record.InstCount; source++ {
			arity, emit, err := semanticOperandCount(f, source, f.Instrs[source])
			if err != nil {
				return nil, fmt.Errorf("railssa: semantic instruction %d %s: %w", source, f.Instrs[source].Kind, err)
			}
			if emit {
				emitCount++
				argCount += arity
			}
		}
	}
	if argCount > maxSemanticArgs {
		return nil, fmt.Errorf("railssa: semantic operand budget exceeded")
	}
	insts := resizeClear(reuse.Insts, emitCount)[:0]
	args := resizeClear(reuse.Args, argCount)[:0]
	blocks := resizeClear(reuse.Blocks, len(cfg.Blocks))
	instructionMap := resizeClear(reuse.InstructionMap, len(f.Instrs))
	stack := reuse.stack[:0]
	if cap(stack) < int(flow.MaxStack) {
		stack = make([]FlowValueID, 0, flow.MaxStack)
	}
	*reuse = SemanticFunc{Insts: insts, Args: args, Blocks: blocks, InstructionMap: instructionMap, stack: stack}

	for blockIndex, record := range cfg.Blocks {
		block := BlockID(blockIndex)
		semanticBlock := &reuse.Blocks[blockIndex]
		semanticBlock.InstStart = uint32(len(reuse.Insts))
		if !flow.Reachable[block] || record.Flags&BlockExit != 0 {
			continue
		}
		depth := flow.EntryDepths[block]
		reuse.stack = reuse.stack[:depth]
		copy(reuse.stack, flow.entry(block)[:depth])
		for source := record.InstStart; source < record.InstStart+record.InstCount; source++ {
			instruction := f.Instrs[source]
			arity, emit, err := semanticOperandCount(f, source, instruction)
			if err != nil {
				return nil, fmt.Errorf("railssa: semantic instruction %d %s: %w", source, instruction.Kind, err)
			}
			if arity > len(reuse.stack) {
				return nil, fmt.Errorf("railssa: semantic instruction %d %s needs %d operands at depth %d", source, instruction.Kind, arity, len(reuse.stack))
			}
			if emit {
				if arity > int(^uint16(0)) || len(reuse.Args)+arity > maxSemanticArgs {
					return nil, fmt.Errorf("railssa: semantic operand budget exceeded")
				}
				result := flow.InstructionValues[source]
				aux := instruction.U64()
				if instruction.Kind == wasm.InstrCall || instruction.Kind == wasm.InstrCallIndirect {
					aux = uint64(uint32(aux)) | uint64(f.InstructionResultCount(source, instruction))<<32
				} else if instruction.Kind == wasm.InstrBrOnCast || instruction.Kind == wasm.InstrBrOnCastFail {
					immediate, ok := f.BranchCastImmediateAt(source)
					if !ok {
						return nil, fmt.Errorf("railssa: semantic branch cast %d has no immediate", source)
					}
					aux = immediate.Target
				}
				item := SemanticInst{Aux: aux, ArgStart: uint32(len(reuse.Args)), ArgLen: uint16(arity), Result: result, Source: source, Op: instruction.Kind}
				reuse.Args = append(reuse.Args, reuse.stack[len(reuse.stack)-arity:]...)
				reuse.InstructionMap[source] = uint32(len(reuse.Insts)) + 1
				reuse.Insts = append(reuse.Insts, item)
			}
			if err := replayValueInstruction(f, flow, source, instruction, &reuse.stack); err != nil {
				return nil, fmt.Errorf("railssa: semantic replay instruction %d %s: %w", source, instruction.Kind, err)
			}
		}
		semanticBlock.InstCount = uint32(len(reuse.Insts)) - semanticBlock.InstStart
		if uint32(len(reuse.stack)) != flow.ExitDepths[block] || !equalFlowValues(reuse.stack, flow.exit(block)[:flow.ExitDepths[block]]) {
			return nil, fmt.Errorf("railssa: semantic replay block %d exit differs from value flow", block)
		}
	}
	if err := VerifySemanticFunc(f, cfg, flow, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func semanticOperandCount(f *StackFunc, source uint32, instruction StackInstr) (count int, emit bool, err error) {
	kind := instruction.Kind
	switch {
	case kind == wasm.InstrInvalid || kind == wasm.InstrNop || kind == wasm.InstrBlock || kind == wasm.InstrLoop || instruction.IsElse(),
		kind == wasm.InstrLocalGet || kind == wasm.InstrLocalSet || kind == wasm.InstrLocalTee || kind == wasm.InstrDrop:
		return 0, false, nil
	case kind == wasm.InstrUnreachable || kind == wasm.InstrBr:
		return 0, true, nil
	case kind == wasm.InstrDataDrop || kind == wasm.InstrElemDrop:
		return 0, true, nil
	case kind == wasm.InstrReturn:
		return len(f.Results), true, nil
	case kind == wasm.InstrIf || kind == wasm.InstrBrIf || kind == wasm.InstrBrTable || kind == wasm.InstrBrOnCast || kind == wasm.InstrBrOnCastFail:
		return 1, true, nil
	case kind == wasm.InstrGlobalSet || kind == wasm.InstrMemoryGrow:
		return 1, true, nil
	case kind >= wasm.InstrI32Store && kind <= wasm.InstrI64Store32:
		return 2, true, nil
	case kind == wasm.InstrMemoryCopy || kind == wasm.InstrMemoryFill:
		return 3, true, nil
	case kind == wasm.InstrCall:
		return int(instruction.Params()), true, nil
	case kind == wasm.InstrCallIndirect:
		return int(instruction.Params()) + 1, true, nil
	case kind == wasm.InstrStructNew || kind == wasm.InstrArrayNewFixed:
		return int(instruction.Params()), true, nil
	case kind == wasm.InstrArrayNewData || kind == wasm.InstrArrayNewElem:
		return 2, true, nil
	case kind == wasm.InstrSelect:
		return 3, true, nil
	case wasm.IsSIMDValidationInstructionKind(kind):
		d, ok := f.SIMDImmediateAt(source)
		if !ok {
			return 0, false, fmt.Errorf("SIMD descriptor is unavailable")
		}
		switch d.Class {
		case wasm.SIMDEffectConst:
			return 0, true, nil
		case wasm.SIMDEffectLoad, wasm.SIMDEffectSplat, wasm.SIMDEffectExtract, wasm.SIMDEffectUnary, wasm.SIMDEffectReduceI32:
			return 1, true, nil
		case wasm.SIMDEffectStore, wasm.SIMDEffectLoadLane, wasm.SIMDEffectStoreLane, wasm.SIMDEffectReplace, wasm.SIMDEffectShift, wasm.SIMDEffectBinary:
			return 2, true, nil
		case wasm.SIMDEffectTernary, wasm.SIMDEffectBitselect:
			return 3, true, nil
		default:
			return 0, false, fmt.Errorf("SIMD effect is not classified")
		}
	case scalarBinaryKind(kind) || scalarComparisonKind(kind):
		return 2, true, nil
	case kind == wasm.InstrRefEq:
		return 2, true, nil
	case kind == wasm.InstrStructSet:
		return 2, true, nil
	case kind == wasm.InstrArraySet:
		return 3, true, nil
	case kind == wasm.InstrArrayFill:
		return 4, true, nil
	case kind == wasm.InstrArrayCopy:
		return 5, true, nil
	case kind == wasm.InstrArrayInitData || kind == wasm.InstrArrayInitElem:
		return 4, true, nil
	case kind == wasm.InstrRefIsNull || kind == wasm.InstrRefAsNonNull || kind == wasm.InstrRefTest || kind == wasm.InstrRefCast || kind == wasm.InstrAnyConvertExtern || kind == wasm.InstrExternConvertAny || kind == wasm.InstrRefI31 || kind == wasm.InstrI31GetS || kind == wasm.InstrI31GetU ||
		kind == wasm.InstrStructGet || kind == wasm.InstrStructGetS || kind == wasm.InstrStructGetU || kind == wasm.InstrArrayNewDefault || kind == wasm.InstrArrayLen:
		return 1, true, nil
	case kind == wasm.InstrArrayGet || kind == wasm.InstrArrayGetS || kind == wasm.InstrArrayGetU:
		return 2, true, nil
	case kind == wasm.InstrArrayNew:
		return 2, true, nil
	case kind == wasm.InstrI32Eqz || kind == wasm.InstrI64Eqz,
		kind >= wasm.InstrI32Clz && kind <= wasm.InstrI32Popcnt,
		kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Popcnt,
		kind >= wasm.InstrI32Extend8S && kind <= wasm.InstrI64Extend32S,
		kind >= wasm.InstrF32Abs && kind <= wasm.InstrF32Sqrt,
		kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Sqrt:
		return 1, true, nil
	case kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U:
		return 1, true, nil
	case kind == wasm.InstrGlobalGet || kind == wasm.InstrMemorySize,
		kind == wasm.InstrI32Const || kind == wasm.InstrI64Const || kind == wasm.InstrF32Const || kind == wasm.InstrF64Const || kind == wasm.InstrRefNull || kind == wasm.InstrRefFunc || kind == wasm.InstrStructNewDefault:
		return 0, true, nil
	default:
		if _, _, ok := conversionTypes(kind); ok {
			return 1, true, nil
		}
		return 0, false, fmt.Errorf("instruction is not classified")
	}
}

func replayValueInstruction(f *StackFunc, flow *ValueFlow, source uint32, instruction StackInstr, stack *[]FlowValueID) error {
	kind := instruction.Kind
	popN := func(count int) error {
		if count > len(*stack) {
			return fmt.Errorf("operand stack underflow")
		}
		*stack = (*stack)[:len(*stack)-count]
		return nil
	}
	pushResult := func(ordinal uint32) error {
		value := flow.InstructionValues[source] + FlowValueID(ordinal)
		if value == 0 {
			return fmt.Errorf("instruction has no result value")
		}
		return pushFlow(stack, value, flow.MaxStack)
	}
	switch {
	case kind == wasm.InstrInvalid || kind == wasm.InstrNop || kind == wasm.InstrBlock || kind == wasm.InstrLoop || instruction.IsElse() || kind == wasm.InstrBr || kind == wasm.InstrUnreachable:
		return nil
	case kind == wasm.InstrIf || kind == wasm.InstrBrIf || kind == wasm.InstrBrTable:
		return popN(1)
	case kind == wasm.InstrBrOnCast || kind == wasm.InstrBrOnCastFail:
		if len(*stack) == 0 {
			return fmt.Errorf("branch cast reference operand is unavailable")
		}
		return nil
	case kind == wasm.InstrReturn:
		return nil
	case kind == wasm.InstrDrop || kind == wasm.InstrLocalSet || kind == wasm.InstrGlobalSet:
		return popN(1)
	case kind == wasm.InstrLocalTee:
		return nil
	case kind == wasm.InstrLocalGet:
		value := flow.InstructionValues[source]
		if value == 0 {
			return fmt.Errorf("local.get has no value")
		}
		return pushFlow(stack, value, flow.MaxStack)
	case kind >= wasm.InstrI32Store && kind <= wasm.InstrI64Store32:
		return popN(2)
	case kind == wasm.InstrStructSet:
		return popN(2)
	case kind == wasm.InstrArraySet:
		return popN(3)
	case kind == wasm.InstrArrayFill:
		return popN(4)
	case kind == wasm.InstrArrayCopy:
		return popN(5)
	case kind == wasm.InstrArrayInitData || kind == wasm.InstrArrayInitElem:
		return popN(4)
	case kind == wasm.InstrDataDrop || kind == wasm.InstrElemDrop:
		return nil
	case kind == wasm.InstrMemoryCopy || kind == wasm.InstrMemoryFill:
		return popN(3)
	case kind == wasm.InstrMemoryGrow:
		if err := popN(1); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrRefNull || kind == wasm.InstrRefFunc || kind == wasm.InstrStructNewDefault:
		return pushResult(0)
	case kind == wasm.InstrStructNew || kind == wasm.InstrArrayNewFixed:
		if err := popN(int(instruction.Params())); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrArrayNew:
		if err := popN(2); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrArrayNewData || kind == wasm.InstrArrayNewElem:
		if err := popN(2); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrRefIsNull || kind == wasm.InstrRefAsNonNull || kind == wasm.InstrRefTest || kind == wasm.InstrRefCast || kind == wasm.InstrAnyConvertExtern || kind == wasm.InstrExternConvertAny || kind == wasm.InstrRefI31 || kind == wasm.InstrI31GetS || kind == wasm.InstrI31GetU ||
		kind == wasm.InstrStructGet || kind == wasm.InstrStructGetS || kind == wasm.InstrStructGetU || kind == wasm.InstrArrayNewDefault || kind == wasm.InstrArrayLen:
		if err := popN(1); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrArrayGet || kind == wasm.InstrArrayGetS || kind == wasm.InstrArrayGetU:
		if err := popN(2); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrRefEq:
		if err := popN(2); err != nil {
			return err
		}
		return pushResult(0)
	case kind == wasm.InstrCall || kind == wasm.InstrCallIndirect:
		count := int(instruction.Params())
		if kind == wasm.InstrCallIndirect {
			count++
		}
		if err := popN(count); err != nil {
			return err
		}
		for result := uint32(0); result < f.InstructionResultCount(source, instruction); result++ {
			if err := pushResult(result); err != nil {
				return err
			}
		}
		return nil
	case kind == wasm.InstrSelect:
		if err := popN(3); err != nil {
			return err
		}
		return pushResult(0)
	default:
		arity, emit, err := semanticOperandCount(f, source, instruction)
		if err != nil {
			return err
		}
		if !emit {
			return nil
		}
		if err := popN(arity); err != nil {
			return err
		}
		if flow.InstructionValues[source] != 0 {
			return pushResult(0)
		}
		return nil
	}
}

func VerifySemanticFunc(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc) error {
	if f == nil || cfg == nil || flow == nil || semantic == nil || len(semantic.Blocks) != len(cfg.Blocks) || len(semantic.InstructionMap) != len(f.Instrs) {
		return fmt.Errorf("railssa: malformed semantic function header")
	}
	expectedStart := uint32(0)
	for block, record := range semantic.Blocks {
		if record.InstStart != expectedStart || uint64(record.InstStart)+uint64(record.InstCount) > uint64(len(semantic.Insts)) {
			return fmt.Errorf("railssa: semantic block %d has invalid instruction range", block)
		}
		expectedStart += record.InstCount
	}
	if expectedStart != uint32(len(semantic.Insts)) {
		return fmt.Errorf("railssa: semantic blocks cover %d of %d instructions", expectedStart, len(semantic.Insts))
	}
	for id, instruction := range semantic.Insts {
		if int(instruction.Source) >= len(f.Instrs) || f.Instrs[instruction.Source].Kind != instruction.Op || semantic.InstructionMap[instruction.Source] != uint32(id)+1 {
			return fmt.Errorf("railssa: semantic instruction %d has invalid source mapping", id)
		}
		end := uint64(instruction.ArgStart) + uint64(instruction.ArgLen)
		if end > uint64(len(semantic.Args)) {
			return fmt.Errorf("railssa: semantic instruction %d operand range is invalid", id)
		}
		want, emit, err := semanticOperandCount(f, instruction.Source, f.Instrs[instruction.Source])
		if err != nil || !emit || want != int(instruction.ArgLen) {
			return fmt.Errorf("railssa: semantic instruction %d has invalid arity %d", id, instruction.ArgLen)
		}
		for _, argument := range semantic.Operands(uint32(id)) {
			if argument == 0 || int(argument) >= len(flow.Values) {
				return fmt.Errorf("railssa: semantic instruction %d has invalid operand %d", id, argument)
			}
		}
		for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
			result := instruction.Result + FlowValueID(ordinal)
			if int(result) >= len(flow.Values) || flow.Values[result].Kind != FlowValueInstruction || flow.Values[result].Instr != instruction.Source {
				return fmt.Errorf("railssa: semantic instruction %d has invalid result %d", id, result)
			}
		}
	}
	return nil
}

// DumpSemantic emits a deterministic, pointer-free debugging form.
func DumpSemantic(semantic *SemanticFunc) string {
	if semantic == nil {
		return ""
	}
	var out strings.Builder
	for block, record := range semantic.Blocks {
		fmt.Fprintf(&out, "b%d:\n", block)
		for id := record.InstStart; id < record.InstStart+record.InstCount; id++ {
			instruction := semantic.Insts[id]
			if instruction.Result != 0 {
				fmt.Fprintf(&out, "  v%d", instruction.Result)
				for ordinal := uint32(1); ordinal < instruction.ResultCount(); ordinal++ {
					fmt.Fprintf(&out, ",v%d", instruction.Result+FlowValueID(ordinal))
				}
				fmt.Fprintf(&out, " = %s", instruction.Op)
			} else {
				fmt.Fprintf(&out, "  %s", instruction.Op)
			}
			for _, argument := range semantic.Operands(id) {
				fmt.Fprintf(&out, " v%d", argument)
			}
			fmt.Fprintf(&out, " @%d\n", instruction.Source)
		}
	}
	return out.String()
}
