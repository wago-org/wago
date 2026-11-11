//go:build arm64

package arm64

import "os"

var descendingRangeEnabled = os.Getenv("WAGO_ARM64_NO_DESCENDING_MEMORY_RANGE") != "1"

// descendingRangeMatch describes a bounded native loop candidate. Each
// iteration loads at p, may exit, stores to p+4, and
// decrements p by four and count by one. The original bounds branches must
// share a trap destination and no branch may enter the middle of the candidate.
type descendingRangeMatch struct {
	start, end, exit, trap      int
	pointer, count, temp, limit Reg
}

func matchDescendingRange(b []byte, start int, targets []uint64) (p descendingRangeMatch, ok bool) {
	const words = 12
	if start < 0 || start+words*4 > len(b) {
		return
	}
	w := func(i int) uint32 { return rdWord(b, start+i*4) }
	a := w(0)
	if a&0xffe0fc1f != 0xeb00001f {
		return
	}
	pointer, limit := Reg(a>>5&31), Reg(a>>16&31)
	if pointer == SP || limit == SP || pointer == limit || pointer == linMemReg || limit == linMemReg {
		return
	}
	if w(1)&0xff00001f != 0x54000008 {
		return
	}
	trap, t := branchTarget(start+4, w(1))
	if !t {
		return
	}
	load := w(2)
	if load&0xffe0fc00 != 0xb8606800 || Reg(load>>5&31) != linMemReg || Reg(load>>16&31) != pointer {
		return
	}
	value := Reg(load & 31)
	if value == pointer || value == limit || value == SP || value == linMemReg {
		return
	}
	cmp := w(3)
	if cmp&0xffe0fc1f != 0x6b00001f || Reg(cmp>>5&31) != value {
		return
	}
	rhs := Reg(cmp >> 16 & 31)
	if rhs == SP || rhs == pointer || rhs == value {
		return
	}
	if w(4)&0xff00001f != 0x54000009 {
		return
	}
	exit, e := branchTarget(start+16, w(4))
	if !e || exit < start+words*4 || exit >= len(b) {
		return
	}
	add := w(5)
	if add&0xffc00000 != 0x11000000 || add>>10&4095 != 4 || Reg(add>>5&31) != pointer {
		return
	}
	temp := Reg(add & 31)
	if temp == SP || temp == pointer || temp == value || temp == limit || temp == rhs || temp == linMemReg {
		return
	}
	if w(6) != 0xeb00001f|uint32(limit)<<16|uint32(temp)<<5 || w(7)&0xff00001f != 0x54000008 {
		return
	}
	trap2, t2 := branchTarget(start+28, w(7))
	if !t2 || trap != trap2 || trap >= start && trap < start+words*4 {
		return
	}
	st := w(8)
	if st&0xffe0fc00 != 0xb8206800 || Reg(st>>5&31) != linMemReg || Reg(st>>16&31) != temp || Reg(st&31) != value {
		return
	}
	if w(9) != 0x51001000|uint32(pointer)<<5|uint32(pointer) {
		return
	}
	sub := w(10)
	if sub&0xffc00000 != 0x71000000 || sub>>10&4095 != 1 {
		return
	}
	count := Reg(sub & 31)
	if Reg(sub>>5&31) != count || count == SP || count == pointer || count == temp || count == value || count == limit || count == rhs || count == linMemReg {
		return
	}
	if w(11)&0xff00001f != 0x54000001 {
		return
	}
	back, valid := branchTarget(start+44, w(11))
	if !valid || back != start {
		return
	}
	for pc := start + 4; pc < start+words*4; pc += 4 {
		if branchTargeted(targets, pc) {
			return
		}
	}
	return descendingRangeMatch{start: start, end: start + words*4, exit: exit, trap: trap, pointer: pointer, count: count, temp: temp, limit: limit}, true
}

