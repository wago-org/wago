package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ValType is a WebAssembly numeric, vector, or reference value type. It is
// wago's self-contained representation so the public API never requires
// importing internal packages.
type ValType uint8

// V128 is the public representation for a WebAssembly v128 value: its exact 16
// bytes in little-endian lane order, matching memory and the public two-slot
// Invoke ABI.
type V128 [16]byte

// FuncRef and ExternRef are opaque WebAssembly reference tokens. Their zero
// values are null. The token is not a Go pointer or native code/data address and
// callers must not interpret it as one.
type FuncRef struct{ token uint64 }
type ExternRef struct{ token uint64 }

const (
	ValI32 ValType = iota
	ValI64
	ValF32
	ValF64
	ValV128
	ValFuncRef
	ValExternRef
)

// NullFuncRef and NullExternRef return the null reference of each public type.
func NullFuncRef() FuncRef     { return FuncRef{} }
func NullExternRef() ExternRef { return ExternRef{} }

// IsNull reports whether a reference is null.
func (r FuncRef) IsNull() bool   { return r.token == 0 }
func (r ExternRef) IsNull() bool { return r.token == 0 }

func (t ValType) String() string {
	switch t {
	case ValI32:
		return "i32"
	case ValI64:
		return "i64"
	case ValF32:
		return "f32"
	case ValF64:
		return "f64"
	case ValV128:
		return "v128"
	case ValFuncRef:
		return "funcref"
	case ValExternRef:
		return "externref"
	default:
		return "unknown"
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
	case 0x70:
		return ValFuncRef
	case 0x6f:
		return ValExternRef
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

// code is the wasm value-type byte used by the current compiled-module codec.
// Codec version 21 defines the reference type codes as structural metadata;
// live reference values remain outside the serialized format.
func (t ValType) code() (byte, bool) {
	switch t {
	case ValI32:
		return 0x7f, true
	case ValI64:
		return 0x7e, true
	case ValF32:
		return 0x7d, true
	case ValF64:
		return 0x7c, true
	case ValV128:
		return 0x7b, true
	case ValFuncRef:
		return 0x70, true
	case ValExternRef:
		return 0x6f, true
	default:
		return 0, false
	}
}

func isReferenceValType(t ValType) bool { return t == ValFuncRef || t == ValExternRef }

func isWideValType(t ValType) bool {
	return t == ValI64 || t == ValF64 || isReferenceValType(t)
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
	case 0x70:
		return ValFuncRef, true
	case 0x6f:
		return ValExternRef, true
	default:
		return 0, false
	}
}
