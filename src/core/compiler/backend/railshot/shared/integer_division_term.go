package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Recognize A*i+B+C*(i/D)+E*(i%D), modulo the integer width. D is one
// nonzero unsigned constant; only the bare counter may feed division. The
// fixed stack/operation budget excludes effects, traps and nonlinear products.
func inspectIntegerDivisionTerm(r *wasm.Reader) (integerAffineTerm, bool) {
	type value struct{ counter, constant, quotient, remainder uint64 }
	var stack [16]value
	var out integerAffineTerm
	depth, start := 0, r.Offset()
	width := 0
	mask := ^uint64(0)
	setWidth := func(w int) bool {
		if width != 0 && width != w {
			return false
		}
		width = w
		if w == 4 {
			mask = (1 << 32) - 1
		}
		return true
	}
	isConstant := func(v value) bool { return v.counter == 0 && v.quotient == 0 && v.remainder == 0 }
	for operations := 0; operations < 32 && r.Offset()-start < 128; operations++ {
		op, ok := r.Peek()
		if !ok {
			return out, false
		}
		if depth == 1 && (op == 0x6a || op == 0x7c) {
			if out.divisor == 0 || width == 4 && op != 0x6a || width == 8 && op != 0x7c {
				return out, false
			}
			v := stack[0]
			out.width = width
			out.coefficient, out.constant = v.counter, v.constant
			out.quotientCoefficient, out.remainderCoefficient = v.quotient, v.remainder
			return out, true
		}
		_, _ = r.Byte()
		switch op {
		case 0x20:
			x, err := r.U32()
			if err != nil || depth == len(stack) || out.hasVariable && out.variable != x {
				return out, false
			}
			out.variable, out.hasVariable = x, true
			stack[depth] = value{counter: 1}
			depth++
		case 0x41, 0x42:
			if depth == len(stack) {
				return out, false
			}
			var v int64
			if op == 0x41 {
				if !setWidth(4) {
					return out, false
				}
				x, err := r.I32()
				if err != nil {
					return out, false
				}
				v = int64(x)
			} else {
				if !setWidth(8) {
					return out, false
				}
				x, err := r.I64()
				if err != nil {
					return out, false
				}
				v = x
			}
			stack[depth] = value{constant: uint64(v) & mask}
			depth++
		case 0x6a, 0x6b, 0x6c, 0x6e, 0x70, 0x7c, 0x7d, 0x7e, 0x80, 0x82:
			w := 4
			if op >= 0x7c {
				w = 8
			}
			if !setWidth(w) || depth < 2 {
				return out, false
			}
			left, right := stack[depth-2], stack[depth-1]
			var v value
			switch op {
			case 0x6a, 0x7c:
				v = value{left.counter + right.counter, left.constant + right.constant, left.quotient + right.quotient, left.remainder + right.remainder}
			case 0x6b, 0x7d:
				v = value{left.counter - right.counter, left.constant - right.constant, left.quotient - right.quotient, left.remainder - right.remainder}
			case 0x6c, 0x7e:
				if !isConstant(left) && !isConstant(right) {
					return out, false
				}
				if isConstant(left) {
					left, right = right, left
				}
				v = value{left.counter * right.constant, left.constant * right.constant, left.quotient * right.constant, left.remainder * right.constant}
			default:
				if left != (value{counter: 1}) || !isConstant(right) || right.constant == 0 || out.divisor != 0 && out.divisor != right.constant {
					return out, false
				}
				out.divisor = right.constant
				if op == 0x6e || op == 0x80 {
					v.quotient = 1
				} else {
					v.remainder = 1
				}
			}
			v.counter &= mask
			v.constant &= mask
			v.quotient &= mask
			v.remainder &= mask
			stack[depth-2] = v
			depth--
		default:
			return out, false
		}
	}
	return out, false
}
