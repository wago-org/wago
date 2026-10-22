//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const trapFloatConversion = 7 // private tag; production TrapTruncOverflow is 11

func scalarWide(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I64) || wasm.EqualValType(t, wasm.F64)
}
func scalarType(t wasm.ValType, floating bool) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) || floating && (wasm.EqualValType(t, wasm.F32) || wasm.EqualValType(t, wasm.F64))
}

func plainScalarConst(expr wasm.Expr, typ wasm.ValType, floating bool) (uint64, error) {
	if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) {
		return plainConst(expr, scalarWide(typ))
	}
	if !floating {
		return 0, unsupported("FP initializer outside production")
	}
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	if err != nil {
		return 0, err
	}
	var v uint64
	if op == 0x43 && wasm.EqualValType(typ, wasm.F32) {
		var x uint32
		x, err = r.LEU32()
		v = uint64(x)
	} else if op == 0x44 && wasm.EqualValType(typ, wasm.F64) {
		v, err = r.LEU64()
	} else {
		return 0, unsupported("nonliteral FP initializer")
	}
	if err != nil {
		return 0, err
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return 0, unsupported("extended FP initializer")
	}
	return v, nil
}

func floatOpcode(op byte) (wide, unary, resultWide, ok bool) {
	switch {
	case op >= 0x5b && op <= 0x66:
		return op >= 0x61, false, false, true
	case op >= 0x8b && op <= 0x98:
		return false, op <= 0x91, false, true
	case op >= 0x99 && op <= 0xa6:
		return true, op <= 0x9f, true, true
	case op >= 0xa8 && op <= 0xab:
		return op >= 0xaa, true, false, true
	case op >= 0xae && op <= 0xb1:
		return op >= 0xb0, true, true, true
	case op >= 0xb2 && op <= 0xbb:
		return op == 0xb4 || op == 0xb5 || op == 0xb6 || op == 0xb9 || op == 0xba, true, op >= 0xb7, true
	case op >= 0xbc && op <= 0xbf:
		return op == 0xbd || op == 0xbf, true, op == 0xbd || op == 0xbf, true
	}
	return false, false, false, false
}

func saturatingFloatKind(kind wasm.InstrKind) (byte, bool) {
	for i, k := range [...]wasm.InstrKind{wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U, wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U} {
		if kind == k {
			return byte(i), true
		}
	}
	return 0, false
}

// Only caller-saved f0..f2 are scratch. All live guest values, including raw NaN
// payloads, reside in canonical GP/stack slots across calls and control joins.
func (f *integerEmitter) floatInput(dst enc.FReg, src enc.Reg, wide bool) {
	if regallocCheckEnabled {
		saved := f.checkProtectRead(uint8(src), false)
		defer f.checkRestoreRead(saved)
	}
	if wide {
		f.a.FmvDX(dst, src)
	} else {
		f.a.FmvWX(dst, src)
	}
}
func (f *integerEmitter) floatSecondInput(wide bool) {
	if regallocCheckEnabled {
		saved := f.checkProtectRead(uint8(enc.F0), true)
		defer f.checkRestoreRead(saved)
	}
	f.floatInput(enc.F1, enc.X6, wide)
}
func (f *integerEmitter) floatOutput(src enc.FReg, wide bool) {
	if wide {
		f.a.FmvXD(enc.X5, src)
	} else {
		f.a.FmvXW(enc.X5, src)
		f.a.ZeroExtend32(enc.X5, enc.X5)
	}
}
func (f *integerEmitter) floatClass(dst enc.Reg, src enc.FReg, wide bool) {
	if wide {
		f.a.FclassD(dst, src)
	} else {
		f.a.FclassS(dst, src)
	}
}
func (f *integerEmitter) floatNaNBits(wide bool) {
	bits := uint64(0x7fc00000)
	if wide {
		bits = 0x7ff8000000000000
	}
	f.Constant(5, int64(bits), wide)
}
func (f *integerEmitter) floatNaNBranch(wide bool) int {
	f.floatClass(enc.X6, enc.F0, wide)
	f.Constant(7, 0x300, false)
	f.a.And(enc.X6, enc.X6, enc.X7)
	return f.a.BranchLong(enc.NE, enc.X6, enc.Zero)
}
func (f *integerEmitter) floatPatch(site int) {
	if err := f.Patch(site, f.Position()); err != nil {
		f.err = err
	}
}

