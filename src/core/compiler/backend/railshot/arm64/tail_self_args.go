//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Integer tail arguments need no round trip through the outgoing frame. Capture
// live register/deferred roots first, resolve register cycles, then load homes
// and constants. This is the same staging order as ordinary integer calls;
// locals need no preservation because re-entry initializes the whole function.
func (f *fn) emitTailIntegerRegisterArgs(ft *wasm.CompType, localHomes bool, jump func()) {
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
	direct := (localHomes || f.preserveCallerPins) && len(ft.Params) == len(f.ft.Params)
	for i := range ft.Params {
		if i >= len(f.ft.Params) || !wasm.EqualValType(ft.Params[i], f.ft.Params[i]) {
			direct = false
			break
		}
	}
	for i := range ft.Params {
		reg, floating, pinned := f.pinReg(i)
		if !pinned || floating || !localHomes && reg != intArgRegs[i] {
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
			root := f.s.back()
			// An unchanged local is already in its final home. If a later
			// assignment captured its old value, realizeLocalRefs has replaced
			// this root with owned storage and the ordinary assignment remains.
			if root != nil && root.elemKind() == ekValue && root.st.kind == stLocalReg && root.st.index() == i {
				f.erase(root)
				continue
			}
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

// A body containing only local register-ABI tail transfers makes no native calls.
// Classifying it as a physical leaf enables argument-register homes. Preserve
// call relocations so recursive cycles remain visible to scheduler admission.
// Helpers and indirect transfers retain ordinary call/frame planning.
func (f *fn) onlyLocalTailTransfers(body []byte) bool { return f.tailTransferLeaf(body, false) }
func (f *fn) onlySelfTailTransfers(body []byte) bool  { return f.tailTransferLeaf(body, true) }
func (f *fn) tailTransferLeaf(body []byte, selfOnly bool) bool {
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
		case 0x13:
			typ, err := r.U32()
			if err != nil {
				return false
			}
			table, err := r.U32()
			if err != nil {
				return false
			}
			target, ok := f.privateMonomorphicTailTarget(typ, table)
			if !ok || target != int(f.traceFuncIdx) {
				return false
			}
			found = true
			continue
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
			if err != nil {
				return false
			}
			ft, ok := f.functionSignature(target)
			if selfOnly && target != f.traceFuncIdx {
				return false
			}
			if !ok || int(target) < f.importedFunctionCount() || !sigFitsRegABI(ft) || !sigIsIntOnly(ft) || !tailResultABICompatible(f.ft.Results, ft.Results) {
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
func (f *fn) threadSelfTailEntryTest(entry int) bool {
	at := f.a.Len()
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

// Reuse two words of a zero-frame tail release for the callee's entry test.
// Both sides must have the exact three-NOP frame reservation, excluding saved
// LR/FP, initialization, stack checks and nonzero frames. Keep the original B
// and its relocation, so call-graph admission still sees recursive cycles.
// Final module layout is fixed here; no code moves or grows.
func threadTailEntryTest(code []byte, site, target int) bool {
	const nop = 0xd503201f
	if site < 12 || site+4 > len(code) || target < 0 || target+20 > len(code) || site%4 != 0 || target%4 != 0 || rdWord(code, site)&0xfc000000 != 0x14000000 {
		return false
	}
	for off := 0; off < 12; off += 4 {
		if rdWord(code, site-12+off) != nop || rdWord(code, target+off) != nop {
			return false
		}
	}
	entry := target + 12
	word := rdWord(code, entry)
	if word&0x7e000000 != 0x34000000 {
		return false
	}
	hot, ok := branchTarget(entry, word)
	first, second := site-12, site-8
	delta := hot - first
	coldDelta := entry + 4 - second
	if !ok || hot <= entry+4 || hot >= len(code) || delta%4 != 0 || delta/4 < -(1<<18) || delta/4 >= 1<<18 || coldDelta/4 < -(1<<25) || coldDelta/4 >= 1<<25 {
		return false
	}
	binary.LittleEndian.PutUint32(code[first:], word&^(0x7ffff<<5)|(uint32(delta/4)&0x7ffff)<<5)
	binary.LittleEndian.PutUint32(code[second:], 0x14000000|uint32(coldDelta/4)&0x3ffffff)
	return true
}

// Every incoming parameter has a permanent integer local home. A self-tail
// transfer can assign those homes directly, then re-enter before declared-local
// initialization rather than shuttling through the public argument registers.
func (f *fn) selfTailLocalEntry() int {
	if f.entryLocalsInit == 0 {
		return 0
	}
	for i := range f.ft.Params {
		if _, floating, ok := f.pinReg(i); !ok || floating {
			return 0
		}
	}
	return f.entryLocalsInit
}
