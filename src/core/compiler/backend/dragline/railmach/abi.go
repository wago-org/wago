package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type ABIClass uint8

const (
	ABIGeneral ABIClass = iota + 1
	ABILeafScalar
	ABILeafFP
	ABINoCollectLeaf
	ABITinyDirect
	ABIPreparedInt
	ABIPreparedIndirect
)

type ABIContract struct {
	GPRClobbers uint64
	FPRClobbers uint64
	CalleeGPRs  uint64
	CalleeFPRs  uint64
	Params      uint16
	Results     uint16
	Class       ABIClass
	// RegisterResults is the source-ordered result prefix returned in private
	// result GPRs. Remaining results use the caller-owned result area.
	RegisterResults uint8
	HasCall         bool
	MayCollect      bool
}

// PrivateResultRegisters is the source-ordered GPR result prefix shared by
// both native targets. Floating values carry their raw bits in these GPRs.
const PrivateResultRegisters = 4

type CallContract struct {
	Instruction  uint32
	Callee       uint32
	GPRClobbers  uint64
	FPRClobbers  uint64
	Class        ABIClass
	Conservative bool
	_            [2]byte
}

func AnalyzeABI(f *Func, allocation *GreedyAllocation, metadata *railssa.Metadata, importedFunctions uint32) (ABIContract, []CallContract, error) {
	if err := verifyAllocationReusingScratch(f, &allocation.Allocation, DefaultGreedyConfig(f.Target).Linear); err != nil {
		return ABIContract{}, nil, err
	}
	return analyzeVerifiedABI(f, allocation, metadata, importedFunctions)
}

// AnalyzeVerifiedABI consumes an allocation returned by a verified allocator
// without replaying that complete verifier at the adjacent ABI-analysis
// boundary. The ABI contract is still derived independently from the product.
func AnalyzeVerifiedABI(f *Func, allocation *GreedyAllocation, metadata *railssa.Metadata, importedFunctions uint32) (ABIContract, []CallContract, error) {
	if f == nil || allocation == nil || metadata == nil {
		return ABIContract{}, nil, fmt.Errorf("railmach: ABI analysis requires verified products")
	}
	return analyzeVerifiedABI(f, allocation, metadata, importedFunctions)
}

func analyzeVerifiedABI(f *Func, allocation *GreedyAllocation, metadata *railssa.Metadata, importedFunctions uint32) (ABIContract, []CallContract, error) {
	if len(f.Results) > int(^uint16(0)) {
		return ABIContract{}, nil, fmt.Errorf("railmach: %d results exceed private ABI limit", len(f.Results))
	}
	registerResults := min(len(f.Results), PrivateResultRegisters)
	contract := ABIContract{Class: ABILeafScalar, Params: f.ParamCount, Results: uint16(len(f.Results)), RegisterResults: uint8(registerResults)}
	var calls []CallContract
	usesFP := false
	for reg, location := range allocation.Locations {
		if reg == 0 || location.Kind != LocationRegister {
			continue
		}
		mask := uint64(1) << location.Index
		if location.Bank == BankFPR {
			usesFP = true
			contract.FPRClobbers |= mask
		} else {
			contract.GPRClobbers |= mask
		}
	}
	// Regional fragments temporarily own a physical register even though the
	// durable allocation remains a spill. Account for that physical write in
	// the function contract just like an ordinary register allocation.
	for _, fragment := range allocation.Fragments {
		location := fragment.Location
		if location.Kind != LocationRegister || location.Index >= 64 {
			continue
		}
		mask := uint64(1) << location.Index
		if location.Bank == BankFPR {
			usesFP = true
			contract.FPRClobbers |= mask
		} else {
			contract.GPRClobbers |= mask
		}
	}
	// Fixed-use repair writes the constrained physical register even when the
	// value's durable allocation is elsewhere.
	for _, move := range allocation.FixedMoves {
		if move.Physical >= 64 {
			continue
		}
		if move.Bank == BankFPR {
			usesFP = true
			contract.FPRClobbers |= uint64(1) << move.Physical
		} else {
			contract.GPRClobbers |= uint64(1) << move.Physical
		}
	}
	// The private result convention materializes a bounded source-ordered
	// prefix in GPRs (floating values retain their bits there as well).
	if contract.RegisterResults != 0 {
		contract.GPRClobbers |= lowMask(contract.RegisterResults)
	}
	config := DefaultGreedyConfig(f.Target)
	contract.CalleeGPRs = contract.GPRClobbers &^ lowMask(config.CallerGPRs)
	contract.CalleeFPRs = contract.FPRClobbers &^ lowMask(config.CallerFPRs)
	for instructionID, instruction := range f.Insts {
		if !IsCall(instruction.Op) {
			continue
		}
		contract.HasCall = true
		meta := metadata.Instructions[instruction.Source]
		contract.MayCollect = contract.MayCollect || meta.Flags&railssa.EffectMayCollect != 0
		call := CallContract{Instruction: uint32(instructionID), Callee: uint32(instruction.Aux), Class: ABIGeneral, Conservative: true, GPRClobbers: lowMask(config.CallerGPRs), FPRClobbers: lowMask(config.CallerFPRs)}
		if instruction.Op == wasm.InstrCall && call.Callee >= importedFunctions {
			call.Conservative = false
		}
		calls = append(calls, call)
	}
	switch {
	case !contract.HasCall && len(f.Insts) <= 4 && hasDirectRegisterParams(f, allocation):
		contract.Class = ABITinyDirect
	case directPreparedIntegerContract(f, allocation, contract):
		contract.Class = ABIPreparedInt
	case !contract.HasCall && usesFP:
		contract.Class = ABILeafFP
	case !contract.HasCall && !contract.MayCollect:
		contract.Class = ABINoCollectLeaf
	case contract.HasCall:
		contract.Class = ABIGeneral
	}
	return contract, calls, nil
}

