//go:build arm64

package arm64

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (fr *ctrlFrame) branchArity() int { return int(fr.branchN) }

func (fr *ctrlFrame) baseTypeStart() int { return int(uint8(fr.baseTypes)) }

func (fr *ctrlFrame) baseTypeCount() int {
	if fr.has(ctrlColdBaseTypes) {
		return len(fr.types) - fr.paramN - fr.resultN
	}
	return int(uint8(fr.baseTypes >> 8))
}

func (fr *ctrlFrame) setBaseTypeRange(start, count int) {
	if uint(start) > 255 || uint(count) > 255 {
		panic("arm64: control base-type range exceeds packed storage")
	}
	fr.baseTypes = uint16(uint8(start)) | uint16(uint8(count))<<8
}

func (fr *ctrlFrame) has(flag ctrlFlags) bool { return fr.flags&flag != 0 }

func (fr *ctrlFrame) set(flag ctrlFlags, enabled bool) {
	if enabled {
		fr.flags |= flag
	} else {
		fr.flags &^= flag
	}
}

func (m *ctrlFrameMerge) setLoopSet(start uint32, count uint16) {
	m.loopSetStart = start
	m.loopSetMeta = m.loopSetMeta&countedLoopCounterMask | uint32(count) | loopSetKnownBit
}

func (m *ctrlFrameMerge) countedLoop() (counter int, ok bool) {
	if m == nil {
		return 0, false
	}
	encoded := (m.loopSetMeta & countedLoopCounterMask) >> countedLoopShift
	if encoded == 0 {
		return 0, false
	}
	return int(encoded - 1), true
}

// tryCountedLoopLatch recognizes the exact tail of a top-tested i32 countdown
// and branches from the decrement flags directly to the loop body. Interruptible
// loops retain their header poll and are deliberately excluded.
func (f *fn) tryCountedLoopLatch(r *wasm.Reader, x int) (bool, error) {
	// The fused latch consumes the branch before the main reader sees it.
	// Retain ordinary backedge reconciliation while regional homes are live.
	if f.intervalControl && f.intervalActive != 0 {
		return false, nil
	}
	if !f.opt(optCountedLoopLatch) || f.interruptible || f.usesCalls || len(f.ctrl) < 2 || f.depth() != 0 {
		return false, nil
	}
	loop := &f.ctrl[len(f.ctrl)-1]
	outer := &f.ctrl[len(f.ctrl)-2]
	if loop.kind != cfLoop || loop.paramN != 0 || loop.resultN != 0 || outer.kind != cfBlock || outer.branchArity() != 0 {
		return false, nil
	}
	counter, ok := f.ctrlMerge(loop).countedLoop()
	if !ok || counter != x || x < 0 || x >= len(f.localType) || f.localType[x] != mtI32 {
		return false, nil
	}
	reg, isFloat, pinned := f.pinReg(x)
	if !pinned || isFloat {
		return false, nil
	}
	r2 := *r
	op, err := r2.Byte()
	if err != nil || op != 0x41 {
		return false, nil
	}
	one, err := r2.I32()
	if err != nil || one != 1 {
		return false, nil
	}
	for _, want := range []byte{0x6b, 0x21} { // i32.sub; local.set
		op, err = r2.Byte()
		if err != nil || op != want {
			return false, nil
		}
	}
	set, err := r2.U32()
	if err != nil || int(set) != x {
		return false, nil
	}
	op, err = r2.Byte()
	if err != nil || op != 0x0c {
		return false, nil
	}
	label, err := r2.U32()
	if err != nil || label != 0 {
		return false, nil
	}
	latchEnd := r2.Offset()
	for range 2 {
		op, err = r2.Byte()
		if err != nil || op != 0x0b {
			return false, nil
		}
	}
	// The replacement branch will be emitted one instruction after the current
	// position. Reject oversized functions before changing reader or local state;
	// the ordinary backedge has the wider imm26 range and remains valid.
	delta := loop.controlSite + 4 - (f.a.Len() + 4)
	if delta&3 != 0 || delta < -(1<<20) || delta >= 1<<20 {
		return false, nil
	}
	if err := r.JumpTo(latchEnd); err != nil {
		return false, err
	}
	f.convergeBranchLocals(loop)
	f.invalidateBoundsCertFor(1, uint32(x))
	f.setFactsForLocal(x, 0)
	if f.tryUnrolledLinearSumLatch(x) {
		f.stats.peep("counted-loop-latch")
		return true, nil
	}
	f.a.SubsImm32(reg, reg, 1)
	f.markLocalDirty(x)
	site := f.a.Bcond(condNE)
	if !f.a.PatchBranch19(site, loop.controlSite+4) {
		return false, fmt.Errorf("arm64: counted loop body branch out of range")
	}
	f.stats.peep("counted-loop-latch")
	return true, nil
}

func (f *fn) frameEH(fr *ctrlFrame) *ctrlFrameEH {
	if merge := f.ctrlMerge(fr); merge != nil && merge.extra != nil {
		return merge.extra.eh
	}
	return nil
}

func (f *fn) ensureCtrlMergeExtra(fr *ctrlFrame) *ctrlFrameMergeExtra {
	merge := f.ensureCtrlMerge(fr)
	if merge.extra == nil {
		merge.extra = new(ctrlFrameMergeExtra)
	}
	return merge.extra
}

func (f *fn) ctrlMerge(fr *ctrlFrame) *ctrlFrameMerge {
	if fr.mergeIndex == 0 {
		return nil
	}
	return &f.scratchState().ctrlMerges[fr.mergeIndex-1]
}

func (f *fn) ensureCtrlMerge(fr *ctrlFrame) *ctrlFrameMerge {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge
	}
	sc := f.scratchState()
	if sc.ctrlMerges == nil {
		sc.ctrlMerges = make([]ctrlFrameMerge, 0, initialCtrlMergeCapacity)
	}
	sc.ctrlMerges = append(sc.ctrlMerges, ctrlFrameMerge{})
	fr.mergeIndex = uint32(len(sc.ctrlMerges))
	return &sc.ctrlMerges[fr.mergeIndex-1]
}

func (f *fn) releaseCtrlMerge(fr *ctrlFrame) {
	if fr.mergeIndex == 0 {
		return
	}
	sc := f.scratchState()
	merge := &sc.ctrlMerges[fr.mergeIndex-1]
	*merge = ctrlFrameMerge{}
	if int(fr.mergeIndex) <= len(sc.ctrlRoots) {
		sc.ctrlRoots[fr.mergeIndex-1] = ctrlFrameRoots{}
	}
}

// pushCtrl reuses a cold-merge slot previously assigned to this nesting depth.
// Control frames are stack-disciplined, so depth is a stable bounded owner and
// the sidecar arena grows with maximum depth rather than total blocks compiled.
func (f *fn) pushCtrl(fr *ctrlFrame) {
	depth := len(f.ctrl)
	if depth < cap(f.ctrl) {
		stale := f.ctrl[:cap(f.ctrl)][depth].mergeIndex
		switch {
		case fr.mergeIndex == 0:
			fr.mergeIndex = stale
		case stale != 0 && stale != fr.mergeIndex:
			sc := f.scratchState()
			fresh := fr.mergeIndex
			sc.ctrlMerges[stale-1] = sc.ctrlMerges[fresh-1]
			sc.ctrlMerges[fresh-1] = ctrlFrameMerge{}
			if int(fresh) <= len(sc.ctrlRoots) {
				for len(sc.ctrlRoots) < int(stale) {
					sc.ctrlRoots = append(sc.ctrlRoots, ctrlFrameRoots{})
				}
				sc.ctrlRoots[stale-1] = sc.ctrlRoots[fresh-1]
				sc.ctrlRoots[fresh-1] = ctrlFrameRoots{}
			} else if int(stale) <= len(sc.ctrlRoots) {
				sc.ctrlRoots[stale-1] = ctrlFrameRoots{}
			}
			if int(fresh) == len(sc.ctrlMerges) {
				sc.ctrlMerges = sc.ctrlMerges[:len(sc.ctrlMerges)-1]
			}
			fr.mergeIndex = stale
		}
	}
	fr.ehDepth = uint16(f.ehTryDepth)
	f.ctrl = append(f.ctrl, *fr)
	if fr.kind == cfLoop {
		f.innermostLoop = uint32(depth + 1)
	}
}

func (f *fn) frameBranchState(fr *ctrlFrame) packedLocStates {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge.branchState
	}
	return packedLocStates{}
}

func (f *fn) frameEntryState(fr *ctrlFrame) packedLocStates {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge.entryState
	}
	return packedLocStates{}
}

func (f *fn) setFrameBranchState(fr *ctrlFrame, state packedLocStates) {
	if !state.empty() {
		f.ensureCtrlMerge(fr).branchState = state
	}
}

func (f *fn) setFrameEntryState(fr *ctrlFrame, state packedLocStates) {
	if !state.empty() {
		f.ensureCtrlMerge(fr).entryState = state
	}
}

func (f *fn) frameBaseGCRoots(fr *ctrlFrame) []bool {
	if !fr.has(ctrlHasBaseGCRoots) {
		return nil
	}
	if roots := f.ctrlRoots(fr); roots != nil && len(roots.flags) >= fr.height {
		return roots.flags[:fr.height]
	}
	return nil
}

func (fr *ctrlFrame) appendParameterTypes(dst []machineType) []machineType {
	start := 0
	if fr.has(ctrlColdBaseTypes) {
		start = fr.baseTypeCount()
	}
	return append(dst, fr.types[start:start+fr.paramN]...)
}

func (fr *ctrlFrame) appendResultTypes(dst []machineType) []machineType {
	if fr.resultN == 1 && fr.types == nil && !fr.has(ctrlColdBaseTypes) {
		return append(dst, fr.res0)
	}
	start := fr.paramN
	if fr.has(ctrlColdBaseTypes) {
		start += fr.baseTypeCount()
	}
	return append(dst, fr.types[start:start+fr.resultN]...)
}

func (f *fn) setFrameBaseTypes(fr *ctrlFrame, types []machineType) {
	fr.set(ctrlBaseTypesSet, true)
	start := int(f.controlBaseTypeN)
	storage := f.scratchState().functionResultTypeArena[:]
	if start+len(types) <= len(storage) {
		copy(storage[start:], types)
		fr.setBaseTypeRange(start, len(types))
		f.controlBaseTypeN += uint8(len(types))
		return
	}

	// Unusually wide/deep control uses the frame's existing cold type backing.
	// This preserves exact semantics without growing persistent worker state.
	all := make([]machineType, len(types)+fr.paramN+fr.resultN)
	copy(all, types)
	sig := all[len(types):]
	copy(sig, fr.types[:fr.paramN])
	if fr.resultN == 1 && fr.types == nil {
		sig[fr.paramN] = fr.res0
	} else {
		copy(sig[fr.paramN:], fr.types[fr.paramN:fr.paramN+fr.resultN])
	}
	fr.types = all
	fr.set(ctrlColdBaseTypes, true)
}

