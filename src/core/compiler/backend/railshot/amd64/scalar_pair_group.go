//go:build amd64

package amd64

type scalarSumSwapGroup struct {
	a, b, temp Reg
	wide       bool
	tempFromA  bool
}

// Prove the complete register transfer, including the temporary's final value.
// Exactly three uniform-width MOV/ADD instructions must implement
// (a,b,temp) = (b,a+b,a or b), independently of the old temporary.
func planScalarSumSwapGroup(body []byte, pins regMask) (scalarSumSwapGroup, bool) {
	if _, _, ok := scalarLoopBodyRegisters(body, pins); !ok {
		return scalarSumSwapGroup{}, false
	}
	var ops [3]struct {
		dst, src Reg
		add      bool
	}
	var used regMask
	wide, p := false, 0
	for i := range ops {
		if p == len(body) {
			return scalarSumSwapGroup{}, false
		}
		rex := byte(0)
		if body[p]&0xf0 == 0x40 {
			rex = body[p]
			p++
		}
		if len(body)-p < 2 || body[p+1]>>6 != 3 || i != 0 && wide != (rex&8 != 0) {
			return scalarSumSwapGroup{}, false
		}
		wide = rex&8 != 0
		op, m := body[p], body[p+1]
		p += 2
		r, rm := Reg((m>>3)&7)|Reg((rex>>2)&1)<<3, Reg(m&7)|Reg(rex&1)<<3
		ops[i].dst, ops[i].src = rm, r
		switch op {
		case 0x01, 0x89:
		case 0x03, 0x8b:
			ops[i].dst, ops[i].src = r, rm
		default:
			return scalarSumSwapGroup{}, false
		}
		ops[i].add = op == 0x01 || op == 0x03
		if r == RSP || rm == RSP {
			return scalarSumSwapGroup{}, false
		}
		used = used.add(r).add(rm)
	}
	if p != len(body) {
		return scalarSumSwapGroup{}, false
	}
	var regs [3]Reg
	var index [16]int
	n := 0
	for r := Reg(0); r < 16; r++ {
		if used.has(r) {
			if n == len(regs) {
				return scalarSumSwapGroup{}, false
			}
			regs[n], index[r] = r, n
			n++
		}
	}
	if n != len(regs) {
		return scalarSumSwapGroup{}, false
	}
	var state [3][3]uint8
	for i := range state {
		state[i][i] = 1
	}
	for _, op := range ops {
		d, s := index[op.dst], index[op.src]
		if op.add {
			for j := range state[d] {
				state[d][j] += state[s][j]
			}
		} else {
			state[d] = state[s]
		}
	}
	for ai, a := range regs {
		for bi, b := range regs {
			if ai == bi || !pins.has(a) || !pins.has(b) {
				continue
			}
			ti := 3 - ai - bi
			var nextA, nextB, tempA, tempB [3]uint8
			nextA[bi], nextB[ai], nextB[bi], tempA[ai], tempB[bi] = 1, 1, 1, 1, 1
			if state[ai] == nextA && state[bi] == nextB && (state[ti] == tempA || state[ti] == tempB) {
				return scalarSumSwapGroup{a, b, regs[ti], wide, state[ti] == tempA}, true
			}
		}
	}
	return scalarSumSwapGroup{}, false
}

func (f *fn) emitScalarSumSwapGroup(g scalarSumSwapGroup) {
	// After four steps: a=2*a0+3*b0, b=3*a0+5*b0. Compute
	// s=a0+b0, a=b0+2*s, and b=b0+s before the final addition.
	// The original temporary ends with a3 or b3, not with s.
	f.a.LeaScaledW(g.temp, g.a, g.b, 0, 0, g.wide)
	f.a.LeaScaledW(g.a, g.b, g.temp, 1, 0, g.wide)
	f.a.LeaScaledW(g.b, g.b, g.temp, 0, 0, g.wide)
	source := g.a
	if g.tempFromA {
		source = g.b
	}
	if g.wide {
		f.a.MovReg64(g.temp, source)
	} else {
		f.a.MovRegReg32(g.temp, source)
	}
	f.a.AluRR(0x01, g.b, g.a, g.wide)
	f.stats.peep("scalar-tuple-sum-swap-group4")
}
