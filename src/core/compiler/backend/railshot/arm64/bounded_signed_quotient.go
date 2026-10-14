//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var boundedSignedQuotientEnabled = os.Getenv("WAGO_ARM64_NO_BOUNDED_SIGNED_QUOTIENT") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT") != "0"

// Match a pure quotient/max/min packet whose three assignments overwrite the
// same local. Consuming the final assignment is essential: otherwise the local
// would retain max(q, lo), while only the operand stack contained the clamp.
func (f *fn) tryBoundedSignedQuotient(reader *wasm.Reader) bool {
	if f.unreachable || f.localBase != 0 || len(f.customInstructions) != 0 {
		return false
	}
	r := *reader
	op := func(want byte) bool { v, err := r.Byte(); return err == nil && v == want }
	if !op(0x22) {
		return false
	}
	x, err := r.U32()
	if err != nil || int(x) >= f.nLocals || f.localType[x] != mtI32 {
		return false
	}
	local := func(code byte) bool {
		if !op(code) {
			return false
		}
		y, err := r.U32()
		return err == nil && y == x
	}
	constant := func() (int32, bool) {
		if !op(0x41) {
			return 0, false
		}
		v, err := r.I32()
		return v, err == nil
	}
	lo, ok := constant()
	if !ok || !local(0x20) {
		return false
	}
	loAgain, ok := constant()
	if !ok || loAgain != lo || !op(0x4a) || !op(0x1b) || !local(0x22) {
		return false
	}
	hi, ok := constant()
	if !ok || !local(0x20) {
		return false
	}
	hiAgain, ok := constant()
	if !ok || hiAgain != hi || !op(0x48) || !op(0x1b) {
		return false
	}
	final, err := r.Byte()
	if err != nil || (final != 0x21 && final != 0x22) {
		return false
	}
	y, err := r.U32()
	if err != nil || y != x || lo > 0 || hi < 0 || lo >= hi || lo < -4095 || int64(hi)-int64(lo) > 4095 {
		return false
	}

	f.materializePendingTraps()
	// Preserve all older aliases of the destination before its first assignment.
	f.realizeLocalRefs(int(x), nil)
	previousPins := f.pinned
	right := f.popValue()
	divisor, divisorOwned := f.materializeRead(right)
	f.pinned = f.pinned.add(divisor)
	left := f.popValue()
	dividend, dividendOwned := f.materializeRead(left)
	f.pinned = f.pinned.add(dividend)
	// Write a non-aliasing pinned destination directly, as ordinary local.set
	// sinks do. The old local version has already been preserved above.
	result, floating, pinned := f.pinReg(int(x))
	if !pinned || floating || result == divisor || result == dividend {
		result = f.allocReg(maskOf(divisor, dividend))
	}
	f.pinned = f.pinned.add(result)
	u := f.allocReg(maskOf(divisor, dividend, result))
	f.pinned = f.pinned.add(u)
	product := f.allocReg(maskOf(divisor, dividend, result, u))

	// For d>0 and lo<=0<=hi, widened i32 products cannot overflow i64.
	// Unsigned(n-lo*d) < (hi-lo)*d proves the ordinary quotient is in range.
	// Outside this interval, the sign of n selects the exact saturation bound.
	f.a.CmpImm32(divisor, 0)
	nonPositive := f.a.Bcond(condLE)
	f.a.Sxtw(u, dividend)
	f.a.MovImm32(result, -lo)
	f.a.Smull(product, divisor, result)
	f.a.Add64(u, u, product)
	f.a.MovImm32(result, hi-lo)
	f.a.Smull(product, divisor, result)
	f.a.CmpReg64(u, product)
	inside := f.a.Bcond(condB)
	f.a.MovImm32(result, lo)
	f.a.MovImm32(product, hi)
	f.a.CmpImm32(dividend, 0)
	f.a.Csel32(result, result, product, condL)
	saturated := f.a.Branch()
	f.patchBranch19(inside, f.a.Len())
	f.a.Sdiv32(result, dividend, divisor)
	divided := f.a.Branch()

	// Preserve zero and signed-overflow traps even when the clamp would hide
	// their result. Every negative denominator follows the original division.
	f.patchBranch19(nonPositive, f.a.Len())
	f.cmpImm(divisor, 0, false)
	f.trapIf(condE, trapDivZero)
	f.cmpImmS(divisor, -1, false)
	noOverflow := f.a.Bcond(condNE)
	f.cmpIntMin(dividend, false)
	f.trapIf(condE, trapDivOverflow)
	f.patchBranch19(noOverflow, f.a.Len())
	f.a.Sdiv32(result, dividend, divisor)
	f.a.MovImm32(product, lo)
	f.a.CmpReg32(result, product)
	f.a.Csel32(result, result, product, condG)
	f.a.MovImm32(product, hi)
	f.a.CmpReg32(result, product)
	f.a.Csel32(result, result, product, condL)
	f.patchBranch26(saturated, f.a.Len())
	f.patchBranch26(divided, f.a.Len())
	f.pinned = previousPins
	f.release(u)
	f.release(product)
	if divisorOwned {
		f.release(divisor)
	}
	if dividendOwned && dividend != divisor {
		f.release(dividend)
	}
	e := f.pushReg(result, mtI32)
	e.st.setValueFacts(factUpper32Zero)
	*reader = r
	if x < 64 {
		f.localWritten |= 1 << x
	}
	f.setLocal(reader, int(x), final == 0x22)
	f.stats.peep("bounded-signed-quotient")
	return true
}
