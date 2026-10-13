package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// This bounded grammar proves an affine i32 expression followed by signed
// narrowing, optionally through i64 and back. A high-word extraction returns
// zero or -1, so its reduction counts negative narrowed values instead.
func inspectIntegerSignedTerm(r *wasm.Reader) (integerAffineTerm, bool) {
	var out integerAffineTerm
	type value struct{ a, b uint32 }
	var stack [16]value
	depth, start := 0, r.Offset()
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, err := r.Byte()
		if err != nil {
			return out, false
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
		case 0x41:
			x, err := r.I32()
			if err != nil || depth == len(stack) {
				return out, false
			}
			stack[depth] = value{b: uint32(x)}
			depth++
		case 0x6a, 0x6b, 0x6c:
			if depth < 2 {
				return out, false
			}
			a, b := stack[depth-2], stack[depth-1]
			var v value
			if op == 0x6a {
				v = value{a.a + b.a, a.b + b.b}
			} else if op == 0x6b {
				v = value{a.a - b.a, a.b - b.b}
			} else {
				if a.a != 0 && b.a != 0 {
					return out, false
				}
				v = value{a.a*b.b + b.a*a.b, a.b * b.b}
			}
			depth--
			stack[depth-1] = v
		case 0xc0, 0xc1, 0xac, 0xad:
			if depth != 1 {
				return out, false
			}
			bits := uint8(8)
			if op == 0xc1 {
				bits = 16
			}
			if op == 0xac || op == 0xad {
				ext, err := r.Byte()
				if err != nil || ext < 0xc2 || ext > 0xc4 {
					return out, false
				}
				bits = 8 << (ext - 0xc2)
				if next, _ := r.Peek(); next == 0x42 {
					_, _ = r.Byte()
					shift, err := r.I64()
					op, e := r.Byte()
					if err != nil || e != nil || shift != 32 || op != 0x87 && op != 0x88 {
						return out, false
					}
					out.negativeCount = true
				}
				wrap, err := r.Byte()
				if err != nil || wrap != 0xa7 {
					return out, false
				}
			}
			out.width = 4
			out.coefficient, out.constant = uint64(stack[0].a), uint64(stack[0].b)
			if bits != 32 || out.negativeCount {
				out.signedBits = bits
			}
			return out, true
		default:
			return out, false
		}
	}
	return out, false
}
