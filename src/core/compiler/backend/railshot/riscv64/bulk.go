//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func (f *integerEmitter) dataDrop(offset int32) {
	base, off := f.contextAddress(offset + 8)
	f.a.Sd(enc.Zero, base, off)
}

// X5=destination, X6=source/value and X7=count are unsigned low-word i32.
// The caller loads canonical slots; X28/X29 and permanently reserved X31 are
// the only additional temporaries. All complete ranges are checked before the
// zero-count exit, any byte access, or the first mutation.
func (f *integerEmitter) memoryBulk(kind wasm.InstrKind, dataOffset int32) {
	f.a.Ld(enc.X28, enc.X24, 0)
	f.a.Ld(enc.X29, enc.X28, 8)
	f.bulkRange(enc.X5)
	if kind == wasm.InstrMemoryCopy {
		// The same memory and count leave the same length-count residual.
		f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X6))
	} else if kind == wasm.InstrMemoryInit {
		base, off := f.contextAddress(dataOffset + 8)
		f.a.Ld(enc.X29, base, off)
		f.bulkRange(enc.X6)
	}
	zero := f.a.BranchLong(enc.EQ, enc.X7, enc.Zero)
	f.a.Ld(enc.X28, enc.X28, 0)
	f.a.Add(enc.X5, enc.X28, enc.X5)
	if kind == wasm.InstrMemoryFill {
		f.bulkLoop(true, false)
		f.patchBulk(zero)
		return
	}
	if kind == wasm.InstrMemoryInit {
		// Recompute the address after branches: long transfers clobber X31.
		base, off := f.contextAddress(dataOffset)
		f.a.Ld(enc.X29, base, off)
		f.a.Add(enc.X6, enc.X29, enc.X6)
		f.bulkLoop(false, false)
		f.patchBulk(zero)
		return
	}
	f.a.Add(enc.X6, enc.X28, enc.X6)
	forward := f.a.BranchLong(enc.GEU, enc.X6, enc.X5)
	f.a.Add(enc.X5, enc.X5, enc.X7)
	f.a.Add(enc.X6, enc.X6, enc.X7)
	f.bulkLoop(false, true)
	end := f.Jump()
	f.patchBulk(forward)
	f.bulkLoop(false, false)
	f.patchBulk(zero, end)
}

// X29 contains a length on entry and its length-count residual on success.
func (f *integerEmitter) bulkRange(offset enc.Reg) {
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X7))
	f.a.Sub(enc.X29, enc.X29, enc.X7)
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, offset))
}

func (f *integerEmitter) bulkLoop(fill, backward bool) {
	loop := f.Position()
	if backward {
		f.a.Addi(enc.X5, enc.X5, -1)
		f.a.Addi(enc.X6, enc.X6, -1)
	}
	if fill {
		f.a.Sb(enc.X6, enc.X5, 0)
	} else {
		f.a.Lbu(enc.X29, enc.X6, 0)
		f.a.Sb(enc.X29, enc.X5, 0)
	}
	if !backward {
		f.a.Addi(enc.X5, enc.X5, 1)
		if !fill {
			f.a.Addi(enc.X6, enc.X6, 1)
		}
	}
	f.a.Addi(enc.X7, enc.X7, -1)
	back := f.a.BranchLong(enc.NE, enc.X7, enc.Zero)
	if err := f.Patch(back, loop); err != nil {
		f.err = err
	}
}
func (f *integerEmitter) patchBulk(sites ...int) {
	for _, site := range sites {
		if err := f.Patch(site, f.Position()); err != nil {
			f.err = err
		}
	}
}