func (f *fn) frameBaseTypes(fr *ctrlFrame) []machineType {
	if fr.has(ctrlColdBaseTypes) {
		return fr.types[:fr.baseTypeCount()]
	}
	start := fr.baseTypeStart()
	return f.scratchState().functionResultTypeArena[start : start+fr.baseTypeCount()]
}

func (f *fn) releaseFrameBaseTypes(fr *ctrlFrame) {
	if !fr.has(ctrlBaseTypesSet) {
		return
	}
	if fr.has(ctrlColdBaseTypes) {
		return
	}
	start := fr.baseTypeStart()
	end := start + fr.baseTypeCount()
	if end != int(f.controlBaseTypeN) {
		panic(fmt.Sprintf("arm64: control base-type arena released out of order: range [%d,%d), top %d", start, end, f.controlBaseTypeN))
	}
	f.controlBaseTypeN = uint8(start)
}

func (f *fn) appendFrameEnd(fr *ctrlFrame, site int, conditional bool) {
	if fr.kind == cfLoop {
		panic("arm64: forward end site on loop frame")
	}
	cold := f.ensureCtrlMerge(fr)
	packed := f.packFrameEndSite(site, conditional)
	if packed == 0 {
		return
	}
	if cold.loopSetStart == 0 {
		cold.loopSetStart = packed
		return
	}
	if cold.loopSetMeta == 0 {
		cold.loopSetMeta = packed
		return
	}
	f.appendEndSite(&cold.ends, packed)
}

func (f *fn) frameEndSites(fr *ctrlFrame) (uint32, uint32, []uint32) {
	if fr.kind != cfLoop {
		if cold := f.ctrlMerge(fr); cold != nil {
			return cold.loopSetStart, cold.loopSetMeta, cold.ends
		}
	}
	return 0, 0, nil
}

func (f *fn) patchFrameEndSites(first, second uint32, overflow []uint32) {
	if first != 0 {
		f.patchFrameEndSite(first)
	}
	if second != 0 {
		f.patchFrameEndSite(second)
	}
	for _, packed := range overflow {
		f.patchFrameEndSite(packed)
	}
}

func (f *fn) patchFrameEndSite(packed uint32) {
	conditional := packed&frameEndConditional != 0
	site := int(packed&^frameEndConditional) - 1
	if conditional {
		f.patchBranch19(site, f.a.Len())
	} else {
		f.patchBranch26(site, f.a.Len())
	}
}

func (f *fn) frameParamGCRoots(fr *ctrlFrame) []bool {
	if !fr.has(ctrlHasParamGCRoots) {
		return nil
	}
	if roots := f.ctrlRoots(fr); roots != nil && len(roots.flags) >= fr.height+fr.paramN {
		return roots.flags[fr.height : fr.height+fr.paramN]
	}
	return nil
}

func (f *fn) frameResultGCRoots(fr *ctrlFrame) []bool {
	if !fr.has(ctrlHasResultGCRoots) {
		return nil
	}
	if roots := f.ctrlRoots(fr); roots != nil && len(roots.flags) >= fr.height+fr.paramN+fr.resultN {
		start := fr.height + fr.paramN
		return roots.flags[start : start+fr.resultN]
	}
	return nil
}

func (f *fn) frameColdEdges(fr *ctrlFrame) []coldEdge {
	if merge := f.ctrlMerge(fr); merge != nil && merge.extra != nil {
		return merge.extra.coldEdges
	}
	return nil
}

func (f *fn) appendFrameColdEdge(fr *ctrlFrame, edge coldEdge) {
	extra := f.ensureCtrlMergeExtra(fr)
	extra.coldEdges = append(extra.coldEdges, edge)
}

func (f *fn) convergeFrameBranchState(fr *ctrlFrame) {
	state := f.frameBranchState(fr)
	f.convergeEdgeTo(&state)
	f.setFrameBranchState(fr, state)
}

func (f *fn) convergeFrameBranchStateWithDead(fr *ctrlFrame, deadGP, deadFP regMask) {
	state := f.frameBranchState(fr)
	f.convergeEdgeToWithDead(&state, deadGP, deadFP)
	f.setFrameBranchState(fr, state)
}

func (f *fn) convergeFrameEntryState(fr *ctrlFrame) {
	state := f.frameEntryState(fr)
	f.convergeEdgeTo(&state)
	f.setFrameEntryState(fr, state)
}

func rootMachineType(root *elem) machineType {
	typ := root.st.typ
	if root.elemKind() == ekDeferred && root.st.typ != mtNone {
		typ = root.st.typ
	}
	return typ
}

func slotsOfTypes(types []machineType) int {
	n := 0
	for _, typ := range types {
		n += typ.stackSlots()
	}
	return n
}

// depth returns the number of logical operands (valent-block roots) on the stack.
func (f *fn) depth() int {
	n := 0
	for cur := f.s.prev(f.s.head); cur != f.s.head; cur = f.s.prev(f.s.baseOfValentBlock(cur)) {
		n++
	}
	return n
}

// rootsBottomToTop returns the logical operands in bottom-to-top order.
// The returned scratch slice is valid only until the next helper using f.tmpRoots.
func (f *fn) rootsBottomToTop() []*elem {
	rs := f.tmpRoots[:0]
	for cur := f.s.prev(f.s.head); cur != f.s.head; cur = f.s.prev(f.s.baseOfValentBlock(cur)) {
		rs = append(rs, cur)
	}
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	f.tmpRoots = rs
	return rs
}

func (f *fn) logicalTypes(roots []*elem) []machineType {
	types := f.tmpTypes[:0]
	for _, root := range roots {
		types = append(types, rootMachineType(root))
	}
	f.tmpTypes = types
	return types
}

func slotOfLogicalTypes(types []machineType, logical int) int {
	if logical < 0 || logical > len(types) {
		panic("arm64: logical stack index out of range")
	}
	return slotsOfTypes(types[:logical])
}

func (f *fn) currentLogicalTypes() []machineType { return f.logicalTypes(f.rootsBottomToTop()) }

func gcRootFlags(roots []*elem) []bool {
	var flags []bool
	for i, root := range roots {
		if root.elemKind() != ekValue || !root.st.hasGCRoot() {
			continue
		}
		if flags == nil {
			flags = make([]bool, len(roots))
		}
		flags[i] = true
	}
	return flags
}

func (f *fn) captureGCFrameShape(fr *ctrlFrame) {
	if !f.tracksGCFrameRoots() {
		return
	}
	roots := f.rootsBottomToTop()
	if fr.height < 0 || fr.height+fr.paramN > len(roots) {
		return
	}
	var flags []bool
	for i, root := range roots[:fr.height+fr.paramN] {
		if root.elemKind() != ekValue || !root.st.hasGCRoot() {
			continue
		}
		if flags == nil {
			flags = make([]bool, fr.height+fr.paramN+fr.resultN)
		}
		flags[i] = true
		if i < fr.height {
			fr.set(ctrlHasBaseGCRoots, true)
		} else {
			fr.set(ctrlHasParamGCRoots, true)
		}
	}
	if flags != nil {
		f.ensureCtrlRoots(fr).flags = flags
	}
}

func (f *fn) recordGCBranchResults(fr *ctrlFrame, n int) {
	if !f.tracksGCFrameRoots() || n == 0 {
		return
	}
	roots := f.rootsBottomToTop()
	if n > len(roots) {
		return
	}
	for i, root := range roots[len(roots)-n:] {
		if root.elemKind() != ekValue || !root.st.hasGCRoot() {
			continue
		}
		f.setFrameResultGCRoot(fr, i)
	}
}

func frameGCRootFlags(base, suffix []bool) []bool {
	if len(base)+len(suffix) == 0 {
		return nil
	}
	flags := make([]bool, 0, len(base)+len(suffix))
	flags = append(flags, base...)
	flags = append(flags, suffix...)
	return flags
}

func (f *fn) moveBranchValues(fr *ctrlFrame, d, a int) {
	if fr.kind != cfLoop {
		f.recordGCBranchResults(fr, a)
	}
	types := f.currentLogicalTypes()
	fromSlot := slotOfLogicalTypes(types, d-a)
	toSlot := slotsOfTypes(f.frameBaseTypes(fr))
	nSlots := slotOfLogicalTypes(types, d) - fromSlot
	f.moveSlots(fromSlot, toSlot, nSlots)
}

func (f *fn) frameDepthTypesForFrame(fr *ctrlFrame, parameters bool) []machineType {
	out := f.tmpTypes[:0]
	out = append(out, f.frameBaseTypes(fr)...)
	if parameters {
		out = fr.appendParameterTypes(out)
	} else {
		out = fr.appendResultTypes(out)
	}
	f.tmpTypes = out
	return out
}

// flush materializes every operand into canonical frame slots, condensing
// deferred nodes, then rebuilds the stack model as canonical slot entries with
// all registers freed. v128 values occupy two adjacent 8-byte slots.
func (f *fn) flush() {
	f.stats.addFlush()
	f.invalidateGlobalsCache() // the cached cell ptr must not span a call/control boundary
	f.invalidateBoundsCert()   // bounds facts are valid only within a straight-line region
	roots := f.rootsBottomToTop()
	if regallocCheckEnabled {
		f.checkBeginFlush(roots)
		defer f.checkEndFlush()
	}
	var gcRoots []bool
	if f.tracksGCFrameRoots() {
		gcRoots = f.tmpGCRoots[:0]
		for _, root := range roots {
			gcRoots = append(gcRoots, root.elemKind() == ekValue && root.st.hasGCRoot())
		}
		f.tmpGCRoots = gcRoots
	}
	if f.flushWideStack(roots, gcRoots) {
		return
	}
	// New allocation spills must stay above the canonical destinations that
	// this pass is still writing, including both halves of every v128.
	oldFloor := f.spillFloor
	f.spillFloor = max(oldFloor, slotsOfTypes(f.tmpFlushTypes))
	types := f.tmpTypes[:0]
	slot := 0
	for _, root := range roots {
		typ := rootMachineType(root)
		if typ == mtCustom {
			panic("custom value cannot cross a control-flow or ordinary call boundary")
		}
		f.stats.addFlushRoot(root.elemKind() == ekDeferred)
		types = append(types, typ)
		if root.elemKind() == ekValue && root.st.kind == stSlot && root.st.slotIndex() == slot && root.st.typ == typ {
			slot += typ.stackSlots()
			continue // already canonical
		}
		if typ == mtV128 {
			x := f.materializeV128(root)
			f.a.StrQ(SP, f.spillOff(slot), x)
			f.releaseF(x)
			slot += 2
			continue
		}
		if root.elemKind() == ekValue && (root.st.kind == stLocalReg || root.st.kind == stGlobReg) {
			if root.st.typ.isFloat() {
				f.fst(SP, f.spillOff(slot), root.st.reg, true)
			} else {
				f.st64(SP, f.spillOff(slot), root.st.reg) // copy pinned local/global's value; never release
			}
			slot++
			continue
		}
		if root.elemKind() == ekValue && root.st.typ.isFloat() {
			x := f.materializeF(root)
			f.fst(SP, f.spillOff(slot), x, true) // 8B store
			f.releaseF(x)
			slot++
			continue
		}
		r := f.materialize(root)
		f.st64(SP, f.spillOff(slot), r)
		f.release(r)
		slot++
	}
	f.spillFloor = oldFloor
	f.tmpTypes = types
	f.setDepthTypesWithGCRoots(types, gcRoots)
}

