//go:build arm64

package arm64

import (
	"os"
	"sort"
)

var affineBoundsLoopEnabled = os.Getenv("WAGO_ARM64_NO_AFFINE_BOUNDS_LOOP") != "1"

// The workspace describes one already-emitted, straight-line loop. Its mutable
// register cells retain only the current affine value, never assignment history.
// Frame reads are invariant because this grammar admits no frame writes.
type affineBoundsValue struct {
	term          [2]uint16 // GP entry value, or 32 + frame byte offset / 8
	n             uint8
	bias          uint32
	valid, narrow bool
}
type affineBoundsStream struct {
	value          affineBoundsValue
	extent, stride uint32
}
type affineBoundsPlan struct {
	start, back    int
	counter        Reg
	guardA, guardB Reg
	iterations     uint32
	streams        [8]affineBoundsStream
	streamN        int
	omit           [32]int // CMP and conditional branch positions
	omitN          int
	deadEndRegs    uint32
}

func affineBoundsAdd(a, b affineBoundsValue) (v affineBoundsValue) {
	if !a.valid || !b.valid || int(a.n)+int(b.n) > 2 {
		return
	}
	v = affineBoundsValue{valid: true, narrow: true, n: a.n + b.n, bias: a.bias + b.bias}
	copy(v.term[:], a.term[:a.n])
	copy(v.term[a.n:], b.term[:b.n])
	if v.n == 2 && v.term[1] < v.term[0] {
		v.term[0], v.term[1] = v.term[1], v.term[0]
	}
	return
}
func (p *affineBoundsPlan) addCheck(v affineBoundsValue, width uint32) bool {
	if !v.valid || p.omitN+2 > len(p.omit) {
		return false
	}
	// Identical symbolic bases can share a widened range, including source
	// unrolling. Modular bias is kept separate from the widened access extent.
	for i := 0; i < p.streamN; i++ {
		g := &p.streams[i]
		if g.value.n == v.n && g.value.term == v.term && g.value.narrow == v.narrow {
			lo, hi := g.value.bias, uint64(g.value.bias)+uint64(g.extent)
			if v.bias < lo {
				lo = v.bias
			}
			if end := uint64(v.bias) + uint64(width); end > hi {
				hi = end
			}
			if hi-uint64(lo) > 0xffffffff {
				return false
			}
			g.value.bias = lo
			g.extent = uint32(hi - uint64(lo))
			return true
		}
	}
	if p.streamN == len(p.streams) {
		return false
	}
	p.streams[p.streamN] = affineBoundsStream{value: v, extent: width}
	p.streamN++
	return true
}

