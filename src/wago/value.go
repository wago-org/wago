package wago

import "fmt"

// Value is a typed WebAssembly value: a ValType plus one raw ABI slot (i32/f32
// live in the low 32 bits). It is the currency of the high-level,
// context-aware Instance.Call API, where the low-level Invoke uses untyped
// uint64 slots. Reference slots contain opaque tokens, never Go or native
// pointers. v128 is not representable as a single Value; the two-slot Invoke
// form handles it.
type Value struct {
	typ  ValType
	bits uint64
}

// ValueI32/I64/F32/F64 construct a typed numeric Value.
func ValueI32(v int32) Value   { return Value{ValI32, I32(v)} }
func ValueI64(v int64) Value   { return Value{ValI64, I64(v)} }
func ValueF32(v float32) Value { return Value{ValF32, F32(v)} }
func ValueF64(v float64) Value { return Value{ValF64, F64(v)} }

// ValueFuncRef and ValueExternRef construct typed reference Values.
func ValueFuncRef(v FuncRef) Value     { return Value{ValFuncRef, v.token} }
func ValueExternRef(v ExternRef) Value { return Value{ValExternRef, v.token} }

// ValueOf builds a Value from raw ABI bits interpreted per t (i32/f32 in the
// low 32 bits). Reference bits are opaque and must not be treated as or derived
// from a Go/native pointer. Non-null funcref tokens are accepted only by the
// Runtime store (or standalone private store) that issued them.
func ValueOf(t ValType, bits uint64) Value { return Value{t, bits} }

// Type returns the value's WebAssembly type.
func (v Value) Type() ValType { return v.typ }

// Bits returns the raw slot bits (i32/f32 in the low 32 bits). Reference bits
// are opaque tokens suitable only for pass-through to wago APIs.
func (v Value) Bits() uint64 { return v.bits }

// I32/I64/F32/F64 decode the value; the result is only meaningful when Type
// matches.
func (v Value) I32() int32   { return AsI32(v.bits) }
func (v Value) I64() int64   { return AsI64(v.bits) }
func (v Value) F32() float32 { return AsF32(v.bits) }
func (v Value) F64() float64 { return AsF64(v.bits) }

// FuncRef, ExternRef, and GCRef return the value's opaque reference token. As
// with the numeric accessors, the result is only meaningful when Type matches.
func (v Value) FuncRef() FuncRef     { return FuncRef{token: v.bits} }
func (v Value) ExternRef() ExternRef { return ExternRef{token: v.bits} }
func (v Value) GCRef() GCRef         { return GCRef{token: v.bits} }

func (v Value) String() string {
	switch v.typ {
	case ValI64:
		return fmt.Sprintf("i64(%d)", v.I64())
	case ValF32:
		return fmt.Sprintf("f32(%g)", v.F32())
	case ValF64:
		return fmt.Sprintf("f64(%g)", v.F64())
	case ValV128:
		return "v128(…)"
	case ValFuncRef:
		if v.FuncRef().IsNull() {
			return "funcref(null)"
		}
		return "funcref(opaque)"
	case ValExternRef:
		if v.ExternRef().IsNull() {
			return "externref(null)"
		}
		return "externref(opaque)"
	case ValAnyRef:
		if v.GCRef().IsNull() {
			return "anyref(null)"
		}
		return "anyref(opaque)"
	case ValExnRef:
		if v.bits == 0 {
			return "exnref(null)"
		}
		return "exnref(unsupported-non-null)"
	case ValI32:
		return fmt.Sprintf("i32(%d)", v.I32())
	default:
		return "unknown(…)"
	}
}
