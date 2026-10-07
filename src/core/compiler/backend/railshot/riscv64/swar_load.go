//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Eight-byte extending loads and four/eight-byte zeroing loads return X6/X7.
// The actual footprint is checked before the first unaligned-safe byte load.
func (f *integerEmitter) swarPartialLoad(offset uint64, kind uint32) {
	f.swarPartialLoadAt(0, offset, kind)
}
func (f *integerEmitter) swarPartialLoadAt(index uint32, offset uint64, kind uint32) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if !(kind >= 1 && kind <= 6) && kind != 92 && kind != 93 {
		f.err = fmt.Errorf("rv64: invalid SWAR partial load opcode")
		return
	}
	footprint := uint8(8)
	if kind == 92 {
		footprint = 4
	}
	f.swarMemoryAddressAt(index, offset, footprint)
	if f.err != nil || f.a.Err() != nil {
		return
	}
	f.Constant(6, 0, true)
	f.Constant(7, 0, true)
	for i := uint8(0); i < footprint; i++ {
		f.a.Lbu(enc.Scratch0, enc.X5, int32(i))
		if i != 0 {
			f.a.Slli(enc.Scratch0, enc.Scratch0, i*8)
		}
		f.a.Or(enc.X6, enc.X6, enc.Scratch0)
	}
	if kind >= 1 && kind <= 6 {
		f.swarWiden(uint8(8<<((kind-1)/2)), kind%2 == 1, false, false)
	}
}

func swarMemoryOpcode(kind uint32) (align uint32, ok bool) {
	switch {
	case kind == 0 || kind == 11:
		return 4, true
	case kind >= 1 && kind <= 6 || kind == 93:
		return 3, true
	case kind == 92:
		return 2, true
	case kind >= 7 && kind <= 10:
		return kind - 7, true
	case kind >= 84 && kind <= 87:
		return kind - 84, true
	case kind >= 88 && kind <= 91:
		return kind - 88, true
	default:
		return 0, false
	}
}
