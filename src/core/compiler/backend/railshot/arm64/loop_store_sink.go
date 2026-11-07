//go:build arm64

package arm64

import (
	"math/bits"
	"os"
)

var loopStoreSinkEnabled = os.Getenv("WAGO_ARM64_NO_LOOP_STORE_SINK") != "1"
var loopStoreSinkLayoutOnly = os.Getenv("WAGO_ARM64_LOOP_STORE_SINK_LAYOUT_ONLY") == "1"

type loopStoreSink struct {
	first, second   int
	output, address Reg
	width           uint32
}

// Every memory read must belong to a guarded input/table envelope. Only the
// two adjacent output stores are permitted; no other exit may skip their flush.
func matchLoopStoreSink(b []byte, g linearLoadSpan, c cyclicLoadSpan, memSize Reg) (s loopStoreSink, ok bool) {
	if c.extent == 0 {
		return
	}
	s.first, s.second = -1, -1
	var firstFP, secondFP Reg
	for pc := g.start; pc <= g.back; pc += 4 {
		w := rdWord(b, pc)
		if _, branch := branchTarget(pc, w); branch {
			if pc != g.start+12 && pc != g.second+8 && pc != c.check+8 && pc != g.back {
				return s, false
			}
			continue
		}
		switch w & 0xffe0fc00 {
		case 0xbc206800, 0xfc206800: // scalar indexed store, output offset0
			if s.second >= 0 || s.first < 0 || Reg(w>>5&31) != linMemReg {
				return s, false
			}
			width := uint32(4)
			if w>>30&1 != 0 {
				width = 8
			}
			if width != s.width || Reg(w>>16&31) != s.output {
				return s, false
			}
			s.second, secondFP = pc, Reg(w&31)
			continue
		case 0xbc606800, 0xfc606800:
			width, base, index, _ := scalarFloatIndexedLoad(w)
			if base != linMemReg {
				return s, false
			}
			if pc == g.start+16 && index == g.address {
				continue
			}
			if pc == g.second+12 && index == g.input {
				continue
			}
			if index == c.index && pc > c.check+8 && width <= c.extent {
				continue
			}
			return s, false
		}
		switch w & 0xffc00000 {
		case 0xbd000000, 0xfd000000: // scalar immediate store, adjacent output word
			if s.first >= 0 || pc < g.start+4 {
				return s, false
			}
			width := uint32(4)
			if w>>30&1 != 0 {
				width = 8
			}
			if (w>>10&4095)*width != width {
				return s, false
			}
			addr := Reg(w >> 5 & 31)
			add := rdWord(b, pc-4)
			out := Reg(add >> 16 & 31)
			if add != 0x8b000000|uint32(out)<<16|uint32(linMemReg)<<5|uint32(addr) {
				return s, false
			}
			for _, r := range []Reg{g.input, g.counter, g.address, g.end, memSize, linMemReg, SP, c.phase, c.step, c.limit, c.base, c.index} {
				if out == r {
					return s, false
				}
			}
			if addr == out || addr == g.input || addr == g.counter || addr == g.address || addr == g.end || addr == memSize || addr == linMemReg || addr == SP {
				return s, false
			}
			s.first, s.output, s.address, s.width, firstFP = pc, out, addr, width, Reg(w&31)
			continue
		case 0xbd400000, 0xfd400000: // scalar immediate load through a table address
			width := uint32(4)
			if w>>30&1 != 0 {
				width = 8
			}
			if pc <= c.check+8 || (w>>10&4095)*width+width > c.extent {
				return s, false
			}
			base := Reg(w >> 5 & 31)
			if rdWord(b, pc-4) != 0x8b000000|uint32(c.index)<<16|uint32(linMemReg)<<5|uint32(base) {
				return s, false
			}
		}
	}
	if s.first < 0 || s.second <= s.first || firstFP == secondFP {
		return s, false
	}
	for pc := g.start; pc <= g.back; pc += 4 {
		w := rdWord(b, pc)
		if _, branch := branchTarget(pc, w); branch {
			continue
		}
		x, valid := linearSpanWord(w)
		canonicalOutput := w == 0x2a0003e0|uint32(s.output)<<16|uint32(s.output)
		if !valid || x.writes&(uint32(1)<<uint32(s.output)) != 0 && !canonicalOutput {
			return s, false
		}
		if pc > s.first {
			if x.writes&(uint32(1)<<uint32(s.address)) != 0 || x.reads&(uint32(1)<<uint32(s.address)) != 0 {
				return s, false
			}
		}
		// Scalar FP loads and ALU instructions write their low register/lane. Stores
		// do not. Wider scalar definitions of the same register also decline.
		fpWrite := w&0xffe0fc00 == 0xbc606800 || w&0xffe0fc00 == 0xfc606800 || w&0xffc00000 == 0xbd400000 || w&0xffc00000 == 0xfd400000
		switch w & 0xffa0fc00 {
		case 0x1e200800, 0x1e202800, 0x1e203800:
			fpWrite = true
		}
		if fpWrite && (pc > s.first && Reg(w&31) == firstFP || pc > s.second && Reg(w&31) == secondFP) {
			return s, false
		}
	}
	return s, true
}

// These checks follow the successful linear/cyclic guards. Touching endpoints
// are disjoint; overlapping conservative envelopes select the checked fallback.
func (f *fn) emitLoopStoreSinkGuard(g linearLoadSpan, c cyclicLoadSpan, s loopStoreSink) [4]int {
	a, z := g.address, g.end
	f.a.MovReg32(a, s.output)
	f.a.CmpReg64(a, s.output)
	carrier := f.a.Bcond(condNE)
	f.a.AddImm64(z, a, 2*s.width)
	f.a.CmpReg64(z, f.memSizeReg)
	extent := f.a.Bcond(condA)
	f.a.SubImm64(a, g.input, g.lower)
	f.a.CmpReg64(z, a)
	beforeInput := f.a.Bcond(condBE)
	f.a.MovReg32(a, g.counter)
	f.a.SubImm64(a, a, 1)
	f.a.AddShifted(a, g.input, a, uint8(bits.TrailingZeros32(g.stride)), false)
	f.a.AddImm64(a, a, g.extent)
	f.a.CmpReg64(s.output, a)
	input := f.a.Bcond(condB)
	f.recordPCRelative(beforeInput)
	f.patchBranch19(beforeInput, f.a.Len())
	f.a.MovReg32(a, c.base)
	f.a.CmpReg64(z, a)
	beforeTable := f.a.Bcond(condBE)
	f.a.MovReg32(a, c.limit)
	f.a.SubImm64(a, a, 1)
	f.a.MovReg32(z, c.base)
	f.a.AddShifted(z, z, a, c.shift, false)
	f.a.AddImm64(z, z, c.extent)
	f.a.CmpReg64(s.output, z)
	table := f.a.Bcond(condB)
	f.recordPCRelative(beforeTable)
	f.patchBranch19(beforeTable, f.a.Len())
	return [4]int{carrier, extent, input, table}
}
