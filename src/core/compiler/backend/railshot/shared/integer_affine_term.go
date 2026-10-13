package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

type integerAffineTerm struct {
	coefficient, constant uint64
	variable              uint32
	hasVariable           bool
	width                 int
}

// inspectIntegerAffineTerm evaluates a bounded pure postfix expression as A*i+B
// modulo its integer width. All local reads must name the same counter. A
// product is affine only when at least one operand has zero coefficient.
func inspectIntegerAffineTerm(r *wasm.Reader, blocked bool) (integerAffineTerm, bool) {
	type affineValue struct{ coefficient, constant uint64 }
	var stack [16]affineValue
	var out integerAffineTerm
	depth, start := 0, r.Offset()
	setWidth := func(width int) bool {
		if out.width != 0 && out.width != width {
			return false
		}
		out.width = width
		return true
	}
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, ok := r.Peek()
		if !ok {
			return out, false
		}
		// With one term left, the unblocked add consumes the earlier accumulator.
		if depth == 1 && (blocked && op == 0x0b || !blocked && (op == 0x6a || op == 0x7c)) {
			out.coefficient, out.constant = stack[0].coefficient, stack[0].constant
			return out, true
		}
		_, _ = r.Byte()
		switch op {
		case 0x20:
			x, err := r.U32()
			if err != nil || depth == len(stack) {
				return out, false
			}
			if out.hasVariable && out.variable != x {
				return out, false
			}
			out.variable, out.hasVariable = x, true
			stack[depth] = affineValue{coefficient: 1}
			depth++
		case 0x41, 0x42:
			if depth == len(stack) {
				return out, false
			}
			var value uint64
			if op == 0x41 {
				v, err := r.I32()
				if err != nil || !setWidth(4) {
					return out, false
				}
				value = uint64(uint32(v))
			} else {
				v, err := r.I64()
				if err != nil || !setWidth(8) {
					return out, false
				}
				value = uint64(v)
			}
			stack[depth] = affineValue{constant: value}
			depth++
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
			width := 4
			if op >= 0x7c {
				width = 8
			}
			if depth < 2 || !setWidth(width) {
				return out, false
			}
			a, b := stack[depth-2], stack[depth-1]
			var v affineValue
			switch op {
			case 0x6a, 0x7c:
				v.coefficient = a.coefficient + b.coefficient
				v.constant = a.constant + b.constant
			case 0x6b, 0x7d:
				v.coefficient = a.coefficient - b.coefficient
				v.constant = a.constant - b.constant
			default:
				if a.coefficient != 0 && b.coefficient != 0 {
					return out, false
				}
				v.coefficient = a.coefficient*b.constant + b.coefficient*a.constant
				v.constant = a.constant * b.constant
			}
			if width == 4 {
				v.coefficient = uint64(uint32(v.coefficient))
				v.constant = uint64(uint32(v.constant))
			}
			depth--
			stack[depth-1] = v
		default:
			return out, false
		}
	}
	return out, false
}
