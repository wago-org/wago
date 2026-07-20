package embedded32

import "testing"

func runI64(op I64Op, a, b uint64) I64Frame {
	f := I64Frame{Op: uint32(op), ALo: uint32(a), AHi: uint32(a >> 32), BLo: uint32(b), BHi: uint32(b >> 32)}
	RunI64(&f)
	return f
}

func i64Out(f I64Frame) uint64 { return uint64(f.OutLo) | uint64(f.OutHi)<<32 }

func TestRunI64IntegerSemantics(t *testing.T) {
	values := []struct {
		op      I64Op
		a, b    uint64
		want    uint64
		wantI32 uint32
		trap    Trap
	}{
		{I64Shl, 1, 63, 1 << 63, 0, 0},
		{I64ShrS, 1 << 63, 63, ^uint64(0), 0, 0},
		{I64Rotl, 0x8000000000000001, 1, 3, 0, 0},
		{I64Clz, 1, 0, 63, 0, 0},
		{I64Ctz, 1 << 40, 0, 40, 0, 0},
		{I64Popcnt, 0xf00000000000000f, 0, 8, 0, 0},
		{I64LtS, ^uint64(0), 0, 0, 1, 0},
		{I64GtU, ^uint64(0), 0, 0, 1, 0},
		{I64DivS, uint64(21), uint64(4), 5, 0, 0},
		{I64RemS, 1 << 63, ^uint64(0), 0, 0, 0},
		{I64DivU, 1, 0, 0, 0, TrapIntegerDivideByZero},
		{I64DivS, 1 << 63, ^uint64(0), 0, 0, TrapIntegerOverflow},
		{I64ExtendI32S, 0x80000000, 0, 0xffffffff80000000, 0, 0},
		{I64Extend8S, 0x80, 0, ^uint64(0x7f), 0, 0},
	}
	for _, tc := range values {
		f := runI64(tc.op, tc.a, tc.b)
		if got := i64Out(f); got != tc.want || f.I32Out != tc.wantI32 || f.Trap != tc.trap {
			t.Fatalf("op %d: out=%#x i32=%d trap=%d want=%#x/%d/%d", tc.op, got, f.I32Out, f.Trap, tc.want, tc.wantI32, tc.trap)
		}
	}
}
