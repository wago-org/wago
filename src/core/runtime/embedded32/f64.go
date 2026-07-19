// Package embedded32 contains architecture-neutral helper semantics for the
// 32-bit bare-metal backends. Helpers use only fixed-width integer slots at the
// ABI boundary so generated Thumb-2 and RV32 code never depends on a native
// double-precision calling convention.
package embedded32

import "math"

// Trap is written by helpers which implement trapping WebAssembly conversions.
type Trap uint32

const (
	TrapNone Trap = iota
	TrapInvalidConversion
	TrapIntegerOverflow
	TrapMemoryOutOfBounds
	TrapIntegerDivideByZero
	TrapCanceled
	TrapUnreachable
	TrapStackOverflow
	TrapTableOutOfBounds
	TrapIndirectCallNull
	TrapIndirectCallTypeMismatch
)

// F64Op identifies one scalar floating-point helper operation. Bitwise abs,
// neg, copysign, and reinterpret operations are normally inlined by the machine
// backend, but remain available here as a reference and fallback.
type F64Op uint8

const (
	F64Abs F64Op = iota
	F64Neg
	F64Ceil
	F64Floor
	F64Trunc
	F64Nearest
	F64Sqrt
	F64Add
	F64Sub
	F64Mul
	F64Div
	F64Min
	F64Max
	F64Copysign
	F64Eq
	F64Ne
	F64Lt
	F64Gt
	F64Le
	F64Ge
	F64PromoteF32
	F64ConvertI32S
	F64ConvertI32U
	F64ConvertI64S
	F64ConvertI64U
	I32TruncF64S
	I32TruncF64U
	I64TruncF64S
	I64TruncF64U
	I32TruncSatF64S
	I32TruncSatF64U
	I64TruncSatF64S
	I64TruncSatF64U
)

// F64Frame is the common 32-bit helper-call frame. A and B are little-endian
// 64-bit inputs. Out is either a 64-bit result or an i32 predicate/conversion in
// OutLo. Trap is cleared on entry and set only by trapping conversions.
type F64Frame struct {
	Op           F64Op
	_            [3]byte
	ALo, AHi     uint32
	BLo, BHi     uint32
	OutLo, OutHi uint32
	Trap         Trap
}

func join64(lo, hi uint32) uint64       { return uint64(lo) | uint64(hi)<<32 }
func split64(v uint64) (uint32, uint32) { return uint32(v), uint32(v >> 32) }
func f64bits(lo, hi uint32) uint64      { return join64(lo, hi) }
func f64from(lo, hi uint32) float64     { return math.Float64frombits(join64(lo, hi)) }
func set64(f *F64Frame, v uint64)       { f.OutLo, f.OutHi = split64(v) }
func setf64(f *F64Frame, v float64)     { set64(f, math.Float64bits(v)) }

const canonicalNaN64 = uint64(0x7ff8000000000000)

func isNaN64(bits uint64) bool {
	return bits&0x7ff0000000000000 == 0x7ff0000000000000 && bits&0x000fffffffffffff != 0
}
func quietNaN64(bits uint64) uint64 {
	if !isNaN64(bits) {
		return canonicalNaN64
	}
	return bits | 0x0008000000000000
}
func minmax64(aBits, bBits uint64, max bool) uint64 {
	if isNaN64(aBits) {
		return quietNaN64(aBits)
	}
	if isNaN64(bBits) {
		return quietNaN64(bBits)
	}
	a, b := math.Float64frombits(aBits), math.Float64frombits(bBits)
	if a == b {
		if a == 0 {
			if max {
				return aBits & bBits
			}
			return aBits | bBits
		}
		return aBits
	}
	if max {
		if a > b {
			return aBits
		}
		return bBits
	}
	if a < b {
		return aBits
	}
	return bBits
}
func preserveZeroSign(inBits uint64, out float64) float64 {
	if out == 0 {
		return math.Copysign(0, math.Float64frombits(inBits))
	}
	return out
}

// F64HelperValid reports whether op is part of the scalar helper ABI.
func F64HelperValid(op F64Op) bool { return op <= I64TruncSatF64U }

