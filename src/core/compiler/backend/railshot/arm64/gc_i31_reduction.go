//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func (f *fn) emitGCI31Reduction() error {
	// Publish after ordinary call-graph finalization, which conservatively sees
	// the original loop. This pure reduction needs no GC helper or extra entry ABI.
	f.gcReducedBounded = f.opt(optPreparedBoundedEntry) && len(f.localType) <= 128
	f.gcI31Induction = f.gcReducedBounded && f.opt(optRegABI) && f.opt(optPreparedDirectEntry)
	if reg, _, ok := f.pinReg(0); ok {
		f.recoverLocal(0)
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: reg, idx: 0})
	} else {
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	}
	n := f.materialize(f.popValue())
	f.a.MovReg32(n, n)
	correction := f.allocReg(maskOf(n))
	f.a.LslImm(correction, n, 31, true)
	f.a.And32(correction, correction, n)
	previous := f.allocReg(maskOf(n) | maskOf(correction))
	f.a.SubImm64(previous, n, 1)
	f.a.Mul64(n, n, previous)
	f.release(previous)
	f.a.LsrImm(n, n, 1, false)
	// Truncating i to 31 bits subtracts 2^31 once per term in the second
	// period. Modulo 2^32 only the parity matters: flip bit31 iff n has both
	// bit31 and bit0 set. The do-while's n=0 case also returns zero.
	f.a.Eor32(n, n, correction)
	f.release(correction)
	f.pushReg(n, mtI32)
	f.stats.peep("gc-i31-induction-reduction")
	end := wasm.ReaderFrom([]byte{0x0b})
	return f.bodyLoop(&end, 0)
}
