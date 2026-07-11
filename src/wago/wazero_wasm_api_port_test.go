package wago

import (
	"fmt"
	"math"
	"testing"
)

// These cases are ported from wazero/api/wasm_test.go at c0f3a4ec. Wago's
// reference values are deliberately opaque, so wazero's uintptr externref
// round-trip is represented by the null/reference-type assertions below.
func TestWazeroPortValTypeString(t *testing.T) {
	tests := []struct {
		name string
		in   ValType
		want string
	}{
		{"i32", ValI32, "i32"},
		{"i64", ValI64, "i64"},
		{"f32", ValF32, "f32"},
		{"f64", ValF64, "f64"},
		{"v128", ValV128, "v128"},
		{"funcref", ValFuncRef, "funcref"},
		{"externref", ValExternRef, "externref"},
		{"unknown", ValType(100), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Fatalf("ValType(%d).String() = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWazeroPortOpaqueNullReferences(t *testing.T) {
	if !NullFuncRef().IsNull() {
		t.Fatal("NullFuncRef is not null")
	}
	if !NullExternRef().IsNull() {
		t.Fatal("NullExternRef is not null")
	}
}

func TestWazeroPortEncodeDecodeF32(t *testing.T) {
	for _, v := range []float32{
		0, 100, -100, 1, -1,
		100.01234124, -100.01234124, 200.12315,
		math.MaxFloat32,
		math.SmallestNonzeroFloat32,
		float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN()),
	} {
		t.Run(fmt.Sprintf("%f", v), func(t *testing.T) {
			encoded := F32(v)
			if encoded>>32 != 0 {
				t.Fatalf("F32(%v) set high bits: %#x", v, encoded)
			}
			got := AsF32(encoded)
			if math.IsNaN(float64(v)) {
				if !math.IsNaN(float64(got)) {
					t.Fatalf("AsF32(F32(NaN)) = %v", got)
				}
			} else if got != v {
				t.Fatalf("AsF32(F32(%v)) = %v", v, got)
			}
		})
	}
}

func TestWazeroPortEncodeDecodeF64(t *testing.T) {
	for _, v := range []float64{
		0, 100, -100, 1, -1,
		100.01234124, -100.01234124, 200.12315,
		math.MaxFloat32,
		math.SmallestNonzeroFloat32,
		math.MaxFloat64,
		math.SmallestNonzeroFloat64,
		6.8719476736e+10,
		1.37438953472e+11,
		math.Inf(1), math.Inf(-1), math.NaN(),
	} {
		t.Run(fmt.Sprintf("%f", v), func(t *testing.T) {
			got := AsF64(F64(v))
			if math.IsNaN(v) {
				if !math.IsNaN(got) {
					t.Fatalf("AsF64(F64(NaN)) = %v", got)
				}
			} else if got != v {
				t.Fatalf("AsF64(F64(%v)) = %v", v, got)
			}
		})
	}
}

func TestWazeroPortEncodeDecodeI32(t *testing.T) {
	minI32 := int32(math.MinInt32)
	for _, v := range []int32{0, 100, -100, 1, -1, math.MaxInt32, math.MinInt32} {
		t.Run(fmt.Sprintf("%d", v), func(t *testing.T) {
			encoded := I32(v)
			if encoded>>32 != 0 {
				t.Fatalf("I32(%d) set high bits: %#x", v, encoded)
			}
			if got := AsI32(encoded); got != v {
				t.Fatalf("AsI32(I32(%d)) = %d", v, got)
			}
		})
	}

	for _, tt := range []struct {
		in   uint64
		want int32
	}{
		{0, 0},
		{1 << 60, 0},
		{1 << 30, 1 << 30},
		{1<<30 | 1<<60, 1 << 30},
		{uint64(uint32(minI32)) | 1<<59, math.MinInt32},
		{uint64(uint32(math.MaxInt32)) | 1<<50, math.MaxInt32},
	} {
		if got := AsI32(tt.in); got != tt.want {
			t.Fatalf("AsI32(%#x) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestWazeroPortEncodeDecodeI64(t *testing.T) {
	for _, v := range []int64{0, 100, -100, 1, -1, math.MaxInt64, math.MinInt64} {
		t.Run(fmt.Sprintf("%d", v), func(t *testing.T) {
			if got := AsI64(I64(v)); got != v {
				t.Fatalf("AsI64(I64(%d)) = %d", v, got)
			}
		})
	}
}

func TestWazeroPortEncodeDecodeU32Slots(t *testing.T) {
	for _, v := range []uint32{0, 100, 1, 1 << 31, math.MaxInt32, math.MaxUint32} {
		t.Run(fmt.Sprintf("%d", v), func(t *testing.T) {
			encoded := I32(int32(v))
			if encoded>>32 != 0 {
				t.Fatalf("u32 slot %d set high bits: %#x", v, encoded)
			}
			if got := uint32(AsI32(encoded)); got != v {
				t.Fatalf("u32 slot round trip = %d, want %d", got, v)
			}
		})
	}

	minI32 := int32(math.MinInt32)
	for _, tt := range []struct {
		in   uint64
		want uint32
	}{
		{0, 0},
		{1 << 60, 0},
		{1 << 30, 1 << 30},
		{1<<30 | 1<<60, 1 << 30},
		{uint64(uint32(minI32)) | 1<<59, uint32(minI32)},
		{uint64(uint32(math.MaxInt32)) | 1<<50, math.MaxInt32},
	} {
		if got := uint32(AsI32(tt.in)); got != tt.want {
			t.Fatalf("decode u32 slot %#x = %d, want %d", tt.in, got, tt.want)
		}
	}
}
