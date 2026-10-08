//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func leafReg(r uint8) regalloccheck.Location { return regalloccheck.Register(regalloccheck.GP, r) }
func leafOperator(op byte, wide bool) wasm.InstrKind {
	if wide {
		switch op {
		case 1:
			return wasm.InstrI64Add
		case 9:
			return wasm.InstrI64Or
		case 0x21:
			return wasm.InstrI64And
		case 0x31:
			return wasm.InstrI64Xor
		}
	}
	switch op {
	case 1:
		return wasm.InstrI32Add
	case 9:
		return wasm.InstrI32Or
	case 0x21:
		return wasm.InstrI32And
	case 0x31:
		return wasm.InstrI32Xor
	}
	return wasm.InstrInvalid
}

// This is deliberately a recipe decoder, not a general x86 disassembler. Only
// zero-frame SubRsp/body/AddRsp/Ret and the listed register forms are admitted.
// No destination can modify the frame origin or callee/module state.
func decodeSourceLeafAMD64(code []byte, out []leafInstruction) ([]leafInstruction, bool) {
	if len(code) < 15 || len(code) > 128 || len(out) != 0 || cap(out) < 32 {
		return nil, false
	}
	start, end := code[:7], code[len(code)-8:]
	if start[0] != 0x48 || start[1] != 0x81 || start[2] != 0xec || binary.LittleEndian.Uint32(start[3:]) != 0 ||
		end[0] != 0x48 || end[1] != 0x81 || end[2] != 0xc4 || binary.LittleEndian.Uint32(end[3:]) != 0 || end[7] != 0xc3 {
		return nil, false
	}
	out = append(out, leafInstruction{writes: 1 << 4})
	allowed := func(r uint8) bool { return r < 16 && r != 3 && r != 4 && r != 5 && r < 12 }
	for pc := 7; pc < len(code)-8; {
		if len(out) >= 30 {
			return nil, false
		}
		rex := byte(0)
		if code[pc] >= 0x40 && code[pc] <= 0x4f {
			rex = code[pc]
			pc++
			if rex&2 != 0 {
				return nil, false
			}
		}
		if pc > len(code)-10 {
			return nil, false
		}
		op, modrm := code[pc], code[pc+1]
		pc += 2
		if modrm&0xc0 != 0xc0 {
			return nil, false
		}
		dst, src := uint8(modrm&7)|(rex&1)<<3, uint8((modrm>>3)&7)|((rex>>2)&1)<<3
		if !allowed(dst) || !allowed(src) {
			return nil, false
		}
		width := uint8(4)
		if rex&8 != 0 {
			width = 8
		}
		in := leafInstruction{writes: uint32(1) << dst, width: width, dst: leafReg(dst)}
		if op == 0x89 {
			in.copy = true
			in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(dst), Src: leafReg(src), Size: int(width)}
		} else {
			in.kind = leafOperator(op, width == 8)
			if in.kind == wasm.InstrInvalid {
				return nil, false
			}
			in.arithmetic = true
			in.left, in.right = leafReg(dst), leafReg(src)
		}
		out = append(out, in)
	}
	out = append(out, leafInstruction{writes: 1 << 4}, leafInstruction{writes: 1 << 4, returns: true})
	return out, true
}

// ARM64 uses X16 for the independently decoded zero-frame setup and teardown.
// No other immediate, SP-changing instruction, frame access, or branch is
// admitted. Destinations X18..X30 are excluded; RET must read preserved LR.
func decodeSourceLeafARM64(code []byte, out []leafInstruction) ([]leafInstruction, bool) {
	if len(code) < 28 || len(code) > 128 || len(code)%4 != 0 || len(out) != 0 || cap(out) < 32 {
		return nil, false
	}
	word := func(pc int) uint32 { return binary.LittleEndian.Uint32(code[pc : pc+4]) }
	// patchFrameAdjusts may rewrite precisely these six reserved words to
	// NOP before optional compaction. The final bytes prove zero SP delta;
	// the original journal masks are reconciled only at these fixed slots.
	neutral := word(0) == 0xd503201f && word(4) == 0xd503201f && word(8) == 0xd503201f &&
		word(len(code)-16) == 0xd503201f && word(len(code)-12) == 0xd503201f && word(len(code)-8) == 0xd503201f
	original := word(0) == 0xd2800010 && word(4) == 0xf2a00010 && word(8) == 0xcb3063ff &&
		word(len(code)-16) == 0xd2800010 && word(len(code)-12) == 0xf2a00010 && word(len(code)-8) == 0x8b3063ff
	if (!neutral && !original) || word(len(code)-4) != 0xd65f03c0 {
		return nil, false
	}
	out = append(out, leafInstruction{writes: 1 << 16, neutralFrame: neutral}, leafInstruction{writes: 1 << 16, neutralFrame: neutral}, leafInstruction{writes: 1 << 31, neutralFrame: neutral})
	for pc := 12; pc < len(code)-16; pc += 4 {
		if len(out) >= 28 {
			return nil, false
		}
		w := word(pc)
		dst, left, right := uint8(w&31), uint8((w>>5)&31), uint8((w>>16)&31)
		if dst >= 18 || right >= 18 {
			return nil, false
		}
		width := uint8(4)
		if w>>31 != 0 {
			width = 8
		}
		base := w & 0x7fe0fc00
		in := leafInstruction{writes: uint32(1) << dst, width: width, dst: leafReg(dst)}
		if base == 0x2a000000 && left == 31 {
			in.copy = true
			in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(dst), Src: leafReg(right), Size: int(width)}
		} else {
			if left >= 18 {
				return nil, false
			}
			op := byte(0)
			switch base {
			case 0x0b000000:
				op = 1
			case 0x0a000000:
				op = 0x21
			case 0x2a000000:
				op = 9
			case 0x4a000000:
				op = 0x31
			default:
				return nil, false
			}
			in.kind = leafOperator(op, width == 8)
			in.arithmetic = true
			in.left, in.right = leafReg(left), leafReg(right)
		}
		out = append(out, in)
	}
	out = append(out, leafInstruction{writes: 1 << 16, neutralFrame: neutral}, leafInstruction{writes: 1 << 16, neutralFrame: neutral}, leafInstruction{writes: 1 << 31, neutralFrame: neutral}, leafInstruction{returns: true})
	return out, true
}
