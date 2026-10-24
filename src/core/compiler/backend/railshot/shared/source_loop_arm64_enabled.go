//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

// This zero-frame recipe keeps the original ABI carriers across the backedge.
// Only fixed, same-length frame-slot rewrites and contiguous alignment NOPs
// precede the loop; an adjacent CMP Wn,#0/B.NE reads the source i32 condition.
func decodeSourceLoopARM64(code []byte) (sourceBranchRecipe, bool) {
	r := sourceBranchRecipe{}
	if len(code) < 48 || len(code) > 128 || len(code)%4 != 0 {
		return r, false
	}
	word := func(pc int) uint32 { return binary.LittleEndian.Uint32(code[pc : pc+4]) }
	end := len(code) - 16
	if word(len(code)-4) != 0xd65f03c0 {
		return r, false
	}
	for _, pc := range []int{0, end} {
		for i := 0; i < 12; i += 4 {
			if word(pc+i) != 0xd503201f {
				return r, false
			}
		}
	}
	r.instructions = make([]sourceBranchInstruction, 0, 16)
	frameSlots := func(section uint8) {
		for _, reg := range []uint8{16, 16, 31} {
			r.instructions = append(r.instructions, sourceBranchInstruction{section: section, journalWrites: 1 << reg})
		}
	}
	frameSlots(0)
	for i := range r.loopLocals {
		r.loopLocals[i] = leafReg(uint8(i))
	}
	pc := 12
	for pc < end && word(pc) == 0xd503201f {
		pc += 4
		if pc-12 > 32 {
			return r, false
		}
	}
	header := pc
	for i := 0; i < 3; i++ {
		if pc >= end {
			return r, false
		}
		w := word(pc)
		pc += 4
		dst, src := uint8(w&31), uint8((w>>16)&31)
		base := w & ^uint32(31|31<<16)
		if (base != 0x2a0003e0 && base != 0xaa0003e0) || dst >= 16 || src >= 16 {
			return r, false
		}
		width := 4
		if base == 0xaa0003e0 {
			width = 8
		}
		r.instructions = append(r.instructions, sourceBranchInstruction{section: 1, copy: true, writes: 1 << dst, effect: regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(dst), Src: leafReg(src), Size: width}})
	}
	// Exact unshifted EOR Wn,Wn,Wn is a physical kill. A dead temporary may
	// be cleared; erasing any live header local loses the next-iteration value.
	if pc < end {
		w := word(pc)
		dst, rn, rm := uint8(w&31), uint8((w>>5)&31), uint8((w>>16)&31)
		if w & ^uint32(31|31<<5|31<<16) == 0x4a000000 {
			if dst >= 16 || dst != rn || dst != rm {
				return r, false
			}
			r.instructions = append(r.instructions, sourceBranchInstruction{section: 1, writes: 1 << dst})
			pc += 4
		}
	}
	if pc+8 != end {
		return r, false
	}
	w := word(pc)
	if w & ^uint32(31<<5) != 0x7100001f {
		return r, false
	}
	condition := uint8((w >> 5) & 31)
	if condition >= 16 {
		return r, false
	}
	branch := word(pc + 4)
	if branch&0xff00001f != 0x54000001 || int64(pc+4)+int64(int32(branch<<8)>>13)*4 != int64(header) {
		return r, false
	}
	r.instructions = append(r.instructions, sourceBranchInstruction{section: 1, condition: true, effect: regalloccheck.Effect{Src: leafReg(condition)}})
	frameSlots(3)
	r.instructions = append(r.instructions, sourceBranchInstruction{section: 4, returns: true})
	return r, true
}