// flushWideStack stages wide or overlapping operand stacks in a disjoint frame
// range before copying them to canonical slots. Existing spilled roots and
// deferred children must survive earlier canonical stores.
func (f *fn) flushWideStack(roots []*elem, gcRoots []bool) bool {
	const wideFlushSlots = 64

	types := f.tmpFlushTypes[:0]
	total := 0
	needsStage := false
	for _, root := range roots {
		typ := rootMachineType(root)
		types = append(types, typ)
		if root.elemKind() == ekValue && root.st.kind == stSlot && root.st.slotIndex() < total ||
			root.isDeferred() && f.flushSpillBefore(root, total) {
			needsStage = true
		}
		total += typ.stackSlots()
	}
	f.tmpFlushTypes = types
	if total <= wideFlushSlots && !needsStage {
		return false
	}

	stageBase := f.curSpillSlot()
	if stageBase < total {
		stageBase = total
	}
	stageEnd := stageBase + total
	if stageEnd > f.maxSpill {
		f.maxSpill = stageEnd
	}
	oldFloor := f.spillFloor
	f.spillFloor = stageEnd

	slot := stageBase
	for i, root := range roots {
		if diagnosticsEnabled {
			f.stats.addFlushRoot(root.elemKind() == ekDeferred)
		}
		typ := types[i]
		switch {
		case typ == mtV128:
			x := f.materializeV128(root)
			f.a.StrQ(SP, f.spillOff(slot), x)
			f.releaseF(x)
		case typ.isFloat():
			x := f.materializeF(root)
			f.fst(SP, f.spillOff(slot), x, true)
			f.releaseF(x)
		default:
			r := f.materialize(root)
			f.st64(SP, f.spillOff(slot), r)
			f.release(r)
		}
		slot += typ.stackSlots()
	}
	f.spillFloor = oldFloor

	f.moveSlots(stageBase, 0, total)
	f.setDepthTypesWithGCRoots(types, gcRoots)
	return true
}

// flushSpillBefore finds existing deferred inputs that an earlier canonical
// store would overwrite. New spills are separately protected by spillFloor.
// Existing maxDeferDepth bounds this walk to maxDeferDepth+1 simultaneous calls
// including the leaf, the same trees already traversed by materialization.
func (f *fn) flushSpillBefore(root *elem, destination int) bool {
	if root == nil || destination == 0 {
		return false
	}
	if root.elemKind() == ekValue {
		return root.st.kind == stSlot && root.st.slotIndex() < destination
	}
	return root.isDeferred() &&
		(f.flushSpillBefore(f.s.arg0(root), destination) || f.flushSpillBefore(f.s.arg1(root), destination))
}

// setDepth consumes the top operands while preserving the exact machine types
// (and therefore slot widths) of the l values below them.
func (f *fn) setDepth(l int) {
	roots := f.rootsBottomToTop()
	if l < 0 || l > len(roots) {
		panic("arm64: invalid operand depth")
	}
	types := f.tmpTypes[:0]
	var gcRoots []bool
	if f.tracksGCFrameRoots() {
		gcRoots = f.tmpGCRoots[:0]
	}
	for _, root := range roots[:l] {
		types = append(types, root.st.typ)
		if gcRoots != nil {
			gcRoots = append(gcRoots, root.elemKind() == ekValue && root.st.hasGCRoot())
		}
	}
	f.tmpTypes = types
	if gcRoots != nil {
		f.tmpGCRoots = gcRoots
	}
	f.setDepthTypesWithGCRoots(types, gcRoots)
}

func (f *fn) setDepthTypesWithGCRoots(types []machineType, gcRoots []bool) {
	if f.reuseOperands {
		f.s.retireLinked()
	}
	f.s.head.prev, f.s.head.next = sentinelNodeID, sentinelNodeID
	slot := 0
	for i, typ := range types {
		value := f.pushValue(storage{kind: stSlot, typ: typ, slot: uint32(slot)})
		if i < len(gcRoots) {
			value.st.setGCRoot(gcRoots[i])
		}
		slot += typ.stackSlots()
	}
	if slot > f.maxSpill {
		f.maxSpill = slot
	}
	for i := range f.regUser {
		f.regUser[i] = nil
		f.fregUser[i] = nil
	}
	f.pinned = 0
	f.fpinned = 0
}

// moveSlots copies n canonical slots from [fromBase, fromBase+n) to
// [toBase, toBase+n). Runs only right after flush, so X0 is free as scratch.
func (f *fn) moveSlots(fromBase, toBase, n int) {
	if regallocCheckEnabled {
		done := f.checkBeginSlots(fromBase, toBase, n)
		defer done()
	}

	if fromBase == toBase {
		return
	}
	for i := 0; i < n; i++ {
		f.ld64(X0, SP, f.spillOff(fromBase+i))
		f.st64(SP, f.spillOff(toBase+i), X0)
	}
}

func isValByte(b byte) bool {
	switch b {
	case 0x7F, 0x7E, 0x7D, 0x7C, 0x7B:
		return true
	}
	return b >= 0x69 && b <= 0x74
}

// valByteMT maps a value-type byte to its machine type.
func valByteMT(b byte) machineType {
	switch b {
	case 0x7F:
		return mtI32
	case 0x7E:
		return mtI64
	case 0x7D:
		return mtF32
	case 0x7C:
		return mtF64
	case 0x7B:
		return mtV128
	case 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71, 0x72, 0x73, 0x74:
		return mtI64
	}
	return mtNone
}

// blockType decodes a block's parameter and result types, plus the first
// result's machine type (res0; mtNone when resultN == 0).
func (f *fn) blockType(r *wasm.Reader) (params, results, frameTypes []machineType, res0 machineType, err error) {
	b, ok := r.Peek()
	if !ok {
		return nil, nil, nil, mtNone, fmt.Errorf("eof in blocktype")
	}
	if b == 0x40 { // empty
		_, _ = r.Byte()
		return nil, nil, nil, mtNone, nil
	}
	if isValByte(b) {
		_, _ = r.Byte()
		mt := valByteMT(b)
		return nil, nil, nil, mt, nil
	}
	if b == 0x63 || b == 0x64 { // ref null <heaptype> / ref <heaptype>
		_, _ = r.Byte()
		if next, ok := r.Peek(); ok && next == 0x62 { // exact indexed heap prefix
			_, _ = r.Byte()
		}
		if _, e := r.S33(); e != nil {
			return nil, nil, nil, mtNone, e
		}
		return nil, nil, nil, mtI64, nil
	}
	x, e := r.I64()
	if e != nil {
		return nil, nil, nil, mtNone, e
	}
	ft, ok := f.m.TypeFunc(uint32(x))
	if x < 0 || !ok {
		return nil, nil, nil, mtNone, fmt.Errorf("bad blocktype index %d", x)
	}
	r0 := mtNone
	if len(ft.Results) > 0 {
		r0 = mtOf(ft.Results[0])
	}
	frameTypes = make([]machineType, len(ft.Params)+len(ft.Results))
	for i, typ := range ft.Params {
		frameTypes[i] = mtOf(typ)
	}
	for i, typ := range ft.Results {
		frameTypes[len(ft.Params)+i] = mtOf(typ)
	}
	return frameTypes[:len(ft.Params)], frameTypes[len(ft.Params):], frameTypes, r0, nil
}

// placeSingleResult produces the single result value (top of the operand stack)
// directly in the return register — X0 (int) or V0 (float) — the WARP target
// hint for returns, skipping the flush-to-slot + epilogue-reload round trip. Only
// used when f.singleRegResult holds.
func (f *fn) placeSingleResult() {
	e := f.s.back()
	if f.resultFloat {
		x := f.materializeF(e)
		if x != 0 {
			// The source is materialized; only the terminal result move may
			// reuse body-cache registers. Other emitted arms keep their masks.
			var writes regallocWriteMask
			if regallocCheckEnabled {
				writes = f.checkTerminalWrites()
			}
			f.a.FmovReg(0, x, f.resultF64) // -> V0
			if regallocCheckEnabled {
				f.checkRestoreWrites(writes)
			}
		}
		f.releaseF(x)
	} else {
		f.condenseInto(e, X0)
	}
	f.erase(e)
}

// reconcileMerge1 is the fall-through edge into a regMerge1 block: flush the
// operands below the result to their canonical slots and produce the single
// result directly in mergeReg (no slot store for the value itself).
func (f *fn) reconcileMerge1(fr *ctrlFrame) {
	top := f.s.back()
	f.flushBelow(top)
	if fr.res0.isFloat() {
		x := f.materializeF(top)
		if x != mergeFReg {
			f.a.FmovReg(mergeFReg, x, fr.res0 == mtF64)
		}
		f.releaseF(x)
	} else {
		f.condenseInto(top, mergeReg)
	}
	f.erase(top)
}

// branchEdgeToMerge1 is a branch edge (br / br_if / br_table / fused) into a
// regMerge1 block: the result has already been flushed to its canonical slot at
// depth d-1; load it into mergeReg so the merge finds the value there. The slot
// copy is left intact so a br_if fall-through still sees the value.
func (f *fn) branchEdgeToMerge1(fr *ctrlFrame, d int) {
	f.recordGCBranchResults(fr, 1)
	slot := slotOfLogicalTypes(f.currentLogicalTypes(), d-1)
	if fr.res0.isFloat() {
		f.fld(mergeFReg, SP, f.spillOff(slot), fr.res0 == mtF64)
	} else {
		f.ld64(mergeReg, SP, f.spillOff(slot))
	}
}

// convergeBranchLocals converges pinned-local state for a br/br_if/br_table
// edge into fr's branch target. Function-frame targets (returns) need nothing —
// the locals die — so nothing is emitted, keeping conditional returns free.
func (f *fn) convergeBranchLocals(fr *ctrlFrame) {
	if fr.kind == cfFunc {
		return
	}
	if callFreeLoopRegionEnabled && (fr.has(ctrlLoopCallFree) || fr.has(ctrlCallFreeRegion)) {
		return
	}
	f.convergeFrameBranchState(fr)
}

