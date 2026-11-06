//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot"

// Division lowering consumes the reciprocal multiplier rather than the source
// divisor. Reuse the ordinary immutable loop lease for that generated constant.
func loopIntConstRegisterValue(next byte, value int64, typ uint8) (int64, uint8, bool) {
	if typ == 1 && (next == 0x6e || next == 0x70) { // i32.div_u / i32.rem_u
		divisor := uint64(uint32(value))
		if divisor > 1 && divisor&(divisor-1) != 0 {
			magic, _, add := railshot.MagicU(divisor, 32)
			if add {
				magic = wideReciprocalU32(divisor)
			}
			return int64(magic), 2, true
		}
		return value, typ, false
	}
	return value, typ, loopIntConstNeedsRegister(next, value, typ)
}
