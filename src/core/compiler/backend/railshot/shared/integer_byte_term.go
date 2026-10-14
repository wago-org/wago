package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Every lane of a byte splat is the counter modulo 256. A constant strict
// swizzle selects that byte or zero, so an unsigned extracted lane has a
// bounded periodic sum. No dynamic masks, arithmetic, memory or calls enter
// this proof; the backend separately proves the counter's zero initialization.
func inspectIntegerByteTerm(r *wasm.Reader) (integerAffineTerm, bool) {
	var out integerAffineTerm
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	if !byteIs(0x20) {
		return out, false
	}
	counter, err := r.U32()
	if err != nil || !byteIs(0xfd) {
		return out, false
	}
	op, err := r.U32()
	if err != nil || op != 15 || !byteIs(0xfd) { // i8x16.splat
		return out, false
	}
	op, err = r.U32()
	if err != nil {
		return out, false
	}
	var mask []byte
	if op == 12 { // v128.const, followed by strict i8x16.swizzle
		mask, err = r.Bytes(16)
		if err != nil || !byteIs(0xfd) {
			return out, false
		}
		op, err = r.U32()
		if err != nil || op != 14 || !byteIs(0xfd) {
			return out, false
		}
		op, err = r.U32()
	}
	if err != nil || op != 22 { // i8x16.extract_lane_u
		return out, false
	}
	lane, err := r.Byte()
	if err != nil || lane >= 16 {
		return out, false
	}
	out = integerAffineTerm{variable: counter, hasVariable: true, width: 4}
	if mask == nil || mask[lane] < 16 {
		out.coefficient, out.unsignedByte = 1, true
	}
	return out, true
}
