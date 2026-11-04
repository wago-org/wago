//go:build amd64

package amd64

import "encoding/binary"

// tupleLoopExitStores decodes the two GP writes to canonical result slots.
// No other memory address, access width, or extra instruction is admitted.
func tupleLoopExitStores(body []byte, offset int32, pins regMask) bool {
	for i := 0; i < 2; i++ {
		source, disp, n, ok := scalarLoopStackStore64(body)
		if !ok || source == RSP || !pins.has(source) || disp != offset+int32(i)*8 {
			return false
		}
		body = body[n:]
	}
	return len(body) == 0
}

// A parameter loop can return a pair through a cold arm of its final if. Copy
// each test and hot body while sharing the original cold result stores. Only
// the last copy takes a backedge. The original if-end jump and its metadata stay
// intact; copied exit tests branch to that same cold arm using current GP homes.
func (f *fn) tryScalarTuplePretestLoop(fr *ctrlFrame) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || f.makesCalls || f.usesCalls || f.interruptible || profileEnabled ||
		f.stats != nil && f.stats.RecordSources || f.moduleEH || f.gcFrameRoots != nil || compactNativePolicy(f.policy) ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.intervalControl ||
		fr.kind != cfLoop || fr.paramN < 1 || fr.paramN > 4 || fr.resultN != 2 || f.depth() != fr.height {
		return false
	}
	loopIndex := int(f.loopLocalParams.depth) - 1
	if loopIndex < 0 || loopIndex+2 != len(f.ctrl) || &f.ctrl[loopIndex] != fr {
		return false
	}
	owner := &f.ctrl[loopIndex+1]
	if owner.kind != cfIf || owner.resultN != 2 || owner.branchN != 2 || owner.height != fr.height ||
		len(owner.types) != 2 || owner.types[0] != mtI32 && owner.types[0] != mtI64 || owner.types[1] != mtI32 && owner.types[1] != mtI64 {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start < 20 || end-start > 96 {
		return false
	}
	for _, reloc := range f.relocs {
		if int(reloc.at) >= start && int(reloc.at) < end {
			return false
		}
	}
	first, second, more := f.frameEndSites(owner)
	if first == 0 || second != 0 || len(more) != 0 {
		return false
	}
	exit := int(first - 1)
	if exit <= start || exit+4 > end || f.a.B[exit-1] != 0xe9 || binary.LittleEndian.Uint32(f.a.B[exit:]) != 0 {
		return false
	}
	// Any other unresolved branch in the copied range would need its own
	// reconciliation and relocation proof; retain the ordinary loop instead.
	for i := range f.ctrl {
		if &f.ctrl[i] == owner {
			continue
		}
		a, b, rest := f.frameEndSites(&f.ctrl[i])
		inside := func(site uint32) bool { return site != 0 && int(site-1) >= start && int(site-1) < end }
		if inside(a) || inside(b) {
			return false
		}
		for _, site := range rest {
			if inside(site) {
				return false
			}
		}
	}
	body := f.a.B[start:end]
	jump := exit - start
	coldEnd, hot := jump-1, jump+4
	if !scalarLoopBodyWithPins(body[hot:], f.pinnedLocalMask) {
		return false
	}
	branch := -1
	slot := slotsOfTypes(f.frameBaseTypes(owner))
	for p := 1; p+6 <= coldEnd; p++ {
		if body[p] != 0x0f || body[p+1]&0xf0 != 0x80 ||
			int(int32(binary.LittleEndian.Uint32(body[p+2:]))) != hot-(p+6) ||
			!scalarLoopBodyWithPins(body[:p], f.pinnedLocalMask) ||
			!tupleLoopExitStores(body[p+6:coldEnd], f.spillOff(slot), f.pinnedLocalMask) {
			continue
		}
		branch = p
		break
	}
	if branch < 0 {
		return false
	}
	var storage [96]byte
	n := copy(storage[:], body)
	if f.tryScalarTupleCountedLoop(storage[:branch], storage[hot:n], Cond(storage[branch+1]&15), start) {
		f.stats.peep("scalar-tuple-pretest-unroll4")
		return true
	}
	condition := invertCond(Cond(storage[branch+1] & 15))
	cold := start + branch + 6
	for phase := 1; phase < 4; phase++ {
		f.a.B = append(f.a.B, storage[:branch]...)
		exit := f.a.JccPlaceholder(condition)
		f.a.PatchRel32(exit, cold)
		f.a.B = append(f.a.B, storage[hot:n]...)
	}
	f.a.JmpBack(start)
	f.stats.peep("scalar-tuple-pretest-unroll4")
	return true
}
