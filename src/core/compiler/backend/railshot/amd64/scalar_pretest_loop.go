//go:build amd64

package amd64

import (
	"encoding/binary"
)

// tryScalarPretestLoop retains every iteration's original test and observable
// register updates. Only the final copy takes a backward edge; earlier copies
// fall through. The sole exit must be an unresolved, empty outer block edge.
func (f *fn) tryScalarPretestLoop(fr *ctrlFrame) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || f.makesCalls || f.usesCalls || f.interruptible || profileEnabled ||
		f.stats != nil && f.stats.RecordSources || f.moduleEH || f.gcFrameRoots != nil ||
		compactNativePolicy(f.policy) || len(f.customInstructions) != 0 ||
		fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || f.depth() != fr.height {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start < 10 || end-start > 64 {
		return false
	}
	budget := f.newScalarLoopMetadataBudget()
	if !budget.take(len(f.relocs)) {
		return false
	}
	for _, reloc := range f.relocs {
		if int(reloc.at) >= start && int(reloc.at) < end {
			return false
		}
	}
	loopIndex := -1
	for i := range f.ctrl {
		if !budget.take(1) {
			return false
		}
		if &f.ctrl[i] == fr {
			loopIndex = i
			break
		}
	}
	if loopIndex < 0 {
		return false
	}
	var owner *ctrlFrame
	exit := -1
	pairedExit := false
	for i := 0; i < loopIndex; i++ {
		if !budget.take(1) {
			return false
		}
		candidate := &f.ctrl[i]
		if candidate.kind != cfBlock && candidate.kind != cfIf || candidate.branchN != 0 {
			continue
		}
		first, second, more := f.frameEndSites(candidate)
		if !budget.take(2 + len(more)) {
			return false
		}
		visit := func(packed uint32) bool {
			if packed == 0 {
				return true
			}
			site := int(packed - 1)
			if site < start || site >= end {
				return true
			}
			if owner != nil || site < start+2 || site+4 > end || binary.LittleEndian.Uint32(f.a.B[site:]) != 0 {
				return false
			}
			direct := f.a.B[site-2] == 0x0f && f.a.B[site-1]&0xf0 == 0x80
			// Before post-assembly branch folding, a br_if uses
			// Jcc(!exit) +5; JMP unresolved-owner; body. Verify that
			// exact empty-edge form rather than interpreting an unknown jump.
			paired := site >= start+7 && f.a.B[site-1] == 0xe9 && f.a.B[site-7] == 0x0f &&
				f.a.B[site-6]&0xf0 == 0x80 && binary.LittleEndian.Uint32(f.a.B[site-5:]) == 5
			if !direct && !paired {
				return false
			}
			owner, exit, pairedExit = candidate, site, paired
			return true
		}
		if !visit(first) || !visit(second) {
			return false
		}
		for _, site := range more {
			if !visit(site) {
				return false
			}
		}
	}
	if owner == nil {
		return false
	}
	var storage [64]byte
	body := storage[:copy(storage[:], f.a.B[start:end])]
	branch := exit - start
	prefix, tail := body[:branch-2], body[branch+4:]
	condition := Cond(body[branch-1] & 15)
	if pairedExit {
		prefix = body[:branch-7]
		condition = invertCond(Cond(body[branch-6] & 15))
	}
	// Conditional branch folding leaves one five-byte NOP before the body.
	// It has no effects and no copied relocation; omit it from later copies.
	if n := scalarLoopNopLen(tail); n != 0 {
		tail = tail[n:]
	}
	if !scalarLoopBodyWithPins(prefix, f.pinnedLocalMask) || !scalarLoopBodyWithPins(tail, f.pinnedLocalMask) {
		return false
	}
	if f.tryScalarPretestCounterGroup(prefix, tail, condition, start, owner) {
		f.stats.peep("scalar-pretest-unroll4")
		return true
	}
	for phase := 1; phase < 4; phase++ {
		f.a.B = append(f.a.B, prefix...)
		f.frameAddEnd(owner, f.a.JccPlaceholder(condition))
		f.a.B = append(f.a.B, tail...)
	}
	f.a.JmpBack(start)
	f.stats.peep("scalar-pretest-unroll4")
	return true
}