// Blocks inherit the nearest loop. Only leaving a loop requires searching for
// its parent, rather than searching the entire control stack at every block.
func (f *fn) inCallFreeLoop() bool {
	return f.innermostLoop != 0 && f.ctrl[f.innermostLoop-1].has(ctrlLoopCallFree)
}

func (f *fn) popCtrl() {
	last := len(f.ctrl) - 1
	fr := f.ctrl[last]
	f.ctrl[last] = ctrlFrame{mergeIndex: fr.mergeIndex}
	f.ctrl = f.ctrl[:last]
	if uint32(last+1) != f.innermostLoop {
		return
	}
	f.innermostLoop = 0
	for i := last - 1; i >= 0; i-- {
		if f.ctrl[i].kind == cfLoop {
			f.innermostLoop = uint32(i + 1)
			return
		}
	}
}

// callFreeLoopExit reports whether a branch to frame fi exits a surrounding
// loop whose body contains no operation that can enter native or Go code. Such
// an edge may defer local reconciliation to its taken path: no call on the hot
// fall-through can observe the canonical slots before the loop is exited.
func (f *fn) callFreeLoopExit(fi int) bool {
	if !f.opt(optCallFreeLoopColdExit) {
		return false
	}
	for i := len(f.ctrl) - 1; i > fi; i-- {
		if f.ctrl[i].kind == cfLoop && f.ctrl[i].has(ctrlLoopCallFree) {
			return true
		}
	}
	return false
}

// branchJump emits the jump for a branch that targets frame fr.
func (f *fn) branchJump(fr *ctrlFrame) {
	switch fr.kind {
	case cfLoop:
		// Backward unconditional branch to the loop top (imm26, ±128 MiB): emit a B
		// placeholder and patch it immediately since the target is already known.
		f.patchBranch26(f.a.Branch(), fr.controlSite)
	case cfFunc:
		// Result slots are canonical; this taken edge leaves the function.
		var writes regallocWriteMask
		if regallocCheckEnabled {
			writes = f.checkTerminalWrites()
		}
		// The caller already converged the result to slot 0 (fr.height == 0); with
		// the register-return hint the epilogue no longer reloads it, so load it
		// into the return register here so every exit agrees on X0/V0 = result.
		if f.singleRegResult {
			if f.resultFloat {
				f.fld(0, SP, f.spillOff(0), f.resultF64)
			} else {
				f.ld64(X0, SP, f.spillOff(0))
			}
		}
		f.appendReturnSite(f.a.Branch())
		if regallocCheckEnabled {
			f.checkRestoreWrites(writes)
		}
	default:
		f.appendFrameEnd(fr, f.a.Branch(), false)
		fr.set(ctrlEndReachable, true)
	}
}

// condBranchJump emits a single conditional branch (taken when cc holds) to
// frame fr's target — the empty-edge fast path for br_if. It replaces the
// `B.cond(skip) ; B target` double-branch with one instruction and no padding
// NOP, which matters in tight loops where the fall-through NOP would otherwise
// execute every iteration. Returns false (emitting nothing) when it cannot lower
// the edge — a function-frame target (conditional return; the branch carries the
// result load) or a backward loop target out of the conditional branch's imm19
// (±1 MiB) range — so the caller falls back to the guarded double-branch form.
func (f *fn) condBranchJump(fr *ctrlFrame, cc Cond) bool {
	switch fr.kind {
	case cfLoop:
		site := f.a.Bcond(cc)
		if !f.a.PatchBranch19(site, fr.controlSite) {
			if profileEnabled && f.stats != nil && f.stats.RecordSources {
				f.rewindProfileEmission(site)
			}
			f.a.B = f.a.B[:site] // out of imm19 range: undo and let the caller fall back
			return false
		}
		return true
	case cfBlock, cfIf:
		f.appendFrameEnd(fr, f.a.Bcond(cc), true) // patched to the block end (imm19)
		fr.set(ctrlEndReachable, true)
		return true
	}
	return false // cfFunc: the guarded form carries the singleRegResult load
}

// zeroBranchJump is condBranchJump for a materialized i32 condition whose
// branch is taken when nonzero. It is used only after proving the edge emitted
// no reconciliation code, so the condition register still holds the tested
// value and no flags window is required.
func (f *fn) zeroBranchJump(fr *ctrlFrame, condition Reg) bool {
	switch fr.kind {
	case cfLoop:
		site := f.a.Cbnz32(condition)
		if !f.a.PatchBranch19(site, fr.controlSite) {
			if profileEnabled && f.stats != nil && f.stats.RecordSources {
				f.rewindProfileEmission(site)
			}
			f.a.B = f.a.B[:site]
			return false
		}
		return true
	case cfBlock, cfIf:
		f.appendFrameEnd(fr, f.a.Cbnz32(condition), true)
		fr.set(ctrlEndReachable, true)
		return true
	}
	return false
}

// alignLoopHeader keeps poll-free loop bodies in the same half of a 32-byte
// fetch block that the former four-instruction cooperative poll selected. The
// padding precedes loopStart, so it executes only on initial entry and never on
// a backedge. Large, loop-dense functions skip it: their extra code footprint
// costs more than preserving the fetch phase.
func (f *fn) alignLoopHeader() {
	// Materialize any phase debt before choosing this hot loop's address. This
	// preserves the exact alignment decision the unoptimized stream would have
	// made while keeping the deleted instructions off every backedge.
	f.emitPhasePadding()
	loopAlign := f.policy.LoopAlignLog2
	if loopAlign == 0 {
		loopAlign = 4
	}
	scopedFloat := f.nLocals <= pollFreeLoopPhaseMaxLocals && len(f.fconsts) != 0 &&
		f.fconsts[0].loopDepth != 0
	if scopedFloat && loopAlign < 5 {
		// A cached FP constant removes one instruction from the loop body.
		// Put the shortened body on a full fetch boundary instead of preserving
		// the phase chosen for the uncached four-instruction poll.
		loopAlign = 5
	}
	f.alignCode(loopAlign)
	if loopAlign >= 4 && !f.interruptible && f.nLocals <= pollFreeLoopPhaseMaxLocals && !scopedFloat {
		f.a.Nop4()
	}
}

// scanLoopSetLocals scans a loop body ahead from the reader's current position
// (the body start, just past the blocktype) to the matching `end`, recording the
// locals it sets. The module-aware classifier keeps mixed memory-width
// immediates synchronized. Any unexpected decode failure returns no proof.
func scanLoopSetLocals(r *wasm.Reader, classifier wasm.ModuleInstructionClassifier, dst []uint16, uses *scopedLoopConstUses) (setLocals []uint16, hasCall bool) {
	start := r.Offset()
	defer func() { _ = r.JumpTo(start) }()
	base := len(dst)
	setLocals = dst
	if setLocals == nil {
		setLocals = []uint16{}
	}
	depth := 0
	var lowLocals uint64
	var imm wasm.InstructionImmediate
	for {
		op, err := r.Byte()
		if err != nil {
			return nil, true
		}
		// Numeric scalar instructions have neither immediates nor effects on
		// locals/call admission. Local operations need only their index, so do
		// not construct a complete classification for these common cases.
		if op >= 0x45 && op <= 0xc4 {
			continue
		}
		if op >= 0x20 && op <= 0x22 {
			index, err := r.U32()
			if err != nil || op != 0x20 && index > uint32(^uint16(0)) {
				return nil, true
			}
			if op != 0x20 {
				if index < 64 {
					lowLocals |= uint64(1) << index
				} else {
					setLocals = append(setLocals, uint16(index))
				}
			}
			continue
		}
		if uses != nil && (op == 0x41 || op == 0x42) {
			imm = wasm.InstructionImmediate{}
			imm.Kind, err = uses.consume(op, r)
			if err != nil {
				return nil, true
			}
		} else if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return nil, true
		}
		if uses != nil && usesBulkScratch(imm.Kind) {
			uses.blocked = true
			uses.mask = 0
		}
		if loopInstructionMayCall(imm.Kind) {
			hasCall = true
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x0b:
			if depth == 0 {
				wide := len(setLocals) != base
				for lowLocals != 0 {
					index := bits.TrailingZeros64(lowLocals)
					setLocals = append(setLocals, uint16(index))
					lowLocals &= lowLocals - 1
				}
				if wide {
					modified := setLocals[base:]
					slices.Sort(modified)
					modified = slices.Compact(modified)
					setLocals = setLocals[:base+len(modified)]
				}
				return
			}
			depth--
		}
	}
}

func loopInstructionMayCall(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrReturnCall,
		wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef,
		wasm.InstrMemoryGrow:
		return true
	default:
		return gcOrAtomicInstructionMayCall(kind)
	}
}

