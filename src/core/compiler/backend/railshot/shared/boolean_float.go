package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
)

// BooleanFloatExpr is a pure i32 expression depending only on one local's low
// bit. Values are the modular integer results at bit zero/one, before conversion.
type BooleanFloatExpr struct {
	Variable     uint32
	Values       [2]uint32
	Signed, Wide bool
	End          int
}

func BooleanFloatBinary(op IntOp) bool {
	switch op {
	case IntAdd, IntSub, IntMul, IntAnd, IntOr, IntXor, IntShl, IntShrU, IntShrS:
		return true
	}
	return false
}

// BooleanFloatBits converts every i32 bit pattern with integer-only IEEE
// rounding. This does not depend on the compiler process's floating environment.
func BooleanFloatBits(value uint32, wide, signed bool) uint64 {
	sign := uint64(0)
	magnitude := value
	if signed && int32(value) < 0 {
		sign = 1
		magnitude = uint32(-int64(int32(value)))
	}
	if magnitude == 0 {
		return 0
	}
	exponent := bits.Len32(magnitude) - 1
	if wide {
		return sign<<63 | uint64(exponent+1023)<<52 | (uint64(magnitude)<<uint(52-exponent))&((1<<52)-1)
	}
	mantissa := magnitude
	if exponent <= 23 {
		mantissa <<= uint(23 - exponent)
	} else {
		shift := uint(exponent - 23)
		tail := mantissa & ((1 << shift) - 1)
		mantissa >>= shift
		half := uint32(1) << (shift - 1)
		if tail > half || tail == half && mantissa&1 != 0 {
			mantissa++
		}
		if mantissa == 1<<24 {
			mantissa >>= 1
			exponent++
		}
	}
	return sign<<31 | uint64(exponent+127)<<23 | uint64(mantissa&((1<<23)-1))
}

// InspectBooleanFloat leaves the reader untouched. The fixed postfix grammar
// admits only local low-bit masks, constants, and nontrapping i32 arithmetic.
func InspectBooleanFloat(source *wasm.Reader) (BooleanFloatExpr, bool) {
	r := *source
	start := r.Offset()
	var out BooleanFloatExpr
	type value struct {
		raw      bool
		variable uint32
		pair     [2]uint32
	}
	var stack [16]value
	depth := 0
	variableSeen := false
	for count := 0; count < 32 && r.Offset()-start < 128; count++ {
		op, err := r.Byte()
		if err != nil {
			return out, false
		}
		switch op {
		case 0x20:
			index, err := r.U32()
			if err != nil || depth == len(stack) {
				return out, false
			}
			stack[depth] = value{raw: true, variable: index}
			depth++
		case 0x41:
			n, err := r.I32()
			if err != nil || depth == len(stack) {
				return out, false
			}
			stack[depth] = value{pair: [2]uint32{uint32(n), uint32(n)}}
			depth++
		case 0xb2, 0xb3, 0xb7, 0xb8:
			if depth != 1 || stack[0].raw || !variableSeen {
				return out, false
			}
			out.Values = stack[0].pair
			out.Wide = op == 0xb7 || op == 0xb8
			out.Signed = op == 0xb2 || op == 0xb7
			out.End = r.Offset()
			return out, true
		default:
			var semantic IntOp
			switch op {
			case 0x6a:
				semantic = IntAdd
			case 0x6b:
				semantic = IntSub
			case 0x6c:
				semantic = IntMul
			case 0x71:
				semantic = IntAnd
			case 0x72:
				semantic = IntOr
			case 0x73:
				semantic = IntXor
			case 0x74:
				semantic = IntShl
			case 0x75:
				semantic = IntShrS
			case 0x76:
				semantic = IntShrU
			default:
				return out, false
			}
			if depth < 2 {
				return out, false
			}
			left, right := stack[depth-2], stack[depth-1]
			var result value
			if left.raw || right.raw {
				if semantic != IntAnd {
					return out, false
				}
				if right.raw {
					left, right = right, left
				}
				if right.raw || right.pair != ([2]uint32{1, 1}) || variableSeen && out.Variable != left.variable {
					return out, false
				}
				variableSeen = true
				out.Variable = left.variable
				result.pair = [2]uint32{0, 1}
			} else {
				for i := range result.pair {
					result.pair[i] = FoldI32(semantic, left.pair[i], right.pair[i])
				}
			}
			stack[depth-2] = result
			depth--
		}
	}
	return out, false
}
