//go:build arm64

package arm64

func (f *fn) loadLinearIdx(dst, index Reg, disp int32, size int, signed, wide bool) {
	f.a.LoadIdx(dst, linMemReg, index, disp, size, signed, wide)
}