func matchAffineBoundsLoop(b []byte, back int, memSize Reg, targets []uint64, entries []int, relocs []callReloc) (p affineBoundsPlan, ok bool) {
	if back < 20 || back+4 > len(b) || memSize == regNone || memSize == SP {
		return
	}
	last := rdWord(b, back)
	if last&0xff00001f != 0x54000001 {
		return
	}
	start, yes := branchTarget(back, last)
	if !yes || start < 0 || back-start < 36 || back-start > 512 {
		return
	}
	p.start, p.back = start, back
	for at := start + 4; at <= back; at += 4 {
		if branchTargeted(targets, at) {
			return p, false
		}
	}
	for _, entry := range entries {
		if entry > start && entry <= back {
			return p, false
		}
	}
	latch := rdWord(b, back-4)
	if latch&0xffc0001f != 0x7100001f {
		return p, false
	}
	p.counter = Reg(latch >> 5 & 31)
	limit := latch >> 10 & 4095
	if p.counter == SP || p.counter == X16 || p.counter == X17 || limit == 0 {
		return p, false
	}
	var values [31]affineBoundsValue
	for i := range values {
		values[i] = affineBoundsValue{term: [2]uint16{uint16(i)}, n: 1, valid: true}
	}
	for pc := start; pc < back-4; pc += 4 {
		w := rdWord(b, pc)
		rd, rn, rm := Reg(w&31), Reg(w>>5&31), Reg(w>>16&31)
		// Entry guards use the same linear memory and size as every original
		// access. Reject writes to either fixed role, including frame reloads.
		if x, valid := linearSpanExitWord(w); valid && x.writes&(uint32(1)<<uint32(memSize)|uint32(1)<<uint32(linMemReg)) != 0 {
			return p, false
		}
		// Do not borrow either fixed guard scratch register from the original loop.
		if x, valid := linearSpanWord(w); valid && (x.reads|x.writes)&((1<<16)|(1<<17)) != 0 {
			return p, false
		}
		if pc+12 <= back-4 && w&0xffc00000 == 0x91000000 && rd != SP && rn != SP {
			cmp, branch := rdWord(b, pc+4), rdWord(b, pc+8)
			if cmp == 0xeb00001f|uint32(memSize)<<16|uint32(rd)<<5 && branch&0xff00001f == 0x54000008 {
				to, valid := branchTarget(pc+8, branch)
				width := w >> 10 & 4095
				if !valid || to >= start && to <= back || to < 0 || to >= len(b) || width == 0 || !p.addCheck(values[rn], width) {
					return p, false
				}
				p.omit[p.omitN], p.omit[p.omitN+1] = pc+4, pc+8
				p.omitN += 2
				values[rd] = affineBoundsValue{}
				pc += 8
				continue
			}
		}
		if _, branch := branchTarget(pc, w); branch {
			return p, false
		}
		switch {
		case w == nopWord:
		case w&0xffe0fc00 == 0xaa000000 && rn == 31 && rd != SP && rm != SP:
			values[rd] = values[rm]
		case w&0xffe0fc00 == 0x2a000000 && rn == 31 && rd != SP && rm != SP:
			values[rd] = values[rm]
			values[rd].narrow = true
		case w&0xffc00000 == 0x11000000 && rd != SP && rn != SP:
			values[rd] = affineBoundsAdd(values[rn], affineBoundsValue{valid: true, bias: w >> 10 & 4095})
		case w&0xffe0fc00 == 0x0b000000 && rd != SP && rn != SP && rm != SP:
			values[rd] = affineBoundsAdd(values[rn], values[rm])
		case w&0xffc00000 == 0xf9400000 && rn == SP && rd != SP:
			values[rd] = affineBoundsValue{term: [2]uint16{32 + uint16(w>>10&4095)}, n: 1, valid: true}
		case w&0xffe0fc00 == 0xfc606800 || w&0xffe0fc00 == 0xfc206800 || w&0xffe0fc00 == 0xbc606800 || w&0xffe0fc00 == 0xbc206800:
			if rn != linMemReg {
				return p, false
			}
		case w&0xffa0fc00 == 0x1e200800 || w&0xffa0fc00 == 0x1e202800 || w&0xffa0fc00 == 0x1e203800:
			// Scalar FP operations keep their original order and exact semantics.
		default:
			return p, false
		}
	}
	c := values[p.counter]
	if !c.valid || c.n != 1 || c.term[0] != uint16(p.counter) || c.bias == 0 || !c.narrow || limit%c.bias != 0 {
		return p, false
	}
	p.iterations = limit / c.bias
	if p.iterations < 16 || p.iterations > 4096 || p.omitN < 8 {
		return p, false
	}
	nonuniformStride := false
	for i := 0; i < p.streamN; i++ {
		g := &p.streams[i]
		var stride uint64
		for _, term := range g.value.term[:g.value.n] {
			if term >= 32 {
				continue
			}
			v := values[term]
			if !v.valid || v.n != 1 || v.term[0] != term {
				return p, false
			}
			stride += uint64(v.bias)
		}
		span := stride*uint64(p.iterations-1) + uint64(g.extent)
		if stride > 0xffffffff || span > 0xffffffff {
			return p, false
		}
		g.stride = uint32(stride)
		g.extent = uint32(span)
		nonuniformStride = nonuniformStride || stride != 0 && stride != uint64(c.bias)
	}
	// Source vector maps already cover same-stride streams and retain a scalar
	// tail. Wrapping that tail in another dispatch adds work without helping
	// vector execution. Focus scalar span hoisting on independently advancing
	// streams, where the ordinary contiguous-map recipe cannot apply.
	if !nonuniformStride {
		return p, false
	}
	// Scratch entry values must be overwritten before their first read and
	// before any branch or guest memory effect. Both original paths therefore
	// restore these cells before any possible observation, without exit liveness.
	var written, observed, available uint32
	for pc := start; pc < back-4; pc += 4 {
		w := rdWord(b, pc)
		if _, branch := branchTarget(pc, w); branch {
			break
		}
		if w&0xffe0fc00 == 0xfc606800 || w&0xffe0fc00 == 0xfc206800 || w&0xffe0fc00 == 0xbc606800 || w&0xffe0fc00 == 0xbc206800 {
			break
		}
		x, valid := linearSpanExitWord(w)
		if !valid {
			break
		}
		observed |= x.reads &^ written
		available |= x.writes &^ observed
		written |= x.writes
	}
	available &^= uint32(1)<<uint32(p.counter) | uint32(1)<<uint32(memSize) | uint32(1)<<uint32(linMemReg)
	for _, g := range p.streams[:p.streamN] {
		for _, term := range g.value.term[:g.value.n] {
			if term < 32 {
				available &^= uint32(1) << term
			}
		}
	}
	p.guardA, p.guardB = regNone, regNone
	for _, reg := range gpAlloc {
		if available&(uint32(1)<<uint32(reg)) != 0 {
			if p.guardA == regNone {
				p.guardA = reg
			} else {
				p.guardB = reg
				break
			}
		}
	}
	if p.guardB == regNone {
		return p, false
	}
	if !affineBoundsKeepEnds {
		p.deadEndRegs = affineBoundsDeadEndRegs(b, &p, relocs)
	}
	return p, true
}

