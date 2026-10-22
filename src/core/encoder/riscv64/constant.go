package riscv64

// Constant64 materializes all 64 bits without scratch registers or literals,
// in at most eight fixed-width instructions. It may overwrite its own input.
func (a *Asm) Constant64(dst Reg, value int64) {
	if !a.registers(dst) {
		return
	}
	if value >= -2048 && value <= 2047 {
		a.Addi(dst, Zero, int32(value))
		return
	}
	low := int64(int32(uint32(value)<<20) >> 20)
	if value >= -1<<31 && value <= 1<<31-1 {
		a.Lui(dst, uint32((value+2048)>>12)&0xfffff)
		if low != 0 {
			// ADDIW corrects LUI's sign extension when high-20 rounding crosses
			// bit 31 (for example 0x7fffffff).
			a.Addiw(dst, dst, int32(low))
		}
		return
	}
	// Compute the rounded upper part without overflowing value-low at INT64_MAX.
	upper := value >> 12
	if low < 0 {
		upper++
	}
	a.Constant64(dst, upper)
	a.Slli(dst, dst, 12)
	if low != 0 {
		a.Addi(dst, dst, int32(low))
	}
}

// Constant32 materializes a zero-normalized shared i32 carrier.
func (a *Asm) Constant32(dst Reg, value uint32) {
	a.Constant64(dst, int64(int32(value)))
	if value>>31 != 0 {
		a.ZeroExtend32(dst, dst)
	}
}
