//go:build arm64

package arm64

import "os"

var guardedBooleanXorEnabled = os.Getenv("WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR") != "1"

// Convert a bounded integer-only guarded boolean XOR to a conditional compare.
// The speculative body cannot change NZCV, trap, touch memory, or modify any
// register except caller scratch. Every changed scratch value must be dead
// after the join, and a W XOR's newly cleared upper half must not be observed.
func (f *fn) foldGuardedBooleanXors(b []byte, n int, targets []uint64, entries ...int) {
	if !f.opt(optGuardedBooleanXor) || f.opaqueFragments || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	for pc := 4; pc+20 <= n; pc += 4 {
		guard := rdWord(b, pc)
		if guard&0xff000010 != 0x54000000 || guard&15 >= 14 || !pureCompareWord(rdWord(b, pc-4)) {
			continue
		}
		join, ok := branchTarget(pc, guard)
		if !ok || join < pc+20 || join > pc+64 || join > n || !deadFlagsPath(b, join, n) {
			continue
		}
		cmpPC := join - 12
		cmp, set, xor := rdWord(b, cmpPC), rdWord(b, cmpPC+4), rdWord(b, cmpPC+8)
		if !pureCompareWord(cmp) || set&0x7fff0fe0 != 0x1a9f07e0 || xor&0x7fe0fc00 != 0x4a000000 || (set^xor)&0x80000000 != 0 {
			continue
		}
		tmp, out := set&31, xor&31
		if tmp < 9 || tmp > 15 || out == 31 || out == tmp || xor>>5&31 != out || xor>>16&31 != tmp {
			continue
		}
		condition := invertCond(Cond(set >> 12 & 15))
		if condition >= 14 {
			continue
		}
		clobbers := uint32(1) << tmp
		valid := true
		for at := pc + 4; at < cmpPC; at += 4 {
			x, recognized := guardedIntegerWord(rdWord(b, at))
			if !recognized || x.flags || x.readsFlags || x.writes & ^uint32(0xfe00) != 0 || x.writes&(1<<out) != 0 {
				valid = false
				break
			}
			clobbers |= x.writes
		}
		for at := pc; valid && at < join; at += 4 {
			if branchTargeted(targets, at) {
				valid = false
			}
			for _, entry := range entries {
				if entry == at {
					valid = false
				}
			}
		}
		high := uint32(0)
		if xor&0x80000000 == 0 {
			high = 1 << out
		}
		retHigh := uint32(0xfe00)
		if !f.preserveCallerPins {
			retHigh |= 0x03f80000
		}
		if !valid || !guardedClobbersDead(b, join, n, clobbers, high, retHigh, targets) {
			continue
		}
		fallback := uint32(0)
		for compareCondHolds(condition, fallback) {
			fallback++
		}
		conditional, ok := conditionalCompareWord(cmp, invertCond(Cond(guard&15)), fallback)
		if !ok {
			continue
		}
		wrWord(b, pc, nopWord)
		wrWord(b, cmpPC, conditional)
		f.stats.peep("guarded-boolean-xor")
		pc = join - 4
	}
}

type guardedIntegerEffects struct {
	reads, writes           uint32
	wide, flags, readsFlags bool
	copy                    bool
}

