//go:build arm64

package arm64

import "os"

var cyclicLoadSpanEnabled = os.Getenv("WAGO_ARM64_NO_CYCLIC_LOAD_SPAN") != "1"
var cyclicLoadSpanLayoutOnly = os.Getenv("WAGO_ARM64_CYCLIC_LOAD_SPAN_LAYOUT_ONLY") == "1"

type cyclicLoadSpan struct {
	update, check                   int
	phase, step, limit, base, index Reg
	end                             Reg
	shift                           uint8
	extent                          uint32
}

// Match a single conditional subtract after a nonnegative cyclic increment.
// The linear matcher has already excluded foreign entries and unknown effects.
func matchCyclicLoadSpan(b []byte, g linearLoadSpan, memSize Reg) (cyclicLoadSpan, bool) {
	for at := g.start + 20; at+28 <= g.back-8; at += 4 {
		add, sub, sel, idx := rdWord(b, at), rdWord(b, at+4), rdWord(b, at+8), rdWord(b, at+12)
		phase, step, limit, tmp := Reg(add&31), Reg(add>>16&31), Reg(sub>>16&31), Reg(sub&31)
		base, index := Reg(idx>>5&31), Reg(idx&31)
		shift := uint8(idx >> 10 & 63)
		if add != 0x0b000000|uint32(step)<<16|uint32(phase)<<5|uint32(phase) ||
			sub != 0x6b000000|uint32(limit)<<16|uint32(phase)<<5|uint32(tmp) ||
			sel != 0x1a800000|uint32(tmp)<<16|uint32(condL)<<12|uint32(phase)<<5|uint32(phase) ||
			shift > 4 || idx != 0x0b000000|uint32(phase)<<16|uint32(shift)<<10|uint32(base)<<5|uint32(index) {
			continue
		}
		// Keep invariant guard inputs distinct from all early guard clobbers and
		// loop induction state. Require the five cyclic roles to be distinct.
		used := uint32(0)
		valid := true
		for _, reg := range []Reg{phase, step, limit, base, index} {
			bit := uint32(1) << uint32(reg)
			if reg == SP || reg == g.input || reg == g.counter || reg == g.address || reg == g.end || reg == linMemReg || reg == memSize || used&bit != 0 {
				valid = false
			}
			used |= bit
		}
		if !valid || tmp == SP || used&(uint32(1)<<uint32(tmp)) != 0 || tmp == g.input || tmp == g.counter || tmp == linMemReg || tmp == memSize {
			continue
		}
		check := at + 16
		endWord := rdWord(b, check)
		end, extent := Reg(endWord&31), endWord>>10&4095
		if extent == 0 || end == SP || used&(uint32(1)<<uint32(end)) != 0 || end == g.input || end == g.counter || end == memSize || end == linMemReg ||
			endWord != 0x91000000|extent<<10|uint32(index)<<5|uint32(end) ||
			rdWord(b, check+4) != 0xeb00001f|uint32(memSize)<<16|uint32(end)<<5 || rdWord(b, check+8)&0xff00001f != 0x54000008 {
			continue
		}
		invariant := uint32(1)<<uint32(step) | uint32(1)<<uint32(limit) | uint32(1)<<uint32(base)
		flags := false
		for pc := g.start; pc <= g.back; pc += 4 {
			if pc == g.start+8 || pc == g.start+12 || pc == g.second+4 || pc == g.second+8 || pc == check+4 || pc == check+8 {
				flags = false
				continue
			}
			w := rdWord(b, pc)
			if _, branch := branchTarget(pc, w); branch {
				if !flags {
					valid = false
					break
				}
				continue
			}
			x, ok := linearSpanWord(w)
			if !ok || x.readsFlags && !flags || x.writes&invariant != 0 ||
				x.writes&(uint32(1)<<uint32(phase)) != 0 && pc != at && pc != at+8 ||
				x.writes&(uint32(1)<<uint32(index)) != 0 && pc != at+12 {
				valid = false
				break
			}
			if x.flags {
				flags = true
			}
		}
		if valid {
			return cyclicLoadSpan{at, check, phase, step, limit, base, index, end, shift, extent}, true
		}
	}
	return cyclicLoadSpan{}, false
}

// For 1 <= limit <= 2^30 and step <= limit, phase+step fits signed i32.
// The unchanged signed conditional subtract then keeps phase in [0,limit).
// A uint64 table-span test proves each original full-width check succeeds.
func (f *fn) emitCyclicSpanGuard(g linearLoadSpan, c cyclicLoadSpan) [4]int {
	a, z := g.address, g.end
	f.a.MovReg32(a, c.limit)
	f.a.SubImm64(a, a, 1)
	f.a.MovImm64(z, 1<<30)
	f.a.CmpReg64(a, z)
	limit := f.a.Bcond(condAE)
	f.a.CmpReg32(c.step, c.limit)
	step := f.a.Bcond(condA)
	f.a.CmpReg32(c.phase, c.limit)
	phase := f.a.Bcond(condAE)
	f.a.MovReg32(z, c.base)
	f.a.AddShifted(z, z, a, c.shift, false)
	f.a.AddImm64(z, z, c.extent)
	f.a.CmpReg64(z, f.memSizeReg)
	end := f.a.Bcond(condA)
	return [4]int{limit, step, phase, end}
}
