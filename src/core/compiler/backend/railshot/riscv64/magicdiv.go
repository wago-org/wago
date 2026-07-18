//go:build riscv64

package riscv64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot"

// Constant-divisor strength reduction. A div/rem by a constant is replaced with
// cheap shifts (power-of-2 divisors) or a multiply-high + shift (the Granlund–
// Montgomery "magic number" method, added on top of this), avoiding the ~20–40-
// cycle SDIV/UDIV. Correctness is checked exhaustively against SDIV/UDIV in the
// tests.

// tryDivByConst lowers node (a div/rem whose divisor arg1 is the constant c) via
// strength reduction, returning (resultReg, true) on success or (regNone, false)
// to fall back to the general div path (e.g. c == 0, which must trap).
func (f *fn) tryDivByConst(node *elem, dest Reg, c int64) (Reg, bool) {
	if c == 0 {
		return regNone, false // div/rem by zero traps — leave it to the div path
	}
	w := node.typ.is64()
	signed := node.op == opDivS || node.op == opRemS
	wantRem := node.op == opRemU || node.op == opRemS

	// Decide whether we can handle this divisor before emitting anything, so a
	// bail-out leaves the operand stack untouched for the div fallback.
	if !strengthReducible(c, w, signed) {
		return regNone, false
	}

	// Compute the dividend into an owned result register. Unlike x86 (which had to
	// keep it clear of RAX/RDX for the one-operand MUL), RV64's multiply-high is
	// orthogonal, so no register needs to be avoided here.
	res := f.allocReg(maskOf())
	f.pinned = f.pinned.add(res)
	f.condenseInto(node.arg0, res) // res = n (dividend)

	if signed {
		f.divConstSigned(res, c, w, wantRem)
	} else {
		ud := uint64(c)
		if !w {
			ud = uint64(uint32(c))
		}
		f.divConstUnsigned(res, ud, w, wantRem)
	}
	f.pinned = f.pinned.remove(res)

	result := res
	if dest != regNone && dest != res {
		f.a.MovReg64(dest, res)
		f.release(res)
		result = dest
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	f.stats.peep("div-by-const")
	return result, true
}

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
	if c == 0 {
		return false
	}
	var ad uint64 // divisor magnitude, as an unsigned W-bit value
	if signed {
		if c == 1 || c == -1 {
			return false
		}
		if c < 0 {
			ad = uint64(-c)
		} else {
			ad = uint64(c)
		}
	} else {
		ad = uint64(c)
		if !w {
			ad = uint64(uint32(c))
		}
	}
	if ad&(ad-1) == 0 {
		return true // power of two (or, unsigned, d == 1)
	}
	if signed {
		return magicDivSignedEnabled
	}
	return magicDivEnabled
}

// divConstUnsigned rewrites res (holding the W-bit dividend) to res / d or res % d
// (unsigned) in place.
func (f *fn) divConstUnsigned(res Reg, d uint64, w, wantRem bool) {
	if d == 1 {
		if wantRem {
			f.a.MovImm64(res, 0) // x % 1 == 0 (register-zero; not a flag op on riscv64)
		}
		return
	}
	if d&(d-1) == 0 { // power of two
		k := log2u(d)
		if wantRem {
			f.andImm(res, int64(d-1), w) // x % 2^k == x & (2^k - 1)
		} else {
			f.shiftImm(shLSR, res, byte(k), w) // lsr res, k
		}
		return
	}
	f.divConstUnsignedMagic(res, d, w, wantRem)
}

// divConstSigned rewrites res (holding the W-bit dividend) to res / d or res % d
// (signed, truncating toward zero) in place. Only called for |d| >= 2.
func (f *fn) divConstSigned(res Reg, d int64, w, wantRem bool) {
	W := uint(32)
	if w {
		W = 64
	}
	ad := d
	neg := d < 0
	if neg {
		ad = -d
	}
	if ad&(ad-1) == 0 { // |d| is a power of two
		k := log2u(uint64(ad))
		// Bias the dividend toward zero before an arithmetic shift: add (2^k - 1)
		// when it is negative. bias = (res >>s (W-1)) >>u (W-k) is 2^k-1 for res<0,
		// else 0.
		bias := f.signBias(res, k, W, w)
		if wantRem {
			// r = ((res + bias) & (2^k-1)) - bias — sign follows the dividend and is
			// independent of d's sign (wasm: x % d == x % -d). RV64 ALU ops are
			// three-operand; the in-place store-form (dst = dst op src) is emitted
			// as Rd==Rn==dst via aluRR.
			f.aluRR(opAdd, res, bias, w) // res += bias
			f.andImm(res, int64(uint64(ad)-1), w)
			f.aluRR(opSub, res, bias, w) // res -= bias
		} else {
			f.aluRR(opAdd, res, bias, w)       // res += bias
			f.shiftImm(shASR, res, byte(k), w) // asr res, k
			if neg {
				f.neg(res, w)
			}
		}
		f.pinned = f.pinned.remove(bias)
		return
	}
	f.divConstSignedMagic(res, d, w, wantRem)
}

// signBias returns an owned register holding (2^k - 1) when res is negative, else
// 0 — the round-toward-zero bias for a signed power-of-2 divide.
func (f *fn) signBias(res Reg, k int, W uint, w bool) Reg {
	t := f.allocReg(maskOf(res))
	f.pinned = f.pinned.add(t) // pinned so a later andImm scratch can't clobber it
	f.a.MovReg64(t, res)
	f.shiftImm(shASR, t, byte(W-1), w)      // asr t, W-1  → 0 or -1
	f.shiftImm(shLSR, t, byte(int(W)-k), w) // lsr t, W-k  → 0 or (2^k - 1)
	return t
}

