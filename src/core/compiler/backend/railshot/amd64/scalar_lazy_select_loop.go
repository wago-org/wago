//go:build amd64

package amd64

type scalarLoopLEA struct {
	end, modRM int
	dst        Reg
	wide       bool
	reads      regMask
}

// Decode only complete, position-independent register LEAs already accepted by
// the scalar loop proof. Address operands are arithmetic, never memory loads.
func decodeScalarLoopLEA(b []byte) (scalarLoopLEA, bool) {
	var p scalarLoopLEA
	op := 0
	if len(b) > 0 && b[0]&0xf0 == 0x40 {
		op++
	}
	if op+2 > len(b) || b[op] != 0x8d {
		return p, false
	}
	p.modRM = op + 1
	m := b[p.modRM]
	mod := m >> 6
	if mod == 3 || mod == 0 && m&7 == 5 {
		return p, false
	}
	end := p.modRM + 1
	if m&7 == 4 {
		if end == len(b) || mod == 0 && b[end]&7 == 5 {
			return p, false
		}
		end++
	}
	if mod == 1 {
		end++
	}
	if mod == 2 {
		end += 4
	}
	if end > len(b) {
		return p, false
	}
	reads, _, ok := scalarLoopBodyRegisters(b[:end], 0)
	if !ok {
		return p, false
	}
	p.end, p.reads = end, reads
	p.dst = Reg((m >> 3) & 7)
	if op != 0 {
		p.dst |= Reg((b[0]>>2)&1) << 3
		p.wide = b[0]&8 != 0
	}
	return p, true
}

// Rewrite a LEA's result register, retaining its address, width and full signed
// displacement. Replacing REX.R is necessary when moving an extended result to
// a low register; setting REX.R alone would leave the old destination selected.
func retargetScalarLoopLEA(b []byte, p scalarLoopLEA, dst Reg, out []byte) int {
	n := 0
	if p.modRM == 2 {
		out[0] = b[0] &^ 4
		if dst >= 8 {
			out[0] |= 4
		}
		n = 1
	} else if dst >= 8 {
		out[0] = 0x44
		n = 1
	}
	out[n] = 0x8d
	n++
	out[n] = (b[p.modRM] & 0xc7) | byte(dst&7)<<3
	n++
	n += copy(out[n:], b[p.modRM+1:p.end])
	return n
}

// Only a dead scratch result may disappear when the unselected LEA is skipped.
// Persistent local/global caches and owners live across the latch are excluded.
func (f *fn) scalarLoopLiveRegisters() regMask {
	live := f.pinned.union(f.pinnedLocalMask).union(f.reserved).add(RBX).add(RSP).add(RBP)
	for _, local := range f.locals {
		if !local.isFloat && local.reg != regNone {
			live = live.add(local.reg)
		}
	}
	for _, state := range f.globalReg {
		if r := globalRegValue(state); r != regNone {
			live = live.add(r)
		}
	}
	for r, user := range f.regUser {
		if user != nil {
			live = live.add(Reg(r))
		}
	}
	return live
}

// Two independent leading LEAs can remain lazy when their sole consumer is a
// register selection. Moving them past a read-only CMP/TEST preserves flags;
// either arm then defines the same result, including CMOV32's upper-half clear.
// All checks, encoded lengths and branch distances precede output mutation.
func scalarLazySelectBranchBody(body, out []byte, live regMask) (int, bool) {
	var storage [16]scalarLoopSelect
	sites := storage[:0]
	if _, _, _, ok := scalarLoopBodyEffectsWithSelects(body, 0, &sites); !ok || len(sites) != 1 {
		return 0, false
	}
	first, ok := decodeScalarLoopLEA(body)
	if !ok {
		return 0, false
	}
	second, ok := decodeScalarLoopLEA(body[first.end:])
	if !ok {
		return 0, false
	}
	prefixEnd := first.end + second.end
	at := sites[0].at
	op := at
	if body[op]&0xf0 == 0x40 {
		op++
	}
	rex := byte(0)
	if op != at {
		rex = body[at]
	}
	m := body[op+2]
	dst := Reg((m>>3)&7) | Reg((rex>>2)&1)<<3
	src := Reg(m&7) | Reg(rex&1)<<3
	wide := rex&8 != 0
	pair := maskOf(dst, src)
	if dst == src || live&pair != 0 || first.wide != wide || second.wide != wide || first.reads&pair != 0 || second.reads&pair != 0 {
		return 0, false
	}
	b := second
	aBytes, bBytes := body[:first.end], body[first.end:prefixEnd]
	if first.dst == src && second.dst == dst {
		b = first
		aBytes, bBytes = bBytes, aBytes
	} else if first.dst != dst || second.dst != src {
		return 0, false
	}
	if prefixEnd >= at {
		return 0, false
	}
	var conditionSites [16]scalarLoopSelect
	empty := conditionSites[:0]
	cond := body[prefixEnd:at]
	reads, writes, flags, ok := scalarLoopBodyEffectsWithSelects(cond, 0, &empty)
	if !ok || len(empty) != 0 || writes != 0 || !flags || reads&pair != 0 {
		return 0, false
	}
	tail := body[op+3:]
	reads, _, ok = scalarLoopBodyRegisters(tail, 0)
	if !ok || reads.has(src) {
		return 0, false
	}
	var retargeted [9]byte
	bLen := retargetScalarLoopLEA(bBytes, b, dst, retargeted[:])
	needed := len(cond) + 2 + bLen + 2 + len(aBytes) + len(tail)
	if needed > len(out) || bLen+2 > 127 || len(aBytes) > 127 {
		return 0, false
	}
	n := copy(out, cond)
	out[n], out[n+1] = 0x70|((body[op+1]&15)^1), byte(bLen+2)
	n += 2
	n += copy(out[n:], retargeted[:bLen])
	out[n], out[n+1] = 0xeb, byte(len(aBytes))
	n += 2
	n += copy(out[n:], aBytes)
	n += copy(out[n:], tail)
	return n, true
}
