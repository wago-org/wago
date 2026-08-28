package railssa

import (
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// StackInstr is one validated scalar instruction retained for Dragline's
// structured-control baseline. Values still use dense SSA for straight-line
// functions; loop-carried locals use canonical frame homes until CFG SSA lands.
type StackInstr struct {
	aux    uint64
	Offset uint32
	Kind   wasm.InstrKind
	meta   uint16
}

// MultiResultRange records the result types of an uncommon instruction whose
// type cannot use StackInstr's scalar inline representation. This includes
// multi-value instructions and single reference results. Records are kept in
// source order, so the Wasm 1.0 numeric path pays no dense per-instruction cost.
type MultiResultRange struct {
	Instruction uint32
	Start       uint32
	Count       uint32
}

// SIMDImmediate retains the uncommon 0xFD payload out of StackInstr so scalar
// functions keep their compact 16-byte instruction record.
type SIMDImmediate struct {
	Instruction uint32
	Descriptor  wasm.SIMDInstructionDescriptor
}

// BranchCastImmediate retains the edge-specific reference types that cannot
// fit in StackInstr without widening the common scalar record.
type BranchCastImmediate struct {
	Instruction uint32
	Label       uint32
	BranchType  wasm.ValType
	Fallthrough wasm.ValType
	Target      uint64
}

const (
	stackInstrResult uint16 = 1 << (3 + iota)
	stackInstrElse
	stackInstrInlineI32Add
)

func (i StackInstr) U64() uint64 { return i.aux }
func (i StackInstr) U32() uint32 { return uint32(i.aux) }
func (i *StackInstr) setU32(value uint32) {
	i.aux = i.aux&0xffffffff00000000 | uint64(value)
}

func (i StackInstr) Params() uint32 { return uint32(i.aux >> 32) }
func (i *StackInstr) setParams(count uint32) {
	i.aux = uint64(count)<<32 | uint64(uint32(i.aux))
}

func (i StackInstr) Inline() wasm.InstrKind {
	if i.meta&stackInstrInlineI32Add != 0 {
		return wasm.InstrI32Add
	}
	return wasm.InstrInvalid
}

func (i StackInstr) ValueType() wasm.ValType {
	switch i.meta & 7 {
	case 1:
		return wasm.I32
	case 2:
		return wasm.I64
	case 3:
		return wasm.F32
	case 4:
		return wasm.F64
	case 5:
		return wasm.V128
	default:
		return wasm.ValType{}
	}
}

func (i *StackInstr) setValueType(typ wasm.ValType) {
	switch typ {
	case wasm.I32:
		i.meta = i.meta&^7 | 1
	case wasm.I64:
		i.meta = i.meta&^7 | 2
	case wasm.F32:
		i.meta = i.meta&^7 | 3
	case wasm.F64:
		i.meta = i.meta&^7 | 4
	case wasm.V128:
		i.meta = i.meta&^7 | 5
	default:
		panic("railssa: unsupported StackInstr type")
	}
}

func (i StackInstr) HasResult() bool { return i.meta&stackInstrResult != 0 }
func (i StackInstr) IsElse() bool    { return i.meta&stackInstrElse != 0 }

func (i StackInstr) LabelLen() uint32 { return uint32(i.aux >> 32) }

func (i StackInstr) Labels(f *StackFunc) []uint32 {
	start := int(uint32(i.aux))
	return f.BranchLabels[start : start+int(i.LabelLen())]
}

// StackFunc is a compact, byte-backed structured function.
type StackFunc struct {
	Module         *wasm.Module
	FunctionIndex  uint32
	Params         []wasm.ValType
	Results        []wasm.ValType
	Locals         []wasm.ValType
	Globals        []wasm.ValType
	Instrs         []StackInstr
	BranchLabels   []uint32
	TypeKeys       []uint64
	MultiResults   []MultiResultRange
	ResultTypes    []wasm.ValType
	SIMDImmediates []SIMDImmediate
	BranchCasts    []BranchCastImmediate
	Regions        []Region
	RegionLocals   []uint32
	MergeLocals    []uint32
	LoopLocals     []uint32
	ImportedFuncs  uint32
	FuncCount      uint32
	MaxStack       uint32
	MaxLoopDepth   uint8
	BuildPeakBytes uint64
	HasMemory      bool
	HasV128        bool
	HasReferences  bool
	MemoryMinBytes uint64

	prepassEvents   []regionLocalEvent
	prepassLastUse  []uint32
	prepassControls []parseControl
}

// ValueSlots returns the canonical 64-bit slot width of a WebAssembly value.
// This is kept here so both native emitters use the same V128 frame contract.
func ValueSlots(typ wasm.ValType) uint32 {
	if typ == wasm.V128 {
		return 2
	}
	return 1
}

// TypeSlots returns the canonical slot width of a value sequence.
func TypeSlots(types []wasm.ValType) uint32 {
	var slots uint32
	for _, typ := range types {
		slots += ValueSlots(typ)
	}
	return slots
}

// TypeSlotOffset returns the canonical slot offset of a logical value index.
func TypeSlotOffset(types []wasm.ValType, index int) uint32 {
	return TypeSlots(types[:index])
}

// CapacityBytes reports all reusable backing storage owned by f.
func (f *StackFunc) CapacityBytes() uint64 {
	if f == nil {
		return 0
	}
	return uint64(cap(f.Params)+cap(f.Results)+cap(f.Locals)+cap(f.Globals))*uint64(unsafe.Sizeof(wasm.ValType{})) +
		uint64(cap(f.Instrs))*uint64(unsafe.Sizeof(StackInstr{})) +
		uint64(cap(f.BranchLabels))*uint64(unsafe.Sizeof(uint32(0))) +
		uint64(cap(f.TypeKeys))*uint64(unsafe.Sizeof(uint64(0))) +
		uint64(cap(f.MultiResults))*uint64(unsafe.Sizeof(MultiResultRange{})) +
		uint64(cap(f.ResultTypes))*uint64(unsafe.Sizeof(wasm.ValType{})) +
		uint64(cap(f.SIMDImmediates))*uint64(unsafe.Sizeof(SIMDImmediate{})) +
		uint64(cap(f.BranchCasts))*uint64(unsafe.Sizeof(BranchCastImmediate{})) +
		uint64(cap(f.Regions))*uint64(unsafe.Sizeof(Region{})) +
		uint64(cap(f.RegionLocals)+cap(f.MergeLocals)+cap(f.LoopLocals))*uint64(unsafe.Sizeof(uint32(0))) +
		uint64(cap(f.prepassEvents))*uint64(unsafe.Sizeof(regionLocalEvent(0))) +
		uint64(cap(f.prepassLastUse))*uint64(unsafe.Sizeof(uint32(0))) +
		uint64(cap(f.prepassControls))*uint64(unsafe.Sizeof(parseControl{}))
}

// BuildStackFunc lowers the admitted scalar instruction family plus full
// type-indexed block signatures. It is deliberately strict about every other
// instruction.
func BuildStackFunc(m *wasm.Module, localIndex int) (*StackFunc, error) {
	return BuildStackFuncInto(m, localIndex, nil)
}

// BuildStackFuncInto lowers into reuse when it is non-nil. The caller may
// reuse one StackFunc across functions after consuming the previous result;
// this bounds temporary IR storage by the largest function in a module.
func BuildStackFuncInto(m *wasm.Module, localIndex int, reuse *StackFunc) (*StackFunc, error) {
	if m == nil || localIndex < 0 || localIndex >= len(m.Code) || localIndex >= len(m.FuncTypes) {
		return nil, fmt.Errorf("railssa: local function %d is unavailable", localIndex)
	}
	ft, ok := m.LocalFuncType(localIndex)
	if !ok || ft == nil {
		return nil, fmt.Errorf("railssa: local function %d has no function type", localIndex)
	}
	if reuse == nil {
		reuse = new(StackFunc)
	}
	params := append(reuse.Params[:0], ft.Params...)
	results := append(reuse.Results[:0], ft.Results...)
	locals := reuse.Locals[:0]
	globals := reuse.Globals[:0]
	instrs := reuse.Instrs[:0]
	branchLabels := reuse.BranchLabels[:0]
	typeKeys := reuse.TypeKeys[:0]
	multiResults := reuse.MultiResults[:0]
	resultTypes := reuse.ResultTypes[:0]
	simdImmediates := reuse.SIMDImmediates[:0]
	branchCasts := reuse.BranchCasts[:0]
	regions := reuse.Regions[:0]
	regionLocals := reuse.RegionLocals[:0]
	mergeLocals := reuse.MergeLocals[:0]
	loopLocals := reuse.LoopLocals[:0]
	prepassEvents := reuse.prepassEvents[:0]
	prepassLastUse := reuse.prepassLastUse[:0]
	prepassControls := reuse.prepassControls[:0]
	*reuse = StackFunc{
		Module: m, FunctionIndex: uint32(localIndex),
		Params: params, Results: results, Locals: locals, Globals: globals,
		Instrs: instrs, BranchLabels: branchLabels, TypeKeys: typeKeys,
		MultiResults: multiResults, ResultTypes: resultTypes, SIMDImmediates: simdImmediates, BranchCasts: branchCasts,
		Regions: regions, RegionLocals: regionLocals, MergeLocals: mergeLocals, LoopLocals: loopLocals,
		ImportedFuncs: uint32(m.ImportedFuncCount()), FuncCount: uint32(m.FuncCount()),
		prepassEvents: prepassEvents, prepassLastUse: prepassLastUse, prepassControls: prepassControls,
	}
	f := reuse
	f.HasMemory = m.MemCount() != 0
	for _, typ := range ft.Params {
		f.HasV128 = f.HasV128 || typ == wasm.V128
		f.HasReferences = f.HasReferences || typ.Kind() == wasm.ValRef
	}
	for _, typ := range ft.Results {
		f.HasV128 = f.HasV128 || typ == wasm.V128
		f.HasReferences = f.HasReferences || typ.Kind() == wasm.ValRef
	}
	if len(m.Memories) == 1 {
		f.MemoryMinBytes = m.Memories[0].Limits.Min << 16
	}
	f.Locals = append(f.Locals, ft.Params...)
	for i, typ := range f.Locals {
		if !stackValueType(typ) {
			return nil, fmt.Errorf("railssa: function %d parameter %d has unsupported type %s", localIndex, i, typ)
		}
	}
	for i, typ := range ft.Results {
		if !stackValueType(typ) {
			return nil, fmt.Errorf("railssa: function %d result %d has unsupported type %s", localIndex, i, typ)
		}
	}
	for runIndex, run := range m.Code[localIndex].Locals.Runs {
		if !stackValueType(run.Type) {
			return nil, fmt.Errorf("railssa: function %d local run %d has unsupported type %s", localIndex, runIndex, run.Type)
		}
		if uint64(len(f.Locals))+uint64(run.Count) > 4096 {
			return nil, &BudgetError{Resource: fmt.Sprintf("function %d structured scalar locals", localIndex), Required: uint64(len(f.Locals)) + uint64(run.Count), Limit: 4096}
		}
		for range run.Count {
			f.Locals = append(f.Locals, run.Type)
		}
		f.HasV128 = f.HasV128 || run.Type == wasm.V128
		f.HasReferences = f.HasReferences || run.Type.Kind() == wasm.ValRef
	}
	for i := uint32(0); i < uint32(m.GlobalCount()); i++ {
		gt, ok := m.GlobalTypeByIndex(i)
		if !ok || !stackValueType(wasm.GlobalValueType(gt)) {
			return nil, fmt.Errorf("railssa: function %d global %d has unsupported type", localIndex, i)
		}
		globalType := wasm.GlobalValueType(gt)
		f.Globals = append(f.Globals, globalType)
		f.HasReferences = f.HasReferences || globalType.Kind() == wasm.ValRef
	}

	r := wasm.NewReader(m.Code[localIndex].BodyBytes)
	compactPrepass := len(m.Code[localIndex].BodyBytes) >= compactPrepassBodyBytes
	// Scalar instructions average at least two encoded bytes in the admitted
	// workloads. Reserving once avoids repeatedly copying the comparatively wide
	// StackInstr records while parsing long unrolled loop bodies.
	reserve := len(m.Code[localIndex].BodyBytes) / 2
	if cap(f.Instrs) < reserve {
		f.Instrs = make([]StackInstr, 0, reserve)
	}
	regionEvents := f.prepassEvents[:0]
	lastLocalUse := f.prepassLastUse[:0]
	depth := 0
	reachable := true
	controls := f.prepassControls[:0]
	for r.HasNext() {
		wasReachable := reachable
		depthBefore := depth
		offset := uint32(r.Offset())
		opcode, err := r.Byte()
		if err != nil {
			return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
		}
		instr := StackInstr{Offset: offset}
		switch opcode {
		case 0x00: // unreachable
			instr.Kind = wasm.InstrUnreachable
			reachable = false
		case 0x02, 0x03, 0x04: // block, loop, if
			if len(f.Regions) == 0 && cap(regionEvents) == 0 {
				// Structured scalar bodies commonly encode one local access in two
				// bytes. Each access is attributed to every enclosing region. One
				// event slot per body byte covers the common nested-loop case without
				// repeated growth and remains capped for corpus-sized functions.
				eventCapacity := min(len(m.Code[localIndex].BodyBytes), maxPrepassLocalEvents)
				if eventCapacity != 0 {
					regionEvents = make([]regionLocalEvent, 0, eventCapacity)
				}
			}
			if len(lastLocalUse) == 0 && len(f.Locals) != 0 {
				if cap(lastLocalUse) < len(f.Locals) {
					lastLocalUse = make([]uint32, len(f.Locals))
				} else {
					lastLocalUse = lastLocalUse[:len(f.Locals)]
					clear(lastLocalUse)
				}
			}
			blockType, err := r.S33()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed block type", localIndex, offset)
			}
			var blockParams, blockResults []wasm.ValType
			switch blockType {
			case -64: // 0x40, empty block type
			case -1: // 0x7f
				blockResults = []wasm.ValType{wasm.I32}
			case -2: // 0x7e
				blockResults = []wasm.ValType{wasm.I64}
			case -3: // 0x7d
				blockResults = []wasm.ValType{wasm.F32}
			case -4: // 0x7c
				blockResults = []wasm.ValType{wasm.F64}
			case -5: // 0x7b
				blockResults = []wasm.ValType{wasm.V128}
			default:
				if blockType < 0 || blockType > int64(^uint32(0)) {
					return nil, fmt.Errorf("railssa: function %d byte %d: invalid block type index %d", localIndex, offset, blockType)
				}
				sig, ok := m.TypeFunc(uint32(blockType))
				if !ok || sig == nil {
					return nil, fmt.Errorf("railssa: function %d byte %d: invalid block type index %d", localIndex, offset, blockType)
				}
				blockParams, blockResults = sig.Params, sig.Results
			}
			if len(blockParams) > int(^uint16(0)) || len(blockResults) > int(^uint16(0)) {
				return nil, &BudgetError{Resource: fmt.Sprintf("function %d block signature arity", localIndex), Required: uint64(max(len(blockParams), len(blockResults))), Limit: uint64(^uint16(0))}
			}
			for i, typ := range blockParams {
				if !stackValueType(typ) {
					return nil, fmt.Errorf("railssa: function %d byte %d: block parameter %d has unsupported type %s", localIndex, offset, i, typ)
				}
			}
			for i, typ := range blockResults {
				if !stackValueType(typ) {
					return nil, fmt.Errorf("railssa: function %d byte %d: block result %d has unsupported type %s", localIndex, offset, i, typ)
				}
			}
			f.setInstructionResults(uint32(len(f.Instrs)), &instr, blockResults)
			switch opcode {
			case 0x02:
				instr.Kind = wasm.InstrBlock
			case 0x03:
				instr.Kind = wasm.InstrLoop
			case 0x04:
				instr.Kind = wasm.InstrIf
				if reachable {
					depth--
				}
			}
			baseDepth := depth - len(blockParams)
			if baseDepth < 0 {
				return nil, fmt.Errorf("railssa: function %d byte %d: operand stack underflow", localIndex, offset)
			}
			if len(f.Regions) >= int(NoRegion) {
				return nil, &BudgetError{Resource: fmt.Sprintf("function %d structured regions", localIndex), Required: uint64(len(f.Regions)) + 1, Limit: uint64(NoRegion)}
			}
			parent := NoRegion
			loopDepth := uint8(0)
			if len(controls) != 0 {
				parent = controls[len(controls)-1].region
				loopDepth = f.Regions[parent].LoopDepth
			}
			if instr.Kind == wasm.InstrLoop {
				if loopDepth == ^uint8(0) {
					return nil, &BudgetError{Resource: fmt.Sprintf("function %d loop nesting", localIndex), Required: uint64(loopDepth) + 1, Limit: uint64(^uint8(0))}
				}
				loopDepth++
			}
			if loopDepth > f.MaxLoopDepth {
				f.MaxLoopDepth = loopDepth
			}
			region := RegionID(len(f.Regions))
			paramArity, resultArity := uint16(len(blockParams)), uint16(len(blockResults))
			f.Regions = append(f.Regions, Region{Parent: parent, Kind: instr.Kind, StartInstr: uint32(len(f.Instrs)), ElseInstr: ^uint32(0), EndInstr: ^uint32(0), StackDepth: uint32(baseDepth), LoopDepth: loopDepth, ParamArity: paramArity, ResultArity: resultArity})
			controls = append(controls, parseControl{
				region: region, kind: instr.Kind, baseDepth: baseDepth, paramArity: paramArity, resultArity: resultArity, parentReachable: reachable,
			})
		case 0x05: // else
			if len(controls) == 0 || controls[len(controls)-1].kind != wasm.InstrIf {
				return nil, fmt.Errorf("railssa: function %d byte %d: else without if", localIndex, offset)
			}
			instr.meta |= stackInstrElse
			control := &controls[len(controls)-1]
			f.Regions[control.region].ElseInstr = uint32(len(f.Instrs))
			control.endReached = control.endReached || reachable
			control.seenElse = true
			depth = control.baseDepth + int(control.paramArity)
			reachable = control.parentReachable
		case 0x0b: // end
			instr.Kind = wasm.InstrInvalid // final/function end or structured end
			if len(controls) > 0 {
				control := controls[len(controls)-1]
				controls = controls[:len(controls)-1]
				f.Regions[control.region].EndInstr = uint32(len(f.Instrs))
				control.endReached = control.endReached || reachable
				if control.kind == wasm.InstrIf && !control.seenElse {
					control.endReached = control.endReached || control.parentReachable
				}
				reachable = control.endReached
				depth = control.baseDepth + int(control.resultArity)
			} else if !reachable {
				// Validation already proved the implicit function label has the
				// declared result shape. Canonicalize the polymorphic stack here.
				depth = len(f.Results)
			}
		case 0x0c, 0x0d: // br, br_if
			label, err := r.U32()
			if err != nil || int(label) > len(controls) {
				return nil, fmt.Errorf("railssa: function %d byte %d: branch label %d is out of range", localIndex, offset, label)
			}
			instr.setU32(label)
			if opcode == 0x0c {
				instr.Kind = wasm.InstrBr
				if int(label) < len(controls) {
					target := &controls[len(controls)-1-int(label)]
					if target.kind != wasm.InstrLoop && reachable {
						target.endReached = true
					}
				}
				reachable = false
			} else {
				instr.Kind = wasm.InstrBrIf
				if reachable {
					depth--
					if int(label) < len(controls) {
						target := &controls[len(controls)-1-int(label)]
						if target.kind != wasm.InstrLoop {
							target.endReached = true
						}
					}
				}
			}
		case 0x0e: // br_table
			count, err := r.U32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed br_table", localIndex, offset)
			}
			instr.Kind = wasm.InstrBrTable
			instr.aux = uint64(count+1)<<32 | uint64(uint32(len(f.BranchLabels)))
			for range count + 1 {
				label, err := r.U32()
				if err != nil || int(label) > len(controls) {
					return nil, fmt.Errorf("railssa: function %d byte %d: br_table label %d is out of range", localIndex, offset, label)
				}
				f.BranchLabels = append(f.BranchLabels, label)
			}
			if reachable {
				depth--
				for _, label := range instr.Labels(f) {
					if int(label) < len(controls) {
						target := &controls[len(controls)-1-int(label)]
						if target.kind != wasm.InstrLoop {
							target.endReached = true
						}
					}
				}
			}
			reachable = false
		case 0x0f: // return
			instr.Kind = wasm.InstrReturn
			reachable = false
		case 0x10: // call
			target, err := r.U32()
			sig, ok := m.FuncSignature(target)
			if err != nil || !ok || sig == nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed call target %d", localIndex, offset, target)
			}
			instr.Kind = wasm.InstrCall
			instr.setU32(target)
			instr.setParams(uint32(len(sig.Params)))
			f.setInstructionResults(uint32(len(f.Instrs)), &instr, sig.Results)
			if inlineI32AddTarget(m, target) {
				instr.meta |= stackInstrInlineI32Add
			}
			depth -= len(sig.Params)
			depth += len(sig.Results)
		case 0x11: // call_indirect
			typeIndex, err := r.U32()
			tableIndex, tableErr := r.U32()
			sig, sigOK := m.TypeFunc(typeIndex)
			key, keyOK := m.StructuralTypeKeyChecked(typeIndex)
			if err != nil || tableErr != nil || tableIndex != 0 || !sigOK || sig == nil || !keyOK {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed scalar call_indirect", localIndex, offset)
			}
			instr.Kind = wasm.InstrCallIndirect
			instr.setU32(uint32(len(f.TypeKeys)))
			f.TypeKeys = append(f.TypeKeys, key)
			instr.setParams(uint32(len(sig.Params)))
			if target, ok := immutableSingleTableTarget(m); ok {
				targetType, typeOK := m.FuncTypeIndex(target)
				if typeOK && targetType.Index == typeIndex && inlineI32AddTarget(m, target) {
					instr.meta |= stackInstrInlineI32Add
				}
			}
			f.setInstructionResults(uint32(len(f.Instrs)), &instr, sig.Results)
			depth -= len(sig.Params) + 1 // arguments plus table index
			depth += len(sig.Results)
		case 0x1a:
			instr.Kind = wasm.InstrDrop
			depth--
		case 0x1b: // select
			instr.Kind = wasm.InstrSelect
			depth -= 2
		case 0x20, 0x21, 0x22:
			index, err := r.U32()
			if err != nil || int(index) >= len(f.Locals) {
				return nil, fmt.Errorf("railssa: function %d byte %d: local %d is out of range", localIndex, offset, index)
			}
			instr.setU32(index)
			switch opcode {
			case 0x20:
				instr.Kind, depth = wasm.InstrLocalGet, depth+1
				if len(lastLocalUse) != 0 {
					lastLocalUse[index] = uint32(len(f.Instrs) + 1)
				}
				if err := recordRegionLocal(&regionEvents, controls, index, true, false, compactPrepass); err != nil {
					return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
				}
			case 0x21:
				instr.Kind, depth = wasm.InstrLocalSet, depth-1
				if err := recordRegionLocal(&regionEvents, controls, index, false, true, compactPrepass); err != nil {
					return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
				}
			case 0x22:
				instr.Kind = wasm.InstrLocalTee
				if err := recordRegionLocal(&regionEvents, controls, index, false, true, compactPrepass); err != nil {
					return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
				}
			}
		case 0x23, 0x24: // global.get, global.set
			index, err := r.U32()
			if err != nil || int(index) >= len(f.Globals) {
				return nil, fmt.Errorf("railssa: function %d byte %d: global %d is out of range", localIndex, offset, index)
			}
			instr.setU32(index)
			if opcode == 0x23 {
				instr.Kind, depth = wasm.InstrGlobalGet, depth+1
			} else {
				instr.Kind, depth = wasm.InstrGlobalSet, depth-1
			}
		case 0x41:
			value, err := r.I32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
			}
			instr.Kind, instr.aux, depth = wasm.InstrI32Const, uint64(uint32(value)), depth+1
		case 0x42:
			value, err := r.I64()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
			}
			instr.Kind, instr.aux, depth = wasm.InstrI64Const, uint64(value), depth+1
		case 0x43:
			value, err := r.LEU32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
			}
			instr.Kind, instr.aux, depth = wasm.InstrF32Const, uint64(value), depth+1
		case 0x44:
			value, err := r.LEU64()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
			}
			instr.Kind, instr.aux, depth = wasm.InstrF64Const, value, depth+1
		case 0xd0: // ref.null
			ref, err := r.RefTypeForNull()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed ref.null type: %w", localIndex, offset, err)
			}
			instr.Kind = wasm.InstrRefNull
			f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{wasm.RefVal(ref)})
			f.HasReferences = true
			depth++
		case 0xd1: // ref.is_null
			instr.Kind = wasm.InstrRefIsNull
			instr.meta |= stackInstrResult
			instr.setValueType(wasm.I32)
		case 0xd2: // ref.func
			functionIndex, err := r.U32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed ref.func index: %w", localIndex, offset, err)
			}
			typeIndex, ok := m.FuncTypeIndex(functionIndex)
			if !ok {
				return nil, fmt.Errorf("railssa: function %d byte %d: ref.func function %d is unavailable", localIndex, offset, functionIndex)
			}
			instr.Kind = wasm.InstrRefFunc
			instr.setU32(functionIndex)
			result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(typeIndex), false))
			f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
			f.HasReferences = true
			depth++
		case 0xd3: // ref.eq
			instr.Kind = wasm.InstrRefEq
			instr.meta |= stackInstrResult
			instr.setValueType(wasm.I32)
			depth--
		case 0xd4: // ref.as_non_null
			// Typed SSA derives the result from its operand: only nullability
			// changes, so retaining another full reference type is redundant.
			instr.Kind = wasm.InstrRefAsNonNull
			instr.meta |= stackInstrResult
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35,
			0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
			if !f.HasMemory {
				return nil, fmt.Errorf("railssa: function %d byte %d: memory instruction without memory", localIndex, offset)
			}
			if _, err := r.U32(); err != nil { // alignment hint, already validated
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed memory alignment", localIndex, offset)
			}
			memoryOffset, err := r.U32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed memory offset", localIndex, offset)
			}
			instr.setU32(memoryOffset)
			switch opcode {
			case 0x28:
				instr.Kind = wasm.InstrI32Load
				instr.setValueType(wasm.I32)
			case 0x29:
				instr.Kind = wasm.InstrI64Load
				instr.setValueType(wasm.I64)
			case 0x2a:
				instr.Kind = wasm.InstrF32Load
				instr.setValueType(wasm.F32)
			case 0x2b:
				instr.Kind = wasm.InstrF64Load
				instr.setValueType(wasm.F64)
			case 0x2c:
				instr.Kind = wasm.InstrI32Load8S
				instr.setValueType(wasm.I32)
			case 0x2d:
				instr.Kind = wasm.InstrI32Load8U
				instr.setValueType(wasm.I32)
			case 0x2e:
				instr.Kind = wasm.InstrI32Load16S
				instr.setValueType(wasm.I32)
			case 0x2f:
				instr.Kind = wasm.InstrI32Load16U
				instr.setValueType(wasm.I32)
			case 0x30:
				instr.Kind = wasm.InstrI64Load8S
				instr.setValueType(wasm.I64)
			case 0x31:
				instr.Kind = wasm.InstrI64Load8U
				instr.setValueType(wasm.I64)
			case 0x32:
				instr.Kind = wasm.InstrI64Load16S
				instr.setValueType(wasm.I64)
			case 0x33:
				instr.Kind = wasm.InstrI64Load16U
				instr.setValueType(wasm.I64)
			case 0x34:
				instr.Kind = wasm.InstrI64Load32S
				instr.setValueType(wasm.I64)
			case 0x35:
				instr.Kind = wasm.InstrI64Load32U
				instr.setValueType(wasm.I64)
			case 0x36:
				instr.Kind, depth = wasm.InstrI32Store, depth-2
			case 0x37:
				instr.Kind, depth = wasm.InstrI64Store, depth-2
			case 0x38:
				instr.Kind, depth = wasm.InstrF32Store, depth-2
			case 0x39:
				instr.Kind, depth = wasm.InstrF64Store, depth-2
			case 0x3a:
				instr.Kind, depth = wasm.InstrI32Store8, depth-2
			case 0x3b:
				instr.Kind, depth = wasm.InstrI32Store16, depth-2
			case 0x3c:
				instr.Kind, depth = wasm.InstrI64Store8, depth-2
			case 0x3d:
				instr.Kind, depth = wasm.InstrI64Store16, depth-2
			case 0x3e:
				instr.Kind, depth = wasm.InstrI64Store32, depth-2
			}
		case 0x3f, 0x40: // memory.size, memory.grow
			if !f.HasMemory {
				return nil, fmt.Errorf("railssa: function %d byte %d: memory instruction without memory", localIndex, offset)
			}
			memoryIndex, err := r.U32()
			if err != nil || memoryIndex != 0 {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed memory index", localIndex, offset)
			}
			instr.setU32(memoryIndex)
			if opcode == 0x3f {
				instr.Kind = wasm.InstrMemorySize
				depth++
			} else {
				instr.Kind = wasm.InstrMemoryGrow
			}
		case 0xfc:
			subopcode, err := r.U32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: unsupported 0xfc subopcode %d", localIndex, offset, subopcode)
			}
			switch {
			case subopcode <= 7:
				instr.Kind = [...]wasm.InstrKind{
					wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
					wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
					wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U,
					wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U,
				}[subopcode]
			case subopcode == 9: // data.drop
				dataIndex, dataErr := r.U32()
				if dataErr != nil || int(dataIndex) >= len(m.Data) {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed data.drop index", localIndex, offset)
				}
				instr.Kind = wasm.InstrDataDrop
				instr.setU32(dataIndex)
			case subopcode == 10: // memory.copy
				if !f.HasMemory {
					return nil, fmt.Errorf("railssa: function %d byte %d: memory.copy without memory", localIndex, offset)
				}
				dstMemory, dstErr := r.U32()
				srcMemory, srcErr := r.U32()
				if dstErr != nil || srcErr != nil || dstMemory != 0 || srcMemory != 0 {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed memory.copy index", localIndex, offset)
				}
				instr.Kind = wasm.InstrMemoryCopy
				depth -= 3
			case subopcode == 11: // memory.fill
				if !f.HasMemory {
					return nil, fmt.Errorf("railssa: function %d byte %d: memory.fill without memory", localIndex, offset)
				}
				memoryIndex, memoryErr := r.U32()
				if memoryErr != nil || memoryIndex != 0 {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed memory.fill index", localIndex, offset)
				}
				instr.Kind = wasm.InstrMemoryFill
				depth -= 3
			case subopcode == 13: // elem.drop
				elemIndex, elemErr := r.U32()
				if elemErr != nil || int(elemIndex) >= len(m.Elements) {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed elem.drop index", localIndex, offset)
				}
				instr.Kind = wasm.InstrElemDrop
				instr.setU32(elemIndex)
				f.HasReferences = true
			default:
				return nil, fmt.Errorf("railssa: function %d byte %d: unsupported 0xfc subopcode %d", localIndex, offset, subopcode)
			}
		case 0xfb:
			subopcode, err := r.U32()
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed 0xfb opcode", localIndex, offset)
			}
			switch subopcode {
			case 0: // struct.new
				typeIndex, err := r.U32()
				fieldCount, ok := m.StructFieldCount(typeIndex)
				if err != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or oversized struct.new type %d", localIndex, offset, typeIndex)
				}
				slots := uint64(1) // trailing type ID
				for fieldIndex := uint32(0); fieldIndex < fieldCount; fieldIndex++ {
					field, _ := m.StructField(typeIndex, fieldIndex)
					slots++
					if !field.Storage().Packed() && field.Storage().Val() == wasm.V128 {
						slots++
					}
				}
				if slots > uint64(abi.SyncHostMaxSlots) {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or oversized struct.new type %d", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrStructNew
				instr.setU32(typeIndex)
				instr.setParams(fieldCount)
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
				depth += 1 - int(fieldCount)
			case 1: // struct.new_default
				typeIndex, err := r.U32()
				if err != nil {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed struct.new_default type", localIndex, offset)
				}
				instr.Kind = wasm.InstrStructNewDefault
				instr.setU32(typeIndex)
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
				depth++
			case 2, 3, 4: // struct.get / struct.get_s / struct.get_u
				typeIndex, typeErr := r.U32()
				fieldIndex, fieldErr := r.U32()
				field, ok := m.StructField(typeIndex, fieldIndex)
				if typeErr != nil || fieldErr != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed struct.get field %d:%d", localIndex, offset, typeIndex, fieldIndex)
				}
				storage := field.Storage()
				if subopcode == 2 && storage.Packed() {
					return nil, fmt.Errorf("railssa: function %d byte %d: plain struct.get field %d:%d is packed", localIndex, offset, typeIndex, fieldIndex)
				}
				if subopcode != 2 && !storage.Packed() {
					return nil, fmt.Errorf("railssa: function %d byte %d: struct.get_s/u field %d:%d is not packed", localIndex, offset, typeIndex, fieldIndex)
				}
				result := storage.Val()
				if subopcode != 2 {
					result = wasm.I32
				}
				instr.Kind = [...]wasm.InstrKind{wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU}[subopcode-2]
				instr.aux = uint64(typeIndex)<<32 | uint64(fieldIndex)
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
			case 5: // struct.set
				typeIndex, typeErr := r.U32()
				fieldIndex, fieldErr := r.U32()
				field, ok := m.StructField(typeIndex, fieldIndex)
				if typeErr != nil || fieldErr != nil || !ok || field.Mut() != wasm.Var {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or immutable struct.set field %d:%d", localIndex, offset, typeIndex, fieldIndex)
				}
				instr.Kind = wasm.InstrStructSet
				instr.aux = uint64(typeIndex)<<32 | uint64(fieldIndex)
				f.HasReferences = true
				depth -= 2
			case 6: // array.new
				typeIndex, err := r.U32()
				_, ok := m.ArrayField(typeIndex)
				if err != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed array.new type %d", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrArrayNew
				instr.setU32(typeIndex)
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
				depth--
			case 7: // array.new_default
				typeIndex, err := r.U32()
				if _, ok := m.ArrayField(typeIndex); err != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed array.new_default type %d", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrArrayNewDefault
				instr.setU32(typeIndex)
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
			case 8: // array.new_fixed
				typeIndex, typeErr := r.U32()
				count, countErr := r.U32()
				field, ok := m.ArrayField(typeIndex)
				v128 := ok && !field.Storage().Packed() && field.Storage().Val() == wasm.V128
				if typeErr != nil || countErr != nil || !ok || !v128 && uint64(count)+2 > uint64(abi.SyncHostMaxSlots) {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or oversized array.new_fixed type %d count %d", localIndex, offset, typeIndex, count)
				}
				instr.Kind = wasm.InstrArrayNewFixed
				instr.setU32(typeIndex)
				instr.setParams(count)
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
				depth += 1 - int(count)
			case 9, 10: // array.new_data / array.new_elem
				typeIndex, typeErr := r.U32()
				segmentIndex, segmentErr := r.U32()
				field, ok := m.ArrayField(typeIndex)
				if typeErr != nil || segmentErr != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed array constructor type/segment %d:%d", localIndex, offset, typeIndex, segmentIndex)
				}
				if subopcode == 9 && field.Storage().Val().Kind() == wasm.ValRef {
					return nil, fmt.Errorf("railssa: function %d byte %d: array.new_data type %d is a reference array", localIndex, offset, typeIndex)
				}
				if subopcode == 10 && (field.Storage().Packed() || field.Storage().Val().Kind() != wasm.ValRef) {
					return nil, fmt.Errorf("railssa: function %d byte %d: array.new_elem type %d is not a reference array", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrArrayNewData
				if subopcode == 10 {
					instr.Kind = wasm.InstrArrayNewElem
				}
				instr.setU32(typeIndex)
				instr.setParams(segmentIndex)
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
				depth--
			case 11, 12, 13: // array.get / array.get_s / array.get_u
				typeIndex, err := r.U32()
				field, ok := m.ArrayField(typeIndex)
				if err != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed array.get type %d", localIndex, offset, typeIndex)
				}
				storage := field.Storage()
				if subopcode == 11 && storage.Packed() {
					return nil, fmt.Errorf("railssa: function %d byte %d: plain array.get type %d is packed", localIndex, offset, typeIndex)
				}
				if subopcode != 11 && !storage.Packed() {
					return nil, fmt.Errorf("railssa: function %d byte %d: array.get_s/u type %d is not packed", localIndex, offset, typeIndex)
				}
				result := storage.Val()
				if subopcode != 11 {
					result = wasm.I32
				}
				instr.Kind = [...]wasm.InstrKind{wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU}[subopcode-11]
				instr.setU32(typeIndex)
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
				depth--
			case 14: // array.set
				typeIndex, err := r.U32()
				field, ok := m.ArrayField(typeIndex)
				if err != nil || !ok || field.Mut() != wasm.Var {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or immutable array.set type %d", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrArraySet
				instr.setU32(typeIndex)
				f.HasReferences = true
				depth -= 3
			case 15: // array.len
				instr.Kind = wasm.InstrArrayLen
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.I32)
				f.HasReferences = true
			case 16: // array.fill
				typeIndex, err := r.U32()
				field, ok := m.ArrayField(typeIndex)
				if err != nil || !ok || field.Mut() != wasm.Var {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or immutable array.fill type %d", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrArrayFill
				instr.setU32(typeIndex)
				f.HasReferences = true
				depth -= 4
			case 17: // array.copy
				dstType, dstErr := r.U32()
				srcType, srcErr := r.U32()
				dstField, dstOK := m.ArrayField(dstType)
				_, srcOK := m.ArrayField(srcType)
				if dstErr != nil || srcErr != nil || !dstOK || !srcOK || dstField.Mut() != wasm.Var {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed array.copy types %d:%d", localIndex, offset, dstType, srcType)
				}
				instr.Kind = wasm.InstrArrayCopy
				instr.setU32(dstType)
				instr.setParams(srcType)
				f.HasReferences = true
				depth -= 5
			case 18, 19: // array.init_data / array.init_elem
				typeIndex, typeErr := r.U32()
				segmentIndex, segmentErr := r.U32()
				field, ok := m.ArrayField(typeIndex)
				if typeErr != nil || segmentErr != nil || !ok || field.Mut() != wasm.Var {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed or immutable array initializer type/segment %d:%d", localIndex, offset, typeIndex, segmentIndex)
				}
				if subopcode == 18 && field.Storage().Val().Kind() == wasm.ValRef {
					return nil, fmt.Errorf("railssa: function %d byte %d: array.init_data type %d is a reference array", localIndex, offset, typeIndex)
				}
				if subopcode == 19 && (field.Storage().Packed() || field.Storage().Val().Kind() != wasm.ValRef) {
					return nil, fmt.Errorf("railssa: function %d byte %d: array.init_elem type %d is not a reference array", localIndex, offset, typeIndex)
				}
				instr.Kind = wasm.InstrArrayInitData
				if subopcode == 19 {
					instr.Kind = wasm.InstrArrayInitElem
				}
				instr.setU32(typeIndex)
				instr.setParams(segmentIndex)
				f.HasReferences = true
				depth -= 4
			case 20, 21: // ref.test / ref.test null
				heap, err := r.S33()
				if err == nil && !draglineGCRefTargetSupported(m, heap) {
					return nil, fmt.Errorf("railssa: function %d byte %d: ref.test heap type %d is not yet supported", localIndex, offset, heap)
				}
				encoded, ok := codegen.EncodeGCRefTarget(heap, subopcode == 21, false)
				if err != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed ref.test heap type", localIndex, offset)
				}
				instr.Kind = wasm.InstrRefTest
				instr.aux = encoded
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.I32)
				f.HasReferences = true
			case 22, 23: // ref.cast / ref.cast null
				exact := false
				if b, ok := r.Peek(); ok && b == 0x62 {
					_, _ = r.Byte()
					exact = true
				}
				heap, err := r.S33()
				if err == nil && !draglineGCRefTargetSupported(m, heap) {
					return nil, fmt.Errorf("railssa: function %d byte %d: ref.cast heap type %d is not yet supported", localIndex, offset, heap)
				}
				encoded, ok := codegen.EncodeGCRefTarget(heap, subopcode == 23, exact)
				if err != nil || !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed ref.cast heap type", localIndex, offset)
				}
				var target wasm.HeapType
				if heap < 0 {
					target = wasm.AbsHeap(wasm.AbsHeapType(byte(heap & 0x7f)))
				} else {
					target = wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)})
				}
				instr.Kind = wasm.InstrRefCast
				instr.aux = encoded
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{wasm.RefVal(wasm.Ref(subopcode == 23, target, exact))})
				f.HasReferences = true
			case 24, 25: // br_on_cast / br_on_cast_fail
				flags, flagsErr := r.Byte()
				label, labelErr := r.U32()
				sourceHeap, sourceErr := r.S33()
				targetHeap, targetErr := r.S33()
				if flagsErr != nil || labelErr != nil || sourceErr != nil || targetErr != nil || flags > 3 || int(label) > len(controls) {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed branch cast", localIndex, offset)
				}
				if !draglineGCRefTargetSupported(m, sourceHeap) || !draglineGCRefTargetSupported(m, targetHeap) {
					return nil, fmt.Errorf("railssa: function %d byte %d: branch cast heap types %d -> %d are not yet supported", localIndex, offset, sourceHeap, targetHeap)
				}
				targetEncoded, ok := codegen.EncodeGCRefTarget(targetHeap, flags&2 != 0, false)
				if !ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: malformed branch cast target", localIndex, offset)
				}
				heapType := func(heap int64) wasm.HeapType {
					if heap < 0 {
						return wasm.AbsHeap(wasm.AbsHeapType(byte(heap & 0x7f)))
					}
					return wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)})
				}
				source := wasm.Ref(flags&1 != 0, heapType(sourceHeap), false)
				target := wasm.Ref(flags&2 != 0, heapType(targetHeap), false)
				failed := source
				if target.Nullable() {
					failed = failed.WithNullable(false)
				}
				branchType, fallthroughType := wasm.RefVal(target), wasm.RefVal(failed)
				instr.Kind = wasm.InstrBrOnCast
				if subopcode == 25 {
					instr.Kind = wasm.InstrBrOnCastFail
					branchType, fallthroughType = fallthroughType, branchType
				}
				f.BranchCasts = append(f.BranchCasts, BranchCastImmediate{
					Instruction: uint32(len(f.Instrs)), Label: label,
					BranchType: branchType, Fallthrough: fallthroughType, Target: targetEncoded,
				})
				if int(label) < len(controls) {
					targetControl := &controls[len(controls)-1-int(label)]
					if targetControl.kind != wasm.InstrLoop && reachable {
						targetControl.endReached = true
					}
				}
				f.HasReferences = true
			case 26, 27: // any.convert_extern / extern.convert_any
				if subopcode == 26 {
					instr.Kind = wasm.InstrAnyConvertExtern
					f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{wasm.AnyRef})
				} else {
					instr.Kind = wasm.InstrExternConvertAny
					f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{wasm.ExternRef})
				}
				f.HasReferences = true
			case 28: // ref.i31
				instr.Kind = wasm.InstrRefI31
				result := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
				f.setInstructionResults(uint32(len(f.Instrs)), &instr, []wasm.ValType{result})
				f.HasReferences = true
			case 29, 30: // i31.get_s / i31.get_u
				if subopcode == 29 {
					instr.Kind = wasm.InstrI31GetS
				} else {
					instr.Kind = wasm.InstrI31GetU
				}
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.I32)
				f.HasReferences = true
			default:
				return nil, fmt.Errorf("railssa: function %d byte %d: unsupported 0xfb subopcode %d", localIndex, offset, subopcode)
			}
		case 0xfd:
			descriptor, err := wasm.DecodeSIMDInstruction(r, m, false)
			if err != nil {
				return nil, fmt.Errorf("railssa: function %d byte %d: malformed SIMD instruction: %w", localIndex, offset, err)
			}
			instr.Kind = descriptor.Kind
			f.HasV128 = true
			f.SIMDImmediates = append(f.SIMDImmediates, SIMDImmediate{Instruction: uint32(len(f.Instrs)), Descriptor: descriptor})
			switch descriptor.Class {
			case wasm.SIMDEffectLoad:
				if !f.HasMemory {
					return nil, fmt.Errorf("railssa: function %d byte %d: SIMD load without memory", localIndex, offset)
				}
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.V128)
			case wasm.SIMDEffectStore, wasm.SIMDEffectStoreLane:
				if !f.HasMemory {
					return nil, fmt.Errorf("railssa: function %d byte %d: SIMD store without memory", localIndex, offset)
				}
				depth -= 2
			case wasm.SIMDEffectLoadLane:
				if !f.HasMemory {
					return nil, fmt.Errorf("railssa: function %d byte %d: SIMD lane load without memory", localIndex, offset)
				}
				depth--
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.V128)
			case wasm.SIMDEffectSplat, wasm.SIMDEffectUnary:
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.V128)
			case wasm.SIMDEffectExtract:
				instr.meta |= stackInstrResult
				instr.setValueType(descriptor.Scalar)
			case wasm.SIMDEffectReplace, wasm.SIMDEffectShift, wasm.SIMDEffectBinary:
				depth--
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.V128)
			case wasm.SIMDEffectTernary, wasm.SIMDEffectBitselect:
				depth -= 2
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.V128)
			case wasm.SIMDEffectReduceI32:
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.I32)
			case wasm.SIMDEffectConst:
				depth++
				instr.meta |= stackInstrResult
				instr.setValueType(wasm.V128)
			default:
				return nil, fmt.Errorf("railssa: function %d byte %d: unsupported SIMD effect for %s", localIndex, offset, descriptor.Kind)
			}
		default:
			kind, ok := wasm.ImmediateFreeInstructionKind(opcode)
			if !ok || !structuredScalarKind(kind) {
				if ok {
					return nil, fmt.Errorf("railssa: function %d byte %d: instruction %s is outside the structured scalar baseline", localIndex, offset, kind)
				}
				return nil, fmt.Errorf("railssa: function %d byte %d: opcode 0x%02x is outside the structured scalar baseline", localIndex, offset, opcode)
			}
			instr.Kind = kind
			if scalarBinaryKind(kind) || scalarComparisonKind(kind) {
				depth--
			}
		}
		if !wasReachable && opcode != 0x05 && opcode != 0x0b {
			depth = depthBefore
		}
		if wasReachable && depth < 0 {
			return nil, fmt.Errorf("railssa: function %d byte %d: operand stack underflow", localIndex, offset)
		}
		if uint32(depth) > f.MaxStack {
			f.MaxStack = uint32(depth)
		}
		for i := range controls {
			region := &f.Regions[controls[i].region]
			pressure := depth - int(region.StackDepth)
			if pressure > int(region.MaxPressure) {
				region.MaxPressure = uint32(pressure)
			}
		}
		f.Instrs = append(f.Instrs, instr)
	}
	if len(controls) != 0 {
		return nil, fmt.Errorf("railssa: function %d has unterminated structured control", localIndex)
	}
	if depth != len(f.Results) {
		return nil, fmt.Errorf("railssa: function %d result stack depth is %d", localIndex, depth)
	}
	if err := finalizePrepass(f, regionEvents, lastLocalUse, compactPrepass); err != nil {
		return nil, fmt.Errorf("railssa: function %d: %w", localIndex, err)
	}
	f.prepassEvents = regionEvents[:0]
	f.prepassLastUse = lastLocalUse[:0]
	f.prepassControls = controls[:0]
	f.BuildPeakBytes = f.CapacityBytes()
	return f, nil
}

