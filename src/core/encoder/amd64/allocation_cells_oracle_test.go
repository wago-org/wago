//go:build amd64

package amd64

func (a *Asm) oldAllocationLoadIdx(dst, base, index Reg, disp int32, size int, signed, wide bool) {
	if regallocCheckEnabled {
		a.regallocGPWrite(regallocGPRegMask(dst))
	}

	var op []byte
	rexW := false
	switch {
	case size == 8:
		op, rexW = []byte{0x8B}, true // mov r64, m64
	case size == 4 && signed && wide:
		op, rexW = []byte{0x63}, true // movsxd r64, m32
	case size == 4:
		op = []byte{0x8B} // mov r32, m32 (zero-extends to 64)
	case size == 1 && signed:
		op, rexW = []byte{0x0F, 0xBE}, wide // movsx r, m8
	case size == 1:
		op = []byte{0x0F, 0xB6} // movzx r, m8 (zero-extends to 64)
	case size == 2 && signed:
		op, rexW = []byte{0x0F, 0xBF}, wide // movsx r, m16
	default: // size == 2 unsigned
		op = []byte{0x0F, 0xB7} // movzx r, m16 (zero-extends to 64)
	}
	if rexW || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(a.rex(rexW, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(op...)
	a.sibAddr(dst, base, index, disp)
}
