//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// SWAR transport uses consecutive little-endian 64-bit cells. These private
// primitives do not admit V128 modules; signatures, control and calls must first
// acquire complete pair-aware lowering. No optional vector ISA is emitted.
type swarPair struct{ low, high enc.Reg }

func (p swarPair) valid() bool {
	return p.low <= enc.X31 && p.high <= enc.X31 && p.low != p.high &&
		enc.ReservedMask&(1<<p.low|1<<p.high) == 0
}

func (f *target) swarCheck(pairs ...swarPair) bool {
	if f.err != nil || f.a.Err() != nil {
		return false
	}
	for _, pair := range pairs {
		if !pair.valid() {
			f.err = fmt.Errorf("rv64: invalid SWAR register pair")
			return false
		}
	}
	return true
}

func (f *target) swarOffset(off int32, pair swarPair) bool {
	if !f.swarCheck(pair) {
		return false
	}
	if off < int32(f.frameHeader()) || off&7 != 0 || off > math.MaxInt32-8 {
		f.err = fmt.Errorf("rv64: invalid SWAR frame offset")
		return false
	}
	return true
}

func (f *target) swarLoad(off int32, pair swarPair) {
	if !f.swarOffset(off, pair) {
		return
	}
	f.Load(uint8(pair.low), off, true)
	f.Load(uint8(pair.high), off+8, true)
}

func (f *target) swarStore(off int32, pair swarPair) {
	if !f.swarOffset(off, pair) {
		return
	}
	f.Store(off, uint8(pair.low), true)
	f.Store(off+8, uint8(pair.high), true)
}

// Both halves are computed before either destination is written, including
// reversed pairs and cross-half aliases. Scratch0/1 are never valid pair inputs.
func (f *target) swarMove(dst, src swarPair) {
	if !f.swarCheck(dst, src) {
		return
	}
	f.a.Addi(enc.Scratch0, src.low, 0)
	f.a.Addi(enc.Scratch1, src.high, 0)
	f.a.Addi(dst.low, enc.Scratch0, 0)
	f.a.Addi(dst.high, enc.Scratch1, 0)
}

func (f *target) swarBits(kind wasm.InstrKind, dst, left, right, mask swarPair) {
	if !f.swarCheck(dst, left) {
		return
	}
	switch kind {
	case wasm.InstrV128Not:
	case wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor:
		if !f.swarCheck(right) {
			return
		}
	case wasm.InstrV128Bitselect:
		if !f.swarCheck(right, mask) {
			return
		}
	default:
		f.err = fmt.Errorf("rv64: unsupported SWAR bit operation")
		return
	}
	for i, out := range []enc.Reg{enc.Scratch0, enc.Scratch1} {
		a, b, m := left.low, right.low, mask.low
		if i != 0 {
			a, b, m = left.high, right.high, mask.high
		}
		switch kind {
		case wasm.InstrV128Not:
			f.a.Xori(out, a, -1)
		case wasm.InstrV128And:
			f.a.And(out, a, b)
		case wasm.InstrV128Andnot:
			f.a.Xori(out, b, -1)
			f.a.And(out, a, out)
		case wasm.InstrV128Or:
			f.a.Or(out, a, b)
		case wasm.InstrV128Xor:
			f.a.Xor(out, a, b)
		case wasm.InstrV128Bitselect:
			f.a.Xor(out, a, b)
			f.a.And(out, out, m)
			f.a.Xor(out, out, b)
		}
	}
	f.a.Addi(dst.low, enc.Scratch0, 0)
	f.a.Addi(dst.high, enc.Scratch1, 0)
}

// Preflight the entire actual access footprint before any guest memory touch.
// X5 is a raw I32 address on entry and the resolved byte pointer on success.
func (f *integerEmitter) swarMemoryAddress(offset uint64, footprint uint8) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if offset > math.MaxUint32 || (footprint != 1 && footprint != 2 && footprint != 4 && footprint != 8 && footprint != 16) {
		f.err = fmt.Errorf("rv64: invalid SWAR memory32 offset")
		return
	}
	f.NormalizeI32(5)
	f.a.Constant64(enc.X28, int64(offset))
	f.a.Add(enc.X5, enc.X5, enc.X28)
	if f.mode.production {
		f.productionMemorySize(enc.X29)
	} else {
		f.a.Ld(enc.X28, enc.X24, 0)
		f.a.Ld(enc.X29, enc.X28, 8)
	}
	f.a.Addi(enc.X28, enc.Zero, int32(footprint))
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X28))
	f.a.Sub(enc.X29, enc.X29, enc.X28)
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X5))
	if f.mode.production {
		f.a.Addi(enc.X28, enc.X25, 0)
	} else {
		f.a.Ld(enc.X28, enc.X24, 0)
		f.a.Ld(enc.X28, enc.X28, 0)
	}
	f.a.Add(enc.X5, enc.X28, enc.X5)
}

// X5 is a raw i32 address, X6/X7 the complete pair. Check all16 bytes before
// touching memory, so a split store cannot partially write at a boundary. Byte
// instructions support every alignment; there is no poll between the halves.
// Stores consume X6/X7; the caller owns all operand spill homes.
func (f *integerEmitter) swarMemory(offset uint64, store bool) {
	f.swarMemoryAddress(offset, 16)
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if !store {
		f.Constant(6, 0, true)
		f.Constant(7, 0, true)
	}
	for half, reg := range []enc.Reg{enc.X6, enc.X7} {
		for lane := 0; lane < 8; lane++ {
			off := int32(half*8 + lane)
			if store {
				f.a.Sb(reg, enc.X5, off)
				if lane != 7 {
					f.a.Srli(reg, reg, 8)
				}
			} else {
				f.a.Lbu(enc.Scratch0, enc.X5, off)
				if lane != 0 {
					f.a.Slli(enc.Scratch0, enc.Scratch0, uint8(lane*8))
				}
				f.a.Or(reg, reg, enc.Scratch0)
			}
		}
	}
}
