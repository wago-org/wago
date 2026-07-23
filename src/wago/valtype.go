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

// FuncRef, ExternRef, and GCRef are opaque WebAssembly reference tokens. Their
// zero values are null. Tokens are not Go pointers, native code/data addresses,
// or compact collector handles and callers must not interpret them as such.
// Non-null GCRef values are currently issued only for exact staged struct/array
// result products and must be released through Instance.ReleaseGCRef.
//
// I31Ref is deliberately separate: it is an immediate value, not an opaque
// object token. Its private bits use the WasmGC low-bit tag, but only the typed
// Signed/Unsigned accessors expose its 31-bit payload.
type FuncRef struct{ token uint64 }
type ExternRef struct{ token uint64 }
type GCRef struct{ token uint64 }
type I31Ref struct{ bits uint32 }

const (
	ValI32 ValType = iota
	ValI64
	ValF32
	ValF64
	ValV128
	ValFuncRef
	ValExternRef
	ValExnRef // internal/product ABI category for rooted exception references
	ValAnyRef // product ABI category for any/none and exact staged GC result tokens
	ValI31Ref // exact i31 immediate category; never an opaque GCRef token
)

// NullFuncRef, NullExternRef, NullGCRef, and NullI31Ref return null reference values.
func NullFuncRef() FuncRef     { return FuncRef{} }
func NullExternRef() ExternRef { return ExternRef{} }
func NullGCRef() GCRef         { return GCRef{} }
func NullI31Ref() I31Ref       { return I31Ref{} }

// NewI31Ref packs the low 31 bits of v into a non-null i31 immediate.
func NewI31Ref(v int32) I31Ref { return I31Ref{bits: uint32(v)<<1 | 1} }

// IsNull reports whether a reference is null.
func (r FuncRef) IsNull() bool   { return r.token == 0 }
func (r ExternRef) IsNull() bool { return r.token == 0 }
func (r GCRef) IsNull() bool     { return r.token == 0 }
func (r I31Ref) IsNull() bool    { return r.bits == 0 }

// Signed and Unsigned decode the i31 payload. The result is meaningful only for
// a non-null I31Ref obtained from NewI31Ref or a typed wago result.
func (r I31Ref) Signed() int32    { return int32(r.bits) >> 1 }
func (r I31Ref) Unsigned() uint32 { return r.bits >> 1 }

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
	case ValExnRef:
		return "exnref"
	case ValAnyRef:
		return "anyref"
	case ValI31Ref:
		return "i31ref"
	default:
		return "unknown"
	}
}

func valTypeFromWasm(t wasm.ValType) ValType {
	if t.Kind == wasm.ValRef && t.Ref.Heap.Kind == wasm.HeapAbs {
		switch t.Ref.Heap.Abs {
		case wasm.HeapAny, wasm.HeapNone:
			return ValAnyRef
		case wasm.HeapI31:
			return ValI31Ref
		case wasm.HeapExn, wasm.HeapNoExn:
			return ValExnRef
		}
	}
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
	case 0x69:
		return ValExnRef
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
// Codec version 22 preserves this legacy ABI category alongside exact structural
// descriptors; live reference values remain outside the serialized format.
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
	case ValExnRef:
		return 0x69, true
	case ValAnyRef:
		return 0x6e, true
	case ValI31Ref:
		return 0x6c, true
	default:
		return 0, false
	}
}

func isReferenceValType(t ValType) bool {
	return t == ValFuncRef || t == ValExternRef || t == ValExnRef || t == ValAnyRef || t == ValI31Ref
}

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
	case 0x69:
		return ValExnRef, true
	case 0x6e:
		return ValAnyRef, true
	case 0x6c:
		return ValI31Ref, true
	default:
		return 0, false
	}
}