func draglineGCRefTargetSupported(m *wasm.Module, heap int64) bool {
	if heap < 0 {
		switch heap {
		case -13, -15, -16, -18, -19, -20, -21, -22:
			return true
		default:
			return false
		}
	}
	if uint64(heap) > uint64(^uint32(0)) {
		return false
	}
	kind, ok := m.GCCompositeKind(uint32(heap))
	return ok && (kind == wasm.CompStruct || kind == wasm.CompArray || kind == wasm.CompFunc)
}

func (f *StackFunc) multiResult(source uint32) (MultiResultRange, bool) {
	lo, hi := 0, len(f.MultiResults)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if f.MultiResults[mid].Instruction < source {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(f.MultiResults) && f.MultiResults[lo].Instruction == source {
		return f.MultiResults[lo], true
	}
	return MultiResultRange{}, false
}

// setInstructionResults keeps common numeric scalar results inline and stores
// all other result vectors in the existing sparse slab. In particular, a
// reference result retains its full heap type without widening StackInstr.
func (f *StackFunc) setInstructionResults(source uint32, instruction *StackInstr, results []wasm.ValType) {
	if len(results) == 0 {
		return
	}
	instruction.meta |= stackInstrResult
	if len(results) == 1 && results[0].Kind() != wasm.ValRef {
		instruction.setValueType(results[0])
		return
	}
	f.MultiResults = append(f.MultiResults, MultiResultRange{Instruction: source, Start: uint32(len(f.ResultTypes)), Count: uint32(len(results))})
	f.ResultTypes = append(f.ResultTypes, results...)
}

// InstructionResultCount returns the result arity without allocating a scalar
// result slice on the common path.
func (f *StackFunc) InstructionResultCount(source uint32, instruction StackInstr) uint32 {
	if !instruction.HasResult() {
		return 0
	}
	if result, ok := f.multiResult(source); ok {
		return result.Count
	}
	return 1
}

// InstructionResultType returns one source-ordered result type.
func (f *StackFunc) InstructionResultType(source uint32, instruction StackInstr, index uint32) (wasm.ValType, bool) {
	if result, ok := f.multiResult(source); ok {
		if index >= result.Count {
			return wasm.ValType{}, false
		}
		return f.ResultTypes[result.Start+index], true
	}
	if instruction.HasResult() && index == 0 {
		return instruction.ValueType(), true
	}
	return wasm.ValType{}, false
}

func inlineI32AddTarget(m *wasm.Module, target uint32) bool {
	local := int(target) - m.ImportedFuncCount()
	if local < 0 || local >= len(m.Code) {
		return false
	}
	body := m.Code[local].BodyBytes
	return len(body) == 6 && body[0] == 0x20 && body[1] == 0x00 && body[2] == 0x20 && body[3] == 0x01 && body[4] == 0x6a && body[5] == 0x0b
}

func immutableSingleTableTarget(m *wasm.Module) (uint32, bool) {
	if len(m.Tables) != 1 || len(m.Elements) != 1 || m.Elements[0].Mode.Kind != wasm.ElemActive ||
		m.Tables[0].Type.Limits.Min != 1 || m.Elements[0].Mode.Table != 0 || !zeroI32ConstExpr(m.Elements[0].Mode.Offset) ||
		m.Elements[0].Kind.Kind != wasm.ElemFuncs || len(m.Elements[0].Kind.Funcs) != 1 {
		return 0, false
	}
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable {
			return 0, false
		}
	}
	return uint32(m.Elements[0].Kind.Funcs[0]), true
}