func (f *fn) opBlock(r *wasm.Reader, op byte) error {
	paramTypes, resultTypes, frameTypes, res0, err := f.blockType(r)
	if err != nil {
		return err
	}
	pN, rN := len(paramTypes), len(resultTypes)
	if frameTypes == nil && res0 != mtNone {
		rN = 1
	}
	kind := cfBlock
	if op == 0x03 {
		kind = cfLoop
	} else if op == 0x04 {
		kind = cfIf
	}
	if kind == cfIf && !f.unreachable && pN == 0 && rN == 1 && res0 == mtI32 {
		if done, err := f.tryAffineIfSelect(r); done || err != nil {
			return err
		}
		if done, err := f.trySimpleIfLocalSet(r); done || err != nil {
			return err
		}
	}
	fr := ctrlFrame{kind: kind, paramN: pN, resultN: rN, controlSite: -1, res0: res0, types: frameTypes}
	fr.set(ctrlEntryUnreachable, f.unreachable)
	if f.straightLineBlock(r, kind, pN, rN, res0) {
		fr.controlSite = straightLineBlockSite
	}
	if kind == cfLoop {
		fr.branchN = uint32(pN)
	} else {
		fr.branchN = uint32(rN)
	}
	// Phase 2/3: a block or if producing exactly one result (int → mergeReg, float
	// → mergeFReg) carries that value in a register across all its edges (fall-
	// through, else, br/br_if/br_table, and an if's cond-false passthrough) instead
	// of a frame slot. Excludes loops (params, back-edge) and multi-value.
	fr.set(ctrlRegMerge1, f.regMerge && (kind == cfBlock || kind == cfIf) && rN == 1 && res0 != mtNone && res0 != mtV128)
	fr.set(ctrlCallFreeRegion, callFreeLoopRegionEnabled && kind != cfLoop && f.inCallFreeLoop())
	if kind == cfLoop && !f.unreachable && ((diagnosticsEnabled && f.stats != nil) || f.usesCalls && (f.pinnedLocalMask != 0 || f.fpinnedLocalMask != 0)) {
		base := len(f.loopSetLocals)
		uses, collectConsts := f.scopedLoopConstUses()
		var collect *scopedLoopConstUses
		if collectConsts {
			collect = &uses
		}
		setLocals, hasCall := scanLoopSetLocals(r, f.classifier, f.loopSetLocals, collect)
		if collectConsts && !hasCall {
			fr.flags |= ctrlFlags(uses.mask) << 12
		}
		if setLocals != nil {
			f.loopSetLocals = setLocals
			cold := f.ensureCtrlMerge(&fr)
			cold.setLoopSet(uint32(base), uint16(len(setLocals)-base))
			callFree := !hasCall && !f.moduleEH && len(f.customInstructions) == 0
			fr.set(ctrlLoopCallFree, callFree)
			if callFree {
				f.stats.peep("callfree-loop")
			}
		}
	}
	if f.unreachable {
		f.pushCtrl(&fr)
		f.releaseCtrlMerge(&fr)
		return nil
	}
	if fr.isStraightLineBlock() {
		fr.set(ctrlRegMerge1, false)
		fr.height = f.depth()
		f.pushCtrl(&fr)
		f.stats.peep("straight-line-block")
		return nil
	}
	if kind == cfIf {
		if !fr.has(ctrlCallFreeRegion) {
			f.convergeFrameEntryState(&fr) // header snapshot: else entry / cond-false edge state
		}
		if isFusableCondition(f.s.back()) {
			cond := f.s.back()
			f.flushBelow(cond)
			if f.opt(optZeroBranch) {
				if creg, cOwned, wide, ok := f.condenseSimpleEqzOperand(cond); ok {
					fr.height = f.depth() - pN
					f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
					f.captureGCFrameShape(&fr)
					if wide {
						fr.controlSite = f.a.Cbnz64(creg)
					} else {
						fr.controlSite = f.a.Cbnz32(creg)
					}
					if cOwned {
						f.release(creg)
					}
					f.stats.peep("zero-branch")
					f.pushCtrl(&fr)
					return nil
				}
			}
			cc := f.condenseToFlags(cond)
			fr.height = f.depth() - pN
			f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
			f.captureGCFrameShape(&fr)
			fr.controlSite = f.a.Bcond(invertCond(cc)) // to else/end when false
			f.pushCtrl(&fr)
			return nil
		}
		creg, cOwned := f.materializeRead(f.popValue()) // the test only reads: a pinned local needs no copy
		fr.height = f.depth() - pN
		f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
		f.captureGCFrameShape(&fr)
		f.flush()
		if f.opt(optZeroBranch) {
			fr.controlSite = f.a.Cbz32(creg) // false edge; flags are dead at this control edge
			f.stats.peep("zero-branch")
		} else {
			f.a.CmpImm32(creg, 0) // CMP creg, #0 — sets NZCV (no x86 test/flag side effect)
			fr.controlSite = f.a.Bcond(condE)
		}
		if cOwned {
			f.release(creg)
		}
	} else {
		fr.height = f.depth() - pN
		f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
		f.captureGCFrameShape(&fr)
		if kind == cfLoop {
			if fr.has(ctrlLoopCallFree) && callFreeLoopEntryEnabled {
				// Keep live pins in registers and reload only memory-resident ones.
				// With no call in the body, every backedge preserves that state.
				f.prepareCallFreeLoopEntry()
			} else {
				// Calling loops converge eagerly (all lsStackReg), hoisting any
				// post-call reload out of the body.
				f.reconcileLocals()
				f.convergeFrameBranchState(&fr) // records the all-lsStackReg target
			}
		}
		f.flush()
		if kind == cfLoop {

			f.preloadScopedLoopConsts(&fr)
			f.preloadScopedFloatConsts(r)
			f.alignLoopHeader()
			fr.controlSite = f.a.Len()
			f.emitInterruptCheck(true)
		}
	}
	f.pushCtrl(&fr)
	return nil
}

