package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// inspectIntegerGlobalRecurrence proves one defined mutable integer recurrence
// g <- g+A*i+B and a returned term C*g_old+D*i+E. The stack tracks references
// before and after the store independently. No other global, call, conversion,
// memory operation or trapping arithmetic is admitted.
func inspectIntegerGlobalRecurrence(r *wasm.Reader, m *wasm.Module, width int) (integerAffineTerm, bool) {
	var out integerAffineTerm
	if m == nil {
		return out, false
	}
	first, ok := r.Peek()
	if !ok || (first != 0x23 && first != 0x20 && first != 0x41 && first != 0x42) {
		return out, false
	}
	type value struct{ g, a, b uint64 }
	var stack [16]value
	current := value{g: 1}
	start, depth := r.Offset(), 0
	globalSeen, written := false, false
	normalize := func(v value) value {
		if width == 4 {
			v.g, v.a, v.b = uint64(uint32(v.g)), uint64(uint32(v.a)), uint64(uint32(v.b))
		}
		return v
	}
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, err := r.Byte()
		if err != nil {
			return out, false
		}
		if op == 0x0b {
			if depth != 1 || !written {
				return out, false
			}
			v := stack[0]
			out.coefficient, out.constant, out.width = v.a, v.b, width
			out.globalCoefficient, out.recurrence = v.g, true
			if r.JumpTo(r.Offset()-1) != nil {
				return out, false
			}
			return out, true
		}
		switch op {
		case 0x20:
			x, err := r.U32()
			if err != nil || depth == len(stack) || out.hasVariable && out.variable != x {
				return out, false
			}
			out.hasVariable, out.variable = true, x
			stack[depth] = value{a: 1}
			depth++
		case 0x41, 0x42:
			if depth == len(stack) || (op == 0x42) != (width == 8) {
				return out, false
			}
			var b uint64
			if op == 0x41 {
				x, err := r.I32()
				if err != nil {
					return out, false
				}
				b = uint64(uint32(x))
			} else {
				x, err := r.I64()
				if err != nil {
					return out, false
				}
				b = uint64(x)
			}
			stack[depth] = value{b: b}
			depth++
		case 0x23, 0x24:
			x, err := r.U32()
			if err != nil || uint64(x) < uint64(m.ImportedGlobalCount()) || globalSeen && x != out.global {
				return out, false
			}
			if !globalSeen {
				gt, ok := m.GlobalTypeByIndex(x)
				if !ok || !gt.Mutable || !(width == 4 && wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I32) || width == 8 && wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64)) {
					return out, false
				}
				globalSeen, out.global = true, x
			}
			if op == 0x23 {
				if depth == len(stack) {
					return out, false
				}
				stack[depth] = current
				depth++
			} else {
				if written || depth == 0 || stack[depth-1].g != 1 {
					return out, false
				}
				depth--
				current = stack[depth]
				written = true
				out.incrementCoefficient, out.incrementConstant = current.a, current.b
			}
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
			if depth < 2 || (op >= 0x7c) != (width == 8) {
				return out, false
			}
			a, b := stack[depth-2], stack[depth-1]
			var v value
			switch op {
			case 0x6a, 0x7c:
				v = value{g: a.g + b.g, a: a.a + b.a, b: a.b + b.b}
			case 0x6b, 0x7d:
				v = value{g: a.g - b.g, a: a.a - b.a, b: a.b - b.b}
			default:
				if (a.g != 0 || a.a != 0) && (b.g != 0 || b.a != 0) {
					return out, false
				}
				v = value{g: a.g*b.b + b.g*a.b, a: a.a*b.b + b.a*a.b, b: a.b * b.b}
			}
			depth--
			stack[depth-1] = normalize(v)
		default:
			return out, false
		}
	}
	return out, false
}