// Deliberately narrow, architecturally pure integer encodings. Register31 is
// zero here; SP forms, extended arithmetic, division and unknown words decline.
func guardedIntegerWord(w uint32) (x guardedIntegerEffects, ok bool) {
	x.wide = w&0x80000000 != 0
	reg := func(r uint32) uint32 { return (uint32(1) << r) & 0x7fffffff }
	rd, rn, rm := w&31, w>>5&31, w>>16&31
	x.writes = reg(rd)
	switch {
	case w == nopWord:
		x.writes = 0
	case w&0x1f800000 == 0x12800000:
		op := w >> 29 & 3
		if op == 1 || !x.wide && w>>21&3 > 1 {
			return x, false
		}
		if op == 3 { // MOVK preserves the other bits of Rd.
			x.reads = reg(rd)
		}
	case w&0x1f000000 == 0x0a000000:
		if !x.wide && w>>10&63 >= 32 {
			return x, false
		}
		x.reads, x.flags = reg(rn)|reg(rm), w>>29&3 == 3
		x.copy = w&0x7fe0ffe0 == 0x2a0003e0 // ORR Rd,ZR,Rm,LSL0 (MOV)
	case w&0x1f800000 == 0x12000000:
		if rd == 31 && w>>29&3 != 3 { // Nonflag logical-immediate Rd31 is SP.
			return x, false
		}
		if !x.wide && w&(1<<22) != 0 {
			return x, false
		}
		encoded := (w>>22&1)<<6 | (^w >> 10 & 63)
		level := uint32(0)
		for encoded > 1 {
			encoded >>= 1
			level++
		}
		if level < 1 || w>>10&((1<<level)-1) == (1<<level)-1 {
			return x, false
		}
		x.reads, x.flags = reg(rn), w>>29&3 == 3
	case w&0x1f800000 == 0x11000000:
		if rn == 31 || rd == 31 && w&(1<<29) == 0 {
			return x, false
		}
		x.reads, x.flags = reg(rn), w&(1<<29) != 0
	case w&0x1f200000 == 0x0b000000:
		if w>>22&3 == 3 || !x.wide && w>>10&63 >= 32 {
			return x, false
		}
		x.reads, x.flags = reg(rn)|reg(rm), w&(1<<29) != 0
	case w&0x7fe00000 == 0x1b000000:
		x.reads = reg(rn) | reg(rm) | reg(w>>10&31)
	case w&0x1f800000 == 0x13000000:
		if w>>29&3 == 3 || (w>>22&1 != w>>31) || !x.wide && (w>>16&63 >= 32 || w>>10&63 >= 32) {
			return x, false
		}
		x.reads = reg(rn)
		if w>>29&3 == 1 { // BFM preserves destination bits.
			x.reads |= reg(rd)
		}
	case w&0x7fff0fe0 == 0x1a9f07e0: // CSET has no GPR inputs.
		x.readsFlags = true
	case w&0x1fe00000 == 0x1a800000: // Other integer conditional selects.
		if w&(1<<29) != 0 || w&(1<<11) != 0 {
			return x, false
		}
		x.readsFlags = true
		x.reads = reg(rn) | reg(rm)
	default:
		return x, false
	}
	return x, true
}

// Follow at most64 branch states/512 instructions. Unknown instructions,
// calls, and non-copy full-width observations of changed upper bits
// decline. Full-value clobbers may never be read; high-only changes may flow
// through MOV X, and W reads observe only the unchanged low half.
func guardedClobbersDead(b []byte, pc, n int, full, high, retHigh uint32, targets []uint64) bool {
	type state struct {
		pc         int
		full, high uint32
	}
	var queue [64]state
	queue[0] = state{pc, full, high}
	count, head, steps := 1, 0, 0
	for head < count {
		s := queue[head]
		head++
		first := true
		for s.full|s.high != 0 {
			if !first && branchTargeted(targets, s.pc) {
				delegated := false
				for _, prev := range queue[:count] {
					if prev.pc == s.pc && prev.full == s.full && prev.high == s.high {
						delegated = true
						break
					}
				}
				if delegated {
					break
				}
			}
			first = false
			steps++
			if steps > 512 || s.pc < 0 || s.pc+4 > n {
				return false
			}
			w := rdWord(b, s.pc)
			if w == 0xd65f03c0 {
				// Original packet writes already belong to this callee's static
				// clobber surface. X0-X7 results and fixed ABI homes never qualify.
				if s.full & ^uint32(0xfe00) != 0 || s.high & ^retHigh != 0 {
					return false
				}
				break
			}
			if to, branch := branchTarget(s.pc, w); branch {
				if w&0xfc000000 == 0x94000000 || to < 0 || to >= n {
					return false
				}
				if w&0x7e000000 == 0x34000000 || w&0x7e000000 == 0x36000000 {
					read := uint32(1) << (w & 31) & 0x7fffffff
					if s.full&read != 0 || w&0x80000000 != 0 && s.high&read != 0 {
						return false
					}
				}
				seen := false
				for _, prev := range queue[:count] {
					if prev.pc == to && prev.full == s.full && prev.high == s.high {
						seen = true
						break
					}
				}
				if !seen {
					if count == len(queue) {
						return false
					}
					queue[count] = state{to, s.full, s.high}
					count++
				}
				if w&0xfc000000 == 0x14000000 {
					break
				}
				s.pc += 4
				continue
			}
			x, recognized := guardedIntegerWord(w)
			if !recognized || x.reads&s.full != 0 {
				return false
			}
			s.full &^= x.writes
			incoming := s.high & x.reads
			s.high &^= x.writes
			if x.wide && incoming != 0 {
				if !x.copy {
					return false
				}
				s.high |= x.writes
			}
			s.pc += 4
		}
	}
	return true
}
