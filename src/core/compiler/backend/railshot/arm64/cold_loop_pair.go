//go:build arm64

package arm64

import "encoding/binary"

// A parallel loop transfer tmp=B; B=A+B; A=tmp can instead compute the sum
// in A and exchange the logical A/B roles. Only the odd cold exits need to
// restore their canonical local homes; the even loop backedge already agrees.
type coldLoopPair struct {
	a, b, temporary   Reg
	save, sum, assign int
}

func coldLoopMove64(word uint32) (Reg, Reg, bool) {
	return Reg(word & 31), Reg(word >> 16 & 31), word&0xffe0ffe0 == 0xaa0003e0
}

func (f *fn) findColdLoopPair(words []uint32, exit int) (coldLoopPair, bool) {
	var empty coldLoopPair
	for save := exit + 1; save+2 < len(words); save++ {
		temporary, b, ok := coldLoopMove64(words[save])
		if !ok {
			continue
		}
		a, source, ok := coldLoopMove64(words[save+2])
		if !ok || source != temporary || a == b || a == temporary || b == temporary || !f.pinnedLocalMask.has(a) || !f.pinnedLocalMask.has(b) || f.pinnedLocalMask.has(temporary) || f.reserved.has(temporary) || f.pinned.has(temporary) || f.regUser[temporary] != nil {
			continue
		}
		allocatable := false
		for _, reg := range gpAlloc {
			allocatable = allocatable || reg == temporary
		}
		if !allocatable {
			continue
		}
		sum := words[save+1]
		n, m := Reg(sum>>5&31), Reg(sum>>16&31)
		if sum&0x7fe0fc00 != 0x0b000000 || Reg(sum&31) != b || !(n == a && m == b || n == b && m == a) {
			continue
		}
		valid := true
		for i, word := range words {
			if i == exit {
				if word&0x7e000000 == 0x34000000 && Reg(word&31) == temporary {
					valid = false
				}
				continue
			}
			destination, _ := scalarLoopWord(word)
			if destination == a && i != save+2 || destination == b && i != save+1 || destination == temporary && i != save {
				valid = false
				break
			}
			fields := scalarLoopSources(word)
			for _, shift := range []uint{5, 10, 16} {
				if fields&(1<<shift) != 0 && Reg(word>>shift&31) == temporary && i != save+2 {
					valid = false
				}
			}
		}
		if valid {
			return coldLoopPair{a, b, temporary, save, save + 1, save + 2}, true
		}
	}
	return empty, false
}

func (pair coldLoopPair) rewriteReads(word uint32, a, b Reg) uint32 {
	fields := scalarLoopSources(word)
	for _, shift := range []uint{5, 10, 16} {
		if fields&(1<<shift) == 0 {
			continue
		}
		source := Reg(word >> shift & 31)
		if source == pair.a {
			word = word&^(31<<shift) | uint32(a)<<shift
		} else if source == pair.b {
			word = word&^(31<<shift) | uint32(b)<<shift
		}
	}
	return word
}

func (pair coldLoopPair) coldRestore(code []byte) []byte {
	result := make([]byte, 0, 12+len(code))
	move := func(to, from Reg) {
		result = binary.LittleEndian.AppendUint32(result, 0xaa0003e0|uint32(from)<<16|uint32(to))
	}
	move(pair.temporary, pair.a)
	move(pair.a, pair.b)
	move(pair.b, pair.temporary)
	return append(result, code...)
}
