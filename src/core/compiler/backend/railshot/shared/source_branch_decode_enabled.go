//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

type sourceBranchInstruction struct {
	effect                   regalloccheck.Effect
	writes                   uint32
	section                  uint8 // entry, then, else, join, return
	copy, condition, returns bool
}
type sourceBranchRecipe struct {
	instructions           []sourceBranchInstruction
	locals                 [2]regalloccheck.Location
	thenResult, elseResult regalloccheck.Location
	conditionReload        regalloccheck.Location
	invalid                string
}

// Decode the ENTIRE bounded final image. One SubRsp establishes body SP; every
// memory Copy uses that actual RSP origin and lies inside its reservation. The
// equal AddRsp and C3 RET preserve caller return control. RBP is allocatable in
// this internal Wasm ABI; RBX/module caches and other reserved GPs are excluded.
// There is no generic addressing, flag writer, call, or raw padding admission.
func decodeSourceBranchAMD64(code []byte) (sourceBranchRecipe, bool) {
	r := sourceBranchRecipe{}
	if len(code) < 35 || len(code) > 128 {
		return r, false
	}
	end := len(code) - 8
	if code[0] != 0x48 || code[1] != 0x81 || code[2] != 0xec || code[end] != 0x48 || code[end+1] != 0x81 || code[end+2] != 0xc4 || code[len(code)-1] != 0xc3 {
		return r, false
	}
	frame := uint64(binary.LittleEndian.Uint32(code[3:7]))
	restore := uint64(binary.LittleEndian.Uint32(code[end+3 : end+7]))
	if frame > 128 {
		return r, false
	}
	if frame == 0 || frame != restore {
		r.invalid = "unbalanced reserved frame"
	}
	r.instructions = make([]sourceBranchInstruction, 0, 16)
	r.instructions = append(r.instructions, sourceBranchInstruction{writes: 1 << 4})
	pc := 7
	allowed := func(reg uint8) bool { return reg < 12 && reg != 3 && reg != 4 }
	// Decode only MOV register/register and direct RSP+disp stores/loads. REX
	// index/base extensions and SIB scale/index are excluded before any facts.
	copyInstruction := func(section uint8) (sourceBranchInstruction, bool) {
		in := sourceBranchInstruction{section: section, copy: true}
		rex := byte(0)
		if pc < end && code[pc] >= 0x40 && code[pc] <= 0x4f {
			rex = code[pc]
			pc++
		}
		if pc+2 > end || rex&2 != 0 {
			return in, false
		}
		op, modrm := code[pc], code[pc+1]
		pc += 2
		if op != 0x89 && op != 0x8b {
			return in, false
		}
		reg := uint8((modrm>>3)&7) | ((rex>>2)&1)<<3
		if !allowed(reg) {
			return in, false
		}
		size := 4
		if rex&8 != 0 {
			size = 8
		}
		in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Size: size}
		if modrm&0xc0 == 0xc0 {
			rm := uint8(modrm&7) | (rex&1)<<3
			if !allowed(rm) {
				return in, false
			}
			in.effect.Dst, in.effect.Src = leafReg(rm), leafReg(reg)
			if op == 0x8b {
				in.effect.Dst, in.effect.Src = in.effect.Src, in.effect.Dst
			}
			in.writes = 1 << uint8(in.effect.Dst.Index)
			return in, true
		}
		if rex&1 != 0 || modrm&7 != 4 || pc >= end || code[pc] != 0x24 {
			return in, false
		}
		pc++
		var offset int32
		switch modrm & 0xc0 {
		case 0:
			offset = 0
		case 0x40:
			if pc >= end {
				return in, false
			}
			offset = int32(int8(code[pc]))
			pc++
		case 0x80:
			if pc+4 > end {
				return in, false
			}
			offset = int32(binary.LittleEndian.Uint32(code[pc : pc+4]))
			pc += 4
		default:
			return in, false
		}
		if offset < 0 || uint64(offset)+uint64(size) > frame {
			r.invalid = "frame Copy lies outside reserved body SP region"
		}
		in.effect.Dst, in.effect.Src = regalloccheck.Slot(offset), leafReg(reg)
		if op == 0x8b {
			in.effect.Dst, in.effect.Src = in.effect.Src, in.effect.Dst
			in.writes = 1 << reg
			if size == 4 {
				in.effect.ClearTo = 8
			}
		}
		return in, true
	}
	for i := 0; i < 4; i++ {
		in, ok := copyInstruction(0)
		if !ok {
			return r, false
		}
		if i < 2 {
			if in.effect.Dst.Bank != regalloccheck.GP || in.effect.Src.Bank != regalloccheck.GP {
				return r, false
			}
			r.locals[i] = in.effect.Dst
		}
		if i == 2 && (in.effect.Dst.Bank != regalloccheck.Frame || in.effect.Src.Bank != regalloccheck.GP) {
			return r, false
		}
		if i == 3 && (in.effect.Src.Bank != regalloccheck.Frame || in.effect.Dst.Bank != regalloccheck.GP || in.effect.Size != 4) {
			return r, false
		}
		if i == 3 {
			r.conditionReload = in.effect.Dst
		}
		r.instructions = append(r.instructions, in)
	}
	// TEST must read precisely the i32 condition against itself. No instruction
	// may overwrite its flags before the immediately adjacent near conditional.
	rex := byte(0)
	if pc < end && code[pc] >= 0x40 && code[pc] <= 0x4f {
		rex = code[pc]
		pc++
	}
	if pc+2 > end || code[pc] != 0x85 || rex&2 != 0 || code[pc+1]&0xc0 != 0xc0 {
		return r, false
	}
	m := code[pc+1]
	left, right := uint8(m&7)|(rex&1)<<3, uint8((m>>3)&7)|((rex>>2)&1)<<3
	pc += 2
	if !allowed(left) || !allowed(right) {
		return r, false
	}
	if left != right || rex&8 != 0 {
		// Wider and aliased-register TEST forms can be correct. They require
		// separate flag/upper-lane contracts and are outside this recipe.
		return r, false
	}
	r.instructions = append(r.instructions, sourceBranchInstruction{condition: true, effect: regalloccheck.Effect{Src: leafReg(left)}})
	if pc+6 > end || code[pc] != 0x0f || code[pc+1] < 0x80 || code[pc+1] > 0x8f {
		return r, false
	}
	condition := code[pc+1]
	falseTarget := int64(pc+6) + int64(int32(binary.LittleEndian.Uint32(code[pc+2:pc+6])))
	pc += 6
	thenMove, ok := copyInstruction(1)
	if !ok || thenMove.effect.Dst.Bank != regalloccheck.GP || thenMove.effect.Src.Bank != regalloccheck.GP {
		return r, false
	}
	r.thenResult = thenMove.effect.Dst
	r.instructions = append(r.instructions, thenMove)
	if pc+5 > end || code[pc] != 0xe9 {
		return r, false
	}
	joinTarget := int64(pc+5) + int64(int32(binary.LittleEndian.Uint32(code[pc+1:pc+5])))
	pc += 5
	falseStart := pc
	elseMove, ok := copyInstruction(2)
	if !ok || elseMove.effect.Dst.Bank != regalloccheck.GP || elseMove.effect.Src.Bank != regalloccheck.GP {
		return r, false
	}
	r.elseResult = elseMove.effect.Dst
	r.instructions = append(r.instructions, elseMove)
	joinStart := pc
	returnMove, ok := copyInstruction(3)
	if !ok || returnMove.effect.Dst.Bank != regalloccheck.GP || returnMove.effect.Src.Bank != regalloccheck.GP || pc != end {
		return r, false
	}
	r.instructions = append(r.instructions, returnMove, sourceBranchInstruction{section: 3, writes: 1 << 4}, sourceBranchInstruction{section: 4, writes: 1 << 4, returns: true})
	// The source ledger preserves both local aliases at the join. This first
	// bridge proves their unchanged pin carriers, rather than source liveness.
	// Valid reuse of a dead pin needs another map and must stay Inconclusive.
	if r.locals[0] == r.locals[1] {
		return r, false
	}
	for _, local := range r.locals {
		if local == r.conditionReload || local == r.thenResult || local == r.elseResult {
			return r, false
		}
	}
	if condition != 0x84 || falseTarget != int64(falseStart) || joinTarget != int64(joinStart) {
		// Inverted predicates with swapped arms and preloaded result carriers
		// can implement equivalent control flow. Only canonical destinations
		// are mapped here; alternative CFGs need a separate source bridge.
		return r, false
	}
	return r, true
}
