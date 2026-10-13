package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// AffineI32Table describes a fully populated initial table whose numeric leaves
// return Scale*x + Bias + Step*index modulo 2^32. It does not prove immutability:
// callers must separately establish that the table is private and never mutated.
// Count zero means no proof. The small fixed bound limits compiler work/storage.
type AffineI32Table struct {
	TypeKey                  uint64
	Count, Scale, Bias, Step uint32
}

func InspectAffineI32TableEntries(m *wasm.Module) (AffineI32Table, bool) {
	var out AffineI32Table
	if m == nil || m.ImportedTableCount() != 0 || m.ImportedFuncCount() != 0 || len(m.Tables) != 1 || m.Tables[0].Init != nil {
		return out, false
	}
	limit := m.Tables[0].Type.Limits
	if limit.Addr64 || limit.Min == 0 || limit.Min > 16 {
		return out, false
	}
	var targets [16]uint32 // function index plus one; zero is an uninitialized slot
	for _, e := range m.Elements {
		if e.Mode.Kind != wasm.ElemActive {
			continue
		}
		if e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs {
			return out, false
		}
		offset, ok := affineTableOffset(e.Mode.Offset)
		if !ok || uint64(offset)+uint64(len(e.Kind.Funcs)) > limit.Min {
			return out, false
		}
		for i, idx := range e.Kind.Funcs {
			if uint64(idx) >= uint64(len(m.Code)) {
				return out, false
			}
			targets[int(offset)+i] = uint32(idx) + 1
		}
	}
	for i := uint32(0); uint64(i) < limit.Min; i++ {
		if targets[i] == 0 {
			return AffineI32Table{}, false
		}
		idx := targets[i] - 1
		typ, ok := m.FuncTypeIndex(idx)
		if !ok {
			return AffineI32Table{}, false
		}
		ft, ok := m.TypeFunc(typ.Index)
		if !ok || len(ft.Params) != 1 || ft.Params[0] != wasm.I32 || len(ft.Results) != 1 || ft.Results[0] != wasm.I32 {
			return AffineI32Table{}, false
		}
		key, ok := m.StructuralTypeKeyChecked(typ.Index)
		if !ok {
			return AffineI32Table{}, false
		}
		scale, bias, ok := inspectAffineI32Leaf(m.Code[idx].BodyBytes)
		if !ok {
			return AffineI32Table{}, false
		}
		if i == 0 {
			out.TypeKey = key
			out.Scale = scale
			out.Bias = bias
		} else {
			if key != out.TypeKey || scale != out.Scale {
				return AffineI32Table{}, false
			}
			if i == 1 {
				out.Step = bias - out.Bias
			}
			if bias != out.Bias+i*out.Step {
				return AffineI32Table{}, false
			}
		}
	}
	out.Count = uint32(limit.Min)
	return out, true
}

func affineTableOffset(e wasm.Expr) (uint32, bool) {
	if len(e.BodyBytes) != 0 {
		r := wasm.NewReader(e.BodyBytes)
		op, err := r.Byte()
		if err != nil || op != 0x41 {
			return 0, false
		}
		v, err := r.I32()
		if err != nil {
			return 0, false
		}
		end, err := r.Byte()
		return uint32(v), err == nil && end == 0x0b && !r.HasNext()
	}
	if len(e.Instrs) == 1 && e.Instrs[0].Kind == wasm.InstrI32Const {
		return uint32(e.Instrs[0].I32), true
	}
	return 0, false
}

// Bounded symbolic execution of pure wrapping i32 arithmetic. Multiplication
// remains affine only when at least one operand is constant; every other opcode
// (including loads, divisions, calls, traps and control flow) rejects the leaf.
func inspectAffineI32Leaf(body []byte) (uint32, uint32, bool) {
	if len(body) == 0 || len(body) > 80 {
		return 0, 0, false
	}
	type value struct{ scale, bias uint32 }
	var stack [4]value
	depth := 0
	r := wasm.NewReader(body)
	for instructions := 0; instructions < 16; instructions++ {
		op, err := r.Byte()
		if err != nil {
			return 0, 0, false
		}
		switch op {
		case 0x20, 0x41:
			if depth == len(stack) {
				return 0, 0, false
			}
			v := value{}
			if op == 0x20 {
				idx, err := r.U32()
				if err != nil || idx != 0 {
					return 0, 0, false
				}
				v.scale = 1
			} else {
				c, err := r.I32()
				if err != nil {
					return 0, 0, false
				}
				v.bias = uint32(c)
			}
			stack[depth] = v
			depth++
		case 0x6a, 0x6b, 0x6c:
			if depth < 2 {
				return 0, 0, false
			}
			a, b := stack[depth-2], stack[depth-1]
			depth--
			switch op {
			case 0x6a:
				a.scale += b.scale
				a.bias += b.bias
			case 0x6b:
				a.scale -= b.scale
				a.bias -= b.bias
			case 0x6c:
				if a.scale != 0 && b.scale != 0 {
					return 0, 0, false
				}
				a.scale = a.scale*b.bias + b.scale*a.bias
				a.bias *= b.bias
			}
			stack[depth-1] = a
		case 0x0b:
			if depth == 1 && !r.HasNext() {
				return stack[0].scale, stack[0].bias, true
			}
			return 0, 0, false
		default:
			return 0, 0, false
		}
	}
	return 0, 0, false
}