// tryAffineIfSelect if-converts a side-effect-free integer diamond of the form
//
//	if (result i32) cond { 0 } else { (local + imm) * powerOfTwo }
//
// into ADD/LSL/CSEL. Besides removing both branches, this deliberately leaves
// operands below the condition in their existing registers. The ordinary
// structured-control path must canonicalize those operands at the split, which
// can otherwise spill a borrowed address in tight construction loops.
func (f *fn) tryAffineIfSelect(r *wasm.Reader) (bool, error) {
	r2 := *r
	op, err := r2.Byte()
	if err != nil || op != 0x41 {
		return false, nil
	}
	zero, err := r2.I32()
	if err != nil || zero != 0 {
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x05 { // else
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x20 { // local.get
		return false, nil
	}
	local, err := r2.U32()
	if err != nil {
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x41 {
		return false, nil
	}
	add, err := r2.I32()
	if err != nil || add < 0 || add > 0xfff {
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x6a { // i32.add
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x41 {
		return false, nil
	}
	mul, err := r2.I32()
	if err != nil || mul <= 0 || mul&(mul-1) != 0 {
		return false, nil
	}
	shift := uint8(0)
	for v := uint32(mul); v > 1; v >>= 1 {
		shift++
	}
	if shift >= 32 {
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x6c { // i32.mul
		return false, nil
	}
	if op, err = r2.Byte(); err != nil || op != 0x0b { // end if
		return false, nil
	}
	x := int(local) + f.localBase
	if x < 0 || x >= len(f.localType) || f.localType[x] != mtI32 {
		return false, nil
	}
	src, isFloat, ok := f.pinReg(x)
	if !ok || isFloat {
		return false, nil
	}
	cond := f.s.back()
	if cond == nil {
		return false, fmt.Errorf("arm64: if without condition")
	}
	if err := r.JumpTo(r2.Offset()); err != nil {
		return false, err
	}
	f.recoverLocal(x)
	// When the condition is `(local + (add-1)) == other`, reuse that exact
	// intermediate with CSINC: equality selects zero, while inequality selects
	// intermediate+1. This computes the affine arm and the selection in four
	// instructions (ADD/CMP/CSINC/LSL) without materializing both arms.
	if cond.isDeferred() && cond.deferredOp() == opEq && add > 0 {
		left, right := f.s.arg0(cond), f.s.arg1(cond)
		var sum, other *elem
		if left != nil && left.isDeferred() && left.deferredOp() == opAdd {
			sum, other = left, right
		} else if right != nil && right.isDeferred() && right.deferredOp() == opAdd {
			sum, other = right, left
		}
		if sum != nil && other != nil && other.elemKind() == ekValue && other.st.kind == stLocalReg {
			a, b := f.s.arg0(sum), f.s.arg1(sum)
			matches := func(localElem, constElem *elem) bool {
				return localElem != nil && localElem.elemKind() == ekValue &&
					(localElem.st.kind == stLocalReg || localElem.st.kind == stLocalRef) && localElem.st.idx == uint32(x) &&
					constElem != nil && constElem.elemKind() == ekValue && constElem.st.kind == stConst && constElem.st.cval == int64(add-1)
			}
			if matches(a, b) || matches(b, a) {
				dest := f.allocReg(maskOf(src, other.st.reg))
				f.a.AddImm32(dest, src, uint32(add-1))
				f.cmpRR(dest, other.st.reg, false)
				f.a.Csinc32(dest, ZR, dest, condE)
				if shift != 0 {
					f.a.LslImm(dest, dest, shift, true)
				}
				f.consumeBlockBelow(cond)
				f.erase(cond)
				result := f.pushReg(dest, mtI32)
				result.st.setValueFacts(factUpper32Zero)
				f.stats.peep("if-affine-select")
				f.stats.peep("if-affine-csinc")
				return true, nil
			}
		}
	}
	dest := f.allocReg(maskOf(src))
	f.a.AddImm32(dest, src, uint32(add))
	if shift != 0 {
		f.a.LslImm(dest, dest, shift, true)
	}
	f.pinned = f.pinned.add(dest)
	var cc Cond
	if isFusableCondition(cond) {
		cc = f.condenseToFlags(cond)
	} else {
		creg, owned := f.materializeRead(f.popValue())
		f.a.CmpImm32(creg, 0)
		cc = condNE
		if owned {
			f.release(creg)
		}
	}
	f.a.Csel(dest, ZR, dest, cc, false)
	f.pinned = f.pinned.remove(dest)
	result := f.pushReg(dest, mtI32)
	result.st.setValueFacts(factUpper32Zero)
	f.stats.peep("if-affine-select")
	return true, nil
}

// trySimpleIfLocalSet fuses a bounded, side-effect-free integer if immediately
// consumed by local.set of the same pinned local:
//
//	if (result i32) cond { x op= immA } else { x op= immB }; local.set x
//
// Both arms are single local.get/constant add-or-sub trees. The chosen arm writes
// x directly, avoiding the merge-register copy which an eager structured merge
// otherwise needs. The branch remains (three dynamic instructions on the common
// path), which is cheaper than evaluating both arms plus CSEL on this shape.
func (f *fn) trySimpleIfLocalSet(r *wasm.Reader) (bool, error) {
	type arm struct {
		local uint32
		op    wOp
		imm   int64
	}
	readArm := func(rr *wasm.Reader) (arm, bool) {
		var a arm
		op, err := rr.Byte()
		if err != nil || op != 0x20 {
			return a, false
		}
		a.local, err = rr.U32()
		if err != nil {
			return a, false
		}
		op, err = rr.Byte()
		if err != nil || op != 0x41 {
			return a, false
		}
		v, err := rr.I32()
		if err != nil {
			return a, false
		}
		a.imm = int64(v)
		op, err = rr.Byte()
		if err != nil {
			return a, false
		}
		switch op {
		case 0x6a:
			a.op = opAdd
		case 0x6b:
			a.op = opSub
		default:
			return a, false
		}
		v = int32(a.imm)
		if v < -0xfff || v > 0xfff {
			return a, false
		}
		return a, true
	}

	r2 := *r
	thenArm, ok := readArm(&r2)
	if !ok {
		return false, nil
	}
	op, err := r2.Byte()
	if err != nil || op != 0x05 { // else
		return false, nil
	}
	elseArm, ok := readArm(&r2)
	if !ok || elseArm.local != thenArm.local {
		return false, nil
	}
	op, err = r2.Byte()
	if err != nil || op != 0x0b { // end if
		return false, nil
	}
	op, err = r2.Byte()
	if err != nil || op != 0x21 { // local.set
		return false, nil
	}
	x32, err := r2.U32()
	if err != nil || x32 != thenArm.local {
		return false, nil
	}
	x := int(x32) + f.localBase
	dest, isFloat, pinned := f.pinReg(x)
	if !pinned || isFloat || x < 0 || x >= len(f.localType) || f.localType[x] != mtI32 {
		return false, nil
	}
	if err := r.JumpTo(r2.Offset()); err != nil {
		return false, err
	}
	f.invalidateBoundsCertFor(1, uint32(x))
	cond := f.s.back()
	if cond == nil {
		return false, fmt.Errorf("arm64: if without condition")
	}
	f.realizeLocalRefs(x, f.s.baseOfValentBlock(cond))
	creg, cOwned := f.materializeRead(f.popValue())
	var toElse int
	if f.opt(optZeroBranch) {
		toElse = f.a.Cbz32(creg)
		f.stats.peep("zero-branch")
	} else {
		f.a.CmpImm32(creg, 0)
		toElse = f.a.Bcond(condE)
	}
	if cOwned {
		f.release(creg)
	}
	if !f.aluImm3(thenArm.op, dest, dest, thenArm.imm, false) {
		panic("arm64: prechecked if arm immediate became unencodable")
	}
	toEnd := f.a.Branch()
	f.patchBranch19(toElse, f.a.Len())
	if !f.aluImm3(elseArm.op, dest, dest, elseArm.imm, false) {
		panic("arm64: prechecked if arm immediate became unencodable")
	}
	f.patchBranch26(toEnd, f.a.Len())
	f.markLocalDirty(x)
	f.stats.peep("if-local-sink")
	return true, nil
}

func (f *fn) markEHReferenceResults(fr *ctrlFrame) {
	eh := f.frameEH(fr)
	if eh == nil {
		return
	}
	e := f.s.back()
	for i := fr.resultN - 1; i >= 0; i-- {
		if i < ehMaxPayloadWords+1 && eh.refResults&(1<<i) != 0 {
			e.st.setEHRoot(true)
		}
		e = f.s.prev(e)
	}
}

func (f *fn) opElse() error {
	fr := &f.ctrl[len(f.ctrl)-1]
	if fr.has(ctrlEntryUnreachable) {
		return nil
	}
	if f.unreachable {
		f.unreachable = false // else edge is reachable (cond-false analogue)
	} else {
		// The then-branch jumps to the if's end — a merge edge like any br
		// (#68's root cause was skipping this). Converge to the end's recorded
		// state; as the chronologically first end edge it usually fixes it.
		f.recordGCBranchResults(fr, fr.resultN)
		if !fr.has(ctrlCallFreeRegion) {
			f.convergeFrameBranchState(fr)
		}
		if fr.has(ctrlRegMerge1) {
			f.reconcileMerge1(fr) // then-branch result → mergeReg
		} else {
			f.flush()
		}
		f.appendFrameEnd(fr, f.a.Branch(), false)
		fr.set(ctrlEndReachable, true)
	}
	f.patchBranch19(fr.controlSite, f.a.Len()) // the false edge is a B.cond (imm19)
	fr.controlSite = -1
	fr.set(ctrlHasElse, true)
	f.setDepthTypesWithGCRoots(f.frameDepthTypesForFrame(fr, true), frameGCRootFlags(f.frameBaseGCRoots(fr), f.frameParamGCRoots(fr)))
	// The else body is entered via the if's false edge: locals are exactly in the
	// header-snapshot state (no code).
	if fr.has(ctrlCallFreeRegion) {
		f.markPinnedLocalsDirty()
	} else {
		f.setLocalsState(f.frameEntryState(fr))
	}
	return nil
}

func (f *fn) opEnd(r *wasm.Reader) error {
	last := len(f.ctrl) - 1
	fr := f.ctrl[last]
	if fr.kind == cfLoop {
		defer f.releaseScopedFloatConsts(last + 1)
	}
	if fr.has(ctrlLoopConstScope) {
		defer f.releaseScopedLoopConsts(uint8(fr.flags >> 12))
	}
	if fr.kind == cfLoop && f.linearSumLoopDepth == uint16(last+1) {
		f.linearSumLoop = 0
		f.linearSumLoopDepth = 0
	}
	branchState := f.frameBranchState(&fr)
	entryState := f.frameEntryState(&fr)
	baseGCRoots := f.frameBaseGCRoots(&fr)
	paramGCRoots := f.frameParamGCRoots(&fr)
	resultGCRoots := f.frameResultGCRoots(&fr)
	firstEnd, secondEnd, ends := f.frameEndSites(&fr)
	coldEdges := f.frameColdEdges(&fr)
	// ctrl backing is reused across functions. Clear the popped slot so its
	// variable-sized type and loop-analysis slices do not stay live in scratch.
	f.popCtrl()

	if fr.isStraightLineBlock() {
		f.releaseCtrlMerge(&fr)
		if f.unreachable || f.depth() != fr.height+fr.resultN {
			return fmt.Errorf("straight-line block stack height mismatch")
		}
		return nil
	}

	if fr.kind == cfFunc {
		if !f.unreachable {
			if f.singleRegResult {
				f.placeSingleResult() // fall-through return: result straight to X0/V0
			} else {
				f.flush() // results land in slots [0, resultN)
			}
		}
		if len(coldEdges) != 0 {
			skip := -1
			if !f.unreachable {
				skip = f.a.Branch()
			}
			for i := range coldEdges {
				f.patchBranch19(coldEdges[i].site, f.a.Len())
				f.a.B = append(f.a.B, coldEdges[i].code...)
				f.branchJump(&fr) // branch from the cold edge to the shared epilogue
			}
			if skip != -1 {
				f.patchBranch26(skip, f.a.Len())
			}
		}
		return nil
	}

	fallthroughReachable := !f.unreachable
	if fallthroughReachable {
		f.recordGCBranchResults(&fr, fr.resultN)
		resultGCRoots = f.frameResultGCRoots(&fr)
		if fr.kind != cfLoop && !fr.has(ctrlCallFreeRegion) {
			// Merge edge: converge to the end's recorded state (or fix it).
			// A loop end is NOT a merge — br edges target the loop TOP — so the
			// fall-through's state simply flows out.
			deadGP, deadFP := f.planForwardMergeDeadLocals(r, branchState, packedLocStates{})
			f.convergeFrameBranchStateWithDead(&fr, deadGP, deadFP)
			branchState = f.frameBranchState(&fr)
		}
		if fr.has(ctrlRegMerge1) {
			f.reconcileMerge1(&fr) // result → mergeReg, operands below → slots
		} else {
			f.flush() // results at [height, height+resultN)
		}
	}
	// An if without else: the cond-false path reaches end with params == results.
	if fr.kind == cfIf && !fr.has(ctrlHasElse) && !fr.has(ctrlEntryUnreachable) {
		for i := 0; i < len(paramGCRoots) && i < fr.resultN; i++ {
			if paramGCRoots[i] {
				f.setFrameResultGCRoot(&fr, i)
			}
		}
		resultGCRoots = f.frameResultGCRoots(&fr)
		// The cond-false edge arrives in the header-snapshot state; if then-side
		// edges fixed a stronger end state (or a regMerge1 passthrough needs its
		// value in mergeReg), a stub on this edge converges it. The then
		// fall-through jumps over the stub.
		deadGP, deadFP := f.planForwardMergeDeadLocals(r, branchState, entryState)
		needLoads := false
		if f.usesCalls && !branchState.empty() && !entryState.empty() {
			for x := 0; x < f.nLocals; x++ {
				reg, isFloat, ok := f.pinReg(x)
				if !ok || branchState.get(x) != lsStackReg || entryState.get(x) != lsMem {
					continue
				}
				dead := deadGP.has(reg)
				if isFloat {
					dead = deadFP.has(reg)
				}
				if !dead {
					needLoads = true
					break
				}
			}
		}
		skip := -1
		if (fr.has(ctrlRegMerge1) || needLoads) && fallthroughReachable {
			skip = f.a.Branch()
		}
		f.patchBranch19(fr.controlSite, f.a.Len()) // the false edge is a B.cond (imm19)
		if fr.has(ctrlRegMerge1) {
			slot := slotsOfTypes(f.frameBaseTypes(&fr))
			if fr.res0.isFloat() {
				f.fld(mergeFReg, SP, f.spillOff(slot), fr.res0 == mtF64) // passthrough → mergeFReg
			} else {
				f.ld64(mergeReg, SP, f.spillOff(slot)) // passthrough value → mergeReg
			}
		}
		// Converge the cond-false edge from the header snapshot into the end state
		// (records it when this is the only end edge).
		if fr.has(ctrlCallFreeRegion) {
			f.markPinnedLocalsDirty()
		} else {
			f.setLocalsState(entryState)
			f.convergeFrameBranchStateWithDead(&fr, deadGP, deadFP)
			branchState = f.frameBranchState(&fr)
		}
		if skip != -1 {
			f.patchBranch26(skip, f.a.Len()) // the skip is an unconditional B (imm26)
		}
		fr.set(ctrlEndReachable, true)
	}
	if fr.kind == cfLoop && len(coldEdges) != 0 {
		skip := -1
		if fallthroughReachable {
			skip = f.a.Branch()
		}
		for i := range coldEdges {
			f.patchBranch19(coldEdges[i].site, f.a.Len())
			f.a.B = append(f.a.B, coldEdges[i].code...)
			f.patchBranch26(f.a.Branch(), fr.controlSite)
		}
		if skip != -1 {
			f.patchBranch26(skip, f.a.Len())
		}
	}
	// Emit deferred cold br_if edges immediately before this frame's target. A
	// hinted false path therefore falls through at its source; only the unlikely
	// true edge reaches these fragments. Each fragment branches to the target
	// below along with ordinary forward edges.
	if fr.kind != cfLoop && len(coldEdges) != 0 {
		// A normal fall-through must not execute a cold reconciliation fragment.
		// Its skip and every cold-edge jump converge at the target below.
		skip := -1
		if fallthroughReachable {
			skip = f.a.Branch()
		}
		for i := range coldEdges {
			f.patchBranch19(coldEdges[i].site, f.a.Len())
			f.a.B = append(f.a.B, coldEdges[i].code...)
			f.appendFrameEnd(&fr, f.a.Branch(), false)
			firstEnd, secondEnd, ends = f.frameEndSites(&fr)
			fr.set(ctrlEndReachable, true)
		}
		if skip != -1 {
			f.patchBranch26(skip, f.a.Len())
		}
	}
	f.patchFrameEndSites(firstEnd, secondEnd, ends)
	endReachable := fallthroughReachable || fr.has(ctrlEndReachable)
	f.unreachable = !endReachable
	if endReachable {
		if fr.kind != cfLoop {
			if fr.has(ctrlCallFreeRegion) {
				f.markPinnedLocalsDirty()
			} else {
				f.setLocalsState(branchState) // merge: what every edge guaranteed
			}
		}
		if fr.has(ctrlRegMerge1) {
			// Every reaching edge left the result in the merge register (int→mergeReg,
			// float→mergeFReg) and the operands below in canonical slots [0, height).
			f.setDepthTypesWithGCRoots(f.frameBaseTypes(&fr), baseGCRoots)
			var result *elem
			if fr.res0.isFloat() {
				result = f.pushFReg(mergeFReg, fr.res0)
			} else {
				result = f.pushReg(mergeReg, fr.res0)
			}
			if len(resultGCRoots) != 0 {
				result.st.setGCRoot(resultGCRoots[0])
			}
		} else {
			f.setDepthTypesWithGCRoots(f.frameDepthTypesForFrame(&fr, false), frameGCRootFlags(baseGCRoots, resultGCRoots))
		}
		f.markEHReferenceResults(&fr)
	}
	if fr.kind == cfTry && !fr.has(ctrlEntryUnreachable) {
		recordOff := f.ehRecordOff(int(f.ensureFrameEH(&fr).recordIndex))
		if endReachable {
			f.restorePreviousEHHandler(recordOff)
		}
		skip := -1
		if endReachable {
			skip = f.a.Branch()
		}
		f.emitEHHandler(&fr)
		if skip != -1 {
			f.patchBranch26(skip, f.a.Len())
		}
		f.ehTryDepth--
	}

	f.releaseCtrlMerge(&fr)
	f.freeEndsBuf(ends)
	f.releaseFrameBaseTypes(&fr)
	return nil
}

// branchHandlerRecord identifies the outermost installed try exited by an edge.
// Handler records form a dense depth-indexed prefix. A target retains the prefix
// it had at entry (including itself for a try target, whose end pops it). The
// packed depth avoids rescanning arbitrarily deep non-try control nests. The top
// frame also excludes the already-popped record while emitting a catch route.
func (f *fn) branchHandlerRecord(fi int) int {
	if f.ehTryDepth != 0 && f.ctrl[fi].ehDepth < f.ctrl[len(f.ctrl)-1].ehDepth {
		return int(f.ctrl[fi].ehDepth)
	}
	return -1
}

// Emit only on the taken edge, alongside its value/local reconciliation.
func (f *fn) restoreBranchHandlers(fi int) {
	if record := f.branchHandlerRecord(fi); record >= 0 {
		f.restorePreviousEHHandler(f.ehRecordOff(record))
	}
}

// branchToFrame emits an unconditional branch edge to control frame fi: converge
// pinned locals, flush operands, move the branched values into the frame's
// canonical slots (or merge register), and jump. Shared by opBr's unconditional
// path and opReturn's inlined-callee routing. The caller sets f.unreachable.
func (f *fn) branchToFrame(fi int) {
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	a, d := fr.branchArity(), f.depth()
	f.flush()
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, a)
	}
	f.restoreBranchHandlers(fi)
	f.branchJump(fr)
}

func (f *fn) opBr(r *wasm.Reader, conditional bool) error {
	if f.unreachable {
		if conditional {
			// still need to consume nothing extra; label follows
		}
		_, err := r.U32() // label
		return err
	}
	// Fuse `<compare> br_if L` into CMP + conditional jump. (Local convergence is
	// per-target and happens after the label frame is resolved.)
	if conditional && isFusableCondition(f.s.back()) {
		top := f.s.back()
		idx, err := r.U32()
		if err != nil {
			return err
		}
		if done, err := f.brIfSimpleEqz(r, top, idx); done || err != nil {
			return err
		}
		return f.brIfFused(top, idx)
	}
	var creg Reg
	cOwned := false
	if conditional {
		creg, cOwned = f.materializeRead(f.popValue()) // the test only reads
	}
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	if !conditional {
		f.branchToFrame(fi)
		f.unreachable = true
		return nil
	}
	fr := &f.ctrl[fi]
	reconcileMark := f.a.Len()
	canDefer := f.callFreeLoopExit(fi)
	var saved localStateSnapshot
	if canDefer {
		saved, canDefer = f.snapshotLocalStates()
	}
	f.convergeBranchLocals(fr)
	var coldEdgeCode []byte
	if canDefer && f.a.Len() != reconcileMark {
		coldEdgeCode = append(coldEdgeCode, f.a.B[reconcileMark:]...)
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.rewindProfileEmission(reconcileMark)
		}
		f.a.B = f.a.B[:reconcileMark]
		f.restoreLocalStates(saved)
	}
	a, d := fr.branchArity(), f.depth()
	f.flush()
	testAt := f.a.Len()
	f.a.CmpImm32(creg, 0) // retained for non-empty edges, which may rewrite creg
	if cOwned {
		f.release(creg)
	}
	// Emit the value-move edge first and measure it. The edge
	// helpers emit only straight-line, position-independent LDR/STR/MOV — no
	// branches or PC-relative ops — so the bytes can be relocated freely below.
	mark := f.a.Len()
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, a)
	}
	f.restoreBranchHandlers(fi)
	if len(coldEdgeCode) != 0 {
		coldEdgeCode = append(coldEdgeCode, f.a.B[mark:]...)
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.rewindProfileEmission(mark)
		}
		f.a.B = f.a.B[:mark]
		site := f.a.Bcond(condNE)
		f.appendFrameColdEdge(fr, coldEdge{site: site, code: coldEdgeCode})
		fr.set(ctrlEndReachable, true)
		f.stats.peep("callfree-loop-exit-cold")
		return nil
	}
	if f.a.Len() == mark {
		if f.opt(optZeroBranch) {
			if profileEnabled && f.stats != nil && f.stats.RecordSources {
				f.rewindProfileEmission(testAt)
			}
			f.a.B = f.a.B[:testAt]
			if f.opt(optBranchFold) && f.zeroBranchJump(fr, creg) {
				f.stats.peep("zero-branch")
				return nil
			}
			over := f.a.Cbz32(creg)
			f.branchJump(fr)
			f.patchBranch19(over, f.a.Len())
			f.stats.peep("zero-branch")
			return nil
		}
		// Empty edge: one conditional branch straight to the target (taken when the
		// condition holds, != 0), with no skip branch and no padding NOP.
		if f.opt(optBranchFold) && f.condBranchJump(fr, condNE) {
			return nil
		}
		// Fold disabled / unsupported target / out of range: guarded form (edge empty).
		over := f.a.Bcond(condE)
		f.branchJump(fr)
		f.patchBranch19(over, f.a.Len())
		return nil
	}
	if f.branchHintUnlikely {
		// Emit the edge into a temporary assembler. It contains only the
		// position-independent local/value reconciliation bytes; its final jump
		// is emitted when the target frame closes.
		edge := append([]byte(nil), f.a.B[mark:]...)
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.rewindProfileEmission(mark)
		}
		f.a.B = f.a.B[:mark]
		site := f.a.Bcond(condNE)
		f.appendFrameColdEdge(fr, coldEdge{site: site, code: edge})
		return nil
	}
	// Non-empty edge: the edge is already emitted at [mark:]; insert the skip guard
	// before it by relocating the (position-independent) edge bytes up one word.
	f.edgeScratch = append(f.edgeScratch[:0], f.a.B[mark:]...)
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		f.rewindProfileEmission(mark)
	}
	f.a.B = f.a.B[:mark]
	over := f.a.Bcond(condE) // skip the edge when the condition is false (== 0)
	f.a.B = append(f.a.B, f.edgeScratch...)
	f.branchJump(fr)
	f.patchBranch19(over, f.a.Len()) // `over` is a B.cond (imm19)
	return nil
}

