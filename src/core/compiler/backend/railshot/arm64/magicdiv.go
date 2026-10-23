//go:build arm64

package arm64

// Constant-divisor strength reduction. A div/rem by a constant is replaced with
// cheap shifts (power-of-2 divisors) or a multiply-high + shift (the Granlund–
// Montgomery "magic number" method, added on top of this), avoiding the ~20–40-
// cycle SDIV/UDIV. Correctness is checked exhaustively against SDIV/UDIV in the
// tests.

// magicDivEnabled / magicDivSignedEnabled gate the multiply-high path for non-
// power-of-2 divisors (power-of-2 shifts are always used). Signed magic is a
// separate flag so it can be disabled independently.
var (
	magicDivEnabled       = true
	magicDivSignedEnabled = true
)

// strengthReducible reports whether div/rem by the constant c is lowered here
// rather than via SDIV/UDIV. Signed ±1 stay on the div path (it handles the
// INT_MIN/-1 trap and x%±1). Power-of-2 divisors are always reducible;
// non-power-of-2 needs the (gated) magic path.
func strengthReducible(c int64, w, signed bool) bool {
	return strengthReducibleWithMagic(c, w, signed, magicDivEnabled)
}

// andImm emits `and reg, imm`. AArch64 logical ops take a bitmask immediate (a
// rotated run of ones); when the mask is encodable the encoder's AndImm* folds it
// directly, otherwise the constant is materialized into a scratch and the reg-reg
// form is used (replacing x86's single fitsImm32 gate).
func (f *fn) andImm(reg Reg, imm int64, w bool) {
	var ok bool
	if w {
		ok = f.a.AndImm64(reg, reg, uint64(imm))
	} else {
		ok = f.a.AndImm32(reg, reg, uint32(imm))
	}
	if ok {
		return
	}
	t := f.allocReg(maskOf(reg))
	f.a.MovImm64(t, uint64(imm))
	f.aluRR(opAnd, reg, t, w) // reg &= t
	f.release(t)
}

// --- magic multiply-high path (non-power-of-2 divisors) ---
