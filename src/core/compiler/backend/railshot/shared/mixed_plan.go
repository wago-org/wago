package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
)

type MixedOp struct {
	Kind        MixedOpKind
	Dst         uint16
	Left, Right uint16
	Third       uint16
	Width       uint8
	Words       [4]uint32
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

func BuildMixedPlan(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) (*MixedPlan, error) {
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

	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x01: // nop
		case 0x0b: // end
			if r.HasNext() {
				return nil, fmt.Errorf("mixed function has instructions after end")
			}
			if len(stack) != len(ft.Results) {
				return nil, fmt.Errorf("mixed result stack has %d values, want %d", len(stack), len(ft.Results))
			}
			for i := range stack {
				if stack[i].Type != ft.Results[i] {
					return nil, fmt.Errorf("mixed result %d has type %s, want %s", i, stack[i].Type, ft.Results[i])
				}
			}
			p.Results = append(p.Results, stack...)
			for _, typ := range ft.Results {
				width, _ := MixedValueSlots(typ)
				p.ResultSlots += uint16(width)
			}
			return p, nil
		case 0x1a: // drop
			if len(stack) == 0 {
				return nil, fmt.Errorf("mixed drop stack underflow")
			}
			if _, err := pop(stack[len(stack)-1].Type); err != nil {
				return nil, err
			}
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
		case 0x7c:
			if err := binaryOp(MixedI64Add, wasm.I64); err != nil {
				return nil, err
			}
		case 0x7d:
			if err := binaryOp(MixedI64Sub, wasm.I64); err != nil {
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
		case 0xa6:
			if err := binaryOp(MixedF64Copysign, wasm.F64); err != nil {
				return nil, err
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
