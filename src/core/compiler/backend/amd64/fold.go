package amd64

import "math"

// Constant folding.
//
// When both operands of an integer op are compile-time constants (vConst), the
// result is computed here and pushed as a single constant, so no arithmetic is
// emitted at all. Producers (LLVM, etc.) already fold most constants, so this
// mainly cleans up hand-written wat and constants exposed by other peepholes.
//
// All results are width-masked and packed exactly like i32.const / i64.const:
// i32 values are sign-extended from bit 31 (matching how the rest of the
// backend stores and re-reads vConst.cval).

// opKind identifies a foldable add-class ALU operation (see aluDesc.op).
type opKind uint8

const (
	opAddK opKind = iota
	opSubK
	opAndK
	opOrK
	opXorK
)

// uw masks a constant to the operating width as an unsigned value.
func uw(v int64, w bool) uint64 {
	if w {
		return uint64(v)
	}
	return uint64(uint32(v))
}

// packConst stores a width-masked result the way i32.const/i64.const do.
func packConst(v uint64, w bool) int64 {
	if w {
		return int64(v)
	}
	return int64(int32(uint32(v)))
}

func foldALU(op opKind, a, b int64, w bool) int64 {
	x, y := uw(a, w), uw(b, w)
	var r uint64
	switch op {
	case opAddK:
		r = x + y
	case opSubK:
		r = x - y
	case opAndK:
		r = x & y
	case opOrK:
		r = x | y
	case opXorK:
		r = x ^ y
	}
	return packConst(r, w)
}

func foldMul(a, b int64, w bool) int64 { return packConst(uw(a, w)*uw(b, w), w) }

// foldShift folds shl/shr/sar/rol/ror, keyed by the x86 shift-group /digit used
// by g.shift (4=shl, 5=shr_u, 7=shr_s, 0=rotl, 1=rotr). The shift count is
// masked to the width, matching wasm semantics.
func foldShift(digit byte, a, b int64, w bool) int64 {
	bits := uint(32)
	if w {
		bits = 64
	}
	x := uw(a, w)
	k := uint(uw(b, w) & uint64(bits-1))
	var r uint64
	switch digit {
	case 4: // shl
		r = x << k
	case 5: // shr_u
		r = x >> k
	case 7: // shr_s (arithmetic)
		if w {
			r = uint64(int64(x) >> k)
		} else {
			r = uint64(uint32(int32(uint32(x)) >> k))
		}
	case 0: // rotl
		if k == 0 {
			r = x
		} else {
			r = (x << k) | (x >> (bits - k))
		}
	case 1: // rotr
		if k == 0 {
			r = x
		} else {
			r = (x >> k) | (x << (bits - k))
		}
	}
	return packConst(r, w)
}

// foldCmp evaluates an integer comparison given the x86 condition g.cmp would
// have set-cc'd. Result is an i32 0/1 (booleans are i32, never wide).
func foldCmp(cond Cond, a, b int64, w bool) int64 {
	x, y := uw(a, w), uw(b, w)
	var sx, sy int64
	if w {
		sx, sy = int64(x), int64(y)
	} else {
		sx, sy = int64(int32(uint32(x))), int64(int32(uint32(y)))
	}
	var res bool
	switch cond {
	case CondE:
		res = x == y
	case CondNE:
		res = x != y
	case CondB:
		res = x < y // unsigned
	case CondAE:
		res = x >= y
	case CondBE:
		res = x <= y
	case CondA:
		res = x > y
	case CondL:
		res = sx < sy // signed
	case CondGE:
		res = sx >= sy
	case CondLE:
		res = sx <= sy
	case CondG:
		res = sx > sy
	}
	if res {
		return 1
	}
	return 0
}

// foldDivRem folds integer div/rem when the result is well-defined. It returns
// ok=false for the cases that trap at runtime (divide by zero, and signed
// div overflow INT_MIN/-1) so the caller falls back to real codegen that
// reproduces the trap. Signed rem of INT_MIN/-1 is defined as 0 (no trap).
func foldDivRem(signed, wantRem, w bool, a, b int64) (int64, bool) {
	x, y := uw(a, w), uw(b, w)
	if y == 0 {
		return 0, false
	}
	if signed {
		var sx, sy, smin int64
		if w {
			sx, sy, smin = int64(x), int64(y), math.MinInt64
		} else {
			sx, sy, smin = int64(int32(uint32(x))), int64(int32(uint32(y))), math.MinInt32
		}
		if sy == -1 && sx == smin {
			if wantRem {
				return packConst(0, w), true
			}
			return 0, false // div overflow traps
		}
		if wantRem {
			return packConst(uint64(sx%sy), w), true
		}
		return packConst(uint64(sx/sy), w), true
	}
	if wantRem {
		return packConst(x%y, w), true
	}
	return packConst(x/y, w), true
}

// bothConst reports whether the two given stack entries are integer constants.
func bothConst(a, b ventry) bool {
	return a.kind == vConst && b.kind == vConst && !a.fp && !b.fp
}
