package embedded32

import (
	"math"
	"testing"
)

func TestRunF32ArithmeticAndComparisons(t *testing.T) {
	f := F32Frame{Op: uint32(F32Add), ALo: math.Float32bits(1.5), BLo: math.Float32bits(2.25)}
	RunF32(&f)
	if got := math.Float32frombits(f.OutLo); got != 3.75 || f.Trap != TrapNone {
		t.Fatalf("add=%v trap=%d", got, f.Trap)
	}
	f = F32Frame{Op: uint32(F32Min), ALo: math.Float32bits(0), BLo: math.Float32bits(float32(math.Copysign(0, -1)))}
	RunF32(&f)
	if f.OutLo != 0x80000000 {
		t.Fatalf("min zero bits=%#x", f.OutLo)
	}
	f = F32Frame{Op: uint32(F32Lt), ALo: math.Float32bits(-1), BLo: math.Float32bits(1)}
	RunF32(&f)
	if f.OutLo != 1 || f.OutHi != 0 {
		t.Fatalf("lt out=%#x:%#x", f.OutHi, f.OutLo)
	}
}

func TestRunF32ConversionsAndTraps(t *testing.T) {
	f := F32Frame{Op: uint32(I32TruncF32S), ALo: math.Float32bits(float32(math.NaN()))}
	RunF32(&f)
	if f.Trap != TrapInvalidConversion {
		t.Fatalf("nan trap=%d", f.Trap)
	}
	f = F32Frame{Op: uint32(I32TruncSatF32U), ALo: math.Float32bits(float32(math.Inf(1)))}
	RunF32(&f)
	if f.Trap != TrapNone || f.OutLo != 0xffffffff {
		t.Fatalf("sat out=%#x trap=%d", f.OutLo, f.Trap)
	}
	f = F32Frame{Op: uint32(F32ConvertI64S), ALo: 0xffffffd6, AHi: 0xffffffff}
	RunF32(&f)
	if got := math.Float32frombits(f.OutLo); got != -42 {
		t.Fatalf("convert=%v", got)
	}
}

func TestF32TruncationUsesExactIEEEBitsAtI32Boundaries(t *testing.T) {
	for _, tc := range []struct {
		op   F32Op
		bits uint32
		want uint32
	}{
		{I32TruncF32S, 0x4effffff, 0x7fffff80},
		{I32TruncF32S, 0xcf000000, 0x80000000},
		{I32TruncF32U, 0x4f7fffff, 0xffffff00},
	} {
		f := F32Frame{Op: uint32(tc.op), ALo: tc.bits}
		RunF32(&f)
		if f.OutLo != tc.want || f.OutHi != 0 || f.Trap != TrapNone {
			t.Errorf("op %d bits=%#x = %08x:%08x trap=%d, want %08x", tc.op, tc.bits, f.OutHi, f.OutLo, f.Trap, tc.want)
		}
	}
}

func TestF32DemoteQuietsSignalingNaN(t *testing.T) {
	f := F32Frame{Op: uint32(F32DemoteF64), ALo: 0, AHi: 0x7ff40000}
	RunF32(&f)
	if f.OutLo != 0x7fe00000 || f.OutHi != 0 || f.Trap != TrapNone {
		t.Fatalf("demote signaling NaN = %08x:%08x trap=%d", f.OutHi, f.OutLo, f.Trap)
	}
}

func TestF32IntegralRoundingPreservesLargeValues(t *testing.T) {
	for _, op := range []F32Op{F32Ceil, F32Floor, F32Trunc, F32Nearest} {
		for _, bits := range []uint32{0x6c7f4d7b, 0x6511a2b4} {
			f := F32Frame{Op: uint32(op), ALo: bits}
			RunF32(&f)
			if f.OutLo != bits {
				t.Errorf("op %d bits=%#x = %#x", op, bits, f.OutLo)
			}
		}
	}
}
