package gc

import "math"

// Value is the GC package's test/runtime storage value. It is intentionally not
// tied to the public wago.Value API so descriptor lowering and codegen can evolve
// without import cycles.
type Value struct {
	Kind StorageKind
	Bits uint64
	Ref  Ref
}

func I32Value(v int32) Value   { return Value{Kind: StorageI32, Bits: uint64(uint32(v))} }
func I64Value(v int64) Value   { return Value{Kind: StorageI64, Bits: uint64(v)} }
func F32Value(v float32) Value { return Value{Kind: StorageF32, Bits: uint64(math.Float32bits(v))} }
func F64Value(v float64) Value { return Value{Kind: StorageF64, Bits: math.Float64bits(v)} }
func RefValue(r Ref) Value     { return Value{Kind: StorageRef, Ref: r} }

func (v Value) I32() int32   { return int32(uint32(v.Bits)) }
func (v Value) I64() int64   { return int64(v.Bits) }
func (v Value) F32() float32 { return math.Float32frombits(uint32(v.Bits)) }
func (v Value) F64() float64 { return math.Float64frombits(v.Bits) }
