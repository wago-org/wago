package wago

import "fmt"

// Value is a typed WebAssembly value: a ValType plus its raw bits (i32/f32 live
// in the low 32 bits). It is the currency of the high-level, context-aware
// Instance.Call API, where the low-level Invoke uses untyped uint64 slots. v128
// is not representable as a single Value; the two-slot Invoke form handles it.
type Value struct {
	typ  ValType
	bits uint64
}

// ValueI32/I64/F32/F64 construct a typed Value.
func ValueI32(v int32) Value   { return Value{ValI32, I32(v)} }
func ValueI64(v int64) Value   { return Value{ValI64, I64(v)} }
func ValueF32(v float32) Value { return Value{ValF32, F32(v)} }
func ValueF64(v float64) Value { return Value{ValF64, F64(v)} }

// ValueOf builds a Value from raw bits interpreted per t (i32/f32 in the low 32
// bits). It is the escape hatch for callers already holding slot bits.
func ValueOf(t ValType, bits uint64) Value { return Value{t, bits} }

// Type returns the value's WebAssembly type.
func (v Value) Type() ValType { return v.typ }

// Bits returns the raw slot bits (i32/f32 in the low 32 bits).
func (v Value) Bits() uint64 { return v.bits }

// I32/I64/F32/F64 decode the value; the result is only meaningful when Type
// matches.
func (v Value) I32() int32   { return AsI32(v.bits) }
func (v Value) I64() int64   { return AsI64(v.bits) }
func (v Value) F32() float32 { return AsF32(v.bits) }
func (v Value) F64() float64 { return AsF64(v.bits) }

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
	default:
		return fmt.Sprintf("i32(%d)", v.I32())
	}
}
