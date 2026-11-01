//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Self-tail arguments need no round trip through the outgoing frame. Capture
// live register/deferred roots first, resolve register cycles, then load homes
// and constants. This is the same staging order as ordinary integer calls;
// locals need no preservation because re-entry initializes the whole function.
func (f *fn) emitTailSelfRegisterJump(ft *wasm.CompType, jump func()) {
	roots := f.rootsBottomToTop()
	p := len(ft.Params)
	base := len(roots) - p
	belowSlots := slotsOfTypes(f.logicalTypes(roots[:base]))
	oldFloor := f.spillFloor
	f.spillFloor = max(f.spillFloor, belowSlots)
	defer func() { f.spillFloor = oldFloor }()
	args := roots[base:]
	// Physical leaves can keep each parameter in its own ABI register. Use
	// ordinary local assignment sinking there: it realizes old local reads
	// before overwriting them, so simultaneous argument permutations remain
	// correct while arithmetic can write directly into the final register.
	direct := f.preserveCallerPins
	for i := range ft.Params {
		reg, floating, pinned := f.pinReg(i)
		if !pinned || floating || reg != intArgRegs[i] {
			direct = false
			break
		}
	}
	if direct {
		if p > 0 {
			f.flushBelow(args[0])
		} else {
			f.flush()
		}
		for i := p - 1; i >= 0; i-- {
			f.setLocal(nil, i, false)
		}
		f.storePinnedGlobals(false)
		f.storeModuleGlobals(X16)
		f.stats.peep("tail-self-local-args")
		jump()
		return
	}
	moves := f.tmpMoves[:0]
	deferred := f.tmpDeferred[:0]
	for i, root := range args {
		capture := root.isDeferred()
		if root.elemKind() == ekValue {
			switch root.st.kind {
			case stReg, stLocalReg, stGlobReg, stMemRef:
				capture = true
			case stSlot:
				capture = root.st.slotIndex() < belowSlots
			}
		}
		if capture {
			reg, owned := f.materializeRead(root)
			// A physical leaf may already keep its locals in argument regs.
			// Retain scratch headroom for subsequent argument expressions;
			// reload overflow values only after resolving all parallel moves.
			block := f.pinned.union(f.pinnedLocalMask).union(f.reserved).union(maskOf(reg))
			free := 0
			for _, r := range gpAlloc {
				if f.regUser[r] == nil && !block.has(r) {
					free++
				}
			}
			if owned && free < 3 {
				f.spill(root)
				deferred = append(deferred, deferredArg{target: intArgRegs[i], root: root})
			} else {
				f.pinned = f.pinned.add(reg)
				moves = append(moves, regMove{dst: intArgRegs[i], src: reg})
			}
		} else {
			deferred = append(deferred, deferredArg{target: intArgRegs[i], root: root})
		}
	}
	if p > 0 {
		f.flushBelow(args[0])
	} else {
		f.flush()
	}
	f.storePinnedGlobals(false)
	f.storeModuleGlobals(X16)
	for _, m := range moves {
		f.pinned = f.pinned.remove(m.src)
	}
	var checkMoves func()
	if regallocCheckEnabled {
		checkMoves = f.checkBeginRegMoves(moves, false)
		defer checkMoves()
	}
	swaps := resolveRegMovesWindow(moves,
		func(dst, src Reg) { f.a.MovReg64(dst, src) },
		func(x, y Reg) { f.a.MovReg64(X16, x); f.a.MovReg64(x, y); f.a.MovReg64(y, X16) },
		func(a, b, c Reg) { f.a.MovReg64(X16, a); f.a.MovReg64(a, b); f.a.MovReg64(b, c); f.a.MovReg64(c, X16) },
	)
	if regallocCheckEnabled {
		checkMoves()
	}
	f.stats.peepN("machine-swap-chain", swaps)
	f.tmpMoves = moves[:0]
	for _, a := range deferred {
		switch a.root.st.kind {
		case stConst:
			f.loadConst(a.target, a.root.st)
		case stSlot:
			f.ld64(a.target, SP, f.spillOff(a.root.st.slotIndex()))
		case stLocalRef:
			f.ld64(a.target, SP, f.localOff(a.root.st.index()))
		}
	}
	f.tmpDeferred = deferred[:0]
	jump()
}

// A body whose only transfers are direct self tails makes no native calls.
// Classifying it as a physical leaf enables argument-register homes. Preserve
// its logical loop identity so it can never acquire bounded scheduler entry.
// Helpers and indirect transfers retain ordinary call/frame planning.
func (f *fn) onlyDirectSelfTailCalls(body []byte) bool {
	if !f.opt(optTailSelfLoop) || !f.opt(optRegABI) || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || !sigFitsRegABI(f.ft) || !sigIsIntOnly(f.ft) {
		return false
	}
	r := wasm.ReaderFrom(body)
	found := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x23, 0x24:
			index, err := r.U32()
			if err != nil {
				return false
			}
			global, ok := f.globalType(index)
			if !ok || global.Type.Kind() != wasm.ValNum || global.Type == wasm.V128 {
				return false
			}
			continue
		case 0x12:
			target, err := r.U32()
			if err != nil || target != f.traceFuncIdx {
				return false
			}
			found = true
			continue
		}
		// Fail closed for helpers, reference operations and future extensions.
		ordinary := op <= 0x05 || op >= 0x0b && op <= 0x0f ||
			op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x24 ||
			op >= 0x28 && op <= 0x3f || op >= 0x41 && op <= 0xc4
		if !ordinary {
			return false
		}
		if err := f.classifier.SkipImmediate(&r, op); err != nil {
			return false
		}
	}
	return found
}

// Thread a self-tail backedge through an entry's standalone zero test. A hot
// iteration then takes one conditional branch instead of an unconditional
// backedge followed by a taken conditional branch. No entry initialization is
// skipped: the test must be the first word at the existing re-entry point.
func (f *fn) threadSelfTailEntryTest() bool {
	entry, at := f.entryTrapEnd, f.a.Len()
	if entry < 0 || entry+4 > at {
		return false
	}
	word := rdWord(f.a.B, entry)
	if word&0x7e000000 != 0x34000000 {
		return false
	}
	target, ok := branchTarget(entry, word)
	if !ok || target <= entry+4 || target >= at || (target-at)/4 < -(1<<18) {
		return false
	}
	reg := Reg(word & 31)
	var site int
	if word&(1<<31) != 0 {
		if word&(1<<24) != 0 {
			site = f.a.Cbnz64(reg)
		} else {
			site = f.a.Cbz64(reg)
		}
	} else {
		if word&(1<<24) != 0 {
			site = f.a.Cbnz32(reg)
		} else {
			site = f.a.Cbz32(reg)
		}
	}
	f.patchBranch19(site, target)
	cold := f.a.Branch()
	if !f.a.PatchBranch26(cold, entry+4) {
		panic("arm64 self-tail cold edge is out of range")
	}
	f.stats.peep("tail-self-entry-test")
	return true
}
