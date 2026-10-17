//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

var atomicLSEEnabled = true

func (f *fn) useAtomicLSE() bool {
	return f.opt(optAtomicLSE) && shared.ARM64SupportsLSE()
}

func (f *fn) atomicRMWLSE(d shared.Atomic, value, old, addr Reg) {
	op := a64.LSEAdd
	src := value
	if d.Operation == shared.AtomicSub || d.Operation == shared.AtomicAnd {
		// Borrowed source registers must survive subtraction/complement. The
		// hardware operation truncates the transformed value to the memory width.
		src = f.allocReg(maskOf(value, old, addr))
		if d.Operation == shared.AtomicSub {
			if d.Size == 8 {
				f.a.Sub64(src, ZR, value)
			} else {
				f.a.Sub32(src, ZR, value)
			}
		} else {
			op = a64.LSEClear
			f.a.MovImm64(src, ^uint64(0))
			if d.Size == 8 {
				f.a.Eor64(src, src, value)
			} else {
				f.a.Eor32(src, src, value)
			}
		}
	} else {
		switch d.Operation {
		case shared.AtomicAdd:
		case shared.AtomicOr:
			op = a64.LSESet
		case shared.AtomicXor:
			op = a64.LSEXor
		case shared.AtomicXchg:
			op = a64.LSESwap
		default:
			panic("arm64: invalid LSE atomic RMW operation")
		}
	}
	f.a.AtomicRMWAL(op, src, old, addr, int(d.Size))
	if src != value {
		f.release(src)
	}
	f.requiresLSE = true
}
