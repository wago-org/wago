package wasm

import "fmt"

// FuncrefElementExpr is the decoded payload of a funcref element expression.
type FuncrefElementExpr struct {
	Null      bool
	FuncIndex uint32
}

// ParseFuncrefElementExpr parses the restricted element-expression forms wago
// supports for funcref tables: exactly one ref.null funcref or ref.func
// instruction followed by end, with no trailing bytes.
func ParseFuncrefElementExpr(e Expr) (FuncrefElementExpr, error) {
	if len(e.Instrs) != 0 {
		if len(e.Instrs) != 1 {
			return FuncrefElementExpr{}, fmt.Errorf("multi-instruction")
		}
		switch in := e.Instrs[0]; in.Kind {
		case InstrRefNull:
			if !EqualValType(RefVal(in.RefType()), FuncRef) {
				return FuncrefElementExpr{}, fmt.Errorf("ref.null type %s is not funcref", RefVal(in.RefType()))
			}
			return FuncrefElementExpr{Null: true}, nil
		case InstrRefFunc:
			return FuncrefElementExpr{FuncIndex: in.Index}, nil
		default:
			return FuncrefElementExpr{}, fmt.Errorf("%s", in.Kind.String())
		}
	}
	if len(e.BodyBytes) == 0 {
		return FuncrefElementExpr{}, fmt.Errorf("empty")
	}
	r := NewReader(e.BodyBytes)
	op, err := r.Byte()
	if err != nil {
		return FuncrefElementExpr{}, err
	}
	var out FuncrefElementExpr
	switch op {
	case 0xd0: // ref.null
		ht, err := r.Byte()
		if err != nil {
			return FuncrefElementExpr{}, err
		}
		if ht != 0x70 {
			return FuncrefElementExpr{}, fmt.Errorf("ref.null type 0x%02x is not funcref", ht)
		}
		out.Null = true
	case 0xd2: // ref.func
		idx, err := r.U32()
		if err != nil {
			return FuncrefElementExpr{}, err
		}
		out.FuncIndex = idx
	default:
		return FuncrefElementExpr{}, fmt.Errorf("opcode 0x%02x", op)
	}
	end, err := r.Byte()
	if err != nil {
		return FuncrefElementExpr{}, err
	}
	if end != 0x0b {
		return FuncrefElementExpr{}, fmt.Errorf("missing end")
	}
	if r.BytesLeft() != 0 {
		return FuncrefElementExpr{}, fmt.Errorf("trailing bytes")
	}
	return out, nil
}
