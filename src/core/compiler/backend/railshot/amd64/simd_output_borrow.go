//go:build amd64

package amd64

import (
	"os"
	"runtime"
)

// Qualified on native Linux AMD64; zero keeps the previous materialization path.
var simdOutputBorrowEnabled = runtime.GOOS == "linux" && os.Getenv("WAGO_AMD64_SIMD_OUTPUT_BORROW") != "0"

// The result owns dst. A borrowed src remains unchanged, including through
// register pressure while allocating dst. Baseline mode keeps the old copy.
func (f *fn) v128OutputOperand(value *elem) (dst, src Reg) {
	if !simdOutputBorrowEnabled {
		x := f.materializeV128(value)
		return x, x
	}
	src, owned := f.operandRegV128(value)
	if owned {
		return src, src
	}
	old := f.fpinned
	f.fpinned = old.add(src)
	dst = f.allocFReg(maskOf(src))
	f.fpinned = old
	f.stats.peep("simd-output-borrow")
	return dst, src
}

func (f *fn) v128BorrowedShuffle(a, b *elem, am, bm [16]byte) {
	old := f.fpinned
	xa, aOwned := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb, bOwned := f.operandRegV128(b)
	f.fpinned = f.fpinned.add(xb)
	da := xa
	if !aOwned || xa == xb {
		da = f.allocFReg(maskOf(xa, xb))
	}
	f.fpinned = f.fpinned.add(da)
	db := xb
	if !bOwned {
		db = f.allocFReg(maskOf(xa, xb, da))
	}
	f.fpinned = f.fpinned.add(db)
	lo, hi := v128MaskBits(am)
	f.v128ShuffleMask(da, xa, lo, hi)
	lo, hi = v128MaskBits(bm)
	f.v128ShuffleMask(db, xb, lo, hi)
	opVPor.emit(f, da, da, db)
	f.fpinned = old
	if aOwned && xa != da && xa != db {
		f.releaseF(xa)
	}
	if bOwned && xb != da && xb != db && xb != xa {
		f.releaseF(xb)
	}
	f.releaseF(db)
	if !aOwned || !bOwned {
		f.stats.peep("simd-shuffle-output-borrow")
	}
	f.pushVReg(da)
}