func (f *fn) opBrTable(r *wasm.Reader) error {
	if f.unreachable {
		n, err := r.U32()
		if err != nil {
			return err
		}
		for i := uint32(0); i <= n; i++ {
			if _, err := r.U32(); err != nil {
				return err
			}
		}
		return nil
	}
	f.reconcileLocals() // eager: one state (all lsStackReg) satisfies every target
	ireg := f.materialize(f.popValue())
	n, err := r.U32()
	if err != nil {
		return err
	}
	if uint64(n)+1 > uint64(r.BytesLeft()) {
		return fmt.Errorf("br_table label count %d exceeds remaining bytecode", n)
	}
	labelN := int(n)
	labels := f.tmpLabels[:0]
	if cap(labels) < labelN {
		labels = make([]uint32, 0, labelN)
	}
	labels = labels[:labelN]
	f.tmpLabels = labels
	for i := range labels {
		if labels[i], err = r.U32(); err != nil {
			return err
		}
	}
	def, err := r.U32()
	if err != nil {
		return err
	}
	d := f.depth()
	f.pinned = f.pinned.add(ireg) // survive the flush
	f.flush()
	// After the flush + reconcile, per-case edge code (converge / slot moves /
	// merge-reg load) uses only fixed scratch and pinned registers and mutates no
	// compile-time state — so case bodies can be emitted in any order and shared.
	emitCase := func(labelIdx uint32) {
		fi := len(f.ctrl) - 1 - int(labelIdx)
		fr := &f.ctrl[fi]
		f.convergeBranchLocals(fr) // post-reconcile state records/no-op converges (no code, no flags)
		if fr.has(ctrlRegMerge1) {
			f.branchEdgeToMerge1(fr, d)
		} else {
			f.moveBranchValues(fr, d, fr.branchArity())
		}
		f.restoreBranchHandlers(fi)
		f.branchJump(fr)
	}
	if f.canBranchVector(labels, def) {
		f.emitBranchVector(labels, def, ireg, emitCase)
		return nil
	}
	if brTableUseJump(labels, def, f.policy) {
		compactIDs, uniqueN, compactStubAt := f.brTableCompactPlan(labels, def)
		// Jump table (P7): bounds-check the index, then one indirect jump through
		// a table of stub offsets — O(1) dispatch instead of a cmp/jne chain.
		// The table base and target live in the backend scratch registers X16/X17
		// (IP0/IP1) — excluded from the allocatable file entirely — so no value or
		// pinned register is clobbered by the dispatch. The amd64 br_table RAX
		// hazard (materialize placing the index in the same reg used as the table
		// base, then the LEA overwriting it) therefore cannot arise here: ireg is an
		// owned value register and can never be X16/X17, so no relocation is needed.
		f.stats.peep("br-table-jump")
		if uint32(len(labels)) <= 0xFFF {
			f.a.CmpImm32(ireg, uint32(len(labels)))
		} else { // out of the 12-bit compare-immediate range: materialize + reg compare
			f.a.MovImm64(X16, uint64(uint32(len(labels)))) // X16 is reused as the table base below, after this compare
			f.a.CmpReg32(ireg, X16)
		}
		defSite := f.a.Bcond(condAE) // idx >= n → default (B.cond, imm19)
		adrSite := f.a.Adr(X16)      // X16 = &table (PC-relative ADR, patched)
		f.recordPCRelative(adrSite)
		if compactIDs {
			f.stats.peep("br-table-compact")
			f.a.LoadIdx(X17, X16, ireg, 0, 1, false, true) // X17 = target ID
			f.a.AddImm64(X16, X16, uint32(align4(len(labels))))
			f.a.AddShifted(X17, X16, X17, 2, false) // X17 = &branchVector[ID]
		} else {
			f.a.LslImm(ireg, ireg, 2, false)              // idx *= 4 (u32 entries)
			f.a.LoadIdx(X17, X16, ireg, 0, 4, true, true) // X17 = (i32)table[idx]
			f.a.Add64(X17, X16, X17)                      // target = table base + entry
		}
		f.a.Br(X17)
		tablePos := f.a.Len()
		f.a.PatchAdr(adrSite, tablePos)
		if compactIDs {
			for _, lbl := range labels {
				f.a.B = append(f.a.B, byte(compactStubAt[lbl]-1))
			}
			for len(f.a.B)&3 != 0 {
				f.a.B = append(f.a.B, 0)
			}
			vectorPos := f.a.Len()
			f.recordOpaqueData(tablePos, vectorPos)
			for range uniqueN {
				f.a.Branch()
			}
			for _, lbl := range labels {
				encodedID := compactStubAt[lbl]
				if encodedID <= 0 {
					continue
				}
				i := encodedID - 1
				p := f.a.Len()
				compactStubAt[lbl] = -p - 1
				f.patchBranch26(vectorPos+4*i, p)
				emitCase(lbl)
			}
			if encoded := compactStubAt[def]; encoded < 0 {
				f.patchBranch19(defSite, -encoded-1)
			} else {
				f.patchBranch19(defSite, f.a.Len())
				emitCase(def)
			}
			f.unreachable = true
			return nil
		}
		for range labels {
			f.a.B = append(f.a.B, 0, 0, 0, 0) // placeholder entries
		}
		f.recordJumpTableData(tablePos, f.a.Len())
		if brTableSmallLabelsUnique(labels) {
			defIdx := -1
			for i, lbl := range labels {
				if lbl == def {
					defIdx = i
					break
				}
			}
			for i, lbl := range labels {
				p := f.a.Len()
				f.a.PatchU32(tablePos+4*i, uint32(p-tablePos))
				if i == defIdx {
					f.patchBranch19(defSite, p)
				}
				emitCase(lbl)
			}
			if defIdx < 0 {
				f.patchBranch19(defSite, f.a.Len())
				emitCase(def)
			}
			f.unreachable = true
			return nil
		}
		// Branch labels are control-stack depths, so use one reusable dense scratch
		// table instead of allocating a hash map for every duplicate-heavy br_table.
		// Initialize only the labels used by this table; stale entries for other
		// depths are unreachable until a later table initializes them.
		sc := f.scratchState()
		stubAt := sc.brTableStubAt
		if cap(stubAt) < len(f.ctrl) {
			stubAt = make([]int, len(f.ctrl))
		} else {
			stubAt = stubAt[:len(f.ctrl)]
		}
		sc.brTableStubAt = stubAt
		for _, lbl := range labels {
			stubAt[lbl] = -1
		}
		stubAt[def] = -1
		stub := func(lbl uint32) int {
			if p := stubAt[lbl]; p >= 0 {
				return p
			}
			p := f.a.Len()
			stubAt[lbl] = p
			emitCase(lbl)
			return p
		}
		for i, lbl := range labels {
			f.a.PatchU32(tablePos+4*i, uint32(stub(lbl)-tablePos))
		}
		if p := stubAt[def]; p >= 0 {
			f.patchBranch19(defSite, p)
		} else {
			f.patchBranch19(defSite, f.a.Len())
			emitCase(def)
		}
		f.unreachable = true
		return nil
	}
	for i, lbl := range labels {
		f.a.CmpImm32(ireg, uint32(i)) // cmp ireg, i (i < brTableJumpMin, always fits imm12)
		skip := f.a.Bcond(condNE)
		emitCase(lbl)
		f.patchBranch19(skip, f.a.Len()) // `skip` is a B.cond (imm19)
	}
	emitCase(def)
	f.unreachable = true
	return nil
}

