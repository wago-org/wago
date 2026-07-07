package amd64

// Constant-divisor strength reduction. A div/rem by a constant is replaced with
// cheap shifts (power-of-2 divisors) or a multiply-high + shift (the Granlund–
// Montgomery "magic number" method, added on top of this), avoiding the ~20–40-
// cycle idiv. Correctness is checked exhaustively against idiv in the tests.

// tryDivByConst lowers node (a div/rem whose divisor arg1 is the constant c) via
// strength reduction, returning (resultReg, true) on success or (regNone, false)
// to fall back to the general idiv path (e.g. c == 0, which must trap).
func (f *fn) tryDivByConst(node *elem, dest Reg, c int64) (Reg, bool) {
	if c == 0 {
		return regNone, false // div/rem by zero traps — leave it to the idiv path
	}
	w := node.typ.is64()
	signed := node.op == opDivS || node.op == opRemS
	wantRem := node.op == opRemU || node.op == opRemS

	// Decide whether we can handle this divisor before emitting anything, so a
	// bail-out leaves the operand stack untouched for the idiv fallback.
	if !strengthReducible(c, w, signed) {
		return regNone, false
	}

	// Compute the dividend into an owned result register (kept clear of RAX/RDX so
	// a magic multiply-high can use them without disturbing it).
	res := f.allocReg(maskOf(RAX, RDX))
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
// rather than via idiv. Signed ±1 stay on idiv (it handles the INT_MIN/-1 trap
// and x%±1). Power-of-2 divisors are always reducible; non-power-of-2 needs the
// (gated) magic path.
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
			f.a.XorSelf32(res) // x % 1 == 0
		}
		return
	}
	if d&(d-1) == 0 { // power of two
		k := log2u(d)
		if wantRem {
			f.andImm(res, int64(d-1), w) // x % 2^k == x & (2^k - 1)
		} else {
			f.a.ShiftImm(5, res, byte(k), w) // shr res, k
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
			// independent of d's sign (wasm: x % d == x % -d). AluRR store-form
			// opcodes (0x01 add, 0x29 sub, 0x21 and) take dst first: dst op= src.
			f.a.AluRR(0x01, res, bias, w) // res += bias
			f.andImm(res, int64(uint64(ad)-1), w)
			f.a.AluRR(0x29, res, bias, w) // res -= bias
		} else {
			f.a.AluRR(0x01, res, bias, w)    // res += bias
			f.a.ShiftImm(7, res, byte(k), w) // sar res, k
			if neg {
				f.a.Neg(res, w)
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
	f.a.ShiftImm(7, t, byte(W-1), w)      // sar t, W-1  → 0 or -1
	f.a.ShiftImm(5, t, byte(int(W)-k), w) // shr t, W-k  → 0 or (2^k - 1)
	return t
}

// andImm emits `and reg, imm`. The mask fits imm32 for k <= 31 (i32, and the
// common i64 case); a wider i64 mask is loaded into a scratch first.
func (f *fn) andImm(reg Reg, imm int64, w bool) {
	if imm >= -1<<31 && imm < 1<<31 {
		f.a.AluRI(4, reg, int32(imm), w) // and reg, imm32
		return
	}
	t := f.allocReg(maskOf(reg))
	f.a.MovImm64(t, uint64(imm))
	f.a.AluRR(0x21, reg, t, w) // reg &= t
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
	magic, shift, add := magicU(d, W)
	q := f.magicMulHigh(res, magic, w, false) // q = high(res * magic), pinned
	if add {
		// q = ((n - q) >> 1) + q  (an overflow-free way to add n before the shift).
		t := f.allocReg(maskOf(res, q))
		f.pinned = f.pinned.add(t)
		f.a.MovReg64(t, res)
		f.a.AluRR(0x29, t, q, w) // t -= q  (= n - q)
		f.a.ShiftImm(5, t, 1, w) // t >>= 1
		f.a.AluRR(0x01, t, q, w) // t += q
		f.a.MovReg64(q, t)
		f.pinned = f.pinned.remove(t)
	}
	if shift > 0 {
		f.a.ShiftImm(5, q, byte(shift), w) // q >>= shift (logical)
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
	magic, shift, addN := magicS(ad, W)
	q := f.magicMulHigh(res, uint64(magic), w, true) // signed high(res * magic), pinned
	if addN {
		f.a.AluRR(0x01, q, res, w) // q += n
	}
	if shift > 0 {
		f.a.ShiftImm(7, q, byte(shift), w) // q >>= shift (arithmetic)
	}
	// Turn floor toward negative infinity into truncation toward zero: q += (q>>W-1).
	sign := f.allocReg(maskOf(res, q))
	f.pinned = f.pinned.add(sign)
	f.a.MovReg64(sign, q)
	f.a.ShiftImm(5, sign, byte(W-1), w) // sign = 1 if q<0 else 0
	f.a.AluRR(0x01, q, sign, w)         // q += sign
	f.pinned = f.pinned.remove(sign)
	if neg {
		f.a.Neg(q, w) // divisor was negative: negate the quotient
	}
	if wantRem {
		f.remFromQuot(res, q, d, w) // res = n - q*d (d carries its sign; rem sign = dividend's)
	} else {
		f.a.MovReg64(res, q)
	}
	f.pinned = f.pinned.remove(q)
}

// magicMulHigh returns a pinned register holding the high W bits of res*magic
// (signed iff signed). It uses RAX/RDX (spilling their occupants) but leaves res
// untouched, since the caller keeps res out of RAX/RDX. The caller unpins.
func (f *fn) magicMulHigh(res Reg, magic uint64, w, signed bool) Reg {
	f.spillIfUsed(RAX)
	f.spillIfUsed(RDX)
	f.pinned = f.pinned.add(RAX)
	f.pinned = f.pinned.add(RDX)
	mr := f.allocReg(maskOf(RAX, RDX, res))
	f.pinned = f.pinned.add(mr)
	f.a.MovImm64(mr, magic)
	f.a.MovReg64(RAX, res)
	if signed {
		f.a.IMulHigh(mr, w)
	} else {
		f.a.Mul(mr, w)
	}
	f.pinned = f.pinned.remove(mr)
	f.pinned = f.pinned.remove(RAX)
	f.pinned = f.pinned.remove(RDX)
	hi := f.allocReg(maskOf(res)) // RAX/RDX are free again; avoid clobbering res
	f.pinned = f.pinned.add(hi)
	f.a.MovReg64(hi, RDX) // high half of the product
	return hi
}

// remFromQuot rewrites nreg (holding the dividend n) to n - q*d — the remainder,
// given the already-computed quotient q. Low-W bits of q*d suffice (mod 2^W).
func (f *fn) remFromQuot(nreg, q Reg, d int64, w bool) {
	t := f.allocReg(maskOf(nreg, q))
	f.pinned = f.pinned.add(t)
	f.a.MovImm64(t, uint64(d))
	f.a.IMul(t, q, w)           // t = d * q
	f.a.AluRR(0x29, nreg, t, w) // nreg -= t
	f.pinned = f.pinned.remove(t)
}