// RunF64 executes one complete scalar f64 helper operation. It is deliberately
// allocation-free and has a stable pointer-only ABI suitable for TinyGo-exported
// firmware helpers.
//
//export wago_embedded32_f64
func RunF64(f *F64Frame) {
	if !F64HelperValid(f.Op) {
		panic("embedded32: invalid f64 helper opcode")
	}
	f.Trap = TrapNone
	aBits, bBits := f64bits(f.ALo, f.AHi), f64bits(f.BLo, f.BHi)
	a, b := math.Float64frombits(aBits), math.Float64frombits(bBits)
	switch f.Op {
	case F64Abs:
		set64(f, aBits&^uint64(1<<63))
	case F64Neg:
		set64(f, aBits^(uint64(1)<<63))
	case F64Ceil:
		setf64(f, preserveZeroSign(aBits, math.Ceil(a)))
	case F64Floor:
		setf64(f, preserveZeroSign(aBits, math.Floor(a)))
	case F64Trunc:
		setf64(f, preserveZeroSign(aBits, math.Trunc(a)))
	case F64Nearest:
		setf64(f, preserveZeroSign(aBits, math.RoundToEven(a)))
	case F64Sqrt:
		setf64(f, math.Sqrt(a))
	case F64Add:
		setf64(f, a+b)
	case F64Sub:
		setf64(f, a-b)
	case F64Mul:
		setf64(f, a*b)
	case F64Div:
		setf64(f, a/b)
	case F64Min:
		set64(f, minmax64(aBits, bBits, false))
	case F64Max:
		set64(f, minmax64(aBits, bBits, true))
	case F64Copysign:
		set64(f, aBits&^uint64(1<<63)|bBits&uint64(1<<63))
	case F64Eq:
		if a == b {
			f.OutLo = 1
		} else {
			f.OutLo = 0
		}
		f.OutHi = 0
	case F64Ne:
		if a != b {
			f.OutLo = 1
		} else {
			f.OutLo = 0
		}
		f.OutHi = 0
	case F64Lt:
		if a < b {
			f.OutLo = 1
		} else {
			f.OutLo = 0
		}
		f.OutHi = 0
	case F64Gt:
		if a > b {
			f.OutLo = 1
		} else {
			f.OutLo = 0
		}
		f.OutHi = 0
	case F64Le:
		if a <= b {
			f.OutLo = 1
		} else {
			f.OutLo = 0
		}
		f.OutHi = 0
	case F64Ge:
		if a >= b {
			f.OutLo = 1
		} else {
			f.OutLo = 0
		}
		f.OutHi = 0
	case F64PromoteF32:
		setf64(f, float64(math.Float32frombits(f.ALo)))
	case F64ConvertI32S:
		setf64(f, float64(int32(f.ALo)))
	case F64ConvertI32U:
		setf64(f, float64(f.ALo))
	case F64ConvertI64S:
		setf64(f, float64(int64(aBits)))
	case F64ConvertI64U:
		setf64(f, float64(aBits))
	case I32TruncF64S:
		f.truncI32(a, true, false)
	case I32TruncF64U:
		f.truncI32(a, false, false)
	case I64TruncF64S:
		f.truncI64(a, true, false)
	case I64TruncF64U:
		f.truncI64(a, false, false)
	case I32TruncSatF64S:
		f.truncI32(a, true, true)
	case I32TruncSatF64U:
		f.truncI32(a, false, true)
	case I64TruncSatF64S:
		f.truncI64(a, true, true)
	case I64TruncSatF64U:
		f.truncI64(a, false, true)
	default:
		panic("embedded32: invalid f64 helper opcode")
	}
}

func (f *F64Frame) conversionFail(saturating bool, overflow bool, negative bool, bits int, signed bool) {
	if !saturating {
		if overflow {
			f.Trap = TrapIntegerOverflow
		} else {
			f.Trap = TrapInvalidConversion
		}
		f.OutLo, f.OutHi = 0, 0
		return
	}
	if !overflow {
		f.OutLo, f.OutHi = 0, 0
		return
	}
	if signed {
		if bits == 32 {
			if negative {
				f.OutLo = 0x80000000
			} else {
				f.OutLo = 0x7fffffff
			}
			f.OutHi = 0
		} else if negative {
			f.OutLo, f.OutHi = 0, 0x80000000
		} else {
			f.OutLo, f.OutHi = 0xffffffff, 0x7fffffff
		}
		return
	}
	if negative {
		f.OutLo, f.OutHi = 0, 0
	} else if bits == 32 {
		f.OutLo, f.OutHi = 0xffffffff, 0
	} else {
		f.OutLo, f.OutHi = 0xffffffff, 0xffffffff
	}
}

func (f *F64Frame) truncI32(x float64, signed, saturating bool) {
	if math.IsNaN(x) {
		f.conversionFail(saturating, false, false, 32, signed)
		return
	}
	if signed {
		// Truncation toward zero admits negative fractions down to, but not
		// including, -2^31-1. Testing x < -2^31 rejects valid values such as
		// -2147483648.9 before truncation.
		if x <= -2147483649 || x >= 0x1p31 {
			f.conversionFail(saturating, true, math.Signbit(x), 32, true)
			return
		}
		f.OutLo, f.OutHi = uint32(int32(math.Trunc(x))), 0
		return
	}
	if x <= -1 || x >= 0x1p32 {
		f.conversionFail(saturating, true, math.Signbit(x), 32, false)
		return
	}
	f.OutLo, f.OutHi = uint32(math.Trunc(x)), 0
}
func (f *F64Frame) truncI64(x float64, signed, saturating bool) {
	if math.IsNaN(x) {
		f.conversionFail(saturating, false, false, 64, signed)
		return
	}
	if signed {
		if x < -0x1p63 || x >= 0x1p63 {
			f.conversionFail(saturating, true, math.Signbit(x), 64, true)
			return
		}
		f.OutLo, f.OutHi = split64(uint64(int64(math.Trunc(x))))
		return
	}
	if x <= -1 || x >= 0x1p64 {
		f.conversionFail(saturating, true, math.Signbit(x), 64, false)
		return
	}
	f.OutLo, f.OutHi = split64(uint64(math.Trunc(x)))
}
