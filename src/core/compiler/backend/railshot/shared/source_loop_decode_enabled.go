//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

// The alias loop keeps three live local carriers across its single backedge.
// Decode all final transfers, bounded canonical NOP padding, the condition
// spill/reload and adjacent TEST32/JNE, then the owned frame's return path.
func decodeSourceLoopAMD64(code []byte) (sourceBranchRecipe, bool) {
	r := sourceBranchRecipe{}
	if len(code) < 45 || len(code) > 128 {
		return r, false
	}
	end := len(code) - 8
	if string(code[:3]) != "\x48\x81\xec" || string(code[end:end+3]) != "\x48\x81\xc4" || code[len(code)-1] != 0xc3 {
		return r, false
	}
	frame := uint64(binary.LittleEndian.Uint32(code[3:7]))
	if frame == 0 || frame > 128 {
		return r, false
	}
	if frame != uint64(binary.LittleEndian.Uint32(code[end+3:end+7])) {
		r.invalid = "unbalanced loop frame"
	}
	r.instructions = make([]sourceBranchInstruction, 0, 32)
	r.instructions = append(r.instructions, sourceBranchInstruction{writes: 1 << 4})
	pc := 7
	transfer := func(section uint8) (sourceBranchInstruction, bool) {
		in, next, invalid, ok := decodeSourceTransferAMD64(code, pc, end, frame, section)
		pc = next
		if invalid != "" {
			r.invalid = invalid
		}
		return in, ok
	}
	padding := func(limit int) bool {
		start := pc
		for pc < end {
			length := sourceNOPAMD64(code[pc:end])
			if length == 0 {
				break
			}
			pc += length
			if pc-start > limit {
				return false
			}
		}
		return true
	}
	for i := 0; i < 3; i++ {
		in, ok := transfer(0)
		if !ok || in.effect.Dst.Bank != regalloccheck.GP || in.effect.Src.Bank != regalloccheck.GP {
			return r, false
		}
		r.loopLocals[i] = in.effect.Dst
		r.instructions = append(r.instructions, in)
	}
	if !padding(31) {
		return r, false
	}
	header := pc
	for i := 0; i < 3; i++ {
		in, ok := transfer(1)
		if !ok || in.effect.Dst.Bank != regalloccheck.GP || in.effect.Src.Bank != regalloccheck.GP {
			return r, false
		}
		r.instructions = append(r.instructions, in)
	}
	// A covered optional XOR-self is a physical kill, not a source definition.
	// Dead scratch writes are harmless; losing a live backedge local must fail.
	if pc < end {
		at, rex := pc, byte(0)
		if code[at] >= 0x40 && code[at] <= 0x4f {
			rex = code[at]
			at++
		}
		if at+2 <= end && code[at] == 0x31 && rex&10 == 0 && code[at+1]&0xc0 == 0xc0 {
			m := code[at+1]
			dst := uint8(m&7) | (rex&1)<<3
			src := uint8((m>>3)&7) | ((rex>>2)&1)<<3
			if dst != src || dst >= 12 || dst == 3 || dst == 4 {
				return r, false
			}
			pc = at + 2
			r.instructions = append(r.instructions, sourceBranchInstruction{section: 1, writes: 1 << dst})
		}
	}
	store, ok := transfer(1)
	if !ok || store.effect.Dst.Bank != regalloccheck.Frame || store.effect.Src.Bank != regalloccheck.GP {
		return r, false
	}
	r.instructions = append(r.instructions, store)
	load, ok := transfer(1)
	if !ok || load.effect.Src.Bank != regalloccheck.Frame || load.effect.Dst.Bank != regalloccheck.GP || load.effect.Size != 4 {
		return r, false
	}
	r.instructions = append(r.instructions, load)
	rex := byte(0)
	if pc < end && code[pc] >= 0x40 && code[pc] <= 0x4f {
		rex = code[pc]
		pc++
	}
	if pc+8 > end || code[pc] != 0x85 || rex&10 != 0 || code[pc+1]&0xc0 != 0xc0 {
		return r, false
	}
	m := code[pc+1]
	left := uint8(m&7) | (rex&1)<<3
	right := uint8((m>>3)&7) | ((rex>>2)&1)<<3
	if left != right || left >= 12 || left == 3 || left == 4 {
		return r, false
	}
	r.instructions = append(r.instructions, sourceBranchInstruction{section: 1, condition: true, effect: regalloccheck.Effect{Src: leafReg(left)}})
	pc += 2
	if code[pc] != 0x0f || code[pc+1] != 0x85 || int64(pc+6)+int64(int32(binary.LittleEndian.Uint32(code[pc+2:pc+6]))) != int64(header) {
		return r, false
	}
	pc += 6
	if !padding(15) {
		return r, false
	}
	ret, ok := transfer(3)
	if !ok || ret.effect.Dst.Bank != regalloccheck.GP || ret.effect.Src.Bank != regalloccheck.GP || pc != end {
		return r, false
	}
	r.instructions = append(r.instructions, ret, sourceBranchInstruction{section: 3, writes: 1 << 4}, sourceBranchInstruction{section: 4, writes: 1 << 4, returns: true})
	return r, true
}

// These are the encoder's documented one-through-nine byte NOPs. Each is
// independently known to have no GP or memory effect; arbitrary gaps fail.
func sourceNOPAMD64(code []byte) int {
	for _, nop := range [...]string{"\x90", "\x66\x90", "\x0f\x1f\x00", "\x0f\x1f\x40\x00", "\x0f\x1f\x44\x00\x00", "\x66\x0f\x1f\x44\x00\x00", "\x0f\x1f\x80\x00\x00\x00\x00", "\x0f\x1f\x84\x00\x00\x00\x00\x00", "\x66\x0f\x1f\x84\x00\x00\x00\x00\x00"} {
		if len(code) >= len(nop) && string(code[:len(nop)]) == nop {
			return len(nop)
		}
	}
	return 0
}
