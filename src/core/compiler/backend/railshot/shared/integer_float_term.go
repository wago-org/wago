package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
)

// inspectIntegerFloatTerm admits only proofs valid for the entire zero-based
// i32 unsigned unit-stride loop. The counter's last value is at most UINT_MAX-1,
// so counter+1 never wraps, even for the largest legal loop bound.
//
// An exact dyadic f64 product K*i+i/D can reuse the integer quotient reduction.
// Its odd numerator times UINT_MAX must fit the 53-bit significand; scaling by
// a power of two does not lose precision. Integral products additionally stay
// inside the selected i64 range. A separate monotone positive product of i+1
// may always saturate to the i32 maximum. No f32 fractional proof is admitted.
func inspectIntegerFloatTerm(r *wasm.Reader) (integerAffineTerm, bool) {
	var out integerAffineTerm
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	if !byteIs(0x20) {
		return out, false
	}
	variable, err := r.U32()
	if err != nil {
		return out, false
	}
	out.variable, out.hasVariable, out.width = variable, true, 4
	increment := false
	if op, _ := r.Peek(); op == 0x41 {
		_, _ = r.Byte()
		one, err := r.I32()
		if err != nil || one != 1 || !byteIs(0x6a) {
			return out, false
		}
		increment = true
	}
	conversion, err := r.Byte()
	if err != nil || (conversion != 0xb3 && conversion != 0xb8) {
		return out, false
	}
	wide := conversion == 0xb8
	var raw uint64
	if wide {
		if !byteIs(0x44) {
			return out, false
		}
		raw, err = r.LEU64()
		if err != nil || raw>>63 != 0 || raw>>52&0x7ff == 0x7ff {
			return out, false
		}
		if !byteIs(0xa2) {
			return out, false
		}
	} else {
		if !byteIs(0x43) {
			return out, false
		}
		v, e := r.LEU32()
		raw, err = uint64(v), e
		if err != nil || raw>>31 != 0 || raw>>23&0xff == 0xff {
			return out, false
		}
		if !byteIs(0x94) {
			return out, false
		}
	}
	if !byteIs(0xfc) {
		return out, false
	}
	sub, err := r.U32()
	if err != nil {
		return out, false
	}
	if increment {
		base := uint32(0)
		if wide {
			base = 2
		}
		if sub != base && sub != base+1 {
			return out, false
		}
		threshold := uint64(0x4f000000)
		if wide {
			threshold = 0x41e0000000000000
		}
		out.constant = 0x7fffffff
		if sub == base+1 {
			out.constant = 0xffffffff
			if wide {
				threshold = 0x41f0000000000000
			} else {
				threshold = 0x4f800000
			}
		}
		if raw < threshold {
			return out, false
		}
	} else {
		if !wide || (sub != 6 && sub != 7) || !byteIs(0xa7) {
			return out, false
		}
		exponent := int(raw>>52&0x7ff) - 1023 - 52
		if raw>>52&0x7ff == 0 {
			return out, false
		}
		numerator := (uint64(1) << 52) | (raw & ((1 << 52) - 1))
		shift := bits.TrailingZeros64(numerator)
		numerator >>= shift
		exponent += shift
		// A normalized odd numerator needs at most 21 bits when multiplied by
		// an arbitrary unsigned i32. All admitted products are therefore exact.
		if numerator > ((uint64(1)<<53)-1)/uint64(0xffffffff) {
			return out, false
		}
		if exponent >= 0 {
			if exponent > 32 {
				return out, false
			}
			coefficient := numerator << uint(exponent)
			limit := uint64(0x7fffffffffffffff) / uint64(0xffffffff)
			if sub == 7 {
				limit = ^uint64(0) / uint64(0xffffffff)
			}
			if coefficient > limit {
				return out, false
			}
			out.coefficient = coefficient
		} else {
			if exponent < -31 {
				return out, false
			}
			divisor := uint64(1) << uint(-exponent)
			if numerator%divisor != 1 {
				return out, false
			}
			out.coefficient, out.divisor, out.quotientCoefficient = numerator/divisor, divisor, 1
		}
	}
	op, ok := r.Peek()
	return out, ok && op == 0x6a
}
