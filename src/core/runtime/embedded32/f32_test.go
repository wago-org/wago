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
