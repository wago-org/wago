//go:build arm64

package arm64

import "encoding/binary"

// unrollScalarPretest copies a bounded register-only loop with one exit. The
// exit must be an unresolved conditional edge owned by an enclosing empty-arity
// block. Every copied edge is registered with that same owner before its end
// is resolved; no already-patched or unknown PC-relative instruction is copied.
func (f *fn) unrollScalarPretest(fr *ctrlFrame) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.policy.CompactNative || f.a.ReserveBranch19 || f.stats != nil && f.stats.RecordSources || fr.paramN != 0 {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start < 8 || end-start > 32 || (end-start)&3 != 0 {
		return false
	}
	var words [8]uint32
	n := (end - start) / 4
	exit := -1
	for i := 0; i < n; i++ {
		word := binary.LittleEndian.Uint32(f.a.B[start+4*i:])
		words[i] = word
		bcond := word&0xff000010 == 0x54000000 && word&15 < 14
		cbz := word&0x7e000000 == 0x34000000
		if (bcond || cbz) && word&0x00ffffe0 == 0 {
			if exit != -1 {
				return false
			}
			exit = i
			continue
		}
		rd, ok := scalarLoopWord(word)
		if !ok || rd != regNone && (f.reserved.has(rd) || rd == X18 || rd == X26 || rd == X28 || rd == Reg(29) || rd == Reg(30)) {
			return false
		}
	}
	if exit < 0 {
		return false
	}
	site := start + 4*exit
	var owner *ctrlFrame
	for i := range f.ctrl {
		candidate := &f.ctrl[i]
		if candidate == fr {
			break
		}
		if candidate.kind != cfBlock && candidate.kind != cfIf || candidate.branchN != 0 {
			continue
		}
		first, second, overflow := f.frameEndSites(candidate)
		matches := func(packed uint32) bool {
			return packed&frameEndConditional != 0 && int(packed&^frameEndConditional)-1 == site
		}
		found := matches(first) || matches(second)
		for _, packed := range overflow {
			found = found || matches(packed)
		}
		if found {
			if owner != nil {
				return false
			}
			owner = candidate
		}
	}
	if owner == nil {
		return false
	}
	// Retain the original body and its original pending edge. New copies use the
	// same register homes and preserve an exit check in every iteration.
	for phase := 1; phase < 4; phase++ {
		for i := 0; i < n; i++ {
			if i == exit {
				f.appendFrameEnd(owner, f.a.Len(), true)
			}
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, words[i])
		}
	}
	f.patchBranch26(f.a.Branch(), start)
	f.stats.peep("scalar-loop-unroll-pretest")
	return true
}
