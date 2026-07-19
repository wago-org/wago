package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const MixedOperandSlotCapacity = 256

type MixedOpKind uint8

const (
	MixedConst MixedOpKind = iota
	MixedCopy
	MixedI32Add
	MixedI32Sub
	MixedI32Mul
	MixedI32And
	MixedI32Or
	MixedI32Xor
	MixedI32Eqz
	MixedI32Eq
	MixedI32Ne
	MixedI32LtS
	MixedI32LtU
	MixedI32GtS
	MixedI32GtU
	MixedI32LeS
	MixedI32LeU
	MixedI32GeS
	MixedI32GeU
	MixedI64Add
	MixedI64Sub
	MixedI64Mul
	MixedI64And
	MixedI64Or
	MixedI64Xor
	MixedF32Abs
	MixedF32Neg
	MixedF32Copysign
	MixedF64Abs
	MixedF64Neg
	MixedF64Copysign
	MixedV128Not
	MixedV128And
	MixedV128AndNot
	MixedV128Or
	MixedV128Xor
	MixedV128Bitselect
	MixedI32x4Add
	MixedI32x4Sub
	MixedCall
	MixedUnreachable
	MixedSelect
	MixedBranchZero
	MixedBranchNonzero
	MixedJump
	MixedPollCancellation
	MixedGlobalGet
	MixedGlobalSet
	MixedF64Helper
	MixedF32Helper
	MixedI64Helper
	MixedMemoryLoad
	MixedMemoryStore
	MixedMemorySize
	MixedMemoryGrow
	MixedMemoryInit
	MixedDataDrop
	MixedMemoryCopy
	MixedMemoryFill
	MixedSIMDHelper
)

type MixedOp struct {
	Kind         MixedOpKind
	Dst          uint16
	Left, Right  uint16
	Third        uint16
	Width        uint8
	Arity        uint8
	InputWidth   uint8
	Words        [4]uint32
	Target       uint32
	Args         []MixedValue
	Results      []MixedValue
	Label        int
	HelperOp     uint32
	MemoryOp     uint8
	MemoryOffset uint32
	Lane         uint32
	HasMemory    bool
}

type MixedValue struct {
	Type wasm.ValType
	Slot uint16
}

type MixedLocal struct {
	Type  wasm.ValType
	Slot  uint16
	Width uint8
}

type MixedPlan struct {
	Locals             []MixedLocal
	Ops                []MixedOp
	Results            []MixedValue
	LocalSlots         uint16
	MaxOperandSlots    uint16
	ParameterSlots     uint16
	ResultSlots        uint16
	DeclaredLocalStart uint16
}

func MixedValueSlots(typ wasm.ValType) (uint8, bool) {
	switch typ {
	case wasm.I32, wasm.F32:
		return 1, true
	case wasm.I64, wasm.F64:
		return 2, true
	case wasm.V128:
		return 4, true
	default:
		if typ.Kind == wasm.ValRef && typ.Ref.Nullable && (typ.Ref.Heap == wasm.AbsHeap(wasm.HeapFunc) || typ.Ref.Heap == wasm.AbsHeap(wasm.HeapExtern)) {
			return 1, true
		}
		return 0, false
	}
}

type MixedSignatureResolver func(uint32) (*wasm.CompType, bool)
type MixedGlobalResolver func(uint32) (wasm.ValType, bool, uint32, bool)
type MixedBlockResolver func(uint32) (*wasm.CompType, bool)

func BuildMixedPlan(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) (*MixedPlan, error) {
	return BuildMixedPlanWithResolvers(ft, locals, body, nil, nil)
}

func mixedEncodedValueType(encoded byte) (wasm.ValType, bool) {
	switch encoded {
	case 0x7f:
		return wasm.I32, true
	case 0x7e:
		return wasm.I64, true
	case 0x7d:
		return wasm.F32, true
	case 0x7c:
		return wasm.F64, true
	case 0x7b:
		return wasm.V128, true
	case 0x70:
		return wasm.FuncRef, true
	case 0x6f:
		return wasm.ExternRef, true
	default:
		return wasm.ValType{}, false
	}
}

func BuildMixedPlanWithCalls(ft *wasm.CompType, locals []wasm.LocalRun, body []byte, resolve MixedSignatureResolver) (*MixedPlan, error) {
	return BuildMixedPlanWithResolvers(ft, locals, body, resolve, nil)
}

func BuildMixedPlanWithResolvers(ft *wasm.CompType, locals []wasm.LocalRun, body []byte, resolve MixedSignatureResolver, resolveGlobal MixedGlobalResolver) (*MixedPlan, error) {
	return BuildMixedPlanWithBlockResolver(ft, locals, body, resolve, resolveGlobal, nil)
}