func zeroI32ConstExpr(expr wasm.Expr) bool {
	if len(expr.Instrs) != 0 {
		return len(expr.Instrs) == 1 && expr.Instrs[0].Kind == wasm.InstrI32Const && expr.Instrs[0].I32 == 0
	}
	return len(expr.BodyBytes) == 3 && expr.BodyBytes[0] == 0x41 && expr.BodyBytes[1] == 0x00 && expr.BodyBytes[2] == 0x0b
}

func scalarNumber(typ wasm.ValType) bool {
	return scalarInt(typ) || typ == wasm.F32 || typ == wasm.F64
}

func stackValueType(typ wasm.ValType) bool {
	return scalarNumber(typ) || typ == wasm.V128 || typ.Kind() == wasm.ValRef
}

// SIMDImmediateAt returns the side payload for one source instruction.
func (f *StackFunc) SIMDImmediateAt(source uint32) (wasm.SIMDInstructionDescriptor, bool) {
	lo, hi := 0, len(f.SIMDImmediates)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if f.SIMDImmediates[mid].Instruction < source {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(f.SIMDImmediates) && f.SIMDImmediates[lo].Instruction == source {
		return f.SIMDImmediates[lo].Descriptor, true
	}
	return wasm.SIMDInstructionDescriptor{}, false
}

func (f *StackFunc) BranchCastImmediateAt(source uint32) (BranchCastImmediate, bool) {
	lo, hi := 0, len(f.BranchCasts)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if f.BranchCasts[mid].Instruction < source {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(f.BranchCasts) && f.BranchCasts[lo].Instruction == source {
		return f.BranchCasts[lo], true
	}
	return BranchCastImmediate{}, false
}

func scalarBinaryKind(kind wasm.InstrKind) bool {
	if integerBinaryKind(kind) {
		return true
	}
	return kind >= wasm.InstrF32Add && kind <= wasm.InstrF32Copysign ||
		kind >= wasm.InstrF64Add && kind <= wasm.InstrF64Copysign
}

func scalarComparisonKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Eq && kind <= wasm.InstrI32GeU ||
		kind >= wasm.InstrI64Eq && kind <= wasm.InstrI64GeU ||
		kind >= wasm.InstrF32Eq && kind <= wasm.InstrF32Ge ||
		kind >= wasm.InstrF64Eq && kind <= wasm.InstrF64Ge
}

func structuredScalarKind(kind wasm.InstrKind) bool {
	if structuredIntegerKind(kind) {
		return true
	}
	if scalarComparisonKind(kind) {
		return true
	}
	switch kind {
	case wasm.InstrUnreachable, wasm.InstrReturn, wasm.InstrNop,
		wasm.InstrF32Abs, wasm.InstrF32Neg, wasm.InstrF32Ceil, wasm.InstrF32Floor,
		wasm.InstrF32Trunc, wasm.InstrF32Nearest, wasm.InstrF32Sqrt,
		wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div,
		wasm.InstrF32Min, wasm.InstrF32Max, wasm.InstrF32Copysign,
		wasm.InstrF64Abs, wasm.InstrF64Neg, wasm.InstrF64Ceil, wasm.InstrF64Floor,
		wasm.InstrF64Trunc, wasm.InstrF64Nearest, wasm.InstrF64Sqrt,
		wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div,
		wasm.InstrF64Min, wasm.InstrF64Max, wasm.InstrF64Copysign,
		wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U,
		wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
		wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
		wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
		wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
		wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
		wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
		wasm.InstrF32DemoteF64, wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U,
		wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U,
		wasm.InstrF64PromoteF32, wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64,
		wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64,
		wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
		wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U:
		return true
	default:
		return false
	}
}

func integerBinaryKind(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul, wasm.InstrI32DivS, wasm.InstrI32DivU,
		wasm.InstrI32RemS, wasm.InstrI32RemU, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor,
		wasm.InstrI32Shl, wasm.InstrI32ShrS, wasm.InstrI32ShrU, wasm.InstrI32Rotl, wasm.InstrI32Rotr,
		wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul, wasm.InstrI64DivS, wasm.InstrI64DivU,
		wasm.InstrI64RemS, wasm.InstrI64RemU, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor,
		wasm.InstrI64Shl, wasm.InstrI64ShrS, wasm.InstrI64ShrU, wasm.InstrI64Rotl, wasm.InstrI64Rotr:
		return true
	default:
		return false
	}
}

func structuredIntegerKind(kind wasm.InstrKind) bool {
	if integerBinaryKind(kind) {
		return true
	}
	switch kind {
	case wasm.InstrI32Eqz, wasm.InstrI64Eqz,
		wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt,
		wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt,
		wasm.InstrI64ExtendI32S,
		wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
		wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		return true
	default:
		return false
	}
}