func directPreparedIntegerContract(f *Func, allocation *GreedyAllocation, contract ABIContract) bool {
	maxInstructions := 8
	if f.Target == TargetARM64 && !contract.HasCall {
		maxInstructions = 48
	}
	if f.ParamCount == 0 || f.ParamCount > 3 || len(f.Results) > 1 || len(f.Insts) > maxInstructions {
		return false
	}
	if f.ParamCount == 1 && !hasSingleDirectRegisterParam(f, allocation) {
		return false
	}
	if f.ParamCount > 1 && (f.Target != TargetARM64 || contract.HasCall || !hasDirectRegisterParams(f, allocation)) {
		return false
	}
	if len(f.Results) == 1 && f.VRegs[f.Results[0]].Bank != BankGPR {
		return false
	}
	for _, instruction := range f.Insts {
		switch instruction.Op {
		case wasm.InstrCall, wasm.InstrI32Const, wasm.InstrI64Const,
			wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or,
			wasm.InstrI32Xor, wasm.InstrI64Xor:
		case wasm.InstrI32Mul, wasm.InstrI64Mul,
			wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS, wasm.InstrI32ShrU, wasm.InstrI64ShrU,
			wasm.InstrI32Rotl, wasm.InstrI64Rotl, wasm.InstrI32Rotr, wasm.InstrI64Rotr,
			wasm.InstrI32Eqz, wasm.InstrI64Eqz,
			wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
			wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
			wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
			wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
			wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU,
			wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U, wasm.InstrI32WrapI64,
			wasm.InstrSelect, wasm.InstrIf, wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn:
			if f.Target != TargetARM64 || contract.HasCall {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func hasSingleDirectRegisterParam(f *Func, allocation *GreedyAllocation) bool {
	if f.ParamCount != 1 {
		return false
	}
	for value := VReg(1); int(value) < len(f.VRegs); value++ {
		data := f.VRegs[value]
		if data.Flags&VRegInitial == 0 || data.InitialLocal != 0 {
			continue
		}
		location := allocation.Locations[value]
		return data.Bank == BankGPR && location.Kind == LocationRegister && location.Bank == BankGPR
	}
	return false
}

func hasDirectRegisterParams(f *Func, allocation *GreedyAllocation) bool {
	if f.ParamCount > 8 {
		return false
	}
	for local := uint16(0); local < f.ParamCount; local++ {
		found := false
		for value := VReg(1); int(value) < len(f.VRegs); value++ {
			data := f.VRegs[value]
			if data.Flags&VRegInitial == 0 || data.InitialLocal != local {
				continue
			}
			location := allocation.Locations[value]
			if data.Bank != BankGPR || location.Kind != LocationRegister || location.Bank != BankGPR || location.Index != local {
				return false
			}
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

// PropagateCallClobbers completes a function contract with the physical
// clobbers of calls it performs. Callee-saved registers are restored by the
// private ABI, so only target caller registers propagate transitively.
func PropagateCallClobbers(contract *ABIContract, calls []CallContract, config GreedyConfig) {
	if contract == nil {
		return
	}
	for _, call := range calls {
		contract.GPRClobbers |= call.GPRClobbers & lowMask(config.CallerGPRs)
		contract.FPRClobbers |= call.FPRClobbers & lowMask(config.CallerFPRs)
	}
	contract.CalleeGPRs = contract.GPRClobbers &^ lowMask(config.CallerGPRs)
	contract.CalleeFPRs = contract.FPRClobbers &^ lowMask(config.CallerFPRs)
}

func RefineCallContracts(calls []CallContract, module []ABIContract, importedFunctions uint32) uint32 {
	refined := uint32(0)
	for index := range calls {
		call := &calls[index]
		if call.Conservative || call.Callee < importedFunctions {
			continue
		}
		local := call.Callee - importedFunctions
		if int(local) >= len(module) || module[local].Class == 0 {
			continue
		}
		callee := module[local]
		call.GPRClobbers, call.FPRClobbers, call.Class, call.Conservative = callee.GPRClobbers, callee.FPRClobbers, callee.Class, false
		refined++
	}
	return refined
}

func lowMask(count uint8) uint64 {
	if count >= 64 {
		return ^uint64(0)
	}
	return uint64(1)<<count - 1
}

type FrameRequirements struct {
	SpillSlots      uint16
	RootSlots       uint16
	CalleeGPRs      uint64
	CalleeFPRs      uint64
	CallAreaBytes   uint32
	ResultAreaBytes uint32
	RuntimeBytes    uint32
}

type FrameLayout struct {
	SpillBytes       uint32
	RootBytes        uint32
	CalleeSaveBytes  uint32
	CallAreaOffset   uint32
	ResultAreaOffset uint32
	RuntimeOffset    uint32
	TotalBytes       uint32
}

func ComposeFrame(requirements FrameRequirements) (FrameLayout, error) {
	total := uint64(requirements.SpillSlots)*8 + uint64(requirements.RootSlots)*8 + uint64(popcount64(requirements.CalleeGPRs)+popcount64(requirements.CalleeFPRs))*8
	total = (total + 15) &^ 15
	total += uint64(requirements.CallAreaBytes) + uint64(requirements.ResultAreaBytes) + uint64(requirements.RuntimeBytes)
	total = (total + 15) &^ 15
	if total > uint64(^uint32(0)) {
		return FrameLayout{}, fmt.Errorf("railmach: frame size overflow")
	}
	layout := FrameLayout{}
	layout.SpillBytes = uint32(requirements.SpillSlots) * 8
	layout.RootBytes = uint32(requirements.RootSlots) * 8
	layout.CalleeSaveBytes = uint32(popcount64(requirements.CalleeGPRs)+popcount64(requirements.CalleeFPRs)) * 8
	offset := align16(layout.SpillBytes + layout.RootBytes + layout.CalleeSaveBytes)
	layout.CallAreaOffset = offset
	offset += requirements.CallAreaBytes
	layout.ResultAreaOffset = offset
	offset += requirements.ResultAreaBytes
	layout.RuntimeOffset = offset
	offset += requirements.RuntimeBytes
	layout.TotalBytes = align16(offset)
	return layout, nil
}

func FrameForAllocation(contract ABIContract, allocation *GreedyAllocation, maxCallSlots uint32) (FrameRequirements, FrameLayout, error) {
	if allocation == nil {
		return FrameRequirements{}, FrameLayout{}, fmt.Errorf("railmach: frame requires an allocation")
	}
	if contract.RegisterResults > PrivateResultRegisters || uint16(contract.RegisterResults) > contract.Results {
		return FrameRequirements{}, FrameLayout{}, fmt.Errorf("railmach: invalid private result convention: %d register results for %d results", contract.RegisterResults, contract.Results)
	}
	requirements := FrameRequirements{SpillSlots: allocation.SpillSlots, CalleeGPRs: contract.CalleeGPRs, CalleeFPRs: contract.CalleeFPRs}
	if maxCallSlots > ^uint32(0)/8 {
		return FrameRequirements{}, FrameLayout{}, fmt.Errorf("railmach: outgoing call area overflow")
	}
	requirements.CallAreaBytes = maxCallSlots * 8
	if contract.Results > 1 {
		// Stage every result before assigning the fixed register prefix. This
		// makes the return a verified parallel transfer even when allocations
		// overlap the result registers. Overflow values are then copied to the
		// caller-owned result vector.
		requirements.ResultAreaBytes = uint32(contract.Results) * 8
	}
	if contract.Results > uint16(contract.RegisterResults) {
		// Preserve the hidden caller result-vector pointer across the body.
		requirements.RuntimeBytes = 8
	}
	layout, err := ComposeFrame(requirements)
	return requirements, layout, err
}

func align16(value uint32) uint32 { return (value + 15) &^ 15 }

func popcount64(value uint64) int {
	count := 0
	for value != 0 {
		value &= value - 1
		count++
	}
	return count
}
