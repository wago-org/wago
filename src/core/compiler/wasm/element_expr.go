package wasm

import "fmt"

// ElementExpr is the decoded payload of a core WebAssembly 2.0 element
// expression. The Release 2 forms are ref.null func/extern and ref.func.
type ElementExpr struct {
	RefType   RefType
	Null      bool
	FuncIndex uint32
}

// ParseElementExpr parses exactly one supported Release 2 element expression
// followed by end, with no trailing bytes.
func ParseElementExpr(e Expr) (ElementExpr, error) {
	if len(e.Instrs) != 0 {
		if len(e.Instrs) != 1 {
			return ElementExpr{}, fmt.Errorf("multi-instruction")
		}
		switch in := e.Instrs[0]; in.Kind {
		case InstrRefNull:
			ref := in.RefType()
			if !EqualValType(RefVal(ref), FuncRef) && !EqualValType(RefVal(ref), ExternRef) {
				return ElementExpr{}, fmt.Errorf("ref.null type %s is not funcref or externref", RefVal(ref))
			}
			return ElementExpr{RefType: ref, Null: true}, nil
		case InstrRefFunc:
			return ElementExpr{RefType: FuncRef.Ref, FuncIndex: in.Index}, nil
		default:
			return ElementExpr{}, fmt.Errorf("%s", in.Kind.String())
		}
	}
	if len(e.BodyBytes) == 0 {
		return ElementExpr{}, fmt.Errorf("empty")
	}
	r := NewReader(e.BodyBytes)
	op, err := r.Byte()
	if err != nil {
		return ElementExpr{}, err
	}
	var out ElementExpr
	switch op {
	case 0xd0: // ref.null
		ht, err := r.S33()
		if err != nil {
			return ElementExpr{}, err
		}
		switch ht {
		case -16:
			out.RefType = FuncRef.Ref
		case -17:
			out.RefType = ExternRef.Ref
		default:
			return ElementExpr{}, fmt.Errorf("ref.null heap type %d is not funcref or externref", ht)
		}
		out.Null = true
	case 0xd2: // ref.func
		idx, err := r.U32()
		if err != nil {
			return ElementExpr{}, err
		}
		out.RefType = FuncRef.Ref
		out.FuncIndex = idx
	default:
		return ElementExpr{}, fmt.Errorf("opcode 0x%02x", op)
	}
	end, err := r.Byte()
	if err != nil {
		return ElementExpr{}, err
	}
	if end != 0x0b {
		return ElementExpr{}, fmt.Errorf("missing end")
	}
	if r.BytesLeft() != 0 {
		return ElementExpr{}, fmt.Errorf("trailing bytes")
	}
	return out, nil
}

// FuncrefElementExpr is the decoded payload of a funcref element expression.
type FuncrefElementExpr struct {
	Null      bool
	FuncIndex uint32
}

// ParseFuncrefElementExpr preserves the focused funcref parser API while using
// the typed Release 2 element-expression decoder.
func ParseFuncrefElementExpr(e Expr) (FuncrefElementExpr, error) {
	out, err := ParseElementExpr(e)
	if err != nil {
		return FuncrefElementExpr{}, err
	}
	if !EqualValType(RefVal(out.RefType), FuncRef) {
		return FuncrefElementExpr{}, fmt.Errorf("element type %s is not funcref", RefVal(out.RefType))
	}
	return FuncrefElementExpr{Null: out.Null, FuncIndex: out.FuncIndex}, nil
}