// neg negates reg in place (NEG Rd,Rm == SUB Rd,ZR,Rm — RV64 has no dedicated
// NEG, it is an alias of the reverse-subtract from the zero register).
func (f *fn) neg(reg Reg, w bool) {
	if w {
		f.a.Sub64(reg, ZR, reg)
	} else {
		f.a.Sub32(reg, ZR, reg)
	}
}

// andImm emits `and reg, imm`. RV64 logical ops take a bitmask immediate (a
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

// divConstUnsignedMagic rewrites res (the W-bit dividend) to res / d or res % d
// (unsigned, d not a power of two) via a multiply-high + shift.
func (f *fn) divConstUnsignedMagic(res Reg, d uint64, w, wantRem bool) {
	W := uint(32)
	if w {
		W = 64
	}
	magic, shift, add := railshot.MagicU(d, W)
	q := f.magicMulHigh(res, magic, w, false) // q = high(res * magic), pinned
	if add {
		// q = ((n - q) >> 1) + q  (an overflow-free way to add n before the shift).
		t := f.allocReg(maskOf(res, q))
		f.pinned = f.pinned.add(t)
		f.a.MovReg64(t, res)
		f.aluRR(opSub, t, q, w)    // t -= q  (= n - q)
		f.shiftImm(shLSR, t, 1, w) // t >>= 1
		f.aluRR(opAdd, t, q, w)    // t += q
		f.a.MovReg64(q, t)
		f.pinned = f.pinned.remove(t)
	}
	if shift > 0 {
		f.shiftImm(shLSR, q, byte(shift), w) // q >>= shift (logical)
	}
	if wantRem {
		f.remFromQuot(res, q, int64(d), w) // res = n - q*d
	} else {
		f.a.MovReg64(res, q)
	}
	f.pinned = f.pinned.remove(q)
}

// divConstSignedMagic rewrites res (the W-bit dividend) to res / d or res % d
// (signed, |d| not a power of two) via a signed multiply-high + shift.
func (f *fn) divConstSignedMagic(res Reg, d int64, w, wantRem bool) {
	W := uint(32)
	if w {
		W = 64
	}
	ad := uint64(d)
	neg := d < 0
	if neg {
		ad = uint64(-d)
	}
	magic, shift, addN := railshot.MagicS(ad, W)
	q := f.magicMulHigh(res, uint64(magic), w, true) // signed high(res * magic), pinned
	if addN {
		f.aluRR(opAdd, q, res, w) // q += n
	}
	if shift > 0 {
		f.shiftImm(shASR, q, byte(shift), w) // q >>= shift (arithmetic)
	}
	// Turn floor toward negative infinity into truncation toward zero: q += (q>>W-1).
	sign := f.allocReg(maskOf(res, q))
	f.pinned = f.pinned.add(sign)
	f.a.MovReg64(sign, q)
	f.shiftImm(shLSR, sign, byte(W-1), w) // sign = 1 if q<0 else 0
	f.aluRR(opAdd, q, sign, w)            // q += sign
	f.pinned = f.pinned.remove(sign)
	if neg {
		f.neg(q, w) // divisor was negative: negate the quotient
	}
	if wantRem {
		f.remFromQuot(res, q, d, w) // res = n - q*d (d carries its sign; rem sign = dividend's)
	} else {
		f.a.MovReg64(res, q)
	}
	f.pinned = f.pinned.remove(q)
}

// magicMulHigh returns a pinned register holding the high W bits of res*magic
// (signed iff signed). RV64's multiply-high is orthogonal — SMULH/UMULH write
// the high 64 bits of a 64×64 product into any register, and the 32-bit high half
// is bits [63:32] of a full SMULL/UMULL 32×32→64 product — so unlike x86's
// one-operand MUL/IMUL there is no RDX:RAX pair to spill and pin, and res is left
// untouched. The caller unpins the result.
func (f *fn) magicMulHigh(res Reg, magic uint64, w, signed bool) Reg {
	mr := f.allocReg(maskOf(res))
	f.pinned = f.pinned.add(mr)
	f.a.MovImm64(mr, magic)
	hi := f.allocReg(maskOf(res, mr)) // clear of res and the magic
	f.pinned = f.pinned.add(hi)
	if w {
		if signed {
			f.a.Smulh(hi, res, mr) // hi = high 64 bits of (res * magic), signed
		} else {
			f.a.Umulh(hi, res, mr) // hi = high 64 bits of (res * magic), unsigned
		}
	} else {
		// 32-bit high half: form the full 32×32→64 product, then take bits [63:32]
		// down into the low half (a subsequent w=false op reads the low 32 bits).
		if signed {
			f.a.Smull(hi, res, mr)          // hi = (int64)res_lo32 * (int64)magic_lo32
			f.shiftImm(shASR, hi, 32, true) // arithmetic: high 32 bits → low half
		} else {
			f.a.Umull(hi, res, mr)          // hi = (uint64)res_lo32 * (uint64)magic_lo32
			f.shiftImm(shLSR, hi, 32, true) // logical: high 32 bits → low half
		}
	}
	f.pinned = f.pinned.remove(mr)
	f.release(mr)
	return hi
}

// remFromQuot rewrites nreg (holding the dividend n) to n - q*d — the remainder,
// given the already-computed quotient q. Low-W bits of q*d suffice (mod 2^W).
// RV64 MSUB fuses the multiply/subtract as `nreg = nreg - q*d`.
func (f *fn) remFromQuot(nreg, q Reg, d int64, w bool) {
	t := f.allocReg(maskOf(nreg, q))
	f.pinned = f.pinned.add(t)
	f.a.MovImm64(t, uint64(d))
	f.msub(nreg, q, t, nreg, w)
	f.pinned = f.pinned.remove(t)
	f.release(t)
}