func (f *fn) emitAffineBoundsValue(v affineBoundsValue, a, z Reg) {
	load := func(reg Reg, term uint16) {
		if term < 32 {
			f.a.MovReg64(reg, Reg(term))
		} else {
			f.ld64(reg, SP, int32(term-32)*8)
		}
	}
	if v.n == 0 {
		f.a.MovImm64(a, uint64(v.bias))
		return
	}
	load(a, v.term[0])
	if v.n == 2 {
		load(z, v.term[1])
		f.a.Add32(a, a, z)
	} else if v.narrow {
		f.a.MovReg32(a, a)
	}
	if v.bias != 0 {
		f.a.MovImm64(z, uint64(v.bias))
		f.a.Add32(a, a, z)
	}
}

// Guards choose an exact scalar clone. They never trap and never reorder memory
// effects; failure enters the original checked body with its original registers.
func (f *fn) finalizeAffineBoundsLoops(entries ...int) {
	if !f.opt(optAffineBoundsLoop) || !f.hasLoop || f.guardMode || f.threadedMemory0 || f.memoryAddr64(0) || f.moduleEH || f.interruptible || f.opaqueFragments || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || f.memSizeReg == regNone {
		return
	}
	n := len(f.a.B) &^ 3
	if n > 64<<10 {
		return
	}
	var targetStorage [256]uint64
	targets := targetStorage[:((n/4)+63)/64]
	for pc := 0; pc < n; pc += 4 {
		w := rdWord(f.a.B, pc)
		if isIndirectBranch(w) {
			return
		}
		if to, yes := branchTarget(pc, w); yes {
			branchTargetAdd(targets, to, n)
		}
	}
	versions := 0
	for back := 20; back < n && versions < 2; back += 4 {
		// Avoid constructing/copying the bounded plan at every instruction.
		// These are necessary conditions already checked by the full matcher.
		if rdWord(f.a.B, back)&0xff00001f != 0x54000001 || rdWord(f.a.B, back-4)&0xffc0001f != 0x7100001f {
			continue
		}
		p, yes := matchAffineBoundsLoop(f.a.B[:n], back, f.memSizeReg, targets, entries, f.relocs)
		if !yes {
			continue
		}
		original := append([]byte{}, f.a.B[p.start:p.back+4]...)
		sources, sites := f.spanProfileDirectories()
		var saved regallocWriteMask
		if regallocCheckEnabled {
			saved = f.checkTerminalWrites()
		}
		guard := f.a.Len()
		var failures [17]int
		nf := 0
		f.a.CmpImm32(p.counter, 0)
		failures[nf] = f.a.Bcond(condNE)
		nf++
		for _, g := range p.streams[:p.streamN] {
			f.emitAffineBoundsValue(g.value, p.guardA, p.guardB)
			f.a.MovImm64(p.guardB, uint64(1<<32)-uint64(g.extent))
			f.a.CmpReg64(p.guardA, p.guardB)
			failures[nf] = f.a.Bcond(condA)
			nf++
			f.a.MovImm64(p.guardB, uint64(g.extent))
			f.a.Add64(p.guardA, p.guardA, p.guardB)
			f.a.CmpReg64(p.guardA, f.memSizeReg)
			failures[nf] = f.a.Bcond(condA)
			nf++
		}
		fast := f.a.Len()
		omit := 0
		removed := 0
		removedEnds := 0
		for old := p.start; old <= p.back; old += 4 {
			if omit < p.omitN && old+4 == p.omit[omit] && p.deadEndRegs&(uint32(1)<<uint32(rdWord(original, old-p.start)&31)) != 0 {
				removed++
				removedEnds++
				continue
			}
			if omit < p.omitN && old == p.omit[omit] {
				omit++
				removed++
				continue
			}
			at := f.a.Len()
			w := rdWord(original, old-p.start)
			if old == p.back {
				w &^= uint32(0x00ffffe0)
				f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
				f.recordPCRelative(at)
				f.patchBranch19(at, fast)
			} else {
				f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
			}
			f.copySpanProfileWord(sources, sites, old, at)
		}
		exit := f.a.Branch()
		f.recordPCRelative(exit)
		f.patchBranch26(exit, p.back+4)
		for range removed {
			f.a.Nop()
		}
		fallback := f.a.Len()
		w := rdWord(original, 0)
		f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
		f.copySpanProfileWord(sources, sites, p.start, fallback)
		resume := f.a.Branch()
		f.recordPCRelative(resume)
		f.patchBranch26(resume, p.start+4)
		for _, at := range failures[:nf] {
			f.recordPCRelative(at)
			f.patchBranch19(at, fallback)
		}
		wrWord(f.a.B, p.start, 0x14000000)
		f.recordPCRelative(p.start)
		f.patchBranch26(p.start, guard)
		wrWord(f.a.B, p.back, rdWord(original, p.back-p.start)&^uint32(0x00ffffe0))
		f.recordPCRelative(p.back)
		f.patchBranch19(p.back, fallback)
		if regallocCheckEnabled {
			f.checkRestoreWrites(saved)
		}
		if removedEnds != 0 {
			f.stats.peep("affine-bounds-end-elide")
		}
		f.stats.peep("affine-bounds-loop")
		versions++
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		sort.Slice(f.stats.SourceRanges, func(i, j int) bool { return f.stats.SourceRanges[i].Offset < f.stats.SourceRanges[j].Offset })
		sort.Slice(f.stats.CodeSites, func(i, j int) bool { return f.stats.CodeSites[i].Offset < f.stats.CodeSites[j].Offset })
	}
}
