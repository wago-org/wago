package embedded32

import "math/bits"

type I64Op uint8

const (
	I64Shl I64Op = iota
	I64ShrS
	I64ShrU
	I64Rotl
	I64Rotr
	I64Clz
	I64Ctz
	I64Popcnt
	I64Eqz
	I64Eq
	I64Ne
	I64LtS
	I64LtU
	I64GtS
	I64GtU
	I64LeS
	I64LeU
	I64GeS
	I64GeU
	I64DivS
	I64DivU
	I64RemS
	I64RemU
	I64ExtendI32S
	I64ExtendI32U
	I64Extend8S
	I64Extend16S
	I64Extend32S
)

func I64HelperValid(op I64Op) bool { return op <= I64Extend32S }

type I64Frame struct {
	Op           uint32
	ALo, AHi     uint32
	BLo, BHi     uint32
	OutLo, OutHi uint32
	I32Out       uint32
	Trap         Trap
}

//export wago_embedded32_i64
func RunI64(f *I64Frame) {
	if !I64HelperValid(I64Op(f.Op)) {
		panic("embedded32: invalid i64 helper opcode")
	}
	f.OutLo, f.OutHi, f.I32Out, f.Trap = 0, 0, 0, TrapNone
	a := uint64(f.ALo) | uint64(f.AHi)<<32
	b := uint64(f.BLo) | uint64(f.BHi)<<32
	set := func(v uint64) { f.OutLo, f.OutHi = uint32(v), uint32(v>>32) }
	boolOut := func(v bool) {
		if v {
			f.I32Out = 1
		}
	}
	switch I64Op(f.Op) {
	case I64Shl:
		set(a << (b & 63))
	case I64ShrS:
		set(uint64(int64(a) >> (b & 63)))
	case I64ShrU:
		set(a >> (b & 63))
	case I64Rotl:
		set(bits.RotateLeft64(a, int(b&63)))
	case I64Rotr:
		set(bits.RotateLeft64(a, -int(b&63)))
	case I64Clz:
		set(uint64(bits.LeadingZeros64(a)))
	case I64Ctz:
		set(uint64(bits.TrailingZeros64(a)))
	case I64Popcnt:
		set(uint64(bits.OnesCount64(a)))
	case I64Eqz:
		boolOut(a == 0)
	case I64Eq:
		boolOut(a == b)
	case I64Ne:
		boolOut(a != b)
	case I64LtS:
		boolOut(int64(a) < int64(b))
	case I64LtU:
		boolOut(a < b)
	case I64GtS:
		boolOut(int64(a) > int64(b))
	case I64GtU:
		boolOut(a > b)
	case I64LeS:
		boolOut(int64(a) <= int64(b))
	case I64LeU:
		boolOut(a <= b)
	case I64GeS:
		boolOut(int64(a) >= int64(b))
	case I64GeU:
		boolOut(a >= b)
	case I64DivS:
		if b == 0 {
			f.Trap = TrapIntegerDivideByZero
		} else if a == 1<<63 && b == ^uint64(0) {
			f.Trap = TrapIntegerOverflow
		} else {
			set(uint64(int64(a) / int64(b)))
		}
	case I64DivU:
		if b == 0 {
			f.Trap = TrapIntegerDivideByZero
		} else {
			set(a / b)
		}
	case I64RemS:
		if b == 0 {
			f.Trap = TrapIntegerDivideByZero
		} else if a == 1<<63 && b == ^uint64(0) {
			set(0)
		} else {
			set(uint64(int64(a) % int64(b)))
		}
	case I64RemU:
		if b == 0 {
			f.Trap = TrapIntegerDivideByZero
		} else {
			set(a % b)
		}
	case I64ExtendI32S:
		set(uint64(int64(int32(f.ALo))))
	case I64ExtendI32U:
		set(uint64(f.ALo))
	case I64Extend8S:
		set(uint64(int64(int8(f.ALo))))
	case I64Extend16S:
		set(uint64(int64(int16(f.ALo))))
	case I64Extend32S:
		set(uint64(int64(int32(f.ALo))))
	}
}
