//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The admitted directory has at most 256 pointers, so its last cell pointer
// loads at offset 2040. The owner supplies live cells of the declared width;
// aliased entries are reloaded on every access, across calls and mutations.
// Scalar stores replace exactly eight bytes; V128 stores replace both halves.
func (f *integerEmitter) swarGlobal(index uint32, class swarClass, store bool) {
	f.a.Ld(enc.X28, enc.X25, -abi.GlobalsPtrOffset)
	f.a.Ld(enc.X28, enc.X28, int32(index*8))
	if store {
		f.a.Sd(enc.X5, enc.X28, 0)
		if class == swarV128 {
			f.a.Sd(enc.X6, enc.X28, 8)
		}
		return
	}
	if class.narrow() {
		f.a.Lwu(enc.X5, enc.X28, 0)
	} else {
		f.a.Ld(enc.X5, enc.X28, 0)
	}
	if class == swarV128 {
		f.a.Ld(enc.X6, enc.X28, 8)
	} else {
		f.a.Addi(enc.X6, enc.Zero, 0)
	}
}
