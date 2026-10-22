//go:build linux && !tinygo

package riscv64

import enc "github.com/wago-org/wago/src/core/encoder/riscv64"

// X5 is the selected raw I32/I64 address, X6/X7 the retained vector. Preserve the preflighted
// pointer in X12 while the existing scalar lane helpers use X5 for payload.
func (f *integerEmitter) swarLaneMemory(offset uint64, width, lane uint8, store bool) {
	f.swarLaneMemoryAt(0, offset, width, lane, store)
}
func (f *integerEmitter) swarLaneMemoryAt(index uint32, offset uint64, width, lane uint8, store bool) {
	f.swarLaneMemoryTypedAt(index, offset, width, lane, store, false)
}
func (f *integerEmitter) swarLaneMemoryTypedAt(index uint32, offset uint64, width, lane uint8, store bool, address64 bool) {
	if !f.swarLaneCheck(width, lane) {
		return
	}
	f.swarMemoryAddressTypedAt(index, offset, width/8, address64)
	if f.err != nil || f.a.Err() != nil {
		return
	}
	f.a.Addi(enc.X12, enc.X5, 0)
	if store {
		f.swarExtract(width, lane, false)
	} else {
		f.Constant(5, 0, true)
	}
	for i := uint8(0); i < width/8; i++ {
		if store {
			f.a.Sb(enc.X5, enc.X12, int32(i))
			if i+1 != width/8 {
				f.a.Srli(enc.X5, enc.X5, 8)
			}
		} else {
			f.a.Lbu(enc.Scratch0, enc.X12, int32(i))
			if i != 0 {
				f.a.Slli(enc.Scratch0, enc.Scratch0, i*8)
			}
			f.a.Or(enc.X5, enc.X5, enc.Scratch0)
		}
	}
	if !store {
		f.swarReplace(width, lane)
	}
}

func (f *integerEmitter) swarSplatMemory(offset uint64, width uint8) {
	f.swarSplatMemoryAt(0, offset, width)
}
func (f *integerEmitter) swarSplatMemoryAt(index uint32, offset uint64, width uint8) {
	f.swarSplatMemoryTypedAt(index, offset, width, false)
}
func (f *integerEmitter) swarSplatMemoryTypedAt(index uint32, offset uint64, width uint8, address64 bool) {
	if !f.swarLaneCheck(width, 0) {
		return
	}
	f.swarMemoryAddressTypedAt(index, offset, width/8, address64)
	if f.err != nil || f.a.Err() != nil {
		return
	}
	f.a.Addi(enc.X12, enc.X5, 0)
	f.Constant(5, 0, true)
	for i := uint8(0); i < width/8; i++ {
		f.a.Lbu(enc.Scratch0, enc.X12, int32(i))
		if i != 0 {
			f.a.Slli(enc.Scratch0, enc.Scratch0, i*8)
		}
		f.a.Or(enc.X5, enc.X5, enc.Scratch0)
	}
	f.swarSplat(width)
}
