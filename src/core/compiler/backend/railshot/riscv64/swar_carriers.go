//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type swarClass uint8

const (
	swarI32 swarClass = iota
	swarI64
	swarF32
	swarF64
	swarV128
	swarInvalid
	swarUnknown // Stack-polymorphic operand only; never a declared carrier.
)

func swarClassOf(typ wasm.ValType) swarClass {
	switch {
	case wasm.EqualValType(typ, wasm.I32):
		return swarI32
	case wasm.EqualValType(typ, wasm.I64):
		return swarI64
	case wasm.EqualValType(typ, wasm.F32):
		return swarF32
	case wasm.EqualValType(typ, wasm.F64):
		return swarF64
	case wasm.EqualValType(typ, wasm.V128):
		return swarV128
	default:
		return swarInvalid
	}
}

func swarByteClass(typ byte) swarClass {
	switch typ {
	case 0x7f:
		return swarI32
	case 0x7e:
		return swarI64
	case 0x7d:
		return swarF32
	case 0x7c:
		return swarF64
	case 0x7b:
		return swarV128
	default:
		return swarInvalid
	}
}

func (class swarClass) narrow() bool { return class == swarI32 || class == swarF32 }