func (f *fn) opReturn() error {
	if f.unreachable {
		return nil
	}
	if f.inlineRetFrame > 0 {
		// Inside an inlined control-flow callee: `return` exits the callee, not the
		// enclosing function — branch to its synthetic boundary frame (its `end`
		// merge), exactly like a br to that label.
		f.branchToFrame(f.inlineRetFrame)
		f.unreachable = true
		return nil
	}
	if f.singleRegResult {
		f.materializePendingTraps()
		f.placeSingleResult() // result straight to X0/V0; epilogue does not reload
		f.appendReturnSite(f.a.Branch())
		f.unreachable = true
		return nil
	}
	fr := &f.ctrl[0]
	a, d := fr.resultN, f.depth()
	f.flush()
	f.moveBranchValues(fr, d, a)
	f.appendReturnSite(f.a.Branch())
	f.unreachable = true
	return nil
}

// skipImmediates advances over a dead-code opcode's operands without emitting.
func skipImmediates(r *wasm.Reader, op byte) error {
	switch {
	case op == 0x10: // call
		_, err := r.U32()
		return err
	case op == 0x11: // call_indirect
		if _, err := r.U32(); err != nil {
			return err
		}
		_, err := r.U32()
		return err
	case op == 0x0C || op == 0x0D: // br / br_if
		_, err := r.U32()
		return err
	case op >= 0x20 && op <= 0x26: // local.*/global.*/table.get/set
		_, err := r.U32()
		return err
	case op >= 0x28 && op <= 0x3E: // memarg
		if _, err := r.U32(); err != nil {
			return err
		}
		_, err := r.U32()
		return err
	case op == 0x3F || op == 0x40: // memory.size/grow
		_, err := r.U32()
		return err
	case op == 0x41: // i32.const
		_, err := r.I32()
		return err
	case op == 0x42: // i64.const
		_, err := r.I64()
		return err
	case op == 0x43: // f32.const
		return r.Step(4)
	case op == 0x44: // f64.const
		return r.Step(8)
	case op == 0xd0 || op == 0xd2: // ref.null / ref.func
		_, err := r.U32()
		return err
	case op == 0xfc: // misc prefix: sub-opcode + its own immediates
		sub, err := r.U32()
		if err != nil {
			return err
		}
		switch sub {
		case 8, 12: // memory.init/table.init: segment index + memory/table index
			if _, err := r.U32(); err != nil {
				return err
			}
			_, err = r.U32()
			return err
		case 9, 13: // data.drop / elem.drop: one index
			_, err := r.U32()
			return err
		case 10, 14: // memory.copy/table.copy: two indexes
			if _, err := r.U32(); err != nil {
				return err
			}
			_, err = r.U32()
			return err
		case 11, 15, 16, 17: // memory.fill/table.grow/table.size/table.fill
			_, err := r.U32()
			return err
		}
		return nil
	case op == 0xfb || op == 0xfd: // GC/SIMD prefixes have subopcode-specific immediates.
		return wasm.SkipInstructionImmediate(r, op)
	}
	return nil
}

// brTableCompactPlan returns a byte target-ID table plus a vector of unique
// direct-branch vector only when its exact bytes beat the dense i32 table. It is
// intentionally compaction-only: the compact form adds one predictable
// direct branch after the indirect dispatch.
func (f *fn) brTableCompactPlan(labels []uint32, def uint32) (bool, int, []int) {
	// The aligned target-ID table precedes the branch vector and is added with
	// an unshifted 12-bit immediate, so its offset must not exceed 4095.
	if !f.policy.CompactNative || align4(len(labels)) > 4095 {
		return false, 0, nil
	}
	var seen [4]uint64
	uniqueN := 0
	for _, lbl := range labels {
		if lbl >= 256 {
			return false, 0, nil
		}
		word, bit := lbl>>6, uint64(1)<<(lbl&63)
		if seen[word]&bit == 0 {
			seen[word] |= bit
			uniqueN++
		}
	}
	compactBytes := align4(len(labels)) + 4*uniqueN
	if compactBytes >= 4*len(labels) {
		return false, 0, nil
	}
	sc := f.scratchState()
	stubAt := sc.brTableStubAt
	if cap(stubAt) < len(f.ctrl) {
		stubAt = make([]int, len(f.ctrl))
	} else {
		stubAt = stubAt[:len(f.ctrl)]
	}
	sc.brTableStubAt = stubAt
	for _, lbl := range labels {
		stubAt[lbl] = 0
	}
	stubAt[def] = 0
	nextID := 0
	for _, lbl := range labels {
		if stubAt[lbl] != 0 {
			continue
		}
		nextID++
		stubAt[lbl] = nextID // ID + 1, reserving zero for unseen.
	}
	return true, uniqueN, stubAt
}

// brTableUseJump keeps the O(1) dispatch threshold for ordinary code, but the
// compaction path accounts for the exact fixed dispatch bytes and the
// minimum four-byte branch tail eliminated for every duplicate target. Case
// setup can only make sharing more profitable, so this bounded calculation
// never chooses a jump table that is larger than the linear form.
func brTableUseJump(labels []uint32, def uint32, policy CodegenPolicy) bool {
	if len(labels) < brTableJumpMin {
		return false
	}
	if !policy.CompactNative {
		return true
	}
	// At seven labels the fixed dispatch and table bytes break even with the
	// linear compares before any shared target tails are counted. Avoid scanning
	// the labels of larger tables: their decision is unconditional.
	if len(labels) >= 7 {
		return true
	}
	const jumpFixedBytes = 7 * 4 // cmp, b.cond, adr, lsl, ldr, add, br
	linearBytes := len(labels) * 2 * 4
	jumpBytes := jumpFixedBytes + len(labels)*4

	unique := 0
	for i, lbl := range labels {
		seen := false
		for _, prev := range labels[:i] {
			if prev == lbl {
				seen = true
				break
			}
		}
		if !seen {
			unique++
		}
	}
	defSeen := false
	for _, lbl := range labels {
		if lbl == def {
			defSeen = true
			break
		}
	}
	if !defSeen {
		unique++
	}
	duplicateTargets := len(labels) + 1 - unique
	jumpBytes -= duplicateTargets * 4 // every shared case has at least its branch tail
	return jumpBytes <= linearBytes
}

func brTableSmallLabelsUnique(labels []uint32) bool {
	// Keep the duplicate check bounded: larger tables use the map-backed path,
	// avoiding an O(n²) scan while still saving the map allocation for the small
	// unique jump tables that dominate compiler benchmarks and generated code.
	if len(labels) > 32 {
		return false
	}
	for i, lbl := range labels {
		for _, prev := range labels[:i] {
			if prev == lbl {
				return false
			}
		}
	}
	return true
}
