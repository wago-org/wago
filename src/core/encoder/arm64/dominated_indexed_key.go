package arm64

import "encoding/binary"

// FoldDominatedIndexedBasesByKey propagates exact register-pair addresses over finalized
// direct branches without changing code size. Callers must supply every
// additional native entry and exclude exception/custom entry points.
// Backward entries start unknown; forward joins intersect exact input keys.
func (a *Asm) FoldDominatedIndexedBasesByKey(entryOffsets ...int) int {
	n := len(a.B) &^ 3
	if n > 4096 {
		return 0
	}
	b := a.B[:n:n]
	// One exact register-pair key and three control flags per instruction.
	// No heap backing; equal addresses merge even from different seed sites.
	const target, backward, seen uint16 = 1 << 15, 1 << 14, 1 << 13
	const seedMask uint16 = 0x7ff
	// The size guard and aligned PC checks prove all table indexes are in
	// range. Extract the ten instruction-index bits to make that proof
	// explicit to Go's bounds-check eliminator.
	var entries [1024]uint16
	branches := 0
	for pc := 0; pc <= len(b)-4; pc += 4 {
		w := binary.LittleEndian.Uint32(b[pc : pc+4])
		if w == 0xD503201F || w&0x1C000000 != 0x14000000 {
			continue
		}
		if w&0xFFFFFC1F == 0xD61F0000 {
			return 0
		}
		if to, ok := dominatedDirectBranchTarget(pc, w); ok {
			branches++
			if branches > 128 {
				return 0
			}
			if to >= 0 && to < n {
				entries[(uint(to)>>2)&uint(len(entries)-1)] |= target
				if pc >= to {
					entries[(uint(to)>>2)&uint(len(entries)-1)] |= backward
				}
			}
		}
	}
	// Wrapper/internal entries have an unknown incoming address state.
	for _, pc := range entryOffsets {
		if pc < 0 || pc >= n || pc&3 != 0 {
			return 0
		}
		entries[(uint(pc)>>2)&uint(len(entries)-1)] |= target | backward
	}
	fact := uint16(0)
	reachable := true
	hits := 0
	for pc := 0; pc <= len(b)-4; pc += 4 {
		entry := entries[(uint(pc)>>2)&uint(len(entries)-1)]
		if entry&target != 0 {
			switch {
			case entry&backward != 0:
				// Later predecessors are not yet proved; never reuse through this entry.
				fact = 0
				reachable = true
			case entry&seen != 0:
				incoming := entry & seedMask
				if !reachable {
					fact = incoming
					reachable = true
				} else if fact != incoming {
					fact = 0
				}
			}
		}
		word := binary.LittleEndian.Uint32(b[pc : pc+4])
		if word == 0xD503201F {
			continue
		}
		if to, ok := dominatedDirectBranchTarget(pc, word); ok {
			incoming := fact
			call := word&0xFC000000 == 0x94000000
			if call {
				incoming = 0
				fact = 0
			}
			if reachable && to >= 0 && to < n {
				slot := &entries[(uint(to)>>2)&uint(len(entries)-1)]
				if *slot&seen == 0 {
					*slot = (*slot &^ seedMask) | seen | incoming
				} else if *slot&seedMask != incoming {
					*slot &^= seedMask
				}
			}
			if word&0xFC000000 == 0x14000000 {
				fact = 0
				reachable = false
			}
			continue
		}
		if !reachable {
			continue
		}
		if word&0xFFE0FC1F == 0x8B000010 {
			base, index := Reg(word>>5&31), Reg(word>>16&31)
			if base == X16 || index == X16 || base == ZR || index == ZR {
				fact = 0
				continue
			}
			key := uint16(base)*32 + uint16(index) + 1
			if fact == key {
				binary.LittleEndian.PutUint32(b[pc:pc+4], 0xD503201F)
				hits++
				continue
			}
			fact = key
			continue
		}
		if fact != 0 {
			base, index := Reg((fact-1)>>5), Reg((fact-1)&31)
			if !preservesDominatedIndexedBase(word, base, index) {
				fact = 0
			}
		}
	}
	return hits
}