func (f *integerEmitter) floatOperation(op byte) {
	wide, _, _, _ := floatOpcode(op)
	if op >= 0xbc {
		return
	} // reinterpret retains raw bits; slot transport normalizes 32 bits.
	if op >= 0xa8 && op <= 0xab {
		f.floatTrunc(wide, false, op&1 != 0, false)
		return
	}
	if op >= 0xae && op <= 0xb1 {
		f.floatTrunc(wide, true, op&1 != 0, false)
		return
	}
	if op >= 0xb2 && op <= 0xbb {
		f.floatConvert(op)
		return
	}
	if op >= 0x5b && op <= 0x66 {
		f.floatInput(enc.F0, enc.X5, wide)
		f.floatSecondInput(wide)
		n := op - 0x5b
		if wide {
			n = op - 0x61
		}
		left, right := enc.F0, enc.F1
		if n == 3 || n == 5 {
			left, right = right, left
		}
		if wide {
			switch n {
			case 0, 1:
				f.a.FeqD(enc.X5, left, right)
			case 2, 3:
				f.a.FltD(enc.X5, left, right)
			case 4, 5:
				f.a.FleD(enc.X5, left, right)
			}
		} else {
			switch n {
			case 0, 1:
				f.a.FeqS(enc.X5, left, right)
			case 2, 3:
				f.a.FltS(enc.X5, left, right)
			case 4, 5:
				f.a.FleS(enc.X5, left, right)
			}
		}
		if n == 1 {
			f.a.Xori(enc.X5, enc.X5, 1)
		}
		return
	}
	n := op - 0x8b
	if wide {
		n = op - 0x99
	}
	if n == 0 || n == 1 || n == 13 {
		sign := uint64(1) << 31
		if wide {
			sign = uint64(1) << 63
		}
		if n == 1 {
			f.Constant(7, int64(sign), wide)
			f.a.Xor(enc.X5, enc.X5, enc.X7)
		} else {
			f.Constant(7, int64(^sign), true)
			f.a.And(enc.X5, enc.X5, enc.X7)
			if n == 13 {
				f.Constant(7, int64(sign), wide)
				f.a.And(enc.X6, enc.X6, enc.X7)
				f.a.Or(enc.X5, enc.X5, enc.X6)
			}
		}
		return
	}
	f.floatInput(enc.F0, enc.X5, wide)
	if n >= 2 && n <= 5 {
		f.floatRound(wide, []enc.RoundingMode{enc.RUP, enc.RDN, enc.RTZ, enc.RNE}[n-2])
		return
	}
	if n >= 7 {
		f.floatSecondInput(wide)
	}
	if n == 11 || n == 12 {
		f.floatMinMax(wide, n == 12)
		return
	}
	if wide {
		switch n {
		case 6:
			f.a.FsqrtD(enc.F0, enc.F0, enc.RNE)
		case 7:
			f.a.FaddD(enc.F0, enc.F0, enc.F1, enc.RNE)
		case 8:
			f.a.FsubD(enc.F0, enc.F0, enc.F1, enc.RNE)
		case 9:
			f.a.FmulD(enc.F0, enc.F0, enc.F1, enc.RNE)
		case 10:
			f.a.FdivD(enc.F0, enc.F0, enc.F1, enc.RNE)
		}
	} else {
		switch n {
		case 6:
			f.a.FsqrtS(enc.F0, enc.F0, enc.RNE)
		case 7:
			f.a.FaddS(enc.F0, enc.F0, enc.F1, enc.RNE)
		case 8:
			f.a.FsubS(enc.F0, enc.F0, enc.F1, enc.RNE)
		case 9:
			f.a.FmulS(enc.F0, enc.F0, enc.F1, enc.RNE)
		case 10:
			f.a.FdivS(enc.F0, enc.F0, enc.F1, enc.RNE)
		}
	}
	f.floatOutput(enc.F0, wide)
}

func (f *integerEmitter) floatMinMax(wide, maximum bool) {
	f.floatClass(enc.X6, enc.F0, wide)
	f.floatClass(enc.X7, enc.F1, wide)
	f.a.Or(enc.X6, enc.X6, enc.X7)
	f.Constant(7, 0x300, false)
	f.a.And(enc.X6, enc.X6, enc.X7)
	nan := f.a.BranchLong(enc.NE, enc.X6, enc.Zero)
	if wide {
		if maximum {
			f.a.FmaxD(enc.F0, enc.F0, enc.F1)
		} else {
			f.a.FminD(enc.F0, enc.F0, enc.F1)
		}
	} else {
		if maximum {
			f.a.FmaxS(enc.F0, enc.F0, enc.F1)
		} else {
			f.a.FminS(enc.F0, enc.F0, enc.F1)
		}
	}
	f.floatOutput(enc.F0, wide)
	end := f.Jump()
	f.floatPatch(nan)
	f.floatNaNBits(wide)
	f.floatPatch(end)
}

