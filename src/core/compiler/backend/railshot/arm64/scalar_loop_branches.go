//go:build arm64

package arm64

import "encoding/binary"

// unrollScalarBranches copies a bounded register-only control-flow graph. Every
// branch must stay inside the copied body or name a pending, empty outer-block
// exit, or a proved conditional division-by-zero trap. Internal relative
// displacements remain identical in each copy; copied outer exits join their
// original owner's patch list. Each iteration retains all its tests and effects,
// including early exits and branches to the body end.
func (f *fn) unrollScalarBranches(fr *ctrlFrame) bool {
	if !f.opt(optScalarLoopUnroll) || !f.hasLoop || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil ||
		len(f.customInstructions) != 0 || f.policy.CompactNative || f.a.ReserveBranch19 || f.opaqueFragments ||
		f.stats != nil && f.stats.RecordSources || len(f.ctrl) == 0 || &f.ctrl[len(f.ctrl)-1] != fr ||
		fr.paramN != 0 || fr.resultN != 0 || f.depth() != fr.height {
		return false
	}
	start, end := fr.controlSite, f.a.Len()
	if start < 0 || end-start <= 32 || end-start > 256 || (end-start)&3 != 0 {
		return false
	}
	budget := f.newScalarLoopMetadataBudget()
	sc := f.scratchState()
	// Return immediates contain linked-list indexes until the epilogue closes,
	// not branch displacements. A link can accidentally look body-local.
	for head := sc.retSiteHead; head != 0; {
		if !budget.take(1) {
			return false
		}
		site := int(head-1) * 4
		if site < 0 || site+4 > end || site >= start {
			return false
		}
		head = rdWord(f.a.B, site) & 0x03ffffff
	}
	var divisionTraps [64]trapSite
	var divisionTrapMask uint64
	for code, sites := range sc.trapSites {
		if !budget.take(len(sites)) {
			return false
		}
		for _, trap := range sites {
			site := int(trap.branch &^ 1)
			if site >= start && site < end {
				if uint32(code) != trapDivZero || trap.branch&1 != 0 || (site-start)&3 != 0 {
					return false
				}
				i := (site - start) / 4
				if divisionTrapMask&(uint64(1)<<i) != 0 {
					return false
				}
				divisionTraps[i] = trap
				divisionTrapMask |= uint64(1) << i
			}
		}
	}
	if !budget.take(len(f.relocs)) {
		return false
	}
	for _, reloc := range f.relocs {
		if int(reloc.at) >= start && int(reloc.at) < end {
			return false
		}
	}
	var words [64]uint32
	var owners [64]*ctrlFrame
	var conditional [64]bool
	n := (end - start) / 4
	internal, exits := 0, 0
	hasInternalBackedge := false
	for i := 0; i < n; i++ {
		word := binary.LittleEndian.Uint32(f.a.B[start+4*i:])
		words[i] = word
		cond := word&0xff000010 == 0x54000000 && word&15 < 14 || word&0x7e000000 == 0x34000000
		jump := word&0xfc000000 == 0x14000000
		divisionTrap := divisionTrapMask&(uint64(1)<<i) != 0
		if divisionTrap && !cond {
			return false
		}
		if cond || jump {
			site := start + 4*i
			for j := 0; j < len(f.ctrl)-1; j++ {
				if !budget.take(1) {
					return false
				}
				owner := &f.ctrl[j]
				if owner.kind != cfBlock && owner.kind != cfIf || owner.branchN != 0 {
					continue
				}
				first, second, overflow := f.frameEndSites(owner)
				if !budget.take(2 + len(overflow)) {
					return false
				}
				matches := func(packed uint32) bool {
					return packed != 0 && int(packed&^frameEndConditional)-1 == site && (packed&frameEndConditional != 0) == cond
				}
				found := matches(first) || matches(second)
				for _, packed := range overflow {
					found = found || matches(packed)
				}
				if found {
					if owners[i] != nil {
						return false
					}
					owners[i] = owner
				}
			}
			mask := uint32(0x03ffffff)
			delta := int(int32(word<<6)>>6) * 4
			if cond {
				mask = 0x00ffffe0
				delta = int(int32(word<<8)>>13) * 4
			}
			if divisionTrap {
				if owners[i] != nil || word&mask != 0 {
					return false
				}
			} else if owners[i] != nil {
				if word&mask != 0 {
					return false // only unresolved edges may enter an owner's patch list
				}
				conditional[i] = cond
				exits++
			} else {
				// An unowned zero displacement may be a pending trap, return or
				// other unsupported edge. Never interpret it as a proved self-loop.
				if delta == 0 || site+delta < start || site+delta > end {
					return false
				}
				hasInternalBackedge = hasInternalBackedge || delta < 0
				internal++
			}
			continue
		}
		rd, ok := scalarLoopWord(word)
		if !ok && word&0x7fff0fe0 == 0x1a9f07e0 && word>>12&15 < 14 {
			// CSET is CSINC with two ZR inputs. It consumes the original
			// iteration's flags and has no PC, SP, or memory effect.
			rd, ok = Reg(word&31), true
			if rd == 31 {
				rd = regNone
			}
		}
		if !ok && word&0x1f800000 == 0x13000000 && word&0x60000000 != 0x60000000 {
			// SBFM/BFM/UBFM, including immediate shifts, have no PC or memory
			// dependence. Unlike SP forms of ADD, destination 31 is always ZR.
			rd, ok = Reg(word&31), true
			if rd == 31 {
				rd = regNone
			}
		}
		if !ok || rd != regNone && (f.reserved.has(rd) || rd == X18 || rd == X26 || rd == X28 || rd == Reg(29) || rd == Reg(30)) {
			return false
		}
	}
	if internal < 1 || exits == 0 {
		return false
	}
	base := f.prepareLoopCounterBase(fr, words[:n], owners[:n], divisionTrapMask)
	// Preserve inner-loop fetch alignment in every outer copy. Padding only
	// separates complete copies, so original body-end edges enter these NOPs
	// before reaching the next copy or final backedge. Internal offsets stay fixed.
	padding := 0
	if hasInternalBackedge {
		padding = (-n) & 3
	}
	pad := func() {
		for i := 0; i < padding; i++ {
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, nopWord)
		}
	}
	pad()
	// Keep the source body's offsets and pending sites intact.
	for phase := 1; phase < 4; phase++ {
		for i, word := range words[:n] {
			if base && i == n-1 {
				// The proved unit ADD uses base+1; later copies use base+2..4.
				word += uint32(phase) << 10
			}
			if divisionTrapMask&(uint64(1)<<i) != 0 {
				trap := divisionTraps[i]
				trap.branch = compactTrapBranch(f.a.Len())
				sc.trapSites[trapDivZero] = append(sc.trapSites[trapDivZero], trap)
			}
			if owners[i] != nil {
				f.appendFrameEnd(owners[i], f.a.Len(), conditional[i])
			}
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
		pad()
	}
	if base {
		start -= 4
	}
	f.patchBranch26(f.a.Branch(), start)
	if divisionTrapMask != 0 {
		f.stats.peep("scalar-loop-unroll-div-trap")
	}
	f.stats.peep("scalar-loop-unroll-branches")
	return true
}
