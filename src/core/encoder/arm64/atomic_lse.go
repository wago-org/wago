package arm64

// LSE atomic read/modify/write op fields, with acquire and release ordering.
const (
	LSEAdd   = 0
	LSEClear = 1
	LSEXor   = 2
	LSESet   = 3
	LSESwap  = 8
)

func lseSizeBits(size int) uint32 {
	switch size {
	case 1:
		return 0
	case 2:
		return 1 << 30
	case 4:
		return 2 << 30
	case 8:
		return 3 << 30
	default:
		panic("arm64: unsupported LSE size")
	}
}

// AtomicRMWAL returns the old value in dst. dst must be a real register:
// using ZR discards the acquire semantics of the load operation.
func (a *Asm) AtomicRMWAL(op int, src, dst, addr Reg, size int) {
	if r(dst) == 31 || (op != LSEAdd && op != LSEClear && op != LSEXor && op != LSESet && op != LSESwap) {
		panic("arm64: invalid LSE RMW operands")
	}
	a.word(0x38E00000 | lseSizeBits(size) | r(src)<<16 | uint32(op)<<12 | r(addr)<<5 | r(dst))
	if regallocCheckEnabled {
		a.regallocGPWrites(regallocGPMask(dst, false))
	}
}

// CasAL replaces expected with the old value, whether or not the comparison
// succeeds. Comparisons and writes use the selected memory width.
func (a *Asm) CasAL(expected, replacement, addr Reg, size int) {
	if r(expected) == 31 {
		panic("arm64: invalid LSE CAS result register")
	}
	a.word(0x08E0FC00 | lseSizeBits(size) | r(expected)<<16 | r(addr)<<5 | r(replacement))
	if regallocCheckEnabled {
		a.regallocGPWrites(regallocGPMask(expected, false))
	}
}