// Baseline F/D has no floating rounding-to-integral instruction. Values below
// 2^63 round through a signed integer; larger finite values are already integral.
// Sign injection preserves negative zero. NaNs take a canonical arithmetic path.
func (f *integerEmitter) floatRound(wide bool, mode enc.RoundingMode) {
	nan := f.floatNaNBranch(wide)
	bits := uint64(0x5f000000)
	if wide {
		bits = 0x43e0000000000000
	}
	f.Constant(7, int64(bits), wide)
	f.floatInput(enc.F1, enc.X7, wide)
	if wide {
		f.a.FsgnjxD(enc.F2, enc.F0, enc.F0)
		f.a.FltD(enc.X6, enc.F2, enc.F1)
	} else {
		f.a.FsgnjxS(enc.F2, enc.F0, enc.F0)
		f.a.FltS(enc.X6, enc.F2, enc.F1)
	}
	large := f.BranchZero(6)
	if wide {
		f.a.FcvtLD(enc.X5, enc.F0, mode)
		f.a.FcvtDL(enc.F2, enc.X5, enc.RNE)
		f.a.FsgnjD(enc.F0, enc.F2, enc.F0)
	} else {
		f.a.FcvtLS(enc.X5, enc.F0, mode)
		f.a.FcvtSL(enc.F2, enc.X5, enc.RNE)
		f.a.FsgnjS(enc.F0, enc.F2, enc.F0)
	}
	f.floatOutput(enc.F0, wide)
	end := f.Jump()
	f.floatPatch(nan)
	f.floatNaNBits(wide)
	f.floatPatch(large)
	f.floatPatch(end)
}

func (f *integerEmitter) floatTrunc(wide, resultWide, unsigned, saturating bool) {
	f.floatInput(enc.F0, enc.X5, wide)
	nan := f.floatNaNBranch(wide)
	if !saturating {
		f.trap(trapFloatConversion, nan)
		f.a.WriteFflags(enc.Zero)
	}
	if wide {
		if resultWide {
			if unsigned {
				f.a.FcvtLuD(enc.X5, enc.F0, enc.RTZ)
			} else {
				f.a.FcvtLD(enc.X5, enc.F0, enc.RTZ)
			}
		} else {
			if unsigned {
				f.a.FcvtWuD(enc.X5, enc.F0, enc.RTZ)
			} else {
				f.a.FcvtWD(enc.X5, enc.F0, enc.RTZ)
			}
		}
	} else {
		if resultWide {
			if unsigned {
				f.a.FcvtLuS(enc.X5, enc.F0, enc.RTZ)
			} else {
				f.a.FcvtLS(enc.X5, enc.F0, enc.RTZ)
			}
		} else {
			if unsigned {
				f.a.FcvtWuS(enc.X5, enc.F0, enc.RTZ)
			} else {
				f.a.FcvtWS(enc.X5, enc.F0, enc.RTZ)
			}
		}
	}
	if !saturating {
		f.a.ReadFflags(enc.X6)
		f.Constant(7, 0x10, false)
		f.a.And(enc.X6, enc.X6, enc.X7)
		f.trap(trapFloatConversion, f.a.BranchLong(enc.NE, enc.X6, enc.Zero))
	} else {
		end := f.Jump()
		f.floatPatch(nan)
		f.Constant(5, 0, resultWide)
		f.floatPatch(end)
	}
	if !resultWide {
		f.a.ZeroExtend32(enc.X5, enc.X5)
	}
}

func (f *integerEmitter) floatConvert(op byte) {
	switch op {
	case 0xb2:
		f.a.FcvtSW(enc.F0, enc.X5, enc.RNE)
	case 0xb3:
		f.a.FcvtSWu(enc.F0, enc.X5, enc.RNE)
	case 0xb4:
		f.a.FcvtSL(enc.F0, enc.X5, enc.RNE)
	case 0xb5:
		f.a.FcvtSLu(enc.F0, enc.X5, enc.RNE)
	case 0xb6:
		f.floatInput(enc.F0, enc.X5, true)
		f.a.FcvtSD(enc.F0, enc.F0, enc.RNE)
	case 0xb7:
		f.a.FcvtDW(enc.F0, enc.X5, enc.RNE)
	case 0xb8:
		f.a.FcvtDWu(enc.F0, enc.X5, enc.RNE)
	case 0xb9:
		f.a.FcvtDL(enc.F0, enc.X5, enc.RNE)
	case 0xba:
		f.a.FcvtDLu(enc.F0, enc.X5, enc.RNE)
	case 0xbb:
		f.floatInput(enc.F0, enc.X5, false)
		f.a.FcvtDS(enc.F0, enc.F0, enc.RNE)
	}
	f.floatOutput(enc.F0, op >= 0xb7)
}
