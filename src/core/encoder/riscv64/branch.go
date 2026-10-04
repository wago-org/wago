package riscv64

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Branch emits a short conditional branch with a signed byte displacement.
// Fixed-width-only code requires four-byte alignment, even though the ISA's
// immediate field can also represent two-byte-aligned compressed-code targets.
func (a *Asm) Branch(cond Cond, left, right Reg, displacement int32) {
	if !a.registers(left, right) {
		return
	}
	if !cond.valid() || displacement&3 != 0 || displacement < -4096 || displacement > 4092 {
		a.fail("invalid short branch condition, alignment, or range")
		return
	}
	u := uint32(displacement) & 0x1fff
	a.word(0x63 | ((u>>11)&1)<<7 | ((u>>1)&15)<<8 | uint32(cond)<<12 |
		uint32(left)<<15 | uint32(right)<<20 | ((u>>5)&63)<<25 | ((u>>12)&1)<<31)
}

// Jal emits a short jump/call with a signed, four-byte-aligned displacement.
func (a *Asm) Jal(link Reg, displacement int32) {
	if !a.registers(link) {
		return
	}
	if displacement&3 != 0 || displacement < -1<<20 || displacement > 1<<20-4 {
		a.fail("invalid JAL alignment or range")
		return
	}
	u := uint32(displacement) & 0x1fffff
	a.word(0x6f | uint32(link)<<7 | ((u>>12)&255)<<12 | ((u>>11)&1)<<20 |
		((u>>1)&1023)<<21 | ((u>>20)&1)<<31)
}

// JumpLong emits an eight-byte AUIPC/JALR envelope and returns its byte offset
// for PatchLong, or -1 on emission failure. It clobbers Scratch1/X31. With call
// true, RA is the link register; otherwise no link is saved.
func (a *Asm) JumpLong(call bool) int {
	pos := a.Len()
	link := Zero
	if call {
		link = RA
	}
	a.Auipc(Scratch1, 0)
	a.Jalr(link, Scratch1, 0)
	if a.Err() != nil {
		return -1
	}
	return pos
}

// BranchLong jumps when cond is true. Its inverted short branch skips a long
// transfer. It occupies twelve bytes and returns the AUIPC site (start+4), not
// the condition's site. The adapter owns i32 operand preparation.
func (a *Asm) BranchLong(cond Cond, left, right Reg) int {
	if !a.registers(left, right) || !cond.valid() {
		a.fail("invalid long branch condition or registers")
		return -1
	}
	a.Branch(cond^1, left, right, 12)
	return a.JumpLong(false)
}

// PatchLong patches an emitted AUIPC/JALR envelope using image-relative byte
// offsets. target may lie outside this slice for later image assembly. Adjust
// both site and target when relocating a worker's bytes into a larger image.
// Alignment, site shape and exact signed high-20/low-12 reach are checked before
// either word changes. Patching never grows B and may replace an earlier fixup.
func (a *Asm) PatchLong(site int, target int64) error {
	if a.Err() != nil {
		return a.Err()
	}
	if site < 0 || site&3 != 0 || target&3 != 0 || site > len(a.B)-8 {
		return fmt.Errorf("riscv64: invalid long fixup site or alignment")
	}
	hiWord := binary.LittleEndian.Uint32(a.B[site:])
	loWord := binary.LittleEndian.Uint32(a.B[site+4:])
	// Only our fixed X31 envelope, with x0 or ra as its link, is patchable.
	if hiWord&0xfff != 0x17|uint32(Scratch1)<<7 ||
		loWord&0xfffff != 0x67|uint32(Scratch1)<<15 &&
			loWord&0xfffff != 0x67|uint32(RA)<<7|uint32(Scratch1)<<15 {
		return fmt.Errorf("riscv64: fixup site is not a long transfer envelope")
	}
	if target < math.MinInt64+int64(site) {
		return fmt.Errorf("riscv64: long fixup displacement overflows")
	}
	delta := target - int64(site)
	// AUIPC sign-extends high-20<<12; JALR adds signed low-12. This interval
	// is asymmetric around zero and narrower at the positive signed-32 edge.
	if delta < -1<<31-2048 || delta > 1<<31-2048-1 {
		return fmt.Errorf("riscv64: long fixup displacement %d is out of range", delta)
	}
	high := (delta + 2048) >> 12
	low := delta - (high << 12)
	binary.LittleEndian.PutUint32(a.B[site:], hiWord&0xfff|(uint32(high)&0xfffff)<<12)
	binary.LittleEndian.PutUint32(a.B[site+4:], loWord&0xfffff|(uint32(low)&0xfff)<<20)
	return nil
}
