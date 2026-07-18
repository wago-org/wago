package embedded32

import (
	"math"
	"testing"
)

func frame(op F64Op, a, b uint64) F64Frame {
	alo, ahi := split64(a)
	blo, bhi := split64(b)
	return F64Frame{Op: op, ALo: alo, AHi: ahi, BLo: blo, BHi: bhi}
}
func outBits(f *F64Frame) uint64 { return join64(f.OutLo, f.OutHi) }

func TestF64ArithmeticAndBits(t *testing.T) {
	cases := []struct {
		op   F64Op
		a, b float64
		want float64
	}{
		{F64Add, 1.25, 2.5, 3.75},
		{F64Sub, 1.25, 2.5, -1.25},
		{F64Mul, -3, 2.5, -7.5},
		{F64Div, 7.5, 2.5, 3},
		{F64Sqrt, 81, 0, 9},
		{F64Ceil, -1.25, 0, -1},
		{F64Floor, -1.25, 0, -2},
		{F64Trunc, -1.75, 0, -1},
		{F64Nearest, 2.5, 0, 2},
		{F64Nearest, 3.5, 0, 4},
	}
	for _, tc := range cases {
		f := frame(tc.op, math.Float64bits(tc.a), math.Float64bits(tc.b))
		RunF64(&f)
		if got := math.Float64frombits(outBits(&f)); got != tc.want {
			t.Fatalf("op %d: got %v, want %v", tc.op, got, tc.want)
		}
	}

	f := frame(F64Abs, math.Float64bits(-0), 0)
	RunF64(&f)
	if outBits(&f) != 0 {
		t.Fatalf("abs(-0) = %#x", outBits(&f))
	}
	f = frame(F64Neg, 0, 0)
	RunF64(&f)
	if outBits(&f) != 1<<63 {
		t.Fatalf("neg(+0) = %#x", outBits(&f))
	}
	f = frame(F64Copysign, math.Float64bits(1.5), math.Float64bits(-2))
	RunF64(&f)
	if math.Float64frombits(outBits(&f)) != -1.5 {
		t.Fatalf("copysign = %v", math.Float64frombits(outBits(&f)))
	}
}

func TestF64MinMaxNaNAndZero(t *testing.T) {
	for _, tc := range []struct {
		op         F64Op
		a, b, want uint64
	}{
		{F64Min, 0, 1 << 63, 1 << 63},
		{F64Max, 0, 1 << 63, 0},
	} {
		f := frame(tc.op, tc.a, tc.b)
		RunF64(&f)
		if got := outBits(&f); got != tc.want {
			t.Fatalf("op %d got %#x want %#x", tc.op, got, tc.want)
		}
	}
	f := frame(F64Min, 0x7ff0000000000001, math.Float64bits(1))
	RunF64(&f)
	if got := outBits(&f); got != 0x7ff8000000000001 {
		t.Fatalf("quiet NaN = %#x", got)
	}
}

func TestF64Comparisons(t *testing.T) {
	for _, tc := range []struct {
		op   F64Op
		a, b float64
		want uint32
	}{
		{F64Eq, 1, 1, 1}, {F64Ne, 1, 2, 1}, {F64Lt, -1, 0, 1}, {F64Gt, 2, 1, 1}, {F64Le, 2, 2, 1}, {F64Ge, 2, 2, 1},
	} {
		f := frame(tc.op, math.Float64bits(tc.a), math.Float64bits(tc.b))
		RunF64(&f)
		if f.OutLo != tc.want || f.OutHi != 0 {
			t.Fatalf("op %d = %d:%d", tc.op, f.OutHi, f.OutLo)
		}
	}
	nan := math.Float64bits(math.NaN())
	f := frame(F64Eq, nan, nan)
	RunF64(&f)
	if f.OutLo != 0 {
		t.Fatal("NaN == NaN")
	}
	f = frame(F64Ne, nan, nan)
	RunF64(&f)
	if f.OutLo != 1 {
		t.Fatal("NaN != NaN false")
	}
}

func TestF64ConversionsAndTraps(t *testing.T) {
	for _, tc := range []struct {
		op     F64Op
		x      float64
		lo, hi uint32
		trap   Trap
	}{
		{I32TruncF64S, -12.75, 0xfffffff4, 0, TrapNone},
		{I32TruncF64U, -0.5, 0, 0, TrapNone},
		{I64TruncF64S, -1, 0xffffffff, 0xffffffff, TrapNone},
		{I32TruncF64S, math.NaN(), 0, 0, TrapInvalidConversion},
		{I32TruncF64S, 0x1p31, 0, 0, TrapIntegerOverflow},
		{I32TruncSatF64S, math.Inf(1), 0x7fffffff, 0, TrapNone},
		{I32TruncSatF64U, -2, 0, 0, TrapNone},
		{I64TruncSatF64U, math.Inf(1), 0xffffffff, 0xffffffff, TrapNone},
	} {
		f := frame(tc.op, math.Float64bits(tc.x), 0)
		RunF64(&f)
		if f.OutLo != tc.lo || f.OutHi != tc.hi || f.Trap != tc.trap {
			t.Fatalf("op %d(%v) = %08x:%08x trap %d, want %08x:%08x trap %d", tc.op, tc.x, f.OutHi, f.OutLo, f.Trap, tc.hi, tc.lo, tc.trap)
		}
	}
}

func TestF64ConversionSources(t *testing.T) {
	f := frame(F64ConvertI32S, uint64(uint32(0xffffffff)), 0)
	RunF64(&f)
	if got := math.Float64frombits(outBits(&f)); got != -1 {
		t.Fatalf("i32_s = %v", got)
	}
	f = frame(F64ConvertI64U, ^uint64(0), 0)
	RunF64(&f)
	if got := math.Float64frombits(outBits(&f)); got != float64(^uint64(0)) {
		t.Fatalf("i64_u = %v", got)
	}
	f = frame(F64PromoteF32, uint64(math.Float32bits(1.25)), 0)
	RunF64(&f)
	if got := math.Float64frombits(outBits(&f)); got != 1.25 {
		t.Fatalf("promote = %v", got)
	}
}
