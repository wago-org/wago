//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

// Decode the complete call-free internal ARM64 body. Its fixed frame patch
// slots were observed as MOVZ X16; MOVK X16; SUB/ADD SP,SP,X16. A same-length
// immediate rewrite changes actual writes, but must retain those journal
// observations. No arbitrary patch, addressing mode or reserved GP is admitted.
func decodeSourceBranchARM64(code []byte) (sourceBranchRecipe, bool) {
	r := sourceBranchRecipe{}
	if len(code) < 48 || len(code) > 128 || len(code)%4 != 0 {
		return r, false
	}
	word := func(pc int) uint32 { return binary.LittleEndian.Uint32(code[pc : pc+4]) }
	end := len(code) - 16
	if word(len(code)-4) != 0xd65f03c0 { // RET X30, untouched throughout the body.
		return r, false
	}
	r.instructions = make([]sourceBranchInstruction, 0, 32)
	frame := func(pc int, restore bool) (uint32, bool) {
		section := uint8(0)
		immediate, register := uint32(0xd10003ff), uint32(0xcb3063ff)
		if restore {
			immediate, register = 0x910003ff, 0x8b3063ff
			section = 3
		}
		w := word(pc)
		if w == 0xd503201f && word(pc+4) == w && word(pc+8) == w {
			r.instructions = append(r.instructions,
				sourceBranchInstruction{section: section, journalWrites: 1 << 16},
				sourceBranchInstruction{section: section, journalWrites: 1 << 16},
				sourceBranchInstruction{section: section, journalWrites: 1 << 31})
			return 0, true
		}
		if w & ^uint32(0xfff<<10) == immediate && word(pc+4) == 0xd503201f && word(pc+8) == 0xd503201f {
			r.instructions = append(r.instructions,
				sourceBranchInstruction{section: section, writes: 1 << 31, journalWrites: 1 << 16},
				sourceBranchInstruction{section: section, journalWrites: 1 << 16},
				sourceBranchInstruction{section: section, journalWrites: 1 << 31})
			return (w >> 10) & 0xfff, true
		}
		if w & ^uint32(0xffff<<5) != 0xd2800010 || word(pc+4) & ^uint32(0xffff<<5) != 0xf2a00010 || word(pc+8) != register {
			return 0, false
		}
		r.instructions = append(r.instructions,
			sourceBranchInstruction{section: section, writes: 1 << 16},
			sourceBranchInstruction{section: section, writes: 1 << 16},
			sourceBranchInstruction{section: section, writes: 1 << 31})
		return (w>>5)&0xffff | ((word(pc+4)>>5)&0xffff)<<16, true
	}
	size, ok := frame(0, false)
	if !ok || size > 128 {
		return r, false
	}
	if size%16 != 0 {
		r.invalid = "unaligned reserved ARM64 frame"
	}
	pc := 12
	allowed := func(reg uint8) bool { return reg < 16 }
	copyInstruction := func(section uint8) (sourceBranchInstruction, bool) {
		in := sourceBranchInstruction{section: section, copy: true}
		if pc >= end {
			return in, false
		}
		w := word(pc)
		pc += 4
		dst, src := uint8(w&31), uint8((w>>16)&31)
		base := w & ^uint32(31|31<<16)
		if base == 0x2a0003e0 || base == 0xaa0003e0 {
			if !allowed(dst) || !allowed(src) {
				return in, false
			}
			width := 4
			if base == 0xaa0003e0 {
				width = 8
			}
			in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(dst), Src: leafReg(src), Size: width}
			in.writes = 1 << dst
			return in, true
		}
		base = w & 0xffc00000
		if (base != 0xb9000000 && base != 0xb9400000 && base != 0xf9000000 && base != 0xf9400000) || (w>>5)&31 != 31 || !allowed(dst) {
			return in, false
		}
		width := 4
		if w&(1<<30) != 0 {
			width = 8
		}
		offset := int32((w>>10)&0xfff) * int32(width)
		if uint32(offset)+uint32(width) > size {
			r.invalid = "frame Copy lies outside reserved body SP region"
		}
		in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Slot(offset), Src: leafReg(dst), Size: width}
		if w&(1<<22) != 0 {
			in.effect.Dst, in.effect.Src = in.effect.Src, in.effect.Dst
			in.writes = 1 << dst
			if width == 4 {
				in.effect.ClearTo = 8
			}
		}
		return in, true
	}
	// Existing canonical-i32 lowering may normalize the two ABI arguments.
	if pc+8 <= end && word(pc) == 0x2a0003e0 && word(pc+4) == 0x2a0103e1 {
		for i := 0; i < 2; i++ {
			in, ok := copyInstruction(0)
			if !ok {
				return r, false
			}
			r.instructions = append(r.instructions, in)
		}
	}
	for i := 0; size != 0 && i < 2; i++ {
		in, ok := copyInstruction(0)
		if !ok || in.effect.Dst.Bank != regalloccheck.Frame || in.effect.Src.Bank != regalloccheck.GP {
			return r, false
		}
		r.instructions = append(r.instructions, in)
	}
	if pc+4 > end {
		return r, false
	}
	w := word(pc)
	condition := uint8(w & 31)
	branch := pc
	if w&0xff000000 == 0x34000000 { // CBZ Wn, never CBNZ or X-width.
		pc += 4
	} else if w & ^uint32(31<<5) == 0x7100001f && pc+8 <= end && word(pc+4)&0xff00001f == 0x54000000 {
		condition = uint8((w >> 5) & 31)
		branch = pc + 4
		w = word(branch)
		pc += 8
	} else {
		return r, false
	}
	if !allowed(condition) {
		return r, false
	}
	falseTarget := int64(branch) + int64(int32(w<<8)>>13)*4
	r.instructions = append(r.instructions, sourceBranchInstruction{condition: true, effect: regalloccheck.Effect{Src: leafReg(condition)}})
	thenMove, ok := copyInstruction(1)
	if !ok || thenMove.effect.Dst.Bank != regalloccheck.GP || (thenMove.effect.Src.Bank == regalloccheck.Frame && thenMove.effect.Size != 4) || pc+4 > end || word(pc)&0xfc000000 != 0x14000000 {
		return r, false
	}
	r.thenSource, r.thenResult = thenMove.effect.Src, thenMove.effect.Dst
	r.instructions = append(r.instructions, thenMove)
	joinTarget := int64(pc) + int64(int32(word(pc)<<6)>>6)*4
	pc += 4
	falseStart := pc
	elseMove, ok := copyInstruction(2)
	if !ok || elseMove.effect.Dst.Bank != regalloccheck.GP || (elseMove.effect.Src.Bank == regalloccheck.Frame && elseMove.effect.Size != 4) {
		return r, false
	}
	r.elseSource, r.elseResult = elseMove.effect.Src, elseMove.effect.Dst
	r.instructions = append(r.instructions, elseMove)
	joinStart := pc
	returnMove, ok := copyInstruction(3)
	if !ok || returnMove.effect.Dst.Bank != regalloccheck.GP || returnMove.effect.Src.Bank != regalloccheck.GP || pc != end || falseTarget != int64(falseStart) || joinTarget != int64(joinStart) {
		return r, false
	}
	r.instructions = append(r.instructions, returnMove)
	restore, ok := frame(end, true)
	if !ok {
		return r, false
	}
	if restore != size {
		r.invalid = "unbalanced reserved ARM64 frame"
	}
	r.instructions = append(r.instructions, sourceBranchInstruction{section: 4, returns: true})
	return r, true
}
