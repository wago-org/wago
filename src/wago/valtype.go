package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ValType is a WebAssembly numeric value type. It is wago's self-contained
// representation so the public API never requires importing internal packages.
type ValType uint8

const (
	ValI32 ValType = iota
	ValI64
	ValF32
	ValF64
	ValV128
)

func (t ValType) String() string {
	switch t {
	case ValI64:
		return "i64"
	case ValF32:
		return "f32"
	case ValF64:
		return "f64"
	case ValV128:
		return "v128"
	default:
		return "i32"
	}
}

func valTypeFromWasm(t wasm.ValType) ValType {
	switch valTypeCode(t) {
	case 0x7e:
		return ValI64
	case 0x7d:
		return ValF32
	case 0x7c:
		return ValF64
	case 0x7b:
		return ValV128
	default:
		return ValI32
	}
}

func valTypesFromWasm(ts []wasm.ValType) []ValType {
	out := make([]ValType, len(ts))
	for i, t := range ts {
		out[i] = valTypeFromWasm(t)
	}
	return out
}

// code is the wasm value-type byte (used by the codec).
func (t ValType) code() byte {
	switch t {
	case ValI64:
		return 0x7e
	case ValF32:
		return 0x7d
	case ValF64:
		return 0x7c
	case ValV128:
		return 0x7b
	default:
		return 0x7f
	}
}

func valTypeFromCode(code byte) (ValType, bool) {
	switch code {
	case 0x7f:
		return ValI32, true
	case 0x7e:
		return ValI64, true
	case 0x7d:
		return ValF32, true
	case 0x7c:
		return ValF64, true
	case 0x7b:
		return ValV128, true
	default:
		return 0, false
	}
}
