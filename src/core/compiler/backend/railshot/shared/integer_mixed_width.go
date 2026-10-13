package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// mixedAffineValue is an integer A*i+B modulo Width. Counter width comes from
// the reduction's validated local types. Widening and logical shifts require
// proofs over that counter's entire unsigned domain, not the benchmark bound.
type mixedAffineValue struct {
	a, b  uint64
	width int
}

func (v mixedAffineValue) normalize() mixedAffineValue {
	if v.width == 4 {
		v.a, v.b = uint64(uint32(v.a)), uint64(uint32(v.b))
	}
	return v
}

func (v mixedAffineValue) unsignedFits(counterMax uint64) bool {
	limit := ^uint64(0)
	if v.width == 4 {
		limit = 0xffffffff
	}
	return v.b <= limit && v.a <= (limit-v.b)/counterMax
}

// inspectIntegerMixedWidthTerm handles conversions and constant shifts that
// preserve an affine integer expression. It is a bounded fallback for the
// common single-width proof; floating/vector expressions retain that proof.
func inspectIntegerMixedWidthTerm(r *wasm.Reader, m *wasm.Module, width int) (integerAffineTerm, bool) {
	start := r.Offset()
	var out integerAffineTerm
	var stack [16]mixedAffineValue
	var written [4]mixedAffineValue
	depth := 0
	counterMax := ^uint64(0)
	if width == 4 {
		counterMax = 0xffffffff
	}
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, err := r.Byte()
		if err != nil {
			return out, false
		}
		if op == 0x0b {
			if depth != 1 || stack[0].width != width {
				return out, false
			}
			out.coefficient, out.constant, out.width = stack[0].a, stack[0].b, width
			// The caller consumes the block end after inspecting the term.
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
			out.variable, out.hasVariable = x, true
			stack[depth] = mixedAffineValue{a: 1, width: width}
			depth++
		case 0x41, 0x42:
			if depth == len(stack) {
				return out, false
			}
			v := mixedAffineValue{width: 4}
			if op == 0x41 {
				x, err := r.I32()
				if err != nil {
					return out, false
				}
				v.b = uint64(uint32(x))
			} else {
				x, err := r.I64()
				if err != nil {
					return out, false
				}
				v.width, v.b = 8, uint64(x)
			}
			stack[depth] = v
			depth++
		case 0xad, 0xa7: // unsigned extension / low-word wrapping
			if depth == 0 {
				return out, false
			}
			v := stack[depth-1]
			if op == 0xad {
				if v.width != 4 || !v.unsignedFits(counterMax) {
					return out, false
				}
				v.width = 8
			} else {
				if v.width != 8 {
					return out, false
				}
				v.width = 4
				v = v.normalize()
			}
			stack[depth-1] = v
		case 0x23:
			x, err := r.U32()
			if err != nil || depth == len(stack) {
				return out, false
			}
			found := false
			for j := uint8(0); j < out.writeCount; j++ {
				if out.writes[j].Index == x {
					stack[depth] = written[j]
					depth++
					found = true
					break
				}
			}
			if !found {
				return out, false
			}
		case 0x24:
			if m == nil || depth == 0 {
				return out, false
			}
			x, err := r.U32()
			if err != nil || uint64(x) < uint64(m.ImportedGlobalCount()) {
				return out, false
			}
			gt, ok := m.GlobalTypeByIndex(x)
			if !ok || !gt.Mutable {
				return out, false
			}
			v := stack[depth-1]
			if !(v.width == 4 && wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I32) || v.width == 8 && wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64)) {
				return out, false
			}
			slot := out.writeCount
			for j := uint8(0); j < out.writeCount; j++ {
				if out.writes[j].Index == x {
					slot = j
					break
				}
			}
			if int(slot) == len(out.writes) {
				return out, false
			}
			depth--
			written[slot] = v
			out.writes[slot] = IntegerAffineGlobalWrite{Index: x, Coefficient: v.a, Constant: v.b, Wide: v.width == 8}
			if slot == out.writeCount {
				out.writeCount++
			}
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e, 0x74, 0x76, 0x86, 0x88:
			w := 4
			if op >= 0x7c {
				w = 8
			}
			if depth < 2 {
				return out, false
			}
			a, b := stack[depth-2], stack[depth-1]
			if a.width != w || b.width != w {
				return out, false
			}
			v := mixedAffineValue{width: w}
			switch op {
			case 0x6a, 0x7c:
				v.a, v.b = a.a+b.a, a.b+b.b
			case 0x6b, 0x7d:
				v.a, v.b = a.a-b.a, a.b-b.b
			case 0x6c, 0x7e:
				if a.a != 0 && b.a != 0 {
					return out, false
				}
				v.a, v.b = a.a*b.b+b.a*a.b, a.b*b.b
			default:
				if b.a != 0 {
					return out, false
				}
				shift := b.b & uint64(w*8-1)
				if op == 0x74 || op == 0x86 {
					v.a, v.b = a.a<<shift, a.b<<shift
				} else {
					// With no unsigned wrap, floor((A*i+B)/2^s) is affine if
					// the discarded remainder cannot carry for any allowed i.
					if !a.unsignedFits(counterMax) {
						return out, false
					}
					mask := (uint64(1) << shift) - 1
					if a.a&mask > (mask-(a.b&mask))/counterMax {
						return out, false
					}
					v.a, v.b = a.a>>shift, a.b>>shift
				}
			}
			depth--
			stack[depth-1] = v.normalize()
		default:
			return out, false
		}
	}
	return out, false
}
