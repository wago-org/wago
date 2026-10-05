package riscv64

// AtomicOrder encodes the baseline A extension's aq/rl memory-domain bits.
// LR.acquire and SC.release form a constrained CAS sequence suitable for planned
// runtime publication. Ordering across memory and I/O domains requires FENCE.
type AtomicOrder uint8

const (
	AtomicRelaxed        AtomicOrder = 0
	AtomicRelease        AtomicOrder = 1
	AtomicAcquire        AtomicOrder = 2
	AtomicAcquireRelease AtomicOrder = 3
)

func (a *Asm) atomic(function, width uint32, dst, value, base Reg, order AtomicOrder) {
	if !a.registers(dst, value, base) {
		return
	}
	if order > AtomicAcquireRelease {
		a.fail("invalid atomic ordering bits")
		return
	}
	if regallocCheckEnabled && a.err == nil && len(a.B)&3 == 0 {
		size := 4
		if width == 3 {
			size = 8
		}
		a.regallocAtomic(dst, base, function == 3, size)
	}
	a.word(0x2f | uint32(dst)<<7 | width<<12 | uint32(base)<<15 | uint32(value)<<20 | uint32(order)<<25 | function<<27)
}

// LrW loads a naturally aligned word and sign-extends it to64 bits. It does not
// establish a zero-normalized i32 carrier. The effective address has no offset.
func (a *Asm) LrW(dst, base Reg, order AtomicOrder) { a.atomic(2, 2, dst, Zero, base, order) }
func (a *Asm) LrD(dst, base Reg, order AtomicOrder) { a.atomic(2, 3, dst, Zero, base, order) }

// ScW stores the low32 bits only if the reservation succeeds. The full register
// result is zero on success or an unspecified nonzero failure code. Never infer
// an unconditional memory copy from this conditional store.
func (a *Asm) ScW(status, value, base Reg, order AtomicOrder) {
	a.atomic(3, 2, status, value, base, order)
}
func (a *Asm) ScD(status, value, base Reg, order AtomicOrder) {
	a.atomic(3, 3, status, value, base, order)
}
