//go:build arm64

package arm64

import (
	"math/bits"
	"os"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

var linearLoadSpanEnabled = os.Getenv("WAGO_ARM64_NO_LINEAR_LOAD_SPAN") != "1"
var linearLoadSpanKeepEnds = os.Getenv("WAGO_ARM64_LINEAR_LOAD_SPAN_KEEP_ENDS") == "1"
var linearLoadSpanLayoutOnly = os.Getenv("WAGO_ARM64_LINEAR_LOAD_SPAN_LAYOUT_ONLY") == "1"

type linearLoadSpan struct {
	start, back, second          int
	input, counter, address, end Reg
	stride, lower, extent        uint32
}

func scalarFloatIndexedLoad(w uint32) (size uint32, base, index Reg, ok bool) {
	switch w & 0xffe0fc00 {
	case 0xbc606800:
		size = 4
	case 0xfc606800:
		size = 8
	default:
		return
	}
	return size, Reg(w >> 5 & 31), Reg(w >> 16 & 31), true
}

// The clone contains only admitted arithmetic and ordinary scalar memory ops.
// Bound comparisons are omitted only after a full-span proof; end-address
// writes additionally require a separate proof across the body and every exit.
func linearSpanWord(w uint32) (x guardedIntegerEffects, ok bool) {
	if w&0xffe0fc00 == 0xbc606800 || w&0xffe0fc00 == 0xfc606800 || w&0xffe0fc00 == 0xbc206800 || w&0xffe0fc00 == 0xfc206800 {
		x.reads = (uint32(1)<<(w>>5&31) | uint32(1)<<(w>>16&31)) & 0x7fffffff
		return x, true
	}
	switch w & 0xffc00000 {
	case 0xbd000000, 0xbd400000, 0xfd000000, 0xfd400000:
		x.reads = uint32(1) << (w >> 5 & 31) & 0x7fffffff
		return x, true
	}
	switch w & 0xffa0fc00 {
	case 0x1e200800, 0x1e202800, 0x1e203800:
		return x, true // scalar MUL/ADD/SUB
	}
	return guardedIntegerWord(w)
}

func matchLinearLoadSpan(b []byte, back int, memSize Reg, targets []uint64, entries []int) (g linearLoadSpan, ok bool) {
	n := len(b) &^ 3
	if back < 36 || back+4 > n || memSize == regNone || memSize == SP {
		return
	}
	branch := rdWord(b, back)
	isNE := branch&0xff00001f == 0x54000001
	isNonzero := branch&0xff000000 == 0x35000000
	if !isNE && !isNonzero {
		return
	}
	start, valid := branchTarget(back, branch)
	if !valid || start < 0 || back-start < 36 || back-start > 512 {
		return
	}
	sub := rdWord(b, back-4)
	counter := Reg(sub & 31)
	if sub&0xfffffc00 != 0x71000400 || Reg(sub>>5&31) != counter || counter == SP || isNonzero && Reg(branch&31) != counter {
		return
	}
	first := rdWord(b, start)
	input, address := Reg(first>>5&31), Reg(first&31)
	lower := first >> 10 & 4095
	if first&0xffc00000 != 0x51000000 || lower == 0 {
		return
	}
	width, base, index, valid := scalarFloatIndexedLoad(rdWord(b, start+16))
	if !valid || base != linMemReg || index != address {
		return
	}
	endWord := rdWord(b, start+4)
	end := Reg(endWord & 31)
	if endWord != 0x91000000|width<<10|uint32(address)<<5|uint32(end) || rdWord(b, start+8) != 0xeb00001f|uint32(memSize)<<16|uint32(end)<<5 || rdWord(b, start+12)&0xff00001f != 0x54000008 {
		return
	}
	// These two original definitions kill both guard temporaries before any read.
	if input == SP || input == counter || input == memSize || input == linMemReg || address == end || address == input || address == counter || address == memSize || address == linMemReg || address == SP || end == input || end == counter || end == memSize || end == linMemReg || end == SP {
		return
	}
	update := rdWord(b, back-8)
	stride := update >> 10 & 4095
	if update != 0x11000000|stride<<10|uint32(input)<<5|uint32(input) || stride == 0 || stride&(stride-1) != 0 {
		return
	}
	second := -1
	extent := uint32(0)
	for at := start + 20; at+16 <= back-8; at += 4 {
		size, base, index, valid := scalarFloatIndexedLoad(rdWord(b, at+12))
		if !valid || base != linMemReg || index != input {
			continue
		}
		add := rdWord(b, at)
		tmp := add & 31
		if tmp == 31 || tmp == uint32(input) || tmp == uint32(counter) || tmp == uint32(linMemReg) || tmp == uint32(memSize) {
			continue
		}
		if add != 0x91000000|size<<10|uint32(input)<<5|tmp || rdWord(b, at+4) != 0xeb00001f|uint32(memSize)<<16|tmp<<5 || rdWord(b, at+8)&0xff00001f != 0x54000008 {
			continue
		}
		second, extent = at, size
		break
	}
	if second < 0 {
		return
	}
	if width > lower && width-lower > extent {
		extent = width - lower
	}
	for _, site := range []int{start + 12, second + 8} {
		to, valid := branchTarget(site, rdWord(b, site))
		if !valid || to < 0 || to >= n || to >= start && to <= back {
			return g, false
		}
	}
	flagsValid := false
	for at := start; at <= back; at += 4 {
		if at > start && branchTargeted(targets, at) {
			return
		}
		for _, entry := range entries {
			if entry > start && entry <= back {
				return
			}
		}
		w := rdWord(b, at)
		omit := at == start+8 || at == start+12 || at == second+4 || at == second+8
		if omit {
			flagsValid = false
			continue
		}
		if to, isBranch := branchTarget(at, w); isBranch {
			if !flagsValid {
				return
			}
			if at == back {
				continue
			}
			if w&0xff00001f != 0x54000008 || to >= start && to <= back || to < 0 || to >= n {
				return
			}
			continue
		}
		x, valid := linearSpanWord(w)
		if !valid || x.readsFlags && !flagsValid {
			return
		}
		if x.flags {
			flagsValid = true
		}
		canonicalInput := w == 0x2a0003e0|uint32(input)<<16|uint32(input)
		if x.writes&(uint32(1)<<uint32(input)) != 0 && at != back-8 && !canonicalInput {
			return
		}
		if x.writes&(uint32(1)<<uint32(counter)) != 0 && at != back-4 {
			return
		}
		if x.writes&(uint32(1)<<uint32(linMemReg)|uint32(1)<<uint32(memSize)) != 0 {
			return
		}
	}
	return linearLoadSpan{start, back, second, input, counter, address, end, stride, lower, extent}, true
}

// Failed guards only select the original loop; none emits a trap. The input
// must have the same canonical carrier used by the original full-width checks.
func (f *fn) emitLinearSpanGuard(g linearLoadSpan) [4]int {
	f.a.MovReg32(g.address, g.input)
	f.a.CmpReg64(g.address, g.input)
	upper := f.a.Bcond(condNE)
	f.a.MovReg32(g.end, g.counter)
	zero := f.a.Cbz32(g.end)
	f.a.CmpImm64(g.address, g.lower)
	under := f.a.Bcond(condB)
	f.a.SubImm64(g.end, g.end, 1)
	f.a.AddShifted(g.address, g.address, g.end, uint8(bits.TrailingZeros32(g.stride)), false)
	f.a.AddImm64(g.address, g.address, g.extent)
	f.a.CmpReg64(g.address, f.memSizeReg)
	over := f.a.Bcond(condA)
	return [4]int{upper, zero, under, over}
}

// A non-trapping uint64 guard selects a bounded induction clone. Failure keeps
// the original checked loop, including zero-count bottom-tested semantics and
// partial effects before a later trap. Unproved end-address writes are retained.
func (f *fn) finalizeLinearLoadSpans(entries ...int) {
	if !f.opt(optLinearLoadSpan) || f.guardMode || f.threadedMemory0 || f.memoryAddr64(0) || f.moduleEH || f.interruptible || f.opaqueFragments || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || f.memSizeReg == regNone {
		return
	}
	n := len(f.a.B) &^ 3
	if n > 64<<10 {
		return
	}
	targets := make([]uint64, ((n/4)+63)/64)
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
	for back := 36; back < n && versions < 2; back += 4 {
		g, ok := matchLinearLoadSpan(f.a.B[:n], back, f.memSizeReg, targets, entries)
		if !ok {
			continue
		}
		var c cyclicLoadSpan
		cyclic := false
		if f.opt(optCyclicLoadSpan) {
			c, cyclic = matchCyclicLoadSpan(f.a.B[:n], g, f.memSizeReg)
		}
		omitCyclic := cyclic && !cyclicLoadSpanLayoutOnly
		var store loopStoreSink
		sink := false
		if omitCyclic && !linearLoadSpanLayoutOnly && f.opt(optLoopStoreSink) {
			store, sink = matchLoopStoreSink(f.a.B[:n], g, c, f.memSizeReg)
		}
		livenessC := cyclicLoadSpan{}
		if omitCyclic {
			livenessC = c
		}
		dropEnds := !linearLoadSpanKeepEnds && spanEndsDeadCyclic(f.a.B[:n], g, f.relocs, livenessC)
		original := append([]byte{}, f.a.B[g.start:g.back+4]...)
		originalSources, originalSites := f.spanProfileDirectories()
		guard := f.a.Len()
		var saved regallocWriteMask
		if regallocCheckEnabled {
			saved = f.checkTerminalWrites()
		}
		// This appended entry has its own proven register context; it does not run
		// in the emitter's final epilogue context.
		linearFailures := f.emitLinearSpanGuard(g)
		failures := append([]int{}, linearFailures[:]...)
		if cyclic {
			cyclicFailures := f.emitCyclicSpanGuard(g, c)
			failures = append(failures, cyclicFailures[:]...)
		}
		if sink {
			storeFailures := f.emitLoopStoreSinkGuard(g, c, store)
			failures = append(failures, storeFailures[:]...)
		}
		for f.a.Len()%32 != 0 {
			f.a.Nop()
		}
		fast := f.a.Len()
		put := func(w uint32) int {
			at := f.a.Len()
			f.a.B = append(f.a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
			return at
		}
		removed := 0
		for old := g.start; old <= g.back; old += 4 {
			if sink && !loopStoreSinkLayoutOnly && (old == store.first || old == store.second) {
				removed++
				continue
			}
			if !linearLoadSpanLayoutOnly && (old == g.start+8 || old == g.start+12 || old == g.second+4 || old == g.second+8 || omitCyclic && (old == c.check+4 || old == c.check+8) || dropEnds && (old == g.start+4 || old == g.second || omitCyclic && old == c.check)) {
				removed++
				continue
			}
			w := rdWord(original, old-g.start)
			at := f.a.Len()
			if to, branch := branchTarget(old, w); branch {
				put(w &^ uint32(0x00ffffe0))
				f.recordPCRelative(at)
				if old == g.back {
					to = fast
				}
				f.patchBranch19(at, to)
			} else {
				put(w)
			}
			f.copySpanProfileWord(originalSources, originalSites, old, at)
		}
		if sink {
			for _, site := range []int{store.first, store.second} {
				at := f.a.Len()
				if loopStoreSinkLayoutOnly {
					put(nopWord)
				} else {
					put(rdWord(original, site-g.start))
					f.copySpanProfileWord(originalSources, originalSites, site, at)
				}
			}
		}
		exit := f.a.Branch()
		f.recordPCRelative(exit)
		f.patchBranch26(exit, g.back+4)
		// Keep the clone/fallback extent identical to the checked-layout oracle.
		for range removed {
			f.a.Nop()
		}
		fallback := f.a.Len()
		put(rdWord(original, 0))
		f.copySpanProfileWord(originalSources, originalSites, g.start, fallback)
		resume := f.a.Branch()
		f.recordPCRelative(resume)
		f.patchBranch26(resume, g.start+4)
		for _, site := range failures {
			f.recordPCRelative(site)
			f.patchBranch19(site, fallback)
		}
		wrWord(f.a.B, g.start, 0x14000000)
		f.recordPCRelative(g.start)
		f.patchBranch26(g.start, guard)
		wrWord(f.a.B, g.back, rdWord(original, g.back-g.start)&^uint32(0x00ffffe0))
		f.recordPCRelative(g.back)
		f.patchBranch19(g.back, fallback)
		if regallocCheckEnabled {
			f.checkRestoreWrites(saved)
		}
		f.stats.peep("linear-load-span")
		if sink {
			f.stats.peep("loop-store-sink")
		}
		if cyclic {
			f.stats.peep("cyclic-load-span")
		}
		versions++
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		sort.Slice(f.stats.SourceRanges, func(i, j int) bool { return f.stats.SourceRanges[i].Offset < f.stats.SourceRanges[j].Offset })
		sort.Slice(f.stats.CodeSites, func(i, j int) bool { return f.stats.CodeSites[i].Offset < f.stats.CodeSites[j].Offset })
	}
}

func (f *fn) spanProfileDirectories() ([]shared.NativeSourceRange, []shared.NativeCodeSite) {
	if !profileEnabled || f.stats == nil || !f.stats.RecordSources {
		return nil, nil
	}
	return append([]shared.NativeSourceRange{}, f.stats.SourceRanges...), append([]shared.NativeCodeSite{}, f.stats.CodeSites...)
}
func (f *fn) copySpanProfileWord(source []shared.NativeSourceRange, sites []shared.NativeCodeSite, old, new int) {
	if !profileEnabled || f.stats == nil || !f.stats.RecordSources {
		return
	}
	for _, r := range source {
		if r.Offset <= uint64(old) && r.Offset+r.Size >= uint64(old+4) {
			r.Offset, r.Size = uint64(new), 4
			f.stats.SourceRanges = append(f.stats.SourceRanges, r)
			break
		}
	}
	for _, r := range sites {
		if r.Offset <= uint64(old) && r.Offset+r.Size >= uint64(old+4) {
			r.Offset, r.Size = uint64(new), 4
			f.stats.CodeSites = append(f.stats.CodeSites, r)
			break
		}
	}
}

// Exit analysis also understands ordinary frame loads/stores and unwind SP
// copies. Unknown instructions and unresolved call/tail relocations decline.
func linearSpanExitWord(w uint32) (x guardedIntegerEffects, ok bool) {
	if x, ok = linearSpanWord(w); ok {
		return
	}
	if (w&0x3b000000 == 0x39000000 || w&0x3b200c00 == 0x38000000) && w&(1<<26) == 0 && w>>22&3 <= 1 {
		x = guardedIntegerEffects{reads: uint32(1) << (w >> 5 & 31) & 0x7fffffff}
		if w&(1<<22) != 0 {
			x.writes = uint32(1) << (w & 31) & 0x7fffffff
		} else {
			x.reads |= uint32(1) << (w & 31) & 0x7fffffff
		}
		return x, true
	}
	if w&0xffc00000 == 0x91000000 { // ADD-immediate, including an unwind SP copy.
		return guardedIntegerEffects{reads: uint32(1) << (w >> 5 & 31) & 0x7fffffff, writes: uint32(1) << (w & 31) & 0x7fffffff}, true
	}
	return x, false
}

func spanExitClobbersDead(b []byte, pc int, dirty uint32, relocs []callReloc) bool {
	return spanExitClobbersDeadWith(b, pc, dirty, relocs, linearSpanExitWord)
}

func spanExitClobbersDeadWith(b []byte, pc int, dirty uint32, relocs []callReloc, effects func(uint32) (guardedIntegerEffects, bool)) bool {
	type state struct {
		pc    int
		dirty uint32
	}
	var queue [64]state
	queue[0] = state{pc, dirty}
	count, head, steps := 1, 0, 0
	for head < count {
		s := queue[head]
		head++
		for s.dirty != 0 {
			steps++
			if steps > 512 || s.pc < 0 || s.pc+4 > len(b) {
				return false
			}
			for _, r := range relocs {
				if int(r.at) == s.pc {
					return false
				}
			}
			w := rdWord(b, s.pc)
			if w == 0xd65f03c0 {
				// These original scratch writes already belong to the callee's clobber
				// surface. Result and fixed ABI registers cannot be discarded at RET.
				if s.dirty & ^uint32(0xfe00) != 0 {
					return false
				}
				break
			}
			if to, branch := branchTarget(s.pc, w); branch {
				if w&0xfc000000 == 0x94000000 || to < 0 || to >= len(b) {
					return false
				}
				if (w&0x7e000000 == 0x34000000 || w&0x7e000000 == 0x36000000) && s.dirty&(uint32(1)<<(w&31)&0x7fffffff) != 0 {
					return false
				}
				seen := false
				for _, prev := range queue[:count] {
					if prev.pc == to && prev.dirty == s.dirty {
						seen = true
						break
					}
				}
				if !seen {
					if count == len(queue) {
						return false
					}
					queue[count] = state{to, s.dirty}
					count++
				}
				if w&0xfc000000 == 0x14000000 {
					break
				}
				s.pc += 4
				continue
			}
			x, ok := effects(w)
			if !ok || x.reads&s.dirty != 0 {
				return false
			}
			s.dirty &^= x.writes
			s.pc += 4
		}
	}
	return true
}

func spanEndsDead(b []byte, g linearLoadSpan, relocs []callReloc) bool {
	return spanEndsDeadCyclic(b, g, relocs, cyclicLoadSpan{})
}

func spanEndsDeadCyclic(b []byte, g linearLoadSpan, relocs []callReloc, c cyclicLoadSpan) bool {
	ends := uint32(1)<<uint32(g.end) | uint32(1)<<(rdWord(b, g.second)&31)
	if c.extent != 0 {
		ends |= uint32(1) << uint32(c.end)
	}
	dirty := ends
	for pc := g.start; pc <= g.back; pc += 4 {
		if pc == g.start+8 || pc == g.start+12 || pc == g.second+4 || pc == g.second+8 || c.extent != 0 && (pc == c.check+4 || pc == c.check+8) {
			continue
		}
		w := rdWord(b, pc)
		if pc == g.start+4 || pc == g.second || c.extent != 0 && pc == c.check {
			dirty |= uint32(1) << (w & 31)
			continue
		}
		if to, branch := branchTarget(pc, w); branch {
			if pc == g.back {
				return spanExitClobbersDead(b, g.back+4, dirty, relocs)
			}
			if !spanExitClobbersDead(b, to, dirty, relocs) {
				return false
			}
			continue
		}
		x, ok := linearSpanWord(w)
		if !ok || x.reads&dirty != 0 {
			return false
		}
		dirty &^= x.writes
	}
	return false
}
