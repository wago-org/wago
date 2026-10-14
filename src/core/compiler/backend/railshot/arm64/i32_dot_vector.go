//go:build arm64

package arm64

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

// emitI32DotVector reduces a checked pair of biased i32 streams. Callers own
// the GP registers and restore the returned vector registers and pin mask.
func (f *fn) emitI32DotVector(left, right, n, sum, tmp, product Reg, leftBias, rightBias int32) [8]Reg {
	const calleeFP regMask = 0xff00
	// Four independent wrapping sums hide the multiply-accumulate dependency.
	// Reduce them only after the complete batches and exact remainder loads.
	var v [8]Reg
	for i := range v {
		v[i] = f.allocFReg(calleeFP)
		f.fpinned = f.fpinned.add(v[i])
	}
	x, y, lb, rb, acc, acc1, acc2, acc3 := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
	f.a.MovImm32(tmp, leftBias)
	f.a.NeonDupGprS(lb, tmp)
	f.a.MovImm32(tmp, rightBias)
	f.a.NeonDupGprS(rb, tmp)
	f.a.NeonEor16b(acc, acc, acc)
	f.a.NeonEor16b(acc1, acc1, acc1)
	f.a.NeonEor16b(acc2, acc2, acc2)
	f.a.NeonEor16b(acc3, acc3, acc3)
	f.a.CmpImm32(n, 128)
	small := f.a.Bcond(a64.CondCC)
	loop := f.a.Len()
	addPair := func(off int32, into Reg) {
		f.a.LdrQ(x, left, off)
		f.a.LdrQ(y, right, off)
		f.a.NeonAddS(x, x, lb)
		f.a.NeonAddS(y, y, rb)
		f.a.NeonMlaS(into, x, y)
	}
	addPair(0, acc)
	addPair(16, acc1)
	addPair(32, acc2)
	addPair(48, acc3)
	addPair(64, acc)
	addPair(80, acc1)
	addPair(96, acc2)
	addPair(112, acc3)
	f.a.AddImm64(left, left, 128)
	f.a.AddImm64(right, right, 128)
	f.a.SubImm32(n, n, 128)
	f.a.CmpImm32(n, 128)
	again := f.a.Bcond(a64.CondCS)
	f.patchBranch19(again, loop)
	f.patchBranch19(small, f.a.Len())
	f.a.CmpImm32(n, 64)
	noSixteen := f.a.Bcond(a64.CondCC)
	addPair(0, acc)
	addPair(16, acc1)
	addPair(32, acc2)
	addPair(48, acc3)
	f.a.AddImm64(left, left, 64)
	f.a.AddImm64(right, right, 64)
	f.a.SubImm32(n, n, 64)
	f.patchBranch19(noSixteen, f.a.Len())
	f.a.CmpImm32(n, 32)
	noEight := f.a.Bcond(a64.CondCC)
	addPair(0, acc)
	addPair(16, acc1)
	f.a.AddImm64(left, left, 32)
	f.a.AddImm64(right, right, 32)
	f.a.SubImm32(n, n, 32)
	f.patchBranch19(noEight, f.a.Len())
	f.a.CmpImm32(n, 16)
	noHalf := f.a.Bcond(a64.CondCC)
	addPair(0, acc)
	f.a.AddImm64(left, left, 16)
	f.a.AddImm64(right, right, 16)
	f.a.SubImm32(n, n, 16)
	f.patchBranch19(noHalf, f.a.Len())
	f.a.NeonAddS(acc, acc, acc1)
	f.a.NeonAddS(acc2, acc2, acc3)
	f.a.NeonAddS(acc, acc, acc2)
	f.a.NeonAddvS(acc, acc)
	f.a.NeonUmovS(sum, acc, 0)
	noTail := f.a.Cbz32(n)
	// Only a final pair remains. Scalar loads avoid any vector over-read.
	for _, off := range []int32{0, 4} {
		f.ld32(tmp, left, off)
		f.a.MovImm32(product, leftBias)
		f.a.Add32(tmp, tmp, product)
		f.ld32(product, right, off)
		f.a.MovImm32(n, rightBias)
		f.a.Add32(product, product, n)
		f.a.Madd32(sum, tmp, product, sum)
	}
	f.patchBranch19(noTail, f.a.Len())
	return v
}
