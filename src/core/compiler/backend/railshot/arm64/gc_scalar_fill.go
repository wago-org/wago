//go:build arm64

package arm64

// emitGCScalarFill writes exactly count numeric elements, including unaligned
// subranges. Pair stores cover sixteen bytes; a scalar tail avoids overrun.
// The pointer and count are consumed. No vector registers or barriers are needed.
func (f *fn) emitGCScalarFill(ptr, count, value Reg, size uint32) {
	if size == 4 {
		f.a.OrrShiftedReg(value, value, value, 0, 32, false)
	}
	group := uint32(16) / size
	f.cmpImm(count, group, false)
	tail := f.a.Bcond(condB)
	loop := f.a.Len()
	f.a.StpOffset(value, value, ptr, 0)
	f.a.AddImm64(ptr, ptr, 16)
	f.a.SubImm32(count, count, group)
	f.cmpImm(count, group, false)
	again := f.a.Bcond(condAE)
	f.patchBranch19(again, loop)
	f.patchBranch19(tail, f.a.Len())
	empty := f.a.Cbz32(count)
	loop = f.a.Len()
	if size == 4 {
		f.st32(ptr, 0, value)
	} else {
		f.st64(ptr, 0, value)
	}
	f.a.AddImm64(ptr, ptr, size)
	f.a.SubImm32(count, count, 1)
	again = f.a.Cbnz32(count)
	f.patchBranch19(again, loop)
	f.patchBranch19(empty, f.a.Len())
}