func BuildMixedPlanWithBlockResolver(ft *wasm.CompType, locals []wasm.LocalRun, body []byte, resolve MixedSignatureResolver, resolveGlobal MixedGlobalResolver, resolveBlock MixedBlockResolver) (*MixedPlan, error) {
	if ft == nil || ft.Kind != wasm.CompFunc {
		return nil, fmt.Errorf("mixed function has invalid type")
	}
	p := &MixedPlan{}
	addLocal := func(typ wasm.ValType) error {
		width, ok := MixedValueSlots(typ)
		if !ok {
			return fmt.Errorf("mixed function value type %s is not supported", typ)
		}
		if uint32(p.LocalSlots)+uint32(width) > MixedOperandSlotCapacity {
			return fmt.Errorf("mixed function local frame exceeds %d slots", MixedOperandSlotCapacity)
		}
		p.Locals = append(p.Locals, MixedLocal{Type: typ, Slot: p.LocalSlots, Width: width})
		p.LocalSlots += uint16(width)
		return nil
	}
	for _, typ := range ft.Params {
		if err := addLocal(typ); err != nil {
			return nil, err
		}
	}
	p.ParameterSlots = p.LocalSlots
	p.DeclaredLocalStart = p.LocalSlots
	for _, run := range locals {
		for i := uint32(0); i < run.Count; i++ {
			if err := addLocal(run.Type); err != nil {
				return nil, err
			}
		}
	}

	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, fmt.Errorf("mixed local declarations: %w", err)
	}
	if int(groups) != len(locals) {
		return nil, fmt.Errorf("mixed local declaration group count mismatch")
	}
	for i := uint32(0); i < groups; i++ {
		n, err := r.U32()
		if err != nil {
			return nil, err
		}
		encoded, err := r.Byte()
		if err != nil {
			return nil, err
		}
		want, ok := wasm.EncodeValType(locals[i].Type)
		if !ok || n != locals[i].Count || encoded != want {
			return nil, fmt.Errorf("mixed local declaration %d does not match validated locals", i)
		}
	}

	var stack []MixedValue
	operandSlots := uint16(0)
	push := func(typ wasm.ValType) (MixedValue, error) {
		width, ok := MixedValueSlots(typ)
		if !ok {
			return MixedValue{}, fmt.Errorf("mixed operand type %s is not supported", typ)
		}
		if uint32(p.LocalSlots)+uint32(operandSlots)+uint32(width) > MixedOperandSlotCapacity {
			return MixedValue{}, fmt.Errorf("mixed operand frame exceeds %d slots", MixedOperandSlotCapacity)
		}
		v := MixedValue{Type: typ, Slot: p.LocalSlots + operandSlots}
		operandSlots += uint16(width)
		if operandSlots > p.MaxOperandSlots {
			p.MaxOperandSlots = operandSlots
		}
		stack = append(stack, v)
		return v, nil
	}
	pop := func(want wasm.ValType) (MixedValue, error) {
		if len(stack) == 0 {
			return MixedValue{}, fmt.Errorf("mixed operand stack underflow")
		}
		v := stack[len(stack)-1]
		if v.Type != want {
			return MixedValue{}, fmt.Errorf("mixed operand has type %s, want %s", v.Type, want)
		}
		stack = stack[:len(stack)-1]
		width, _ := MixedValueSlots(v.Type)
		operandSlots -= uint16(width)
		if v.Slot != p.LocalSlots+operandSlots {
			panic("shared: non-LIFO mixed operand slots")
		}
		return v, nil
	}
	unary := func(kind MixedOpKind, typ wasm.ValType) error {
		v, err := pop(typ)
		if err != nil {
			return err
		}
		out, err := push(typ)
		if err != nil {
			return err
		}
		p.Ops = append(p.Ops, MixedOp{Kind: kind, Dst: out.Slot, Left: v.Slot})
		return nil
	}
	bitcast := func(input, output wasm.ValType) error {
		value, err := pop(input)
		if err != nil {
			return err
		}
		inWidth, _ := MixedValueSlots(input)
		outWidth, _ := MixedValueSlots(output)
		if inWidth != outWidth {
			return fmt.Errorf("mixed bitcast width mismatch")
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		p.Ops = append(p.Ops, MixedOp{Kind: MixedCopy, Dst: out.Slot, Left: value.Slot, Width: inWidth})
		return nil
	}
	unaryOp := func(kind MixedOpKind, typ wasm.ValType) error {
		value, err := pop(typ)
		if err != nil {
			return err
		}
		out, err := push(typ)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(typ)
		p.Ops = append(p.Ops, MixedOp{Kind: kind, Dst: out.Slot, Left: value.Slot, Width: width})
		return nil
	}
	binaryOp := func(kind MixedOpKind, typ wasm.ValType) error {
		right, err := pop(typ)
		if err != nil {
			return err
		}
		left, err := pop(typ)
		if err != nil {
			return err
		}
		out, err := push(typ)
		if err != nil {
			return err
		}
		p.Ops = append(p.Ops, MixedOp{Kind: kind, Dst: out.Slot, Left: left.Slot, Right: right.Slot})
		return nil
	}
	f32HelperUnary := func(op embedded32.F32Op, input, output wasm.ValType) error {
		value, err := pop(input)
		if err != nil {
			return err
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(output)
		inputWidth, _ := MixedValueSlots(input)
		p.Ops = append(p.Ops, MixedOp{Kind: MixedF32Helper, Dst: out.Slot, Left: value.Slot, Width: width, Arity: 1, InputWidth: inputWidth, HelperOp: uint32(op)})
		return nil
	}
	f32HelperBinary := func(op embedded32.F32Op, output wasm.ValType) error {
		right, err := pop(wasm.F32)
		if err != nil {
			return err
		}
		left, err := pop(wasm.F32)
		if err != nil {
			return err
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(output)
		p.Ops = append(p.Ops, MixedOp{Kind: MixedF32Helper, Dst: out.Slot, Left: left.Slot, Right: right.Slot, Width: width, Arity: 2, InputWidth: 1, HelperOp: uint32(op)})
		return nil
	}
	helperUnary := func(op uint32, input, output wasm.ValType) error {
		value, err := pop(input)
		if err != nil {
			return err
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(output)
		inputWidth, _ := MixedValueSlots(input)
		p.Ops = append(p.Ops, MixedOp{Kind: MixedF64Helper, Dst: out.Slot, Left: value.Slot, Width: width, Arity: 1, InputWidth: inputWidth, HelperOp: op})
		return nil
	}
	helperBinary := func(op uint32, input, output wasm.ValType) error {
		right, err := pop(input)
		if err != nil {
			return err
		}
		left, err := pop(input)
		if err != nil {
			return err
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(output)
		inputWidth, _ := MixedValueSlots(input)
		p.Ops = append(p.Ops, MixedOp{Kind: MixedF64Helper, Dst: out.Slot, Left: left.Slot, Right: right.Slot, Width: width, Arity: 2, InputWidth: inputWidth, HelperOp: op})
		return nil
	}
	i64HelperUnary := func(op embedded32.I64Op, input, output wasm.ValType) error {
		value, err := pop(input)
		if err != nil {
			return err
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(output)
		inputWidth, _ := MixedValueSlots(input)
		p.Ops = append(p.Ops, MixedOp{Kind: MixedI64Helper, Dst: out.Slot, Left: value.Slot, Width: width, Arity: 1, InputWidth: inputWidth, HelperOp: uint32(op)})
		return nil
	}
	i64HelperBinary := func(op embedded32.I64Op, output wasm.ValType) error {
		right, err := pop(wasm.I64)
		if err != nil {
			return err
		}
		left, err := pop(wasm.I64)
		if err != nil {
			return err
		}
		out, err := push(output)
		if err != nil {
			return err
		}
		width, _ := MixedValueSlots(output)
		p.Ops = append(p.Ops, MixedOp{Kind: MixedI64Helper, Dst: out.Slot, Left: left.Slot, Right: right.Slot, Width: width, Arity: 2, InputWidth: 2, HelperOp: uint32(op)})
		return nil
	}
	simdHelperSignature := func(op uint32, inputTypes, outputTypes []wasm.ValType) error {
		inputs := make([]MixedValue, len(inputTypes))
		for i := len(inputTypes) - 1; i >= 0; i-- {
			value, err := pop(inputTypes[i])
			if err != nil {
				return err
			}
			inputs[i] = value
		}
		outputs := make([]MixedValue, len(outputTypes))
		for i, typ := range outputTypes {
			value, err := push(typ)
			if err != nil {
				return err
			}
			outputs[i] = value
		}
		p.Ops = append(p.Ops, MixedOp{Kind: MixedSIMDHelper, HelperOp: op, Args: inputs, Results: outputs})
		return nil
	}
	simdHelper := func(op uint32, arity uint8) error {
		inputs := make([]wasm.ValType, arity)
		for i := range inputs {
			inputs[i] = wasm.V128
		}
		return simdHelperSignature(op, inputs, []wasm.ValType{wasm.V128})
	}

	type mixedBlockSignature struct {
		params, results []wasm.ValType
	}
	type mixedControl struct {
		kind       byte
		base       []MixedValue
		start      []MixedValue
		baseSlots  uint16
		startSlots uint16
		header     int
		falseOp    int
		jumpOp     int
		elseSeen   bool
		pending    []int
		params     []wasm.ValType
		results    []wasm.ValType
		armResults []MixedValue
	}
	var controls []mixedControl
	stackMatches := func(want []MixedValue) bool {
		if len(stack) != len(want) {
			return false
		}
		for i := range stack {
			if stack[i] != want[i] {
				return false
			}
		}
		return true
	}
	readBlockSignature := func() (mixedBlockSignature, error) {
		encoded, ok := r.Peek()
		if !ok {
			return mixedBlockSignature{}, fmt.Errorf("mixed control type is truncated")
		}
		if encoded == 0x40 {
			_, _ = r.Byte()
			return mixedBlockSignature{}, nil
		}
		if typ, ok := mixedEncodedValueType(encoded); ok {
			_, _ = r.Byte()
			return mixedBlockSignature{results: []wasm.ValType{typ}}, nil
		}
		index, err := r.S33()
		if err != nil || index < 0 || resolveBlock == nil {
			return mixedBlockSignature{}, fmt.Errorf("mixed control type index is invalid")
		}
		block, ok := resolveBlock(uint32(index))
		if !ok || block == nil || block.Kind != wasm.CompFunc {
			return mixedBlockSignature{}, fmt.Errorf("mixed control type %d is invalid", index)
		}
		for _, typ := range append(append([]wasm.ValType(nil), block.Params...), block.Results...) {
			if _, ok := MixedValueSlots(typ); !ok {
				return mixedBlockSignature{}, fmt.Errorf("mixed control value type %s is not supported", typ)
			}
		}
		return mixedBlockSignature{params: append([]wasm.ValType(nil), block.Params...), results: append([]wasm.ValType(nil), block.Results...)}, nil
	}
	newControl := func(kind byte, sig mixedBlockSignature) (mixedControl, error) {
		if len(stack) < len(sig.params) {
			return mixedControl{}, fmt.Errorf("mixed control parameter stack underflow")
		}
		paramSlots := uint16(0)
		baseLen := len(stack) - len(sig.params)
		for i, typ := range sig.params {
			if stack[baseLen+i].Type != typ {
				return mixedControl{}, fmt.Errorf("mixed control parameter %d has type %s, want %s", i, stack[baseLen+i].Type, typ)
			}
			width, _ := MixedValueSlots(typ)
			paramSlots += uint16(width)
		}
		return mixedControl{
			kind: kind, base: append([]MixedValue(nil), stack[:baseLen]...), start: append([]MixedValue(nil), stack...),
			baseSlots: operandSlots - paramSlots, startSlots: operandSlots, falseOp: -1, jumpOp: -1,
			params: append([]wasm.ValType(nil), sig.params...), results: append([]wasm.ValType(nil), sig.results...),
		}, nil
	}
	controlMatches := func(control *mixedControl) ([]MixedValue, bool) {
		if len(stack) != len(control.base)+len(control.results) {
			return nil, false
		}
		for i := range control.base {
			if stack[i] != control.base[i] {
				return nil, false
			}
		}
		expectedSlots := control.baseSlots
		for i, typ := range control.results {
			value := stack[len(control.base)+i]
			if value.Type != typ || value.Slot != p.LocalSlots+expectedSlots {
				return nil, false
			}
			width, _ := MixedValueSlots(typ)
			expectedSlots += uint16(width)
		}
		if operandSlots != expectedSlots {
			return nil, false
		}
		values := append([]MixedValue(nil), stack[len(control.base):]...)
		return values, true
	}
	terminated := false
	terminalUnreachable := false
	branchTerminated := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if terminated && op != 0x0b {
			return nil, fmt.Errorf("mixed function currently requires terminal unreachable")
		}
		if branchTerminated && op != 0x05 && op != 0x0b {
			return nil, fmt.Errorf("mixed unconditional branch currently requires an immediate else or end")
		}
		switch op {
		case 0x00: // unreachable
			p.Ops = append(p.Ops, MixedOp{Kind: MixedUnreachable})
			terminated = true
			terminalUnreachable = true
		case 0x01: // nop
		case 0x02, 0x03: // block / loop
			sig, err := readBlockSignature()
			if err != nil {
				return nil, err
			}
			control, err := newControl(op, sig)
			if err != nil {
				return nil, err
			}
			if op == 0x03 {
				control.header = len(p.Ops)
				p.Ops = append(p.Ops, MixedOp{Kind: MixedPollCancellation})
			}
			controls = append(controls, control)
		case 0x04: // if
			sig, err := readBlockSignature()
			if err != nil {
				return nil, err
			}
			condition, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			control, err := newControl(op, sig)
			if err != nil {
				return nil, err
			}
			branch := len(p.Ops)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedBranchZero, Third: condition.Slot, Label: -1})
			control.falseOp = branch
			controls = append(controls, control)
		case 0x05: // else
			if len(controls) == 0 || controls[len(controls)-1].kind != 0x04 || controls[len(controls)-1].elseSeen {
				return nil, fmt.Errorf("mixed function has unexpected else")
			}
			control := &controls[len(controls)-1]
			armResults, ok := controlMatches(control)
			if !ok {
				return nil, fmt.Errorf("mixed if true arm does not match block results")
			}
			control.armResults = armResults
			control.jumpOp = len(p.Ops)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedJump, Label: -1})
			p.Ops[control.falseOp].Label = len(p.Ops)
			control.elseSeen = true
			stack = append(stack[:0], control.start...)
			operandSlots = control.startSlots
			branchTerminated = false
		case 0x0b: // end
			if len(controls) != 0 {
				control := controls[len(controls)-1]
				results, ok := controlMatches(&control)
				if !ok {
					return nil, fmt.Errorf("mixed control arm does not match block results")
				}
				if control.kind == 0x04 && len(control.results) != 0 {
					if !control.elseSeen {
						return nil, fmt.Errorf("mixed result if requires else")
					}
					if len(results) != len(control.armResults) {
						return nil, fmt.Errorf("mixed if result arity mismatch")
					}
					for i := range results {
						if results[i] != control.armResults[i] {
							return nil, fmt.Errorf("mixed if result slots do not merge atomically")
						}
					}
				}
				target := len(p.Ops)
				if control.kind == 0x04 {
					if control.elseSeen {
						p.Ops[control.jumpOp].Label = target
					} else {
						p.Ops[control.falseOp].Label = target
					}
				}
				for _, branch := range control.pending {
					p.Ops[branch].Label = target
				}
				controls = controls[:len(controls)-1]
				branchTerminated = false
				continue
			}
			if r.HasNext() {
				return nil, fmt.Errorf("mixed function has instructions after end")
			}
			if !terminalUnreachable {
				if len(stack) != len(ft.Results) {
					return nil, fmt.Errorf("mixed result stack has %d values, want %d", len(stack), len(ft.Results))
				}
				for i := range stack {
					if stack[i].Type != ft.Results[i] {
						return nil, fmt.Errorf("mixed result %d has type %s, want %s", i, stack[i].Type, ft.Results[i])
					}
				}
				p.Results = append(p.Results, stack...)
			}
			for _, typ := range ft.Results {
				width, _ := MixedValueSlots(typ)
				p.ResultSlots += uint16(width)
			}
			return p, nil
		case 0x0c: // br to the innermost non-loop label
			depth, err := r.U32()
			if err != nil || depth != 0 || len(controls) == 0 {
				return nil, fmt.Errorf("mixed br currently supports only depth zero")
			}
			target := &controls[len(controls)-1]
			if target.kind == 0x03 {
				return nil, fmt.Errorf("mixed unconditional loop backedge is not yet supported")
			}
			if _, ok := controlMatches(target); !ok {
				return nil, fmt.Errorf("mixed br values do not match the target label")
			}
			branch := len(p.Ops)
			target.pending = append(target.pending, branch)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedJump, Label: -1})
			branchTerminated = true
		case 0x0d: // br_if to a typed label
			depth, err := r.U32()
			if err != nil || int(depth) >= len(controls) {
				return nil, fmt.Errorf("mixed br_if depth %d is invalid", depth)
			}
			condition, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			targetIndex := len(controls) - 1 - int(depth)
			target := &controls[targetIndex]
			matches := false
			if target.kind == 0x03 {
				matches = stackMatches(target.start) && operandSlots == target.startSlots
			} else {
				_, matches = controlMatches(target)
			}
			if !matches {
				return nil, fmt.Errorf("mixed br_if values do not match the target label")
			}
			branch := len(p.Ops)
			label := -1
			if target.kind == 0x03 {
				label = target.header
			} else {
				target.pending = append(target.pending, branch)
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedBranchNonzero, Third: condition.Slot, Label: label})
		case 0x0f: // return
			terminated = true
		case 0x10: // call
			target, err := r.U32()
			if err != nil {
				return nil, err
			}
			if resolve == nil {
				return nil, fmt.Errorf("mixed function call requires module signatures")
			}
			callee, ok := resolve(target)
			if !ok || callee == nil || callee.Kind != wasm.CompFunc {
				return nil, fmt.Errorf("mixed function call target %d is invalid", target)
			}
			args := make([]MixedValue, len(callee.Params))
			for i := len(callee.Params) - 1; i >= 0; i-- {
				v, err := pop(callee.Params[i])
				if err != nil {
					return nil, fmt.Errorf("mixed call target %d argument %d: %w", target, i, err)
				}
				args[i] = v
			}
			results := make([]MixedValue, len(callee.Results))
			for i, typ := range callee.Results {
				v, err := push(typ)
				if err != nil {
					return nil, fmt.Errorf("mixed call target %d result %d: %w", target, i, err)
				}
				results[i] = v
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedCall, Target: target, Args: args, Results: results})
		case 0x1a: // drop
			if len(stack) == 0 {
				return nil, fmt.Errorf("mixed drop stack underflow")
			}
			if _, err := pop(stack[len(stack)-1].Type); err != nil {
				return nil, err
			}
		case 0x1b, 0x1c: // select / typed select
			var selectedType wasm.ValType
			if op == 0x1c {
				n, err := r.U32()
				if err != nil || n != 1 {
					return nil, fmt.Errorf("mixed typed select must contain one type")
				}
				encoded, err := r.Byte()
				if err != nil {
					return nil, err
				}
				var ok bool
				selectedType, ok = mixedEncodedValueType(encoded)
				if !ok {
					return nil, fmt.Errorf("mixed typed select type %#x is not supported", encoded)
				}
			}
			condition, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			if len(stack) == 0 {
				return nil, fmt.Errorf("mixed select value stack underflow")
			}
			if op == 0x1b {
				selectedType = stack[len(stack)-1].Type
			}
			right, err := pop(selectedType)
			if err != nil {
				return nil, err
			}
			left, err := pop(selectedType)
			if err != nil {
				return nil, err
			}
			out, err := push(selectedType)
			if err != nil {
				return nil, err
			}
			width, _ := MixedValueSlots(selectedType)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedSelect, Dst: out.Slot, Left: left.Slot, Right: right.Slot, Third: condition.Slot, Width: width})
		case 0x20, 0x21, 0x22: // local.get/set/tee
			idx, err := r.U32()
			if err != nil || int(idx) >= len(p.Locals) {
				return nil, fmt.Errorf("mixed local index %d", idx)
			}
			local := p.Locals[idx]
			if op == 0x20 {
				out, err := push(local.Type)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedCopy, Dst: out.Slot, Left: local.Slot, Width: local.Width})
			} else if op == 0x21 {
				v, err := pop(local.Type)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedCopy, Dst: local.Slot, Left: v.Slot, Width: local.Width})
			} else {
				if len(stack) == 0 || stack[len(stack)-1].Type != local.Type {
					return nil, fmt.Errorf("mixed local.tee type mismatch")
				}
				v := stack[len(stack)-1]
				p.Ops = append(p.Ops, MixedOp{Kind: MixedCopy, Dst: local.Slot, Left: v.Slot, Width: local.Width})
			}
		case 0x23, 0x24: // global.get/set
			index, err := r.U32()
			if err != nil {
				return nil, err
			}
			if resolveGlobal == nil {
				return nil, fmt.Errorf("mixed global operation requires module globals")
			}
			typ, mutable, slot, ok := resolveGlobal(index)
			if !ok {
				return nil, fmt.Errorf("mixed global index %d is invalid", index)
			}
			width, supported := MixedValueSlots(typ)
			if !supported || slot > ^uint32(0)-uint32(width) {
				return nil, fmt.Errorf("mixed global %d type %s is not supported", index, typ)
			}
			if op == 0x23 {
				out, err := push(typ)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedGlobalGet, Dst: out.Slot, Target: slot, Width: width})
			} else {
				if !mutable {
					return nil, fmt.Errorf("mixed global %d is immutable", index)
				}
				value, err := pop(typ)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedGlobalSet, Left: value.Slot, Target: slot, Width: width})
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35:
			if _, err := r.U32(); err != nil { // alignment hint
				return nil, err
			}
			offset, err := r.U32()
			if err != nil {
				return nil, err
			}
			loadOp, ok := embedded32.ScalarLoadForWasmOpcode(op)
			if !ok {
				return nil, fmt.Errorf("mixed scalar load opcode %#x", op)
			}
			address, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			resultType := wasm.I32
			switch op {
			case 0x29, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35:
				resultType = wasm.I64
			case 0x2a:
				resultType = wasm.F32
			case 0x2b:
				resultType = wasm.F64
			}
			out, err := push(resultType)
			if err != nil {
				return nil, err
			}
			width, _ := MixedValueSlots(resultType)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedMemoryLoad, Dst: out.Slot, Left: address.Slot, Width: width, MemoryOp: uint8(loadOp), MemoryOffset: offset})
		case 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
			if _, err := r.U32(); err != nil { // alignment hint
				return nil, err
			}
			offset, err := r.U32()
			if err != nil {
				return nil, err
			}
			storeOp, ok := embedded32.ScalarStoreForWasmOpcode(op)
			if !ok {
				return nil, fmt.Errorf("mixed scalar store opcode %#x", op)
			}
			valueType := wasm.I32
			switch op {
			case 0x37, 0x3c, 0x3d, 0x3e:
				valueType = wasm.I64
			case 0x38:
				valueType = wasm.F32
			case 0x39:
				valueType = wasm.F64
			}
			value, err := pop(valueType)
			if err != nil {
				return nil, err
			}
			address, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			width, _ := MixedValueSlots(valueType)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedMemoryStore, Left: address.Slot, Right: value.Slot, Width: width, MemoryOp: uint8(storeOp), MemoryOffset: offset})
		case 0x3f: // memory.size
			memory, err := r.U32()
			if err != nil || memory != 0 {
				return nil, fmt.Errorf("mixed memory.size requires memory zero")
			}
			out, err := push(wasm.I32)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedMemorySize, Dst: out.Slot, Width: 1})
		case 0x40: // memory.grow
			memory, err := r.U32()
			if err != nil || memory != 0 {
				return nil, fmt.Errorf("mixed memory.grow requires memory zero")
			}
			delta, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			out, err := push(wasm.I32)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedMemoryGrow, Dst: out.Slot, Left: delta.Slot, Width: 1})
		case 0x41:
			value, err := r.I32()
			if err != nil {
				return nil, err
			}
			out, err := push(wasm.I32)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 1, Words: [4]uint32{uint32(value)}})
		case 0x42:
			value, err := r.I64()
			if err != nil {
				return nil, err
			}
			out, err := push(wasm.I64)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 2, Words: [4]uint32{uint32(value), uint32(uint64(value) >> 32)}})
		case 0x43:
			bits, err := r.Bytes(4)
			if err != nil {
				return nil, err
			}
			out, err := push(wasm.F32)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 1, Words: [4]uint32{binary.LittleEndian.Uint32(bits)}})
		case 0x44:
			bits, err := r.Bytes(8)
			if err != nil {
				return nil, err
			}
			out, err := push(wasm.F64)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 2, Words: [4]uint32{binary.LittleEndian.Uint32(bits), binary.LittleEndian.Uint32(bits[4:])}})
		case 0x45:
			if err := unaryOp(MixedI32Eqz, wasm.I32); err != nil {
				return nil, err
			}
		case 0x46:
			if err := binaryOp(MixedI32Eq, wasm.I32); err != nil {
				return nil, err
			}
		case 0x47:
			if err := binaryOp(MixedI32Ne, wasm.I32); err != nil {
				return nil, err
			}
		case 0x48:
			if err := binaryOp(MixedI32LtS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x49:
			if err := binaryOp(MixedI32LtU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x4a:
			if err := binaryOp(MixedI32GtS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x4b:
			if err := binaryOp(MixedI32GtU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x4c:
			if err := binaryOp(MixedI32LeS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x4d:
			if err := binaryOp(MixedI32LeU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x4e:
			if err := binaryOp(MixedI32GeS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x4f:
			if err := binaryOp(MixedI32GeU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x5b:
			if err := f32HelperBinary(embedded32.F32Eq, wasm.I32); err != nil {
				return nil, err
			}
		case 0x5c:
			if err := f32HelperBinary(embedded32.F32Ne, wasm.I32); err != nil {
				return nil, err
			}
		case 0x5d:
			if err := f32HelperBinary(embedded32.F32Lt, wasm.I32); err != nil {
				return nil, err
			}
		case 0x5e:
			if err := f32HelperBinary(embedded32.F32Gt, wasm.I32); err != nil {
				return nil, err
			}
		case 0x5f:
			if err := f32HelperBinary(embedded32.F32Le, wasm.I32); err != nil {
				return nil, err
			}
		case 0x60:
			if err := f32HelperBinary(embedded32.F32Ge, wasm.I32); err != nil {
				return nil, err
			}
		case 0x50:
			if err := i64HelperUnary(embedded32.I64Eqz, wasm.I64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x51:
			if err := i64HelperBinary(embedded32.I64Eq, wasm.I32); err != nil {
				return nil, err
			}
		case 0x52:
			if err := i64HelperBinary(embedded32.I64Ne, wasm.I32); err != nil {
				return nil, err
			}
		case 0x53:
			if err := i64HelperBinary(embedded32.I64LtS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x54:
			if err := i64HelperBinary(embedded32.I64LtU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x55:
			if err := i64HelperBinary(embedded32.I64GtS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x56:
			if err := i64HelperBinary(embedded32.I64GtU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x57:
			if err := i64HelperBinary(embedded32.I64LeS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x58:
			if err := i64HelperBinary(embedded32.I64LeU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x59:
			if err := i64HelperBinary(embedded32.I64GeS, wasm.I32); err != nil {
				return nil, err
			}
		case 0x5a:
			if err := i64HelperBinary(embedded32.I64GeU, wasm.I32); err != nil {
				return nil, err
			}
		case 0x61:
			if err := helperBinary(uint32(embedded32.F64Eq), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x62:
			if err := helperBinary(uint32(embedded32.F64Ne), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x63:
			if err := helperBinary(uint32(embedded32.F64Lt), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x64:
			if err := helperBinary(uint32(embedded32.F64Gt), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x65:
			if err := helperBinary(uint32(embedded32.F64Le), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x66:
			if err := helperBinary(uint32(embedded32.F64Ge), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0x6a:
			if err := binaryOp(MixedI32Add, wasm.I32); err != nil {
				return nil, err
			}
		case 0x6b:
			if err := binaryOp(MixedI32Sub, wasm.I32); err != nil {
				return nil, err
			}
		case 0x6c:
			if err := binaryOp(MixedI32Mul, wasm.I32); err != nil {
				return nil, err
			}
		case 0x71:
			if err := binaryOp(MixedI32And, wasm.I32); err != nil {
				return nil, err
			}
		case 0x72:
			if err := binaryOp(MixedI32Or, wasm.I32); err != nil {
				return nil, err
			}
		case 0x73:
			if err := binaryOp(MixedI32Xor, wasm.I32); err != nil {
				return nil, err
			}
		case 0x79:
			if err := i64HelperUnary(embedded32.I64Clz, wasm.I64, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7a:
			if err := i64HelperUnary(embedded32.I64Ctz, wasm.I64, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7b:
			if err := i64HelperUnary(embedded32.I64Popcnt, wasm.I64, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7c:
			if err := binaryOp(MixedI64Add, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7d:
			if err := binaryOp(MixedI64Sub, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7e:
			if err := binaryOp(MixedI64Mul, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7f:
			if err := i64HelperBinary(embedded32.I64DivS, wasm.I64); err != nil {
				return nil, err
			}
		case 0x80:
			if err := i64HelperBinary(embedded32.I64DivU, wasm.I64); err != nil {
				return nil, err
			}
		case 0x81:
			if err := i64HelperBinary(embedded32.I64RemS, wasm.I64); err != nil {
				return nil, err
			}
		case 0x82:
			if err := i64HelperBinary(embedded32.I64RemU, wasm.I64); err != nil {
				return nil, err
			}
		case 0x83:
			if err := binaryOp(MixedI64And, wasm.I64); err != nil {
				return nil, err
			}
		case 0x84:
			if err := binaryOp(MixedI64Or, wasm.I64); err != nil {
				return nil, err
			}
		case 0x85:
			if err := binaryOp(MixedI64Xor, wasm.I64); err != nil {
				return nil, err
			}
		case 0x86:
			if err := i64HelperBinary(embedded32.I64Shl, wasm.I64); err != nil {
				return nil, err
			}
		case 0x87:
			if err := i64HelperBinary(embedded32.I64ShrS, wasm.I64); err != nil {
				return nil, err
			}
		case 0x88:
			if err := i64HelperBinary(embedded32.I64ShrU, wasm.I64); err != nil {
				return nil, err
			}
		case 0x89:
			if err := i64HelperBinary(embedded32.I64Rotl, wasm.I64); err != nil {
				return nil, err
			}
		case 0x8a:
			if err := i64HelperBinary(embedded32.I64Rotr, wasm.I64); err != nil {
				return nil, err
			}
		case 0x8b:
			if err := unary(MixedF32Abs, wasm.F32); err != nil {
				return nil, err
			}
		case 0x8c:
			if err := unary(MixedF32Neg, wasm.F32); err != nil {
				return nil, err
			}
		case 0x8d:
			if err := f32HelperUnary(embedded32.F32Ceil, wasm.F32, wasm.F32); err != nil {
				return nil, err
			}
		case 0x8e:
			if err := f32HelperUnary(embedded32.F32Floor, wasm.F32, wasm.F32); err != nil {
				return nil, err
			}
		case 0x8f:
			if err := f32HelperUnary(embedded32.F32Trunc, wasm.F32, wasm.F32); err != nil {
				return nil, err
			}
		case 0x90:
			if err := f32HelperUnary(embedded32.F32Nearest, wasm.F32, wasm.F32); err != nil {
				return nil, err
			}
		case 0x91:
			if err := f32HelperUnary(embedded32.F32Sqrt, wasm.F32, wasm.F32); err != nil {
				return nil, err
			}
		case 0x92:
			if err := f32HelperBinary(embedded32.F32Add, wasm.F32); err != nil {
				return nil, err
			}
		case 0x93:
			if err := f32HelperBinary(embedded32.F32Sub, wasm.F32); err != nil {
				return nil, err
			}
		case 0x94:
			if err := f32HelperBinary(embedded32.F32Mul, wasm.F32); err != nil {
				return nil, err
			}
		case 0x95:
			if err := f32HelperBinary(embedded32.F32Div, wasm.F32); err != nil {
				return nil, err
			}
		case 0x96:
			if err := f32HelperBinary(embedded32.F32Min, wasm.F32); err != nil {
				return nil, err
			}
		case 0x97:
			if err := f32HelperBinary(embedded32.F32Max, wasm.F32); err != nil {
				return nil, err
			}
		case 0x98:
			if err := binaryOp(MixedF32Copysign, wasm.F32); err != nil {
				return nil, err
			}
		case 0x99:
			if err := unary(MixedF64Abs, wasm.F64); err != nil {
				return nil, err
			}
		case 0x9a:
			if err := unary(MixedF64Neg, wasm.F64); err != nil {
				return nil, err
			}
		case 0x9b:
			if err := helperUnary(uint32(embedded32.F64Ceil), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0x9c:
			if err := helperUnary(uint32(embedded32.F64Floor), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0x9d:
			if err := helperUnary(uint32(embedded32.F64Trunc), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0x9e:
			if err := helperUnary(uint32(embedded32.F64Nearest), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0x9f:
			if err := helperUnary(uint32(embedded32.F64Sqrt), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa0:
			if err := helperBinary(uint32(embedded32.F64Add), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa1:
			if err := helperBinary(uint32(embedded32.F64Sub), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa2:
			if err := helperBinary(uint32(embedded32.F64Mul), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa3:
			if err := helperBinary(uint32(embedded32.F64Div), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa4:
			if err := helperBinary(uint32(embedded32.F64Min), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa5:
			if err := helperBinary(uint32(embedded32.F64Max), wasm.F64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa6:
			if err := binaryOp(MixedF64Copysign, wasm.F64); err != nil {
				return nil, err
			}
		case 0xa8:
			if err := f32HelperUnary(embedded32.I32TruncF32S, wasm.F32, wasm.I32); err != nil {
				return nil, err
			}
		case 0xa9:
			if err := f32HelperUnary(embedded32.I32TruncF32U, wasm.F32, wasm.I32); err != nil {
				return nil, err
			}
		case 0xae:
			if err := f32HelperUnary(embedded32.I64TruncF32S, wasm.F32, wasm.I64); err != nil {
				return nil, err
			}
		case 0xaf:
			if err := f32HelperUnary(embedded32.I64TruncF32U, wasm.F32, wasm.I64); err != nil {
				return nil, err
			}
		case 0xac:
			if err := i64HelperUnary(embedded32.I64ExtendI32S, wasm.I32, wasm.I64); err != nil {
				return nil, err
			}
		case 0xad:
			if err := i64HelperUnary(embedded32.I64ExtendI32U, wasm.I32, wasm.I64); err != nil {
				return nil, err
			}
		case 0xaa:
			if err := helperUnary(uint32(embedded32.I32TruncF64S), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0xab:
			if err := helperUnary(uint32(embedded32.I32TruncF64U), wasm.F64, wasm.I32); err != nil {
				return nil, err
			}
		case 0xb0:
			if err := helperUnary(uint32(embedded32.I64TruncF64S), wasm.F64, wasm.I64); err != nil {
				return nil, err
			}
		case 0xb1:
			if err := helperUnary(uint32(embedded32.I64TruncF64U), wasm.F64, wasm.I64); err != nil {
				return nil, err
			}
		case 0xb2:
			if err := f32HelperUnary(embedded32.F32ConvertI32S, wasm.I32, wasm.F32); err != nil {
				return nil, err
			}
		case 0xb3:
			if err := f32HelperUnary(embedded32.F32ConvertI32U, wasm.I32, wasm.F32); err != nil {
				return nil, err
			}
		case 0xb4:
			if err := f32HelperUnary(embedded32.F32ConvertI64S, wasm.I64, wasm.F32); err != nil {
				return nil, err
			}
		case 0xb5:
			if err := f32HelperUnary(embedded32.F32ConvertI64U, wasm.I64, wasm.F32); err != nil {
				return nil, err
			}
		case 0xb6:
			if err := f32HelperUnary(embedded32.F32DemoteF64, wasm.F64, wasm.F32); err != nil {
				return nil, err
			}
		case 0xb7:
			if err := helperUnary(uint32(embedded32.F64ConvertI32S), wasm.I32, wasm.F64); err != nil {
				return nil, err
			}
		case 0xb8:
			if err := helperUnary(uint32(embedded32.F64ConvertI32U), wasm.I32, wasm.F64); err != nil {
				return nil, err
			}
		case 0xb9:
			if err := helperUnary(uint32(embedded32.F64ConvertI64S), wasm.I64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xba:
			if err := helperUnary(uint32(embedded32.F64ConvertI64U), wasm.I64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xbb:
			if err := helperUnary(uint32(embedded32.F64PromoteF32), wasm.F32, wasm.F64); err != nil {
				return nil, err
			}
		case 0xbc:
			if err := bitcast(wasm.F32, wasm.I32); err != nil {
				return nil, err
			}
		case 0xbd:
			if err := bitcast(wasm.F64, wasm.I64); err != nil {
				return nil, err
			}
		case 0xbe:
			if err := bitcast(wasm.I32, wasm.F32); err != nil {
				return nil, err
			}
		case 0xbf:
			if err := bitcast(wasm.I64, wasm.F64); err != nil {
				return nil, err
			}
		case 0xc2:
			if err := i64HelperUnary(embedded32.I64Extend8S, wasm.I64, wasm.I64); err != nil {
				return nil, err
			}
		case 0xc3:
			if err := i64HelperUnary(embedded32.I64Extend16S, wasm.I64, wasm.I64); err != nil {
				return nil, err
			}
		case 0xc4:
			if err := i64HelperUnary(embedded32.I64Extend32S, wasm.I64, wasm.I64); err != nil {
				return nil, err
			}
		case 0xd0: // ref.null
			heap, err := r.S33()
			if err != nil {
				return nil, err
			}
			var typ wasm.ValType
			switch heap {
			case -16:
				typ = wasm.FuncRef
			case -17:
				typ = wasm.ExternRef
			default:
				return nil, fmt.Errorf("mixed ref.null heap type %d is not supported", heap)
			}
			out, err := push(typ)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 1})
		case 0xd1: // ref.is_null
			if len(stack) == 0 || stack[len(stack)-1].Type.Kind != wasm.ValRef {
				return nil, fmt.Errorf("mixed ref.is_null requires reference operand")
			}
			value := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			out, err := push(wasm.I32)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedI32Eqz, Dst: out.Slot, Left: value.Slot, Width: 1})
		case 0xd2: // ref.func
			index, err := r.U32()
			if err != nil {
				return nil, err
			}
			if resolve == nil {
				return nil, fmt.Errorf("mixed ref.func requires module functions")
			}
			if _, ok := resolve(index); !ok || index == ^uint32(0) {
				return nil, fmt.Errorf("mixed ref.func index %d is invalid", index)
			}
			out, err := push(wasm.FuncRef)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 1, Words: [4]uint32{index + 1}})
		case 0xfc:
			sub, err := r.U32()
			if err != nil {
				return nil, err
			}
			switch sub {
			case 0:
				if err := f32HelperUnary(embedded32.I32TruncSatF32S, wasm.F32, wasm.I32); err != nil {
					return nil, err
				}
			case 1:
				if err := f32HelperUnary(embedded32.I32TruncSatF32U, wasm.F32, wasm.I32); err != nil {
					return nil, err
				}
			case 2:
				if err := helperUnary(uint32(embedded32.I32TruncSatF64S), wasm.F64, wasm.I32); err != nil {
					return nil, err
				}
			case 3:
				if err := helperUnary(uint32(embedded32.I32TruncSatF64U), wasm.F64, wasm.I32); err != nil {
					return nil, err
				}
			case 4:
				if err := f32HelperUnary(embedded32.I64TruncSatF32S, wasm.F32, wasm.I64); err != nil {
					return nil, err
				}
			case 5:
				if err := f32HelperUnary(embedded32.I64TruncSatF32U, wasm.F32, wasm.I64); err != nil {
					return nil, err
				}
			case 6:
				if err := helperUnary(uint32(embedded32.I64TruncSatF64S), wasm.F64, wasm.I64); err != nil {
					return nil, err
				}
			case 7:
				if err := helperUnary(uint32(embedded32.I64TruncSatF64U), wasm.F64, wasm.I64); err != nil {
					return nil, err
				}
			case 8: // memory.init
				dataIndex, err := r.U32()
				if err != nil {
					return nil, err
				}
				memory, err := r.U32()
				if err != nil || memory != 0 {
					return nil, fmt.Errorf("mixed memory.init requires memory zero")
				}
				n, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				src, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				dst, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedMemoryInit, Left: dst.Slot, Right: src.Slot, Third: n.Slot, Target: dataIndex})
			case 9: // data.drop
				dataIndex, err := r.U32()
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedDataDrop, Target: dataIndex})
			case 10: // memory.copy
				dstMemory, err1 := r.U32()
				srcMemory, err2 := r.U32()
				if err1 != nil || err2 != nil || dstMemory != 0 || srcMemory != 0 {
					return nil, fmt.Errorf("mixed memory.copy requires memory zero")
				}
				n, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				src, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				dst, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedMemoryCopy, Left: dst.Slot, Right: src.Slot, Third: n.Slot})
			case 11: // memory.fill
				memory, err := r.U32()
				if err != nil || memory != 0 {
					return nil, fmt.Errorf("mixed memory.fill requires memory zero")
				}
				n, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				value, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				dst, err := pop(wasm.I32)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedMemoryFill, Left: dst.Slot, Right: value.Slot, Third: n.Slot})
			default:
				return nil, fmt.Errorf("mixed function unsupported 0xfc subopcode %d", sub)
			}
		case 0xfd:
			sub, err := r.U32()
			if err != nil {
				return nil, err
			}
			switch sub {
			case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93:
				if _, err := r.U32(); err != nil { // alignment hint
					return nil, err
				}
				offset, err := r.U32()
				if err != nil {
					return nil, err
				}
				inputs, outputs, hasLane, ok := wasm.SIMDMemorySignature(sub)
				if !ok {
					return nil, fmt.Errorf("mixed function invalid SIMD memory subopcode %d", sub)
				}
				lane := byte(0)
				if hasLane {
					lane, err = r.Byte()
					if err != nil {
						return nil, err
					}
				}
				if err := simdHelperSignature(sub, inputs, outputs); err != nil {
					return nil, err
				}
				planned := &p.Ops[len(p.Ops)-1]
				planned.HasMemory = true
				planned.MemoryOffset = offset
				planned.Lane = uint32(lane)
			case 12:
				bits, err := r.Bytes(16)
				if err != nil {
					return nil, err
				}
				out, err := push(wasm.V128)
				if err != nil {
					return nil, err
				}
				var words [4]uint32
				for i := range words {
					words[i] = binary.LittleEndian.Uint32(bits[i*4:])
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedConst, Dst: out.Slot, Width: 4, Words: words})
			case 13:
				lanes, err := r.Bytes(16)
				if err != nil {
					return nil, err
				}
				if err := simdHelperSignature(sub, []wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}); err != nil {
					return nil, err
				}
				for i := range p.Ops[len(p.Ops)-1].Words {
					p.Ops[len(p.Ops)-1].Words[i] = binary.LittleEndian.Uint32(lanes[i*4:])
				}
			case 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34:
				lane, err := r.Byte()
				if err != nil {
					return nil, err
				}
				inputs, outputs, ok := wasm.SIMDLaneSignature(sub)
				if !ok {
					return nil, fmt.Errorf("mixed function invalid SIMD lane subopcode %d", sub)
				}
				if err := simdHelperSignature(sub, inputs, outputs); err != nil {
					return nil, err
				}
				p.Ops[len(p.Ops)-1].Lane = uint32(lane)
			case 77:
				if err := unary(MixedV128Not, wasm.V128); err != nil {
					return nil, err
				}
			case 78:
				if err := binaryOp(MixedV128And, wasm.V128); err != nil {
					return nil, err
				}
			case 79:
				if err := binaryOp(MixedV128AndNot, wasm.V128); err != nil {
					return nil, err
				}
			case 80:
				if err := binaryOp(MixedV128Or, wasm.V128); err != nil {
					return nil, err
				}
			case 81:
				if err := binaryOp(MixedV128Xor, wasm.V128); err != nil {
					return nil, err
				}
			case 82:
				third, err := pop(wasm.V128)
				if err != nil {
					return nil, err
				}
				right, err := pop(wasm.V128)
				if err != nil {
					return nil, err
				}
				left, err := pop(wasm.V128)
				if err != nil {
					return nil, err
				}
				out, err := push(wasm.V128)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedV128Bitselect, Dst: out.Slot, Left: left.Slot, Right: right.Slot, Third: third.Slot})
			case 174:
				if err := binaryOp(MixedI32x4Add, wasm.V128); err != nil {
					return nil, err
				}
			case 177:
				if err := binaryOp(MixedI32x4Sub, wasm.V128); err != nil {
					return nil, err
				}
			case 103, 104, 105, 106, 116, 117, 118, 119, 227, 239:
				if err := simdHelper(sub, 1); err != nil {
					return nil, err
				}
			case 228, 229, 230, 231, 232, 233, 234, 235, 240, 241, 242, 243, 244, 245, 246, 247:
				if err := simdHelper(sub, 2); err != nil {
					return nil, err
				}
			default:
				inputs, outputs, ok := wasm.SIMDNoImmediateSignature(sub)
				if !ok {
					return nil, fmt.Errorf("mixed function unsupported SIMD subopcode %d", sub)
				}
				if err := simdHelperSignature(sub, inputs, outputs); err != nil {
					return nil, err
				}
			}
		default:
			return nil, fmt.Errorf("mixed function unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("mixed function missing end")
}
