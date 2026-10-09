//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

// Decode a GP MOV or owned direct-RSP frame transfer. Both source bridges use
// final operands and widths; no caller-supplied home or allocator pin is trusted.
func decodeSourceTransferAMD64(code []byte, at, end int, frame uint64, section uint8) (sourceBranchInstruction, int, string, bool) {
	return decodeSourceTransferPinsAMD64(code, at, end, frame, section, 12)
}

// The final call recipe also admits R12/R13 argument pins. Other source
// families retain their original register ceiling, including observer coverage.
func decodeSourceTransferPinsAMD64(code []byte, at, end int, frame uint64, section uint8, ceiling uint8) (sourceBranchInstruction, int, string, bool) {
	pc := at
	invalid := ""
	allowed := func(reg uint8) bool { return reg < ceiling && reg != 3 && reg != 4 }
	in := sourceBranchInstruction{section: section, copy: true}
	rex := byte(0)
	if pc < end && code[pc] >= 0x40 && code[pc] <= 0x4f {
		rex = code[pc]
		pc++
	}
	if pc+2 > end || rex&2 != 0 {
		return in, pc, invalid, false
	}
	op, modrm := code[pc], code[pc+1]
	pc += 2
	if op != 0x89 && op != 0x8b {
		return in, pc, invalid, false
	}
	reg := uint8((modrm>>3)&7) | ((rex>>2)&1)<<3
	if !allowed(reg) {
		return in, pc, invalid, false
	}
	size := 4
	if rex&8 != 0 {
		size = 8
	}
	in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Size: size}
	if modrm&0xc0 == 0xc0 {
		rm := uint8(modrm&7) | (rex&1)<<3
		if !allowed(rm) {
			return in, pc, invalid, false
		}
		in.effect.Dst, in.effect.Src = leafReg(rm), leafReg(reg)
		if op == 0x8b {
			in.effect.Dst, in.effect.Src = in.effect.Src, in.effect.Dst
		}
		in.writes = 1 << uint8(in.effect.Dst.Index)
		return in, pc, invalid, true
	}
	if rex&1 != 0 || modrm&7 != 4 || pc >= end || code[pc] != 0x24 {
		return in, pc, invalid, false
	}
	pc++
	var offset int32
	switch modrm & 0xc0 {
	case 0:
		offset = 0
	case 0x40:
		if pc >= end {
			return in, pc, invalid, false
		}
		offset = int32(int8(code[pc]))
		pc++
	case 0x80:
		if pc+4 > end {
			return in, pc, invalid, false
		}
		offset = int32(binary.LittleEndian.Uint32(code[pc : pc+4]))
		pc += 4
	default:
		return in, pc, invalid, false
	}
	if offset < 0 || uint64(offset)+uint64(size) > frame {
		invalid = "frame Copy lies outside reserved body SP region"
	}
	in.effect.Dst, in.effect.Src = regalloccheck.Slot(offset), leafReg(reg)
	if op == 0x8b {
		in.effect.Dst, in.effect.Src = in.effect.Src, in.effect.Dst
		in.writes = 1 << reg
		if size == 4 {
			in.effect.ClearTo = 8
		}
	}
	return in, pc, invalid, true
}
