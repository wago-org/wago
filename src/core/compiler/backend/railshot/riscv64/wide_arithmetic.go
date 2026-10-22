//go:build linux && !tinygo

package riscv64

import enc "github.com/wago-org/wago/src/core/encoder/riscv64"

// wideAddSub consumes four i64 spill slots in low/high pair order and replaces
// them with the low/high result. SLTU computes carry and borrow without any
// optional ISA extension.
func (f *integerEmitter) wideAddSub(base int, subtract bool) {
	f.Load(5, f.SpillOffset(base), true)
	f.Load(6, f.SpillOffset(base+1), true)
	f.Load(7, f.SpillOffset(base+2), true)
	f.Load(28, f.SpillOffset(base+3), true)
	if subtract {
		f.a.Sltu(enc.Scratch0, enc.X5, enc.X7)
		f.a.Sub(enc.X5, enc.X5, enc.X7)
		f.a.Sub(enc.X6, enc.X6, enc.X28)
		f.a.Sub(enc.X6, enc.X6, enc.Scratch0)
	} else {
		f.a.Add(enc.X5, enc.X5, enc.X7)
		f.a.Sltu(enc.Scratch0, enc.X5, enc.X7)
		f.a.Add(enc.X6, enc.X6, enc.X28)
		f.a.Add(enc.X6, enc.X6, enc.Scratch0)
	}
	f.Store(f.SpillOffset(base), 5, true)
	f.Store(f.SpillOffset(base+1), 6, true)
}

// wideMultiply consumes two i64 spill slots and replaces them with the low and
// high halves of the product. RV64G provides both required high-half variants.
func (f *integerEmitter) wideMultiply(base int, signed bool) {
	f.Load(5, f.SpillOffset(base), true)
	f.Load(6, f.SpillOffset(base+1), true)
	if signed {
		f.a.Mulh(enc.X7, enc.X5, enc.X6)
	} else {
		f.a.Mulhu(enc.X7, enc.X5, enc.X6)
	}
	f.a.Mul(enc.X5, enc.X5, enc.X6)
	f.Store(f.SpillOffset(base), 5, true)
	f.Store(f.SpillOffset(base+1), 7, true)
}
