package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A parity predicate partitions the unsigned unit-stride counter into two
// arithmetic progressions. Each branch must be a bounded pure i32 affine term
// in that same counter. No nested control, memory, globals, calls or traps are
// admitted, and the enclosing reduction separately proves zero initialization.
func inspectIntegerParityTerm(r *wasm.Reader) (integerAffineTerm, IntegerSumLoop, bool) {
	var p IntegerSumLoop
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	if !byteIs(0x20) {
		return integerAffineTerm{}, p, false
	}
	counter, err := r.U32()
	if err != nil || !byteIs(0x41) {
		return integerAffineTerm{}, p, false
	}
	mask, err := r.I32()
	if err != nil || mask != 1 || !byteIs(0x71) {
		return integerAffineTerm{}, p, false
	}
	inverted := false
	if op, _ := r.Peek(); op == 0x45 {
		_, _ = r.Byte()
		inverted = true
	}
	if !byteIs(0x04) || !byteIs(0x7f) {
		return integerAffineTerm{}, p, false
	}
	odd, ok := inspectIntegerAffineTermUntil(r, true, nil, false, 0x05)
	if !ok || !byteIs(0x05) {
		return integerAffineTerm{}, p, false
	}
	even, ok := inspectIntegerAffineTermUntil(r, true, nil, false, 0x0b)
	if !ok || !byteIs(0x0b) {
		return integerAffineTerm{}, p, false
	}
	for _, branch := range []integerAffineTerm{odd, even} {
		if branch.width != 0 && branch.width != 4 || branch.hasVariable && branch.variable != counter || branch.hasGlobal || branch.calls != 0 || branch.writeCount != 0 || branch.recurrence {
			return integerAffineTerm{}, p, false
		}
	}
	if inverted {
		odd, even = even, odd
	}
	p.Periodic = true
	p.OddCoefficient, p.OddConstant = odd.coefficient, odd.constant
	p.EvenCoefficient, p.EvenConstant = even.coefficient, even.constant
	return integerAffineTerm{width: 4, variable: counter, hasVariable: true}, p, true
}
