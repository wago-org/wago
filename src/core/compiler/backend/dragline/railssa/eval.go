package railssa

import (
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var (
	errIntegerDivideTrap               = errors.New("integer divide trap")
	errUnsupportedPureIntegerOperation = errors.New("unsupported pure integer operation")
)

// EvalSemanticInteger executes the pure integer/control subset of dense
// RailSSA. It is a construction oracle, not a runtime interpreter: memory,
// globals, calls, and floats intentionally return an explicit error.
func EvalSemanticInteger(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, params []uint64) (uint64, error) {
	if err := VerifySemanticFunc(f, cfg, flow, semantic); err != nil {
		return 0, err
	}
	if len(params) != len(f.Params) {
		return 0, fmt.Errorf("railssa: got %d parameters, want %d", len(params), len(f.Params))
	}
	values := make([]uint64, len(flow.Values))
	for id, value := range flow.Values {
		if value.Kind != FlowValueInitialLocal {
			continue
		}
		if int(value.Local) < len(params) {
			values[id] = normalizeInteger(params[value.Local], value.Type)
		}
	}
	block := BlockID(0)
	// A malformed graph must not hang the oracle. Valid terminating test cases
	// have ample room even for moderately long loops.
	budget := len(cfg.Blocks)*1024 + len(semantic.Insts)*1024 + 1
	for step := 0; step < budget; step++ {
		if int(block) >= len(cfg.Blocks) || !flow.Reachable[block] {
			return 0, fmt.Errorf("railssa: evaluator entered invalid block %d", block)
		}
		if cfg.Blocks[block].Flags&BlockExit != 0 {
			if len(f.Results) == 0 {
				return 0, nil
			}
			result := flow.entry(block)[0]
			return normalizeInteger(values[result], f.Results[0]), nil
		}
		var condition uint32
		tableSource := ^uint32(0)
		record := semantic.Blocks[block]
		for id := record.InstStart; id < record.InstStart+record.InstCount; id++ {
			instruction := semantic.Insts[id]
			args := semantic.Operands(id)
			if instruction.Op == wasm.InstrIf || instruction.Op == wasm.InstrBrIf {
				condition = uint32(values[args[0]])
				continue
			}
			if instruction.Op == wasm.InstrBr || instruction.Op == wasm.InstrReturn {
				continue
			}
			if instruction.Op == wasm.InstrBrTable {
				tableSource = instruction.Source
				condition = uint32(values[args[0]])
				continue
			}
			if instruction.Op == wasm.InstrUnreachable {
				return 0, fmt.Errorf("railssa: unreachable trap at instruction %d", instruction.Source)
			}
			result, err := evalIntegerInstruction(instruction, args, values)
			if err != nil {
				return 0, fmt.Errorf("railssa: evaluate instruction %d %s: %w", instruction.Source, instruction.Op, err)
			}
			if instruction.Result != 0 {
				values[instruction.Result] = normalizeInteger(result, flow.Values[instruction.Result].Type)
			}
		}
		var edgeIndex uint32
		var err error
		if tableSource != ^uint32(0) {
			edgeIndex, err = selectIntegerTableEdge(f, cfg, block, tableSource, condition)
		} else {
			edgeIndex, err = selectIntegerEdge(cfg, block, condition)
		}
		if err != nil {
			return 0, err
		}
		for _, argument := range flow.EdgeArgs {
			if argument.Edge == edgeIndex {
				values[argument.Param] = values[argument.Argument]
			}
		}
		block = cfg.Edges[edgeIndex].To
	}
	return 0, fmt.Errorf("railssa: semantic integer evaluator exceeded %d steps", budget)
}

func selectIntegerTableEdge(f *StackFunc, cfg *CFG, block BlockID, source, selector uint32) (uint32, error) {
	if int(source) >= len(f.Instrs) || f.Instrs[source].Kind != wasm.InstrBrTable {
		return 0, fmt.Errorf("railssa: block %d has invalid br_table source %d", block, source)
	}
	labels := f.Instrs[source].Labels(f)
	if len(labels) == 0 {
		return 0, fmt.Errorf("railssa: br_table at %d has no default label", source)
	}
	choice := len(labels) - 1
	if uint64(selector) < uint64(choice) {
		choice = int(selector)
	}
	label := labels[choice]
	region := cfg.Blocks[block].Region
	for depth := uint32(0); depth < label && region != NoRegion; depth++ {
		region = f.Regions[region].Parent
	}
	targetStart := uint32(len(f.Instrs))
	if region != NoRegion {
		target := f.Regions[region]
		if target.Kind == wasm.InstrLoop {
			targetStart = target.StartInstr + 1
		} else {
			targetStart = target.EndInstr + 1
		}
	}
	targetBlock := BlockID(len(cfg.Blocks))
	for id, candidate := range cfg.Blocks {
		if candidate.InstStart == targetStart {
			targetBlock = BlockID(id)
			break
		}
	}
	if int(targetBlock) == len(cfg.Blocks) {
		return 0, fmt.Errorf("railssa: br_table at %d has no target block for label %d", source, label)
	}
	for index, edge := range cfg.Edges {
		if edge.From == block && edge.To == targetBlock && edge.Kind == EdgeTable {
			return uint32(index), nil
		}
	}
	return 0, fmt.Errorf("railssa: br_table at %d has no edge from block %d to %d", source, block, targetBlock)
}

func selectIntegerEdge(cfg *CFG, block BlockID, condition uint32) (uint32, error) {
	sole, successorCount := uint32(0), 0
	for index, edge := range cfg.Edges {
		if edge.From == block {
			sole, successorCount = uint32(index), successorCount+1
		}
	}
	if successorCount == 1 {
		if cfg.Edges[sole].Kind == EdgeTable {
			return 0, fmt.Errorf("railssa: semantic integer evaluator does not support table edge from block %d", block)
		}
		return sole, nil
	}
	var candidate uint32
	found := false
	for index, edge := range cfg.Edges {
		if edge.From != block {
			continue
		}
		match := edge.Kind != EdgeTrue && edge.Kind != EdgeFalse || edge.Kind == EdgeTrue && condition != 0 || edge.Kind == EdgeFalse && condition == 0
		if !match {
			continue
		}
		if edge.Kind == EdgeTable {
			return 0, fmt.Errorf("railssa: semantic integer evaluator does not support table edge from block %d", block)
		}
		if found {
			return 0, fmt.Errorf("railssa: block %d has ambiguous executable edges", block)
		}
		candidate, found = uint32(index), true
	}
	if !found {
		return 0, fmt.Errorf("railssa: block %d has no executable edge", block)
	}
	return candidate, nil
}

func evalIntegerInstruction(instruction SemanticInst, args []FlowValueID, values []uint64) (uint64, error) {
	return evalIntegerInstructionFromFacts(instruction, args, values, nil)
}

func evalIntegerInstructionFromFacts(instruction SemanticInst, args []FlowValueID, values []uint64, facts *SimplifyResult) (uint64, error) {
	arg := func(index int) uint64 {
		if facts != nil {
			return facts.IntegerFactAt(args[index]).Min
		}
		return values[args[index]]
	}
	switch instruction.Op {
	case wasm.InstrI32Const:
		return uint64(uint32(instruction.Aux)), nil
	case wasm.InstrI64Const:
		return instruction.Aux, nil
	case wasm.InstrI32Eqz:
		return boolInteger(uint32(arg(0)) == 0), nil
	case wasm.InstrI64Eqz:
		return boolInteger(arg(0) == 0), nil
	case wasm.InstrI32Eq:
		return boolInteger(uint32(arg(0)) == uint32(arg(1))), nil
	case wasm.InstrI32Ne:
		return boolInteger(uint32(arg(0)) != uint32(arg(1))), nil
	case wasm.InstrI32LtS:
		return boolInteger(int32(arg(0)) < int32(arg(1))), nil
	case wasm.InstrI32LtU:
		return boolInteger(uint32(arg(0)) < uint32(arg(1))), nil
	case wasm.InstrI32GtS:
		return boolInteger(int32(arg(0)) > int32(arg(1))), nil
	case wasm.InstrI32GtU:
		return boolInteger(uint32(arg(0)) > uint32(arg(1))), nil
	case wasm.InstrI32LeS:
		return boolInteger(int32(arg(0)) <= int32(arg(1))), nil
	case wasm.InstrI32LeU:
		return boolInteger(uint32(arg(0)) <= uint32(arg(1))), nil
	case wasm.InstrI32GeS:
		return boolInteger(int32(arg(0)) >= int32(arg(1))), nil
	case wasm.InstrI32GeU:
		return boolInteger(uint32(arg(0)) >= uint32(arg(1))), nil
	case wasm.InstrI64Eq:
		return boolInteger(arg(0) == arg(1)), nil
	case wasm.InstrI64Ne:
		return boolInteger(arg(0) != arg(1)), nil
	case wasm.InstrI64LtS:
		return boolInteger(int64(arg(0)) < int64(arg(1))), nil
	case wasm.InstrI64LtU:
		return boolInteger(arg(0) < arg(1)), nil
	case wasm.InstrI64GtS:
		return boolInteger(int64(arg(0)) > int64(arg(1))), nil
	case wasm.InstrI64GtU:
		return boolInteger(arg(0) > arg(1)), nil
	case wasm.InstrI64LeS:
		return boolInteger(int64(arg(0)) <= int64(arg(1))), nil
	case wasm.InstrI64LeU:
		return boolInteger(arg(0) <= arg(1)), nil
	case wasm.InstrI64GeS:
		return boolInteger(int64(arg(0)) >= int64(arg(1))), nil
	case wasm.InstrI64GeU:
		return boolInteger(arg(0) >= arg(1)), nil
	case wasm.InstrI32Clz:
		return uint64(bits.LeadingZeros32(uint32(arg(0)))), nil
	case wasm.InstrI32Ctz:
		return uint64(bits.TrailingZeros32(uint32(arg(0)))), nil
	case wasm.InstrI32Popcnt:
		return uint64(bits.OnesCount32(uint32(arg(0)))), nil
	case wasm.InstrI64Clz:
		return uint64(bits.LeadingZeros64(arg(0))), nil
	case wasm.InstrI64Ctz:
		return uint64(bits.TrailingZeros64(arg(0))), nil
	case wasm.InstrI64Popcnt:
		return uint64(bits.OnesCount64(arg(0))), nil
	case wasm.InstrI32Add:
		return uint64(uint32(arg(0)) + uint32(arg(1))), nil
	case wasm.InstrI32Sub:
		return uint64(uint32(arg(0)) - uint32(arg(1))), nil
	case wasm.InstrI32Mul:
		return uint64(uint32(arg(0)) * uint32(arg(1))), nil
	case wasm.InstrI32DivS:
		a, b := int32(arg(0)), int32(arg(1))
		if b == 0 || a == math.MinInt32 && b == -1 {
			return 0, errIntegerDivideTrap
		}
		return uint64(uint32(a / b)), nil
	case wasm.InstrI32DivU:
		a, b := uint32(arg(0)), uint32(arg(1))
		if b == 0 {
			return 0, errIntegerDivideTrap
		}
		return uint64(a / b), nil
	case wasm.InstrI32RemS:
		a, b := int32(arg(0)), int32(arg(1))
		if b == 0 {
			return 0, errIntegerDivideTrap
		}
		return uint64(uint32(a % b)), nil
	case wasm.InstrI32RemU:
		a, b := uint32(arg(0)), uint32(arg(1))
		if b == 0 {
			return 0, errIntegerDivideTrap
		}
		return uint64(a % b), nil
	case wasm.InstrI32And:
		return uint64(uint32(arg(0)) & uint32(arg(1))), nil
	case wasm.InstrI32Or:
		return uint64(uint32(arg(0)) | uint32(arg(1))), nil
	case wasm.InstrI32Xor:
		return uint64(uint32(arg(0)) ^ uint32(arg(1))), nil
	case wasm.InstrI32Shl:
		return uint64(uint32(arg(0)) << (uint32(arg(1)) & 31)), nil
	case wasm.InstrI32ShrS:
		return uint64(uint32(int32(arg(0)) >> (uint32(arg(1)) & 31))), nil
	case wasm.InstrI32ShrU:
		return uint64(uint32(arg(0)) >> (uint32(arg(1)) & 31)), nil
	case wasm.InstrI32Rotl:
		return uint64(bits.RotateLeft32(uint32(arg(0)), int(uint32(arg(1))))), nil
	case wasm.InstrI32Rotr:
		return uint64(bits.RotateLeft32(uint32(arg(0)), -int(uint32(arg(1))))), nil
	case wasm.InstrI64Add:
		return arg(0) + arg(1), nil
	case wasm.InstrI64Sub:
		return arg(0) - arg(1), nil
	case wasm.InstrI64Mul:
		return arg(0) * arg(1), nil
	case wasm.InstrI64DivS:
		a, b := int64(arg(0)), int64(arg(1))
		if b == 0 || a == math.MinInt64 && b == -1 {
			return 0, errIntegerDivideTrap
		}
		return uint64(a / b), nil
	case wasm.InstrI64DivU:
		if arg(1) == 0 {
			return 0, errIntegerDivideTrap
		}
		return arg(0) / arg(1), nil
	case wasm.InstrI64RemS:
		a, b := int64(arg(0)), int64(arg(1))
		if b == 0 {
			return 0, errIntegerDivideTrap
		}
		return uint64(a % b), nil
	case wasm.InstrI64RemU:
		if arg(1) == 0 {
			return 0, errIntegerDivideTrap
		}
		return arg(0) % arg(1), nil
	case wasm.InstrI64And:
		return arg(0) & arg(1), nil
	case wasm.InstrI64Or:
		return arg(0) | arg(1), nil
	case wasm.InstrI64Xor:
		return arg(0) ^ arg(1), nil
	case wasm.InstrI64Shl:
		return arg(0) << (arg(1) & 63), nil
	case wasm.InstrI64ShrS:
		return uint64(int64(arg(0)) >> (arg(1) & 63)), nil
	case wasm.InstrI64ShrU:
		return arg(0) >> (arg(1) & 63), nil
	case wasm.InstrI64Rotl:
		return bits.RotateLeft64(arg(0), int(arg(1))), nil
	case wasm.InstrI64Rotr:
		return bits.RotateLeft64(arg(0), -int(arg(1))), nil
	case wasm.InstrI32WrapI64:
		return uint64(uint32(arg(0))), nil
	case wasm.InstrI64ExtendI32S:
		return uint64(int64(int32(arg(0)))), nil
	case wasm.InstrI64ExtendI32U:
		return uint64(uint32(arg(0))), nil
	case wasm.InstrI32Extend8S:
		return uint64(uint32(int32(int8(arg(0))))), nil
	case wasm.InstrI32Extend16S:
		return uint64(uint32(int32(int16(arg(0))))), nil
	case wasm.InstrI64Extend8S:
		return uint64(int64(int8(arg(0)))), nil
	case wasm.InstrI64Extend16S:
		return uint64(int64(int16(arg(0)))), nil
	case wasm.InstrI64Extend32S:
		return uint64(int64(int32(arg(0)))), nil
	case wasm.InstrSelect:
		if uint32(arg(2)) != 0 {
			return arg(0), nil
		}
		return arg(1), nil
	default:
		return 0, errUnsupportedPureIntegerOperation
	}
}

func boolInteger(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func normalizeInteger(value uint64, typ wasm.ValType) uint64 {
	if typ == wasm.I32 {
		return uint64(uint32(value))
	}
	return value
}
