// Package railmach implements Dragline's dense machine SSA.
package railmach

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type Target uint8

const (
	TargetInvalid Target = iota
	TargetAMD64
	TargetARM64
)

func (t Target) String() string {
	switch t {
	case TargetAMD64:
		return "amd64"
	case TargetARM64:
		return "arm64"
	default:
		return "invalid"
	}
}

type MachineType uint8

const (
	TypeInvalid MachineType = iota
	TypeI32
	TypeI64
	TypeF32
	TypeF64
	TypeRef
)

func (t MachineType) IsWideGPR() bool { return t == TypeI64 || t == TypeRef }

type Bank uint8

const (
	BankInvalid Bank = iota
	BankGPR
	BankFPR
	BankFlags
)

type VReg uint32

// IsCall reports machine operations that cross a native ABI boundary. Runtime
// helpers are explicit semantic operations, but allocation and scheduling must
// treat them exactly like calls for clobbers and live-range placement.
func IsCall(kind wasm.InstrKind) bool {
	return kind == wasm.InstrCall || kind == wasm.InstrCallIndirect || kind == wasm.InstrStructNew || kind == wasm.InstrStructNewDefault ||
		kind == wasm.InstrStructGet || kind == wasm.InstrStructGetS || kind == wasm.InstrStructGetU || kind == wasm.InstrStructSet ||
		kind == wasm.InstrRefTest || kind == wasm.InstrRefCast || kind == wasm.InstrBrOnCast || kind == wasm.InstrBrOnCastFail || kind == wasm.InstrAnyConvertExtern || kind == wasm.InstrExternConvertAny ||
		kind == wasm.InstrArrayNew || kind == wasm.InstrArrayNewDefault || kind == wasm.InstrArrayNewFixed || kind == wasm.InstrArrayNewData || kind == wasm.InstrArrayNewElem || kind == wasm.InstrArrayGet || kind == wasm.InstrArrayGetS || kind == wasm.InstrArrayGetU || kind == wasm.InstrArraySet || kind == wasm.InstrArrayLen || kind == wasm.InstrArrayFill || kind == wasm.InstrArrayCopy || kind == wasm.InstrArrayInitData || kind == wasm.InstrArrayInitElem || kind == wasm.InstrElemDrop
}

const NoFixedReg uint8 = 0xff

const (
	OperandUse uint8 = 1 << iota
	OperandFixed
	OperandTied
	// OperandColdRemat makes this particular use independent of the value's
	// allocated location. The finalizer reconstructs the value in a scratch
	// register immediately before the consumer.
	OperandColdRemat
)

// Operand carries a virtual-register use and its target constraint. Fixed is a
// bank-relative physical register number, or NoFixedReg.
type Operand struct {
	Reg    VReg
	Fixed  uint8
	Bank   Bank
	Flags  uint8
	TiedTo uint8
	_      [3]byte
}

type VRegData struct {
	Def          uint32
	InitialLocal uint16
	Type         MachineType
	Bank         Bank
	Flags        uint8
	_            uint8
}

const (
	VRegInitial uint8 = 1 << iota
	VRegBlockParam
	VRegRematerializable
	VRegElided
	VRegColdRematerializable
)

// Inst is a 24-byte source-stable machine-SSA instruction. Target-legal
// operands live in the shared operand slab.
type Inst struct {
	Aux          uint64
	OperandStart uint32
	Result       VReg
	Source       uint32
	OperandCount uint16
	Op           wasm.InstrKind
}

// ResultCount returns the number of consecutive VReg definitions. Multi-result
// call arity occupies Aux's high word; all other machine operations are scalar.
func (i Inst) ResultCount() uint32 {
	if i.Result == 0 {
		return 0
	}
	if i.Op == wasm.InstrCall || i.Op == wasm.InstrCallIndirect {
		return uint32(i.Aux >> 32)
	}
	return 1
}

type Block struct {
	InstStart uint32
	InstCount uint32
	Region    railssa.RegionID
	Flags     uint16
	_         uint16
	Weight    uint32
}

type EdgeTransfer struct {
	Src    VReg
	Dst    VReg
	Edge   uint32
	Weight uint32
	From   railssa.BlockID
	To     railssa.BlockID
	Type   MachineType
	_      [3]byte
}

type Edge struct {
	From railssa.BlockID
	To   railssa.BlockID
	Kind railssa.EdgeKind
	_    [3]byte
}

