//go:build arm64

package arm64

func (f *fn) loadTableDescriptor(dst Reg, tableIdx uint32) {
	if tableIdx == 0 {
		f.ld64(dst, linMemReg, -int32(offTablePtr))
		return
	}
	f.ld64(dst, linMemReg, -int32(offTableDirPtr))
	f.ld64(dst, dst, int32(tableIdx*8))
}

func (f *fn) tableAddr64(tableIdx uint32) bool {
	tt, ok := f.m.TableType(tableIdx)
	return ok && tt.Limits.Addr64
}

// canonicalizeTableOperand zero-extends table32 operands through a W-register
// write before any X-width address arithmetic or loop use. Host-produced i32
// values may have arbitrary upper bits in their 64-bit ABI slots.
func (f *fn) canonicalizeTableOperand(reg Reg, tableIdx uint32) {
	if !f.tableAddr64(tableIdx) {
		f.a.MovReg32(reg, reg)
	}
}
