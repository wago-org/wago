package embedded32

import "math"

type F32Op uint8

const (
	F32Abs F32Op = iota
	F32Neg
	F32Ceil
	F32Floor
	F32Trunc
	F32Nearest
	F32Sqrt
	F32Add
	F32Sub
	F32Mul
	F32Div
	F32Min
	F32Max
	F32Copysign
	F32Eq
	F32Ne
	F32Lt
	F32Gt
	F32Le
	F32Ge
	F32DemoteF64
	F32ConvertI32S
	F32ConvertI32U
	F32ConvertI64S
	F32ConvertI64U
	I32TruncF32S
	I32TruncF32U
	I64TruncF32S
	I64TruncF32U
	I32TruncSatF32S
	I32TruncSatF32U
	I64TruncSatF32S
	I64TruncSatF32U
)

type F32Frame struct {
	Op           uint32
	ALo, AHi     uint32
	BLo, BHi     uint32
	OutLo, OutHi uint32
	Trap         Trap
}

func F32HelperValid(op F32Op) bool { return op <= I64TruncSatF32U }

func preserveZeroSign32(bits uint32, out float32) float32 {
	if out == 0 {
		return math.Float32frombits(math.Float32bits(out)&0x7fffffff | bits&0x80000000)
	}
	return out
}

func truncF32Magnitude(bits uint32) uint64 {
	exponent := int(bits>>23&0xff) - 127
	if exponent < 0 {
		return 0
	}
	significand := uint64(bits&0x007fffff | 0x00800000)
	if exponent >= 23 {
		return significand << uint(exponent-23)
	}
	return significand >> uint(23-exponent)
}

func roundF32Bits(bits uint32, mode floatRoundMode) uint32 {
	const (
		signMask = uint32(1) << 31
		absMask  = signMask - 1
		halfBits = uint32(0x3f000000)
		oneBits  = uint32(0x3f800000)
	)
	abs := bits & absMask
	exponentBits := abs >> 23
	if exponentBits == 0xff {
		if abs&0x007fffff != 0 {
			return quiet32(bits)
		}
		return bits
	}
	exponent := int(exponentBits) - 127
	if exponent >= 23 || abs == 0 {
		return bits
	}
	negative := bits&signMask != 0
	if exponent < 0 {
		signedZero := bits & signMask
		switch mode {
		case roundCeil:
			if !negative {
				return oneBits
			}
		case roundFloor:
			if negative {
				return signMask | oneBits
			}
		case roundNearest:
			if abs > halfBits {
				return bits&signMask | oneBits
			}
		}
		return signedZero
	}
	shift := uint(23 - exponent)
	unit := uint32(1) << shift
	mask := unit - 1
	remainder := bits & mask
	if remainder == 0 {
		return bits
	}
	result := bits &^ mask
	switch mode {
	case roundCeil:
		if !negative {
			result += unit
		}
	case roundFloor:
		if negative {
			result += unit
		}
	case roundNearest:
		half := unit >> 1
		if remainder > half || remainder == half && result&unit != 0 {
			result += unit
		}
	}
	return result
}

func demoteF64Bits(bits uint64) uint32 {
	if bits&0x7ff0000000000000 == 0x7ff0000000000000 && bits&0x000fffffffffffff != 0 {
		return uint32(bits>>32)&0x80000000 | 0x7fc00000 | uint32(bits>>29)&0x003fffff
	}
	return math.Float32bits(float32(math.Float64frombits(bits)))
}