// Func owns one function's machine SSA and contains no Go pointers in its hot
// instruction, operand, vreg, block, or transfer records.
type Func struct {
	Target     Target
	ParamCount uint16
	Insts      []Inst
	Operands   []Operand
	VRegs      []VRegData
	Blocks     []Block
	Edges      []Edge
	Transfers  []EdgeTransfer
	Results    []VReg
}

func (f *Func) InstructionOperands(id uint32) []Operand {
	instruction := f.Insts[id]
	start := int(instruction.OperandStart)
	return f.Operands[start : start+int(instruction.OperandCount)]
}

func Build(target Target, cfg *railssa.CFG, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, reuse *Func) (*Func, error) {
	return BuildWithSimplify(target, cfg, flow, semantic, nil, reuse)
}

func BuildWithSimplify(target Target, cfg *railssa.CFG, flow *railssa.ValueFlow, semantic *railssa.SemanticFunc, simplified *railssa.SimplifyResult, reuse *Func) (*Func, error) {
	if target != TargetAMD64 && target != TargetARM64 {
		return nil, fmt.Errorf("railmach: unsupported target %s", target)
	}
	if cfg == nil || flow == nil || semantic == nil || len(semantic.Blocks) != len(cfg.Blocks) {
		return nil, fmt.Errorf("railmach: build requires CFG, value flow, and semantic SSA")
	}
	if simplified != nil && len(simplified.Aliases) != len(flow.Values) {
		return nil, fmt.Errorf("railmach: simplified alias table does not match value flow")
	}
	if reuse == nil {
		reuse = new(Func)
	}
	insts := resize(reuse.Insts, len(semantic.Insts))[:0]
	operands := resize(reuse.Operands, len(semantic.Args))[:0]
	vregs := resize(reuse.VRegs, len(flow.Values))
	blocks := resize(reuse.Blocks, len(cfg.Blocks))
	edges := resize(reuse.Edges, len(cfg.Edges))
	transfers := resize(reuse.Transfers, len(flow.EdgeArgs))[:0]
	results := reuse.Results[:0]
	*reuse = Func{Target: target, ParamCount: flow.ParamCount, Insts: insts, Operands: operands, VRegs: vregs, Blocks: blocks, Edges: edges, Transfers: transfers, Results: results}
	for id, edge := range cfg.Edges {
		reuse.Edges[id] = Edge{From: edge.From, To: edge.To, Kind: edge.Kind}
	}

	for id, value := range flow.Values {
		if id == 0 {
			continue
		}
		typ, bank, err := machineType(value.Type)
		if err != nil {
			return nil, fmt.Errorf("railmach: value %d: %w", id, err)
		}
		data := VRegData{Type: typ, Bank: bank}
		if simplified != nil && machineAliasSafe(flow, simplified.Aliases, railssa.FlowValueID(id)) {
			data.Flags |= VRegElided
		}
		switch value.Kind {
		case railssa.FlowValueInitialLocal:
			data.Flags |= VRegInitial
			if value.Local > ^uint32(0)>>16 {
				return nil, &BudgetError{Resource: "initial-local compact identity", Required: uint64(value.Local) + 1, Limit: uint64(^uint16(0)) + 1}
			}
			data.InitialLocal = uint16(value.Local)
		case railssa.FlowValueBlockParam:
			data.Flags |= VRegBlockParam
			data.Def = semantic.Blocks[value.Block].InstStart * 6
		}
		reuse.VRegs[id] = data
	}
	for blockIndex, sourceBlock := range semantic.Blocks {
		cfgBlock := cfg.Blocks[blockIndex]
		block := &reuse.Blocks[blockIndex]
		*block = Block{InstStart: uint32(len(reuse.Insts)), Region: cfgBlock.Region, Flags: cfgBlock.Flags, Weight: cfgBlock.Weight}
		for semanticID := sourceBlock.InstStart; semanticID < sourceBlock.InstStart+sourceBlock.InstCount; semanticID++ {
			source := semantic.Insts[semanticID]
			args := semantic.Operands(semanticID)
			if len(args) > int(^uint16(0)) {
				return nil, &BudgetError{Resource: fmt.Sprintf("instruction %d operands", semanticID), Required: uint64(len(args)), Limit: uint64(^uint16(0))}
			}
			instruction := Inst{Aux: source.Aux, OperandStart: uint32(len(reuse.Operands)), OperandCount: uint16(len(args)), Result: VReg(source.Result), Source: source.Source, Op: source.Op}
			for operandIndex, value := range args {
				if simplified != nil {
					canonical := resolveMachineAlias(simplified.Aliases, value)
					if machineAliasSafe(flow, simplified.Aliases, value) {
						value = canonical
					}
				}
				if value == 0 || int(value) >= len(reuse.VRegs) {
					return nil, fmt.Errorf("railmach: instruction %d has invalid value %d", semanticID, value)
				}
				constraint := Operand{Reg: VReg(value), Fixed: NoFixedReg, Bank: reuse.VRegs[value].Bank, Flags: OperandUse}
				applyTargetConstraint(target, &instruction, &constraint, operandIndex, len(args))
				reuse.Operands = append(reuse.Operands, constraint)
			}
			reuse.Insts = append(reuse.Insts, instruction)
			for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
				result := instruction.Result + VReg(ordinal)
				reuse.VRegs[result].Def = (uint32(len(reuse.Insts))-1)*6 + 3
				if instruction.Op == wasm.InstrI32Const || instruction.Op == wasm.InstrI64Const || instruction.Op == wasm.InstrF32Const || instruction.Op == wasm.InstrF64Const || instruction.Op == wasm.InstrRefNull {
					reuse.VRegs[result].Flags |= VRegRematerializable
				}
			}
		}
		block.InstCount = uint32(len(reuse.Insts)) - block.InstStart
	}
	for _, transfer := range flow.EdgeArgs {
		if int(transfer.Edge) >= len(cfg.Edges) || int(transfer.Param) >= len(reuse.VRegs) || int(transfer.Argument) >= len(reuse.VRegs) {
			return nil, fmt.Errorf("railmach: invalid edge transfer %#v", transfer)
		}
		edge := cfg.Edges[transfer.Edge]
		if simplified != nil && machineAliasSafe(flow, simplified.Aliases, transfer.Param) {
			continue
		}
		source := transfer.Argument
		if simplified != nil {
			canonical := resolveMachineAlias(simplified.Aliases, source)
			if machineAliasSafe(flow, simplified.Aliases, source) {
				source = canonical
			}
		}
		reuse.Transfers = append(reuse.Transfers, EdgeTransfer{
			Src: VReg(source), Dst: VReg(transfer.Param), Edge: transfer.Edge,
			Weight: cfg.Blocks[edge.From].Weight, From: edge.From, To: edge.To, Type: reuse.VRegs[transfer.Param].Type,
		})
	}
	exit := railssa.BlockID(len(cfg.Blocks) - 1)
	for _, result := range flow.BlockEntry(exit) {
		if simplified != nil {
			canonical := resolveMachineAlias(simplified.Aliases, result)
			if machineAliasSafe(flow, simplified.Aliases, result) {
				result = canonical
			}
		}
		reuse.Results = append(reuse.Results, VReg(result))
	}
	if err := Verify(reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

// ApplyColdRematerialization commits verifier-planned constant cold uses to
// machine SSA. It deliberately marks individual operands rather than whole
// vregs: hot uses retain their ordinary allocation while the cold use no
// longer lengthens that live interval.
func ApplyColdRematerialization(f *Func, pressure *railssa.PressurePlan, priced *RematPlan) (uint32, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	if pressure == nil {
		return 0, nil
	}
	kind := make([]railssa.RematKind, len(f.VRegs))
	for _, recipe := range pressure.Remats {
		if int(recipe.Value) < len(kind) && (recipe.Kind == railssa.RematConstant || recipe.Kind == railssa.RematExtend) {
			kind[recipe.Value] = recipe.Kind
		}
	}
	if priced != nil {
		for _, decision := range priced.Decisions {
			if decision.Profitable && int(decision.Value) < len(kind) && affineColdRematEncodable(f, VReg(decision.Value)) {
				kind[decision.Value] = railssa.RematAffine
			}
		}
	}
	var committed uint32
	for _, use := range pressure.ColdUses {
		if int(use.Instruction) >= len(f.Insts) || int(use.Value) >= len(f.VRegs) || kind[use.Value] == railssa.RematInvalid {
			continue
		}
		f.VRegs[use.Value].Flags |= VRegColdRematerializable
		operands := f.InstructionOperands(use.Instruction)
		for index := range operands {
			operand := &operands[index]
			if operand.Reg != VReg(use.Value) || operand.Flags&OperandFixed != 0 || operand.Flags&OperandColdRemat != 0 {
				continue
			}
			operand.Flags |= OperandColdRemat
			committed++
		}
	}
	if err := Verify(f); err != nil {
		return 0, err
	}
	return committed, nil
}

func affineColdRematEncodable(f *Func, value VReg) bool {
	if value == 0 || int(value) >= len(f.VRegs) {
		return false
	}
	instructionID := f.VRegs[value].Def / 6
	if int(instructionID) >= len(f.Insts) {
		return false
	}
	instruction := f.Insts[instructionID]
	if instruction.Op != wasm.InstrI32Add && instruction.Op != wasm.InstrI64Add && instruction.Op != wasm.InstrI32Sub && instruction.Op != wasm.InstrI64Sub {
		return false
	}
	operands := f.InstructionOperands(instructionID)
	if len(operands) != 2 {
		return false
	}
	constant := f.VRegs[operands[1].Reg]
	if constant.Flags&VRegRematerializable == 0 || int(constant.Def/6) >= len(f.Insts) {
		return false
	}
	immediate := f.Insts[constant.Def/6].Aux
	if f.Target == TargetARM64 {
		return immediate <= 4095
	}
	return int64(immediate) == int64(int32(immediate)) || instruction.Op == wasm.InstrI32Add || instruction.Op == wasm.InstrI32Sub
}

// coldRematerializationBase returns the non-immediate input that the finalizer
// reads when reconstructing value at a cold use. The allocator must retain this
// implicit use even though the rematerialized value's ordinary operand is
// deliberately omitted from liveness.
func coldRematerializationBase(f *Func, value VReg) (VReg, bool) {
	if value == 0 || int(value) >= len(f.VRegs) {
		return 0, false
	}
	instructionID := f.VRegs[value].Def / 6
	if int(instructionID) >= len(f.Insts) {
		return 0, false
	}
	instruction := f.Insts[instructionID]
	operands := f.InstructionOperands(instructionID)
	switch instruction.Op {
	case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const, wasm.InstrRefNull:
		return 0, len(operands) == 0
	case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U, wasm.InstrI64ExtendI32S,
		wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
		wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		if len(operands) == 1 {
			return operands[0].Reg, true
		}
	case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub:
		if len(operands) == 2 {
			return operands[0].Reg, true
		}
	}
	return 0, false
}

func resolveMachineAlias(aliases []railssa.FlowValueID, value railssa.FlowValueID) railssa.FlowValueID {
	for aliases[value] != value {
		value = aliases[value]
	}
	return value
}

// machineAliasSafe admits aliases whose defining value is directly available
// at every use. Sparse simplification proves trivial block parameters have one
// canonical non-self incoming value, so their edge copies and machine vregs
// can be removed. Instruction aliases remain limited to one basic block until
// RailMach models cross-block dominating definitions independently.
func machineAliasSafe(flow *railssa.ValueFlow, aliases []railssa.FlowValueID, value railssa.FlowValueID) bool {
	if value == 0 || int(value) >= len(flow.Values) || int(value) >= len(aliases) {
		return false
	}
	canonical := resolveMachineAlias(aliases, value)
	if canonical == value {
		return false
	}
	switch flow.Values[value].Kind {
	case railssa.FlowValueBlockParam:
		return true
	case railssa.FlowValueInstruction:
		return flow.Values[canonical].Kind == railssa.FlowValueInstruction && flow.Values[value].Block == flow.Values[canonical].Block
	default:
		return false
	}
}

func applyTargetConstraint(target Target, instruction *Inst, operand *Operand, index, count int) {
	switch instruction.Op {
	case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
		if target == TargetARM64 && index < 3 {
			operand.Fixed, operand.Flags = uint8(index), operand.Flags|OperandFixed
		}
	case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet, wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail, wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny, wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData, wasm.InstrArrayNewElem, wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen, wasm.InstrArrayFill, wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem, wasm.InstrElemDrop:
		// Dragline's current private scalar ABI exposes eight bank-relative
		// argument registers. An indirect call's trailing table index is not a
		// callee argument and remains freely allocatable.
		if instruction.Op == wasm.InstrStructNew || instruction.Op == wasm.InstrStructNewDefault || instruction.Op == wasm.InstrStructGet || instruction.Op == wasm.InstrStructGetS || instruction.Op == wasm.InstrStructGetU || instruction.Op == wasm.InstrStructSet || instruction.Op == wasm.InstrRefTest || instruction.Op == wasm.InstrRefCast || instruction.Op == wasm.InstrBrOnCast || instruction.Op == wasm.InstrBrOnCastFail || instruction.Op == wasm.InstrAnyConvertExtern || instruction.Op == wasm.InstrExternConvertAny || instruction.Op == wasm.InstrArrayNew || instruction.Op == wasm.InstrArrayNewDefault || instruction.Op == wasm.InstrArrayNewFixed || instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem || instruction.Op == wasm.InstrArrayGet || instruction.Op == wasm.InstrArrayGetS || instruction.Op == wasm.InstrArrayGetU || instruction.Op == wasm.InstrArraySet || instruction.Op == wasm.InstrArrayLen || instruction.Op == wasm.InstrArrayFill || instruction.Op == wasm.InstrArrayCopy || instruction.Op == wasm.InstrArrayInitData || instruction.Op == wasm.InstrArrayInitElem || instruction.Op == wasm.InstrElemDrop || instruction.Op == wasm.InstrCallIndirect && index == count-1 {
			return
		}
		if index < 8 {
			operand.Fixed, operand.Flags = uint8(index), operand.Flags|OperandFixed
		}
	case wasm.InstrI32DivS, wasm.InstrI32DivU, wasm.InstrI32RemS, wasm.InstrI32RemU,
		wasm.InstrI64DivS, wasm.InstrI64DivU, wasm.InstrI64RemS, wasm.InstrI64RemU:
		if target == TargetAMD64 && index == 0 {
			operand.Fixed, operand.Flags = 0, operand.Flags|OperandFixed
		}
	case wasm.InstrI32Shl, wasm.InstrI32ShrS, wasm.InstrI32ShrU, wasm.InstrI32Rotl, wasm.InstrI32Rotr,
		wasm.InstrI64Shl, wasm.InstrI64ShrS, wasm.InstrI64ShrU, wasm.InstrI64Rotl, wasm.InstrI64Rotr:
		if target == TargetAMD64 && index == 1 {
			operand.Fixed, operand.Flags = 1, operand.Flags|OperandFixed
		}
	}
}

func machineType(typ wasm.ValType) (MachineType, Bank, error) {
	if typ.Kind() == wasm.ValRef {
		return TypeRef, BankGPR, nil
	}
	switch typ {
	case wasm.I32:
		return TypeI32, BankGPR, nil
	case wasm.I64:
		return TypeI64, BankGPR, nil
	case wasm.F32:
		return TypeF32, BankFPR, nil
	case wasm.F64:
		return TypeF64, BankFPR, nil
	default:
		return TypeInvalid, BankInvalid, fmt.Errorf("unsupported value type %s", typ)
	}
}

func Verify(f *Func) error {
	if f == nil || f.Target == TargetInvalid || len(f.VRegs) == 0 || f.VRegs[0].Type != TypeInvalid {
		return fmt.Errorf("railmach: malformed function header")
	}
	expectedStart := uint32(0)
	lastSource := uint32(0)
	seenSource := false
	for blockID, block := range f.Blocks {
		if block.InstStart != expectedStart || uint64(block.InstStart)+uint64(block.InstCount) > uint64(len(f.Insts)) {
			return fmt.Errorf("railmach: block %d has invalid instruction range", blockID)
		}
		expectedStart += block.InstCount
		for id := block.InstStart; id < block.InstStart+block.InstCount; id++ {
			instruction := f.Insts[id]
			if seenSource && instruction.Source < lastSource {
				return fmt.Errorf("railmach: instruction %d violates source-stable order", id)
			}
			seenSource, lastSource = true, instruction.Source
			end := uint64(instruction.OperandStart) + uint64(instruction.OperandCount)
			if end > uint64(len(f.Operands)) || uint64(instruction.Result)+uint64(instruction.ResultCount()) > uint64(len(f.VRegs)) {
				return fmt.Errorf("railmach: instruction %d has invalid dense references", id)
			}
			for _, operand := range f.InstructionOperands(id) {
				if operand.Reg == 0 || int(operand.Reg) >= len(f.VRegs) || operand.Bank != f.VRegs[operand.Reg].Bank || operand.Flags&OperandUse == 0 {
					return fmt.Errorf("railmach: instruction %d has invalid operand %#v", id, operand)
				}
				if operand.Flags&OperandFixed == 0 && operand.Fixed != NoFixedReg || operand.Flags&OperandFixed != 0 && operand.Fixed == NoFixedReg {
					return fmt.Errorf("railmach: instruction %d has inconsistent fixed constraint", id)
				}
				if operand.Flags&OperandColdRemat != 0 && (operand.Flags&OperandFixed != 0 || f.VRegs[operand.Reg].Flags&(VRegRematerializable|VRegColdRematerializable) == 0) {
					return fmt.Errorf("railmach: instruction %d has invalid cold rematerialization", id)
				}
			}
		}
	}
	if expectedStart != uint32(len(f.Insts)) {
		return fmt.Errorf("railmach: blocks cover %d of %d instructions", expectedStart, len(f.Insts))
	}
	for id, transfer := range f.Transfers {
		if transfer.Src == 0 || transfer.Dst == 0 || int(transfer.Edge) >= len(f.Edges) || int(transfer.Src) >= len(f.VRegs) || int(transfer.Dst) >= len(f.VRegs) || f.VRegs[transfer.Src].Type != transfer.Type || f.VRegs[transfer.Dst].Type != transfer.Type || f.Edges[transfer.Edge].From != transfer.From || f.Edges[transfer.Edge].To != transfer.To {
			return fmt.Errorf("railmach: transfer %d is invalid", id)
		}
	}
	for _, result := range f.Results {
		if result == 0 || int(result) >= len(f.VRegs) {
			return fmt.Errorf("railmach: invalid result vreg %d", result)
		}
	}
	return nil
}

// ScheduleSourceStable validates and returns the existing source order. It is
// the deterministic baseline candidate against which later schedulers compete.
func ScheduleSourceStable(f *Func) error { return Verify(f) }

func Dump(f *Func) string {
	if f == nil {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "target %s\n", f.Target)
	for blockID, block := range f.Blocks {
		fmt.Fprintf(&out, "b%d:\n", blockID)
		for id := block.InstStart; id < block.InstStart+block.InstCount; id++ {
			instruction := f.Insts[id]
			if instruction.Result != 0 {
				fmt.Fprintf(&out, "  r%d", instruction.Result)
				for ordinal := uint32(1); ordinal < instruction.ResultCount(); ordinal++ {
					fmt.Fprintf(&out, ",r%d", instruction.Result+VReg(ordinal))
				}
				fmt.Fprintf(&out, " = %s", instruction.Op)
			} else {
				fmt.Fprintf(&out, "  %s", instruction.Op)
			}
			for _, operand := range f.InstructionOperands(id) {
				fmt.Fprintf(&out, " r%d", operand.Reg)
				if operand.Fixed != NoFixedReg {
					fmt.Fprintf(&out, "[p%d]", operand.Fixed)
				}
			}
			fmt.Fprintf(&out, " @%d\n", instruction.Source)
		}
	}
	for _, transfer := range f.Transfers {
		fmt.Fprintf(&out, "edge%d r%d -> r%d\n", transfer.Edge, transfer.Src, transfer.Dst)
	}
	return out.String()
}

func CapacityBytes(f *Func) uint64 {
	if f == nil {
		return 0
	}
	return uint64(cap(f.Insts))*uint64(unsafe.Sizeof(Inst{})) +
		uint64(cap(f.Operands))*uint64(unsafe.Sizeof(Operand{})) +
		uint64(cap(f.VRegs))*uint64(unsafe.Sizeof(VRegData{})) +
		uint64(cap(f.Blocks))*uint64(unsafe.Sizeof(Block{})) +
		uint64(cap(f.Edges))*uint64(unsafe.Sizeof(Edge{})) +
		uint64(cap(f.Transfers))*uint64(unsafe.Sizeof(EdgeTransfer{})) +
		uint64(cap(f.Results))*uint64(unsafe.Sizeof(VReg(0)))
}

func resize[T any](values []T, length int) []T {
	if cap(values) < length {
		return make([]T, length)
	}
	values = values[:length]
	clear(values)
	return values
}
