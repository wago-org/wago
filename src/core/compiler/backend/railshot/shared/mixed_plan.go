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
	MixedI64Helper
	MixedMemoryLoad
	MixedMemoryStore
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
		return 0, false
	}
}

type MixedSignatureResolver func(uint32) (*wasm.CompType, bool)
type MixedGlobalResolver func(uint32) (wasm.ValType, bool, bool)

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
	default:
		return wasm.ValType{}, false
	}
}

func BuildMixedPlanWithCalls(ft *wasm.CompType, locals []wasm.LocalRun, body []byte, resolve MixedSignatureResolver) (*MixedPlan, error) {
	return BuildMixedPlanWithResolvers(ft, locals, body, resolve, nil)
}

func BuildMixedPlanWithResolvers(ft *wasm.CompType, locals []wasm.LocalRun, body []byte, resolve MixedSignatureResolver, resolveGlobal MixedGlobalResolver) (*MixedPlan, error) {
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

	type mixedControl struct {
		kind       byte
		entry      []MixedValue
		slots      uint16
		header     int
		falseOp    int
		jumpOp     int
		elseSeen   bool
		pending    []int
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
	readBlockResults := func() ([]wasm.ValType, error) {
		encoded, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if encoded == 0x40 {
			return nil, nil
		}
		typ, ok := mixedEncodedValueType(encoded)
		if !ok {
			return nil, fmt.Errorf("mixed control type %#x requires a type-index planner", encoded)
		}
		return []wasm.ValType{typ}, nil
	}
	controlMatches := func(control *mixedControl) ([]MixedValue, bool) {
		if len(stack) != len(control.entry)+len(control.results) {
			return nil, false
		}
		for i := range control.entry {
			if stack[i] != control.entry[i] {
				return nil, false
			}
		}
		for i, typ := range control.results {
			if stack[len(control.entry)+i].Type != typ {
				return nil, false
			}
		}
		values := append([]MixedValue(nil), stack[len(control.entry):]...)
		return values, true
	}
	terminated := false
	terminalUnreachable := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if terminated && op != 0x0b {
			return nil, fmt.Errorf("mixed function currently requires terminal unreachable")
		}
		switch op {
		case 0x00: // unreachable
			p.Ops = append(p.Ops, MixedOp{Kind: MixedUnreachable})
			terminated = true
			terminalUnreachable = true
		case 0x01: // nop
		case 0x02, 0x03: // block / loop
			results, err := readBlockResults()
			if err != nil {
				return nil, err
			}
			if op == 0x03 && len(results) != 0 {
				return nil, fmt.Errorf("mixed loop results require typed backedge merges")
			}
			control := mixedControl{kind: op, entry: append([]MixedValue(nil), stack...), slots: operandSlots, falseOp: -1, jumpOp: -1, results: results}
			if op == 0x03 {
				control.header = len(p.Ops)
				p.Ops = append(p.Ops, MixedOp{Kind: MixedPollCancellation})
			}
			controls = append(controls, control)
		case 0x04: // if
			results, err := readBlockResults()
			if err != nil {
				return nil, err
			}
			condition, err := pop(wasm.I32)
			if err != nil {
				return nil, err
			}
			branch := len(p.Ops)
			p.Ops = append(p.Ops, MixedOp{Kind: MixedBranchZero, Third: condition.Slot, Label: -1})
			controls = append(controls, mixedControl{kind: op, entry: append([]MixedValue(nil), stack...), slots: operandSlots, falseOp: branch, jumpOp: -1, results: results})
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
			stack = append(stack[:0], control.entry...)
			operandSlots = control.slots
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
		case 0x0d: // br_if to a void label
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
			if len(target.results) != 0 || !stackMatches(target.entry) || operandSlots != target.slots {
				return nil, fmt.Errorf("mixed br_if currently requires a void label stack")
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
			typ, mutable, ok := resolveGlobal(index)
			if !ok {
				return nil, fmt.Errorf("mixed global index %d is invalid", index)
			}
			width, supported := MixedValueSlots(typ)
			if !supported || width != 1 {
				return nil, fmt.Errorf("mixed global %d type %s is not yet supported", index, typ)
			}
			if op == 0x23 {
				out, err := push(typ)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedGlobalGet, Dst: out.Slot, Target: index, Width: width})
			} else {
				if !mutable {
					return nil, fmt.Errorf("mixed global %d is immutable", index)
				}
				value, err := pop(typ)
				if err != nil {
					return nil, err
				}
				p.Ops = append(p.Ops, MixedOp{Kind: MixedGlobalSet, Left: value.Slot, Target: index, Width: width})
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
		case 0xfc:
			sub, err := r.U32()
			if err != nil {
				return nil, err
			}
			switch sub {
			case 2:
				if err := helperUnary(uint32(embedded32.I32TruncSatF64S), wasm.F64, wasm.I32); err != nil {
					return nil, err
				}
			case 3:
				if err := helperUnary(uint32(embedded32.I32TruncSatF64U), wasm.F64, wasm.I32); err != nil {
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
			default:
				return nil, fmt.Errorf("mixed function unsupported 0xfc subopcode %d", sub)
			}
		case 0xfd:
			sub, err := r.U32()
			if err != nil {
				return nil, err
			}
			switch sub {
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
			default:
				return nil, fmt.Errorf("mixed function unsupported SIMD subopcode %d", sub)
			}
		default:
			return nil, fmt.Errorf("mixed function unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("mixed function missing end")
}