//export wago_embedded32_f32
func RunF32(f *F32Frame) {
	op := F32Op(f.Op)
	if !F32HelperValid(op) {
		panic("embedded32: invalid f32 helper opcode")
	}
	f.OutLo, f.OutHi, f.Trap = 0, 0, TrapNone
	aBits, bBits := f.ALo, f.BLo
	a, b := math.Float32frombits(aBits), math.Float32frombits(bBits)
	set := func(x float32) { f.OutLo = math.Float32bits(x) }
	boolOut := func(ok bool) {
		if ok {
			f.OutLo = 1
		}
	}
	switch op {
	case F32Abs:
		f.OutLo = aBits & 0x7fffffff
	case F32Neg:
		f.OutLo = aBits ^ 0x80000000
	case F32Ceil:
		f.OutLo = roundF32Bits(aBits, roundCeil)
	case F32Floor:
		f.OutLo = roundF32Bits(aBits, roundFloor)
	case F32Trunc:
		f.OutLo = roundF32Bits(aBits, roundTrunc)
	case F32Nearest:
		f.OutLo = roundF32Bits(aBits, roundNearest)
	case F32Sqrt:
		set(float32(math.Sqrt(float64(a))))
	case F32Add:
		set(a + b)
	case F32Sub:
		set(a - b)
	case F32Mul:
		set(a * b)
	case F32Div:
		set(a / b)
	case F32Min:
		f.OutLo = minmax32(aBits, bBits, false)
	case F32Max:
		f.OutLo = minmax32(aBits, bBits, true)
	case F32Copysign:
		f.OutLo = aBits&0x7fffffff | bBits&0x80000000
	case F32Eq:
		boolOut(a == b)
	case F32Ne:
		boolOut(a != b)
	case F32Lt:
		boolOut(a < b)
	case F32Gt:
		boolOut(a > b)
	case F32Le:
		boolOut(a <= b)
	case F32Ge:
		boolOut(a >= b)
	case F32DemoteF64:
		f.OutLo = demoteF64Bits(join64(f.ALo, f.AHi))
	case F32ConvertI32S:
		set(float32(int32(f.ALo)))
	case F32ConvertI32U:
		set(float32(f.ALo))
	case F32ConvertI64S:
		set(float32(int64(join64(f.ALo, f.AHi))))
	case F32ConvertI64U:
		set(float32(join64(f.ALo, f.AHi)))
	case I32TruncF32S:
		f.truncI32(a, true, false)
	case I32TruncF32U:
		f.truncI32(a, false, false)
	case I64TruncF32S:
		f.truncI64(a, true, false)
	case I64TruncF32U:
		f.truncI64(a, false, false)
	case I32TruncSatF32S:
		f.truncI32(a, true, true)
	case I32TruncSatF32U:
		f.truncI32(a, false, true)
	case I64TruncSatF32S:
		f.truncI64(a, true, true)
	case I64TruncSatF32U:
		f.truncI64(a, false, true)
	}
}

func (f *F32Frame) conversionFail(saturating, overflow, negative bool, bits int, signed bool) {
	if !saturating {
		if overflow {
			f.Trap = TrapIntegerOverflow
		} else {
			f.Trap = TrapInvalidConversion
		}
		return
	}
	if !overflow {
		return
	}
	if signed {
		if bits == 32 {
			if negative {
				f.OutLo = 0x80000000
			} else {
				f.OutLo = 0x7fffffff
			}
		} else if negative {
			f.OutHi = 0x80000000
		} else {
			f.OutLo, f.OutHi = 0xffffffff, 0x7fffffff
		}
	} else if negative {
		f.OutLo, f.OutHi = 0, 0
	} else if bits == 32 {
		f.OutLo = 0xffffffff
	} else {
		f.OutLo, f.OutHi = 0xffffffff, 0xffffffff
	}
}

func (f *F32Frame) truncI32(x float32, signed, saturating bool) {
	if math.IsNaN(float64(x)) {
		f.conversionFail(saturating, false, false, 32, signed)
		return
	}
	if signed {
		if x < -0x1p31 || x >= 0x1p31 {
			f.conversionFail(saturating, true, math.Signbit(float64(x)), 32, true)
			return
		}
		f.OutLo = uint32(truncF32Magnitude(f.ALo))
		if f.ALo>>31 != 0 {
			f.OutLo = 0 - f.OutLo
		}
		return
	}
	if x <= -1 || x >= 0x1p32 {
		f.conversionFail(saturating, true, math.Signbit(float64(x)), 32, false)
		return
	}
	f.OutLo = uint32(truncF32Magnitude(f.ALo))
}

func (f *F32Frame) truncI64(x float32, signed, saturating bool) {
	if math.IsNaN(float64(x)) {
		f.conversionFail(saturating, false, false, 64, signed)
		return
	}
	if signed {
		if x < -0x1p63 || x >= 0x1p63 {
			f.conversionFail(saturating, true, math.Signbit(float64(x)), 64, true)
			return
		}
		value := truncF32Magnitude(f.ALo)
		if f.ALo>>31 != 0 {
			value = 0 - value
		}
		f.OutLo, f.OutHi = split64(value)
		return
	}
	if x <= -1 || x >= 0x1p64 {
		f.conversionFail(saturating, true, math.Signbit(float64(x)), 64, false)
		return
	}
	f.OutLo, f.OutHi = split64(truncF32Magnitude(f.ALo))
}