// finalizeDescendingMemoryRange versions only fully matched loops. A
// guard failure runs the original bounds-checked loop and never traps early.
func (f *fn) finalizeDescendingMemoryRange(entries ...int) {
	if !f.hasLoop || !f.descendingRangeCandidate || f.guardMode || f.threadedMemory0 || f.memoryAddr64(0) || f.moduleEH || f.interruptible || f.opaqueFragments || profileEnabled || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || f.memLimitReg == regNone || f.memLimitExtent != 4 {
		return
	}
	n := len(f.a.B) &^ 3
	if n > 8<<10 {
		return
	}
	// Most loops cannot match. Avoid building a function-wide branch target set
	// unless the indispensable indexed-load/compare pair is present.
	possible := false
	for pc := 8; pc+8 <= n; pc += 4 {
		if rdWord(f.a.B, pc)&0xffe0fc00 == 0xb8606800 && rdWord(f.a.B, pc+4)&0xffe0fc1f == 0x6b00001f {
			possible = true
			break
		}
	}
	if !possible {
		return
	}
	var storage [256]uint64
	targets := storage[:((n/4)+63)/64]
	for pc := 0; pc < n; pc += 4 {
		w := rdWord(f.a.B, pc)
		if isIndirectBranch(w) {
			return
		}
		if to, ok := branchTarget(pc, w); ok {
			branchTargetAdd(targets, to, n)
		}
	}
	versions := 0
	for start := 0; start+48 <= n && versions < 2; start += 4 {
		p, ok := matchDescendingRange(f.a.B[:n], start, targets)
		if !ok || p.limit != f.memLimitReg {
			continue
		}
		unsafe := false
		for _, e := range entries {
			if e > p.start && e < p.end {
				unsafe = true
			}
		}
		for _, r := range f.relocs {
			if int(r.at) >= p.start && int(r.at) < p.end {
				unsafe = true
			}
		}
		dirty := uint32(1) << uint32(p.temp)
		if unsafe || !spanExitClobbersDeadWith(f.a.B[:n], p.exit, dirty, f.relocs, linearSpanExitWord) || !spanExitClobbersDeadWith(f.a.B[:n], p.end, dirty, f.relocs, linearSpanExitWord) || !spanExitClobbersDeadWith(f.a.B[:n], p.trap, dirty, f.relocs, linearSpanExitWord) {
			continue
		}
		original := append([]byte(nil), f.a.B[p.start:p.end]...)
		sources, sites := f.spanProfileDirectories()
		var saved regallocWriteMask
		if regallocCheckEnabled {
			saved = f.checkTerminalWrites()
		}
		guard := f.a.Len()
		f.a.MovReg32(p.temp, p.pointer)
		f.a.CmpReg64(p.temp, p.pointer)
		narrow := f.a.Bcond(condNE)
		f.a.CmpImm32(p.count, 0)
		z := f.a.Bcond(condE)
		f.a.MovReg32(p.temp, p.count)
		f.a.SubImm64(p.temp, p.temp, 1)
		f.a.LslImm(p.temp, p.temp, 2, false)
		f.a.CmpReg64(p.pointer, p.temp)
		under := f.a.Bcond(condB)
		f.a.AddImm64(p.temp, p.pointer, 4)
		f.a.CmpReg64(p.temp, p.limit)
		over := f.a.Bcond(condA)
		fast := f.a.Len()
		for i := 0; i < 12; i++ {
			if i == 0 || i == 1 || i == 6 || i == 7 {
				continue
			}
			w := rdWord(original, i*4)
			if i == 4 || i == 11 {
				w &^= uint32(0x00ffffe0)
			}
			at := f.a.Len()
			f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
			f.copySpanProfileWord(sources, sites, p.start+i*4, at)
			if i == 4 {
				f.recordPCRelative(at)
				f.patchBranch19(at, p.exit)
			}
			if i == 11 {
				f.recordPCRelative(at)
				f.patchBranch19(at, fast)
			}
		}
		out := f.a.Branch()
		f.recordPCRelative(out)
		f.patchBranch26(out, p.end)
		fallback := f.a.Len()
		w := rdWord(original, 0)
		f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
		f.copySpanProfileWord(sources, sites, p.start, fallback)
		resume := f.a.Branch()
		f.recordPCRelative(resume)
		f.patchBranch26(resume, p.start+4)
		for _, at := range []int{narrow, z, under, over} {
			f.recordPCRelative(at)
			f.patchBranch19(at, fallback)
		}
		wrWord(f.a.B, p.start, 0x14000000)
		f.recordPCRelative(p.start)
		f.patchBranch26(p.start, guard)
		wrWord(f.a.B, p.end-4, rdWord(original, 44)&^uint32(0x00ffffe0))
		f.recordPCRelative(p.end - 4)
		f.patchBranch19(p.end-4, fallback)
		if regallocCheckEnabled {
			f.checkRestoreWrites(saved)
		}
		f.stats.peep("descending-memory-range")
		versions++
		start = p.end - 4
	}
}
