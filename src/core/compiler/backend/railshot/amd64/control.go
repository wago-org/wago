//go:build amd64

package amd64

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Control flow: block / loop / if / else / end / br / br_if / br_table / return /
// unreachable. Ported from WARP's control-flow lowering, but using the canonical-
// slots reconciliation model (the same one backend/railshot/amd64 uses against this
// runtime): at every control boundary the operand stack is flushed to position-
// indexed frame slots, so all edges into a join agree on where each value lives.
// This trades WARP's RegisterCopyResolver register-shuffling for a simpler,
// proven scheme; register residency of locals is layered on separately.

var errBadLabel = fmt.Errorf("amd64: br label out of range")

type ctrlKind uint8
type ctrlFlags uint16

const (
	cfFunc ctrlKind = iota
	cfBlock
	cfLoop
	cfIf
	cfTry
)

const (
	ctrlHasElse ctrlFlags = 1 << iota
	ctrlEntryUnreachable
	ctrlEndReachable
	ctrlRegMerge1
	ctrlBaseTypesSet
	ctrlColdBaseTypes
)

// ctrlFrame is one open control construct (or the implicit function frame).
type ctrlFrame struct {
	kind       ctrlKind
	res0       machineType // first result's machine type (valid when resultN >= 1)
	flags      ctrlFlags
	mergeIndex uint32 // index+1 into scratch.ctrlMerges; zero has no cold merge state

	height          int // operand depth at the frame's result base
	paramN, resultN int
	branchN         int // values transferred on a branch to this label
	loopStart       int // cfLoop: backward target byte offset
	elseSite        int // cfIf: the jz site (to else/end), -1 once patched
	baseTypeStart   uint32
	baseTypeCount   uint32
	types           []machineType // parameters followed by results; split by paramN/resultN
}

func (fr *ctrlFrame) has(flag ctrlFlags) bool { return fr.flags&flag != 0 }

func (fr *ctrlFrame) set(flag ctrlFlags, enabled bool) {
	if enabled {
		fr.flags |= flag
	} else {
		fr.flags &^= flag
	}
}

// ctrlFrameMerge contains feature-specific state needed only by frames that
// merge pinned locals, track GC roots, patch branches, or analyze loops. Keeping it in
// compact scratch removes pointer-rich fields from ordinary frames.
type ctrlFrameMerge struct {
	ends         []uint32 // overflow after the first forward-end site
	branchState  []locState
	entryState   []locState
	loopSetStart uint32 // loop: modified-local range; other frames: first packed end site
	loopSetCount uint16
	loopSetKnown bool
	eh           *ctrlFrameEH
}

// ctrlFrameRoots is allocated as a depth-parallel sidecar only when exact GC
// root tracking reaches structured control. Scalar merges and fact-only control
// do not retain its scanned slice headers.
type ctrlFrameRoots struct {
	baseGCRoots   []bool
	paramGCRoots  []bool
	resultGCRoots []bool
}

const initialCtrlMergeCapacity = 16

type ctrlFrameEH struct {
	// cfTry only: one of a bounded set of fixed six-slot native-stack records
	// plus an ordered compile-time catch dispatch table. Scalar exceptions carry
	// at most two payload words; reference catches copy those words into a fixed
	// rooted exception slot before exposing its stable frame-relative address.
	targetSite  int
	recordIndex int
	catches     []ehCatchClause
	refResults  [3]bool // branch-result positions that carry rooted exception identities
}

func (f *fn) frameEH(fr *ctrlFrame) *ctrlFrameEH {
	if cold := f.ctrlMerge(fr); cold != nil {
		return cold.eh
	}
	return nil
}

func (f *fn) ensureFrameEH(fr *ctrlFrame) *ctrlFrameEH {
	cold := f.ensureCtrlMerge(fr)
	if cold.eh == nil {
		cold.eh = new(ctrlFrameEH)
	}
	return cold.eh
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

func (f *fn) ctrlRoots(fr *ctrlFrame) *ctrlFrameRoots {
	if fr.mergeIndex == 0 {
		return nil
	}
	sc := f.scratchState()
	if int(fr.mergeIndex) <= len(sc.ctrlRoots) {
		return &sc.ctrlRoots[fr.mergeIndex-1]
	}
	return nil
}

func (f *fn) ensureCtrlRoots(fr *ctrlFrame) *ctrlFrameRoots {
	f.ensureCtrlMerge(fr)
	sc := f.scratchState()
	if sc.ctrlRoots == nil {
		sc.ctrlRoots = make([]ctrlFrameRoots, 0, initialCtrlMergeCapacity)
	}
	for len(sc.ctrlRoots) < int(fr.mergeIndex) {
		sc.ctrlRoots = append(sc.ctrlRoots, ctrlFrameRoots{})
	}
	return &sc.ctrlRoots[fr.mergeIndex-1]
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

func moveCtrlSidecarSlot[T any](sidecar *[]T, stale, fresh uint32) {
	s := *sidecar
	var zero T
	if int(fresh) <= len(s) {
		for len(s) < int(stale) {
			s = append(s, zero)
		}
		s[stale-1] = s[fresh-1]
		s[fresh-1] = zero
	} else if int(stale) <= len(s) {
		s[stale-1] = zero
	}
	*sidecar = s
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
			moveCtrlSidecarSlot(&sc.ctrlRoots, stale, fresh)
			if int(fresh) == len(sc.ctrlMerges) {
				sc.ctrlMerges = sc.ctrlMerges[:len(sc.ctrlMerges)-1]
			}
			fr.mergeIndex = stale
		}
	}
	f.ctrl = append(f.ctrl, *fr)
}

func (f *fn) frameBranchState(fr *ctrlFrame) []locState {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge.branchState
	}
	return nil
}

func (f *fn) frameEntryState(fr *ctrlFrame) []locState {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge.entryState
	}
	return nil
}

func (f *fn) setFrameBranchState(fr *ctrlFrame, state []locState) {
	if state != nil {
		f.ensureCtrlMerge(fr).branchState = state
	}
}

func (f *fn) setFrameEntryState(fr *ctrlFrame, state []locState) {
	if state != nil {
		f.ensureCtrlMerge(fr).entryState = state
	}
}

func (f *fn) frameBaseGCRoots(fr *ctrlFrame) []bool {
	if roots := f.ctrlRoots(fr); roots != nil {
		return roots.baseGCRoots
	}
	return nil
}

func (fr *ctrlFrame) appendParameterTypes(dst []machineType) []machineType {
	start := 0
	if fr.has(ctrlColdBaseTypes) {
		start = int(fr.baseTypeCount)
	}
	return append(dst, fr.types[start:start+fr.paramN]...)
}

func (fr *ctrlFrame) appendResultTypes(dst []machineType) []machineType {
	if fr.resultN == 1 && fr.types == nil && !fr.has(ctrlColdBaseTypes) {
		return append(dst, fr.res0)
	}
	start := fr.paramN
	if fr.has(ctrlColdBaseTypes) {
		start += int(fr.baseTypeCount)
	}
	return append(dst, fr.types[start:start+fr.resultN]...)
}

func (f *fn) setFrameBaseTypes(fr *ctrlFrame, types []machineType) {
	fr.set(ctrlBaseTypesSet, true)
	start := int(f.controlBaseTypeN)
	fr.baseTypeCount = uint32(len(types))
	storage := f.scratchState().functionResultTypeArena[:]
	if start+len(types) <= len(storage) {
		copy(storage[start:], types)
		fr.baseTypeStart = uint32(start)
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
		return fr.types[:fr.baseTypeCount]
	}
	start := int(fr.baseTypeStart)
	return f.scratchState().functionResultTypeArena[start : start+int(fr.baseTypeCount)]
}

func (f *fn) releaseFrameBaseTypes(fr *ctrlFrame) {
	if !fr.has(ctrlBaseTypesSet) {
		return
	}
	if fr.has(ctrlColdBaseTypes) {
		return
	}
	start := int(fr.baseTypeStart)
	end := start + int(fr.baseTypeCount)
	if end != int(f.controlBaseTypeN) {
		panic(fmt.Sprintf("amd64: control base-type arena released out of order: range [%d,%d), top %d", start, end, f.controlBaseTypeN))
	}
	f.controlBaseTypeN = uint8(start)
}

func (f *fn) frameEndSites(fr *ctrlFrame) (uint32, uint32, []uint32) {
	if fr.kind != cfLoop {
		if cold := f.ctrlMerge(fr); cold != nil {
			return cold.loopSetStart, uint32(fr.loopStart), cold.ends
		}
	}
	return 0, 0, nil
}

func (f *fn) patchFrameEndSites(first, second uint32, overflow []uint32) {
	if first != 0 {
		f.a.PatchRel32(int(first-1), f.a.Len())
	}
	if second != 0 {
		f.a.PatchRel32(int(second-1), f.a.Len())
	}
	for _, packed := range overflow {
		f.a.PatchRel32(int(packed-1), f.a.Len())
	}
}

func (f *fn) frameParamGCRoots(fr *ctrlFrame) []bool {
	if roots := f.ctrlRoots(fr); roots != nil {
		return roots.paramGCRoots
	}
	return nil
}

func (f *fn) frameResultGCRoots(fr *ctrlFrame) []bool {
	if roots := f.ctrlRoots(fr); roots != nil {
		return roots.resultGCRoots
	}
	return nil
}

func (f *fn) frameLoopSetLocals(fr *ctrlFrame) []uint16 {
	if cold := f.ctrlMerge(fr); cold != nil && cold.loopSetKnown {
		start := int(cold.loopSetStart)
		return f.loopSetLocals[start : start+int(cold.loopSetCount)]
	}
	return nil
}

func (f *fn) setFrameLoopSetLocals(fr *ctrlFrame, locals []uint16) {
	if locals == nil || uint64(len(f.loopSetLocals)) > uint64(^uint32(0)) {
		return
	}
	start := len(f.loopSetLocals)
	f.loopSetLocals = append(f.loopSetLocals, locals...)
	cold := f.ensureCtrlMerge(fr)
	cold.loopSetStart, cold.loopSetCount, cold.loopSetKnown = uint32(start), uint16(len(locals)), true
}

func loopSetsLocal(locals []uint16, index uint32) bool {
	if index > uint32(^uint16(0)) {
		return false
	}
	_, ok := slices.BinarySearch(locals, uint16(index))
	return ok
}

func (f *fn) convergeFrameBranchState(fr *ctrlFrame) {
	state := f.frameBranchState(fr)
	f.convergeEdgeTo(&state)
	f.setFrameBranchState(fr, state)
}

func (f *fn) convergeFrameEntryState(fr *ctrlFrame) {
	state := f.frameEntryState(fr)
	f.convergeEdgeTo(&state)
	f.setFrameEntryState(fr, state)
}

type ehCatchClause struct {
	kind        wasm.CatchKind
	tag         uint32
	frame       int
	scalarN     int
	payloadN    int
	payloadType [3]machineType
	rootIndex   int
	matchSite   int
}

// --- operand-stack canonicalization ---

func rootMachineType(root *elem) machineType {
	typ := root.st.typ
	if root.kind == ekDeferred && root.typ != mtNone {
		typ = root.typ
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

func typesOfVals(vals []wasm.ValType) []machineType {
	types := make([]machineType, len(vals))
	for i, val := range vals {
		types[i] = mtOf(val)
	}
	return types
}

// depth returns the number of logical operands (valent-block roots) on the stack.
func (f *fn) depth() int {
	n := 0
	for cur := f.s.head.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		n++
	}
	return n
}

// rootsBottomToTop returns the logical operands in bottom-to-top order.
// The returned scratch slice is valid only until the next helper using f.tmpRoots.
func (f *fn) rootsBottomToTop() []*elem {
	rs := f.tmpRoots[:0]
	for cur := f.s.head.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
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
		panic("amd64: logical stack index out of range")
	}
	return slotsOfTypes(types[:logical])
}

func (f *fn) currentLogicalTypes() []machineType { return f.logicalTypes(f.rootsBottomToTop()) }

func gcRootFlags(roots []*elem) []bool {
	var flags []bool
	for i, root := range roots {
		if root.kind != ekValue || !root.st.hasGCRoot() {
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
	roots := f.rootsBottomToTop()
	if fr.height < 0 || fr.height+fr.paramN > len(roots) {
		return
	}
	baseRoots := gcRootFlags(roots[:fr.height])
	paramRoots := gcRootFlags(roots[fr.height : fr.height+fr.paramN])
	if baseRoots != nil || paramRoots != nil {
		rootState := f.ensureCtrlRoots(fr)
		rootState.baseGCRoots = baseRoots
		rootState.paramGCRoots = paramRoots
	}
}

func (f *fn) recordGCBranchResults(fr *ctrlFrame, n int) {
	if n == 0 {
		return
	}
	roots := f.rootsBottomToTop()
	if n > len(roots) {
		return
	}
	resultRoots := roots[len(roots)-n:]
	for i, root := range resultRoots {
		if root.kind != ekValue || !root.st.hasGCRoot() {
			continue
		}
		rootState := f.ensureCtrlRoots(fr)
		if len(rootState.resultGCRoots) < n {
			rootState.resultGCRoots = make([]bool, n)
		}
		rootState.resultGCRoots[i] = true
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

func (f *fn) frameDepthTypes(base, suffix []machineType) []machineType {
	out := f.tmpTypes[:0]
	out = append(out, base...)
	out = append(out, suffix...)
	f.tmpTypes = out
	return out
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
	f.flushWithPressure(false)
}

// flushWrapper reserves register headroom before canonicalizing the operand
// image consumed by an ordinary wrapper-ABI call.
func (f *fn) flushWrapper() {
	f.flushWithPressure(true)
}

func (f *fn) flushWithPressure(stageRegisterPressure bool) {
	f.stats.addFlush()
	f.invalidateGlobalsCache() // the cached cell ptr must not span a call/control boundary
	f.invalidateBoundsCert()   // bounds facts are valid only within a straight-line region
	roots := f.rootsBottomToTop()
	gcRoots := f.tmpGCRoots[:0]
	for _, root := range roots {
		gcRoots = append(gcRoots, root.kind == ekValue && root.st.hasGCRoot())
	}
	f.tmpGCRoots = gcRoots
	if f.flushWideStack(roots, gcRoots, stageRegisterPressure) {
		return
	}
	types := f.tmpTypes[:0]
	slot := 0
	for _, root := range roots {
		typ := rootMachineType(root)
		if typ == mtCustom {
			panic("custom value cannot cross a control-flow or ordinary call boundary")
		}
		types = append(types, typ)
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slotIndex() == slot && root.st.typ == typ {
			slot += typ.stackSlots()
			continue // already canonical
		}
		if typ == mtV128 {
			x := f.materializeV128(root)
			f.a.VMovdquStoreDisp(RSP, f.spillOff(slot), x)
			f.releaseF(x)
			slot += 2
			continue
		}
		if root.kind == ekValue && (root.st.kind == stLocalReg || root.st.kind == stGlobReg) {
			if root.st.typ.isFloat() {
				f.a.FStoreDisp(RSP, f.spillOff(slot), root.st.reg, true)
			} else {
				f.a.Store64(RSP, f.spillOff(slot), root.st.reg) // copy pinned local/global's value; never release
			}
			slot++
			continue
		}
		if root.kind == ekValue && root.st.typ.isFloat() {
			x := f.materializeF(root)
			f.a.FStoreDisp(RSP, f.spillOff(slot), x, true) // 8B store
			f.releaseF(x)
			slot++
			continue
		}
		r := f.materialize(root)
		f.a.Store64(RSP, f.spillOff(slot), r)
		f.release(r)
		slot++
	}
	f.tmpTypes = types
	f.setDepthTypesWithGCRoots(types, gcRoots)
}

// flushWideStack stages unusually wide or overlapping operand stacks in a
// disjoint frame range before copying them to canonical slots. The normal
// one-pass flush is faster, but a value already spilled below its destination
// may be overwritten by an earlier canonical store before it is reloaded. This
// occurs below the historical 64-slot width threshold when register pressure
// spills a later wrapper argument into an earlier argument's destination.
func (f *fn) flushWideStack(roots []*elem, gcRoots []bool, stageRegisterPressure bool) bool {
	const wideFlushSlots = 64

	types := f.tmpFlushTypes[:0]
	total, gpValues, fpValues := 0, 0, 0
	needsStage := false
	for _, root := range roots {
		typ := rootMachineType(root)
		types = append(types, typ)
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slotIndex() < total {
			needsStage = true
		}
		if typ.isFloat() || typ.isV128() {
			fpValues++
		} else {
			gpValues++
		}
		total += typ.stackSlots()
	}
	if stageRegisterPressure {
		// Keep the scratch-register reserve available while materializing deferred
		// wrapper arguments. Otherwise a flush that begins with no spilled roots
		// can create a later low-slot spill, then overwrite it before that operand
		// is visited.
		gpBlocked := f.pinnedLocalMask.union(f.reserved)
		gpBudget := 0
		for _, reg := range gpAlloc {
			if !gpBlocked.has(reg) {
				gpBudget++
			}
		}
		if gpValues > max(0, gpBudget-numScratchGP) {
			needsStage = true
		}
		fpBlocked := f.fpinnedLocalMask.union(f.fconstMask()).union(f.v128ConstMask())
		fpBudget := 0
		for reg := Reg(0); reg < 16; reg++ {
			if !fpBlocked.has(reg) {
				fpBudget++
			}
		}
		if fpValues > fpBudget {
			needsStage = true
		}
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
	f.spillFloor = stageEnd // allocator spills must not overwrite staged values

	slot := stageBase
	for i, root := range roots {
		typ := types[i]
		switch {
		case typ == mtV128:
			x := f.materializeV128(root)
			f.a.VMovdquStoreDisp(RSP, f.spillOff(slot), x)
			f.releaseF(x)
		case typ.isFloat():
			x := f.materializeF(root)
			f.a.FStoreDisp(RSP, f.spillOff(slot), x, true)
			f.releaseF(x)
		default:
			r := f.materialize(root)
			f.a.Store64(RSP, f.spillOff(slot), r)
			f.release(r)
		}
		slot += typ.stackSlots()
	}
	f.spillFloor = oldFloor

	f.moveSlots(stageBase, 0, total)
	f.setDepthTypesWithGCRoots(types, gcRoots)
	return true
}

// setDepth consumes the top operands while preserving the exact machine types
// (and therefore slot widths) of the l values below them.
func (f *fn) setDepth(l int) {
	roots := f.rootsBottomToTop()
	if l < 0 || l > len(roots) {
		panic("amd64: invalid operand depth")
	}
	types := f.tmpTypes[:0]
	gcRoots := f.tmpGCRoots[:0]
	for _, root := range roots[:l] {
		types = append(types, root.st.typ)
		gcRoots = append(gcRoots, root.kind == ekValue && root.st.hasGCRoot())
	}
	f.tmpTypes = types
	f.tmpGCRoots = gcRoots
	f.setDepthTypesWithGCRoots(types, gcRoots)
}

func (f *fn) setDepthTypes(types []machineType) {
	f.setDepthTypesWithGCRoots(types, nil)
}

func (f *fn) setDepthTypesWithGCRoots(types []machineType, gcRoots []bool) {
	f.s.head.prev, f.s.head.next = f.s.head, f.s.head
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
// [toBase, toBase+n). Runs only right after flush, so RAX is free as scratch.
func (f *fn) moveSlots(fromBase, toBase, n int) {
	if fromBase == toBase {
		return
	}
	for i := 0; i < n; i++ {
		f.a.Load64(RAX, RSP, f.spillOff(fromBase+i))
		f.a.Store64(RSP, f.spillOff(toBase+i), RAX)
	}
}

// --- block types ---

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

// blockType decodes a block's parameter and result types and the first result's
// machine type.
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
	if x < 0 {
		return nil, nil, nil, mtNone, fmt.Errorf("bad blocktype index %d", x)
	}
	ft, ok := f.m.TypeFunc(uint32(x))
	if !ok {
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
// directly in the return register — RAX (int) or XMM0 (float) — the WARP target
// hint for returns, skipping the flush-to-slot + epilogue-reload round trip. Only
// used when f.singleRegResult holds.
func (f *fn) placeSingleResult() {
	e := f.s.back()
	if f.resultFloat {
		x := f.materializeF(e)
		if x != 0 {
			f.a.FMov(0, x, f.resultF64) // -> XMM0
		}
		f.releaseF(x)
	} else {
		f.condenseInto(e, RAX)
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
			f.a.FMov(mergeFReg, x, fr.res0 == mtF64)
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
		f.a.FLoadDisp(mergeFReg, RSP, f.spillOff(slot), fr.res0 == mtF64)
	} else {
		f.a.Load64(mergeReg, RSP, f.spillOff(slot))
	}
}

// convergeBranchLocals converges pinned-local state for a br/br_if/br_table
// edge into fr's branch target. Function-frame targets (returns) need nothing —
// the locals die — so nothing is emitted, keeping conditional returns free.
func (f *fn) convergeBranchLocals(fr *ctrlFrame) {
	if fr.kind == cfFunc {
		return
	}
	f.convergeFrameBranchState(fr)
}

// branchJump emits the jump for a branch that targets frame fr.
func (f *fn) branchJump(fr *ctrlFrame) {
	switch fr.kind {
	case cfLoop:
		f.a.JmpBack(fr.loopStart)
	case cfFunc:
		// The caller already converged the result to slot 0 (fr.height == 0); with
		// the register-return hint the epilogue no longer reloads it, so load it
		// into the return register here so every exit agrees on RAX/XMM0 = result.
		if f.singleRegResult {
			if f.resultFloat {
				f.a.FLoadDisp(0, RSP, f.spillOff(0), f.resultF64)
			} else {
				f.a.Load64(RAX, RSP, f.spillOff(0))
			}
		}
		f.appendReturnSite(f.a.JmpPlaceholder())
	default:
		f.frameAddEnd(fr, f.a.JmpPlaceholder())
		fr.set(ctrlEndReachable, true)
	}
}

// --- control opcodes ---

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
	fr := ctrlFrame{kind: kind, paramN: pN, resultN: rN, elseSite: -1, res0: res0, types: frameTypes}
	fr.set(ctrlEntryUnreachable, f.unreachable)
	if kind == cfLoop {
		fr.branchN = pN
	} else {
		fr.branchN = rN
	}
	// Phase 2/3: a block or if producing exactly one result (int → mergeReg, float
	// → mergeFReg) carries that value in a register across all its edges (fall-
	// through, else, br/br_if/br_table, and an if's cond-false passthrough) instead
	// of a frame slot. Excludes loops (params, back-edge) and multi-value.
	fr.set(ctrlRegMerge1, f.regMerge && (kind == cfBlock || kind == cfIf) && rN == 1 && res0 != mtNone && res0 != mtV128)
	if kind == cfLoop && !f.unreachable {
		setLocals, _, valid := scanLoopBodyWithClassifier(r, f.m, f.classifier, f.loopScanLocals) // reader restored
		f.loopScanLocals = setLocals
		if setLocals != nil {
			f.setFrameLoopSetLocals(&fr, setLocals)
		}
		_ = valid // A failed optional prewalk simply supplies no reusable local set.
	}
	if f.unreachable {
		f.pushCtrl(&fr)
		f.releaseCtrlMerge(&fr)
		return nil
	}
	if kind == cfIf {
		f.convergeFrameEntryState(&fr) // header snapshot: else entry / cond-false edge state
		if isFusableCompare(f.s.back()) {
			cond := f.s.back()
			f.flushBelow(cond)
			cc := f.condenseToFlags(cond)
			fr.height = f.depth() - pN
			f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
			f.captureGCFrameShape(&fr)
			fr.elseSite = f.a.JccPlaceholder(invertCond(cc)) // to else/end when false
			f.pushCtrl(&fr)
			return nil
		}
		creg, cOwned := f.materializeRead(f.popValue()) // TEST only reads: a pinned local needs no copy
		fr.height = f.depth() - pN
		f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
		f.captureGCFrameShape(&fr)
		f.flush()
		f.a.TestSelf(creg, false)
		if cOwned {
			f.release(creg)
		}
		fr.elseSite = f.a.JccPlaceholder(condE) // jz else/end
	} else {
		fr.height = f.depth() - pN
		f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
		f.captureGCFrameShape(&fr)
		if kind == cfLoop {
			// Loop tops converge eagerly (all lsStackReg): hoists any post-call
			// reload OUT of the body — a lazy (lsMem) loop target would push the
			// reload into every iteration instead.
			f.reconcileLocals()
			f.convergeFrameBranchState(&fr) // records the all-lsStackReg target
			f.flush()
		} else {
			f.flush()
		}
		if kind == cfLoop {
			f.a.AlignLoop() // padding runs on entry, not per iteration
			fr.loopStart = f.a.Len()
			f.emitInterruptCheck(RSI) // RSI freed by the flush() above; poll once per iteration
		}
	}
	f.pushCtrl(&fr)
	return nil
}

const (
	ehRecordSlots    = 7
	ehRootSlots      = 3
	maxEHTryRecords  = 4
	maxEHRootRecords = 4
	maxEHCatches     = 8
	ehPrevOff        = 0
	ehSavedRSPOff    = 8
	ehTagOff         = 16
	ehPayload0Off    = 24
	ehPayload1Off    = 32
	ehTargetOff      = 40
	ehSavedRBXOff    = 48
	offEHTagDirPtr   = abi.EHTagDirPtrOffset
)

func exceptionPayloadMachineType(m *wasm.Module, typ wasm.ValType) (machineType, bool) {
	if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) || wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
		return mtOf(typ), true
	}
	if typ.Kind() != wasm.ValRef || typ.Ref().Nullable() || typ.Ref().Exact() || typ.Ref().Heap().Kind() != wasm.HeapTypeIndex {
		return mtNone, false
	}
	var ft wasm.CompType
	if !m.ResolveTypeFunc(typ.Ref().Heap().Type().Index, &ft) {
		return mtNone, false
	}
	return mtI64, true
}

func moduleTagType(m *wasm.Module, index uint32) (wasm.TagType, bool) {
	for i := range m.Imports {
		im := &m.Imports[i]
		if im.Type.Kind != wasm.ExternTag {
			continue
		}
		if index == 0 {
			return im.Type.TagType(), true
		}
		index--
	}
	if int(index) >= len(m.Tags) {
		return wasm.TagType{}, false
	}
	return m.Tags[index], true
}

func (f *fn) opTryTable(r *wasm.Reader) error {
	paramTypes, resultTypes, frameTypes, res0, err := f.blockType(r)
	if err != nil {
		return err
	}
	n, err := r.U32()
	if err != nil {
		return err
	}
	if n > maxEHCatches {
		return fmt.Errorf("bounded exception handling supports at most %d catches per try_table", maxEHCatches)
	}
	pN, rN := len(paramTypes), len(resultTypes)
	if frameTypes == nil && res0 != mtNone {
		rN = 1
	}
	fr := ctrlFrame{kind: cfTry, paramN: pN, resultN: rN, branchN: rN, elseSite: -1, res0: res0, types: frameTypes}
	fr.set(ctrlEntryUnreachable, f.unreachable)
	eh := f.ensureFrameEH(&fr)
	for i := uint32(0); i < n; i++ {
		kindByte, err := r.Byte()
		if err != nil {
			return err
		}
		kind := wasm.CatchKind(kindByte)
		clause := ehCatchClause{kind: kind}
		switch kind {
		case wasm.CatchTag, wasm.CatchRef:
			clause.tag, err = r.U32()
			if err != nil {
				return err
			}
			tagType, ok := moduleTagType(f.m, clause.tag)
			if !ok {
				return fmt.Errorf("bounded exception handling catch tag %d is unavailable", clause.tag)
			}
			var ft wasm.CompType
			if !f.m.ResolveTypeFunc(tagType.Type.Index, &ft) || len(ft.Params) > 2 {
				return fmt.Errorf("bounded exception handling catch tag %d signature unavailable", clause.tag)
			}
			clause.scalarN = len(ft.Params)
			clause.payloadN = clause.scalarN
			for j, typ := range ft.Params {
				mt, ok := exceptionPayloadMachineType(f.m, typ)
				if !ok {
					return fmt.Errorf("bounded exception handling requires scalar or non-null indexed-function tag payloads")
				}
				clause.payloadType[j] = mt
			}
			if kind == wasm.CatchRef {
				if f.ehRootCount >= maxEHRootRecords {
					return fmt.Errorf("bounded exception handling supports at most %d rooted exception values per function", maxEHRootRecords)
				}
				clause.rootIndex = f.ehRootCount
				f.ehRootCount++
				clause.payloadType[clause.payloadN] = mtI64
				clause.payloadN++
			}
		case wasm.CatchAll:
		case wasm.CatchAllRef:
			if f.ehRootCount >= maxEHRootRecords {
				return fmt.Errorf("bounded exception handling supports at most %d rooted exception values per function", maxEHRootRecords)
			}
			clause.rootIndex = f.ehRootCount
			f.ehRootCount++
			clause.payloadType[0] = mtI64
			clause.payloadN = 1
		default:
			return fmt.Errorf("bounded exception handling rejects unknown catch kind %d", kind)
		}
		label, err := r.U32()
		if err != nil {
			return err
		}
		clause.frame = len(f.ctrl) - 1 - int(label)
		if clause.frame < 0 {
			return errBadLabel
		}
		if f.ctrl[clause.frame].branchN != clause.payloadN {
			return fmt.Errorf("bounded exception handler payload arity mismatch")
		}
		if kind == wasm.CatchRef || kind == wasm.CatchAllRef {
			f.ensureFrameEH(&f.ctrl[clause.frame]).refResults[clause.payloadN-1] = true
		}
		// The exception edge can arrive with only the conservative local-fact state
		// established before try_table. Intersect it at registration time just like
		// an ordinary branch; the out-of-line route restores physical locals only.
		// Catch dispatch writes canonical slots before jumping. Keep the target on
		// that representation instead of the ordinary single-result register merge.
		f.ctrl[clause.frame].set(ctrlRegMerge1, false)
		eh.catches = append(eh.catches, clause)
	}
	fr.height = f.depth() - fr.paramN
	f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
	f.captureGCFrameShape(&fr)
	if f.unreachable {
		f.pushCtrl(&fr)
		return nil
	}
	if f.ehTryDepth >= maxEHTryRecords {
		return fmt.Errorf("bounded exception handling supports at most %d nested try_table records", maxEHTryRecords)
	}
	eh.recordIndex = f.ehTryDepth
	f.ehTryDepth++
	f.reconcileLocals()
	f.flush()
	for i := range eh.catches {
		clause := &eh.catches[i]
		if clause.kind != wasm.CatchRef && clause.kind != wasm.CatchAllRef {
			continue
		}
		f.a.XorSelf32(RAX)
		rootOff := f.ehRootOff(clause.rootIndex)
		for word := int32(0); word < ehRootSlots*8; word += 8 {
			f.a.Store64(RSP, rootOff+word, RAX)
		}
		f.stats.peep("eh-root-init")
	}
	recordOff := f.ehRecordOff(eh.recordIndex)
	f.a.LeaRsp(R11, recordOff)
	f.a.Store64(R11, ehPrevOff, RBP)
	f.a.Store64(R11, ehSavedRSPOff, RSP)
	eh.targetSite = f.a.LeaRipPlaceholder(RAX)
	f.a.Store64(R11, ehTargetOff, RAX)
	f.a.Store64(R11, ehSavedRBXOff, RBX)
	f.a.MovReg64(RBP, R11)
	f.pushCtrl(&fr)
	return nil
}

func (f *fn) opThrow(r *wasm.Reader) error {
	tag, err := r.U32()
	if err != nil {
		return err
	}
	tagType, ok := moduleTagType(f.m, tag)
	if !ok {
		return fmt.Errorf("bounded exception handling throw tag %d is unavailable", tag)
	}
	var ft wasm.CompType
	if !f.m.ResolveTypeFunc(tagType.Type.Index, &ft) || len(ft.Params) > 2 {
		return fmt.Errorf("bounded exception handling tag signature unavailable")
	}
	types := f.currentLogicalTypes()
	if len(types) < len(ft.Params) {
		return fmt.Errorf("bounded exception handling payload stack underflow")
	}
	f.reconcileLocals()
	f.flush()
	f.a.MovReg64(R11, RBP)
	f.a.TestSelf(R11, true)
	noHandler := f.a.JccPlaceholder(condE)
	f.a.Load64(RAX, RBX, -int32(offEHTagDirPtr))
	f.a.Load64(RAX, RAX, int32(tag*8))
	f.a.Store64(R11, ehTagOff, RAX)
	base := len(types) - len(ft.Params)
	for i := range ft.Params {
		slot := slotOfLogicalTypes(types, base+i)
		f.a.Load64(RAX, RSP, f.spillOff(slot))
		off := int32(ehPayload0Off)
		if i == 1 {
			off = ehPayload1Off
		}
		f.a.Store64(R11, off, RAX)
	}
	f.a.Load64(RSP, R11, ehSavedRSPOff)
	f.a.Load64(RAX, R11, ehTargetOff)
	f.a.JmpReg(RAX)
	trapPos := f.a.Len()
	f.a.PatchRel32(noHandler, trapPos)
	f.trapAlways(trapUnhandledException)
	f.unreachable = true
	return nil
}

func (f *fn) opThrowRef() error {
	types := f.currentLogicalTypes()
	if len(types) == 0 || types[len(types)-1] != mtI64 {
		return fmt.Errorf("bounded exception handling throw_ref requires an exception reference")
	}
	refSlot := slotOfLogicalTypes(types, len(types)-1)
	f.reconcileLocals()
	f.flush()
	f.a.Load64(R10, RSP, f.spillOff(refSlot))
	f.a.TestSelf(R10, true)
	f.trapIf(condE, trapNullReference)
	f.a.MovReg64(R11, RBP)
	f.a.TestSelf(R11, true)
	noHandler := f.a.JccPlaceholder(condE)
	for _, off := range [...]int32{0, 8, 16} {
		f.a.Load64(RAX, R10, off)
		f.a.Store64(R11, ehTagOff+off, RAX)
	}
	f.a.Load64(RSP, R11, ehSavedRSPOff)
	f.a.Load64(RAX, R11, ehTargetOff)
	f.a.JmpReg(RAX)
	f.a.PatchRel32(noHandler, f.a.Len())
	f.trapAlways(trapUnhandledException)
	f.unreachable = true
	return nil
}

func (f *fn) emitEHCatchRoute(fr *ctrlFrame, clause *ehCatchClause, recordOff int32) {
	target := &f.ctrl[clause.frame]
	f.a.Load64(RBP, RSP, recordOff+ehPrevOff)

	rootOff := int32(0)
	if clause.kind == wasm.CatchRef || clause.kind == wasm.CatchAllRef {
		rootOff = f.ehRootOff(clause.rootIndex)
		for _, off := range [...]int32{ehTagOff, ehPayload0Off, ehPayload1Off} {
			f.a.Load64(RAX, RSP, recordOff+off)
			f.a.Store64(RSP, rootOff+off-ehTagOff, RAX)
		}
	}

	loadPayload := func(reg Reg, i int) {
		if i == clause.scalarN {
			f.a.LeaRsp(reg, rootOff)
			return
		}
		off := recordOff + ehPayload0Off
		if i == 1 {
			off = recordOff + ehPayload1Off
		}
		f.a.Load64(reg, RSP, off)
	}
	if target.has(ctrlRegMerge1) && clause.payloadN == 1 {
		if clause.scalarN == 0 {
			f.a.LeaRsp(mergeReg, rootOff)
		} else if clause.payloadType[0].isFloat() {
			off := recordOff + ehPayload0Off
			f.a.FLoadDisp(mergeFReg, RSP, off, clause.payloadType[0] == mtF64)
		} else {
			loadPayload(mergeReg, 0)
		}
	} else {
		toSlot := slotsOfTypes(f.frameBaseTypes(target))
		for i := 0; i < clause.payloadN; i++ {
			loadPayload(RAX, i)
			f.a.Store64(RSP, f.spillOff(toSlot+i), RAX)
		}
	}

	// A throw may arrive from a nested callee after call-clobbered pinned locals
	// were left memory-only. Emit whatever reloads the target's previously fixed
	// merge state requires, then restore the normal codegen state for the code
	// emitted after this out-of-line handler.
	var saved [8]locState
	if f.usesCalls {
		for i, x := range f.pinnedLocals {
			saved[i] = f.locals[x].state
			f.locals[x].state = lsMem
		}
	}
	f.convergeBranchLocals(target)
	f.branchJump(target)
	if f.usesCalls {
		for i, x := range f.pinnedLocals {
			f.locals[x].state = saved[i]
		}
	}
}

func (f *fn) emitEHHandler(fr *ctrlFrame) {
	eh := f.ensureFrameEH(fr)
	recordOff := f.ehRecordOff(eh.recordIndex)
	handlerPos := f.a.Len()
	f.a.PatchRel32(eh.targetSite, handlerPos)
	// A throw may arrive from a foreign instance with its RBX installed. The
	// record owns the target handler and restores its basedata before dispatch.
	f.a.Load64(RBX, RSP, recordOff+ehSavedRBXOff)

	dispatchN := len(eh.catches)
	for i := range eh.catches {
		clause := &eh.catches[i]
		if clause.kind == wasm.CatchAll {
			clause.matchSite = f.a.JmpPlaceholder()
			dispatchN = i + 1
			break
		}
		f.a.Load64(RAX, RSP, recordOff+ehTagOff)
		f.a.Load64(RDX, RBX, -int32(offEHTagDirPtr))
		f.a.Load64(RDX, RDX, int32(clause.tag*8))
		f.a.Cmp64(RAX, RDX)
		clause.matchSite = f.a.JccPlaceholder(condE)
	}

	// No clause matched: transfer the exception words into the previous fixed
	// record and continue unwinding. The previous record carries its own RBX, so
	// this path composes local nesting with one exact foreign-instance transfer.
	f.a.LeaRsp(R10, recordOff)
	f.a.Load64(R11, R10, ehPrevOff)
	f.a.TestSelf(R11, true)
	noPrevious := f.a.JccPlaceholder(condE)
	for _, off := range [...]int32{ehTagOff, ehPayload0Off, ehPayload1Off} {
		f.a.Load64(RAX, R10, off)
		f.a.Store64(R11, off, RAX)
	}
	f.a.MovReg64(RBP, R11)
	f.a.Load64(RBX, R11, ehSavedRBXOff)
	f.a.Load64(RAX, R11, ehTargetOff)
	f.a.Load64(RSP, R11, ehSavedRSPOff)
	f.a.JmpReg(RAX)
	f.a.PatchRel32(noPrevious, f.a.Len())
	f.trapAlways(trapUnhandledException)

	for i := 0; i < dispatchN; i++ {
		clause := &eh.catches[i]
		f.a.PatchRel32(clause.matchSite, f.a.Len())
		f.emitEHCatchRoute(fr, clause, recordOff)
	}
}

func (f *fn) markEHReferenceResults(fr *ctrlFrame) {
	eh := f.frameEH(fr)
	if eh == nil {
		return
	}
	e := f.s.back()
	for i := fr.resultN - 1; i >= 0; i-- {
		if i < len(eh.refResults) && eh.refResults[i] {
			e.st.setEHRoot(true)
		}
		e = e.prev
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
		f.convergeFrameBranchState(fr)
		if fr.has(ctrlRegMerge1) {
			f.reconcileMerge1(fr) // then-branch result → mergeReg
		} else {
			f.flush()
		}
		f.frameAddEnd(fr, f.a.JmpPlaceholder())
		fr.set(ctrlEndReachable, true)
	}
	f.a.PatchRel32(fr.elseSite, f.a.Len())
	fr.elseSite = -1
	fr.set(ctrlHasElse, true)
	f.setDepthTypesWithGCRoots(
		f.frameDepthTypesForFrame(fr, true),
		frameGCRootFlags(f.frameBaseGCRoots(fr), f.frameParamGCRoots(fr)),
	)
	// The else body is entered via the if's false edge: locals are exactly in the
	// header-snapshot state (no code).
	f.setLocalsState(f.frameEntryState(fr))
	return nil
}

func (f *fn) opEnd() error {
	last := len(f.ctrl) - 1
	fr := f.ctrl[last]
	branchState := f.frameBranchState(&fr)
	entryState := f.frameEntryState(&fr)
	baseGCRoots := f.frameBaseGCRoots(&fr)
	paramGCRoots := f.frameParamGCRoots(&fr)
	resultGCRoots := f.frameResultGCRoots(&fr)
	firstEnd, secondEnd, ends := f.frameEndSites(&fr)
	f.ctrl[last] = ctrlFrame{mergeIndex: fr.mergeIndex}
	f.ctrl = f.ctrl[:len(f.ctrl)-1]

	if fr.kind == cfFunc {
		if !f.unreachable {
			if f.singleRegResult {
				f.placeSingleResult() // fall-through return: result straight to RAX/XMM0
			} else {
				f.flush() // results land in slots [0, resultN)
			}
		}
		return nil
	}

	fallthroughReachable := !f.unreachable
	if fallthroughReachable {
		f.recordGCBranchResults(&fr, fr.resultN)
		resultGCRoots = f.frameResultGCRoots(&fr)
		if fr.kind != cfLoop {
			// Merge edge: converge to the end's recorded state (or fix it).
			// A loop end is NOT a merge — br edges target the loop TOP — so the
			// fall-through's state simply flows out.
			f.convergeFrameBranchState(&fr)
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
			if len(resultGCRoots) < fr.resultN {
				resultGCRoots = append(resultGCRoots, make([]bool, fr.resultN-len(resultGCRoots))...)
				f.ensureCtrlRoots(&fr).resultGCRoots = resultGCRoots
			}
			resultGCRoots[i] = resultGCRoots[i] || paramGCRoots[i]
		}
		// The cond-false edge arrives in the header-snapshot state; if then-side
		// edges fixed a stronger end state (or a regMerge1 passthrough needs its
		// value in mergeReg), a stub on this edge converges it. The then
		// fall-through jumps over the stub.
		needLoads := false
		if f.usesCalls && branchState != nil && entryState != nil {
			for i := range f.pinnedLocals {
				if branchState[i] == lsStackReg && entryState[i] == lsMem {
					needLoads = true
					break
				}
			}
		}
		skip := -1
		if (fr.has(ctrlRegMerge1) || needLoads) && fallthroughReachable {
			skip = f.a.JmpPlaceholder()
		}
		f.a.PatchRel32(fr.elseSite, f.a.Len())
		if fr.has(ctrlRegMerge1) {
			slot := slotsOfTypes(f.frameBaseTypes(&fr))
			if fr.res0.isFloat() {
				f.a.FLoadDisp(mergeFReg, RSP, f.spillOff(slot), fr.res0 == mtF64) // passthrough → mergeFReg
			} else {
				f.a.Load64(mergeReg, RSP, f.spillOff(slot)) // passthrough value → mergeReg
			}
		}
		// Converge the cond-false edge from the header snapshot into the end state
		// (records it when this is the only end edge).
		f.setLocalsState(entryState)
		f.convergeFrameBranchState(&fr)
		branchState = f.frameBranchState(&fr)
		if skip != -1 {
			f.a.PatchRel32(skip, f.a.Len())
		}
		fr.set(ctrlEndReachable, true)
	}
	f.patchFrameEndSites(firstEnd, secondEnd, ends)
	endReachable := fallthroughReachable || fr.has(ctrlEndReachable)
	f.unreachable = !endReachable
	if endReachable {
		if fr.kind != cfLoop {
			f.setLocalsState(branchState) // merge: what every edge guaranteed
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
		recordOff := f.ehRecordOff(f.ensureFrameEH(&fr).recordIndex)
		if fallthroughReachable {
			f.a.Load64(RBP, RSP, recordOff+ehPrevOff)
		}
		skip := -1
		if fallthroughReachable {
			skip = f.a.JmpPlaceholder()
		}
		f.emitEHHandler(&fr)
		if skip != -1 {
			f.a.PatchRel32(skip, f.a.Len())
		}
		f.ehTryDepth--
	}
	// The frame is popped and its buffers are dead — recycle them for the next
	// frame pushed at this or a shallower depth.
	f.freeLocStateBuf(branchState)
	f.freeLocStateBuf(entryState)
	f.releaseCtrlMerge(&fr)
	f.freeEndsBuf(ends)
	f.releaseFrameBaseTypes(&fr)
	return nil
}

// branchToFrame emits an unconditional branch edge to control frame fi: converge
// pinned locals, flush operands, move the branched values into the frame's
// canonical slots (or merge register), and jump. Shared by opBr's unconditional
// path and opReturn's inlined-callee routing. The caller sets f.unreachable.
func (f *fn) branchToFrame(fi int) {
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	a, d := fr.branchN, f.depth()
	f.flush()
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, a)
	}
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
	if conditional && isFusableCompare(f.s.back()) {
		top := f.s.back()
		idx, err := r.U32()
		if err != nil {
			return err
		}
		return f.brIfFused(top, idx)
	}
	var creg Reg
	cOwned := false
	if conditional {
		creg, cOwned = f.materializeRead(f.popValue()) // TEST only reads
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
	f.convergeBranchLocals(fr)
	a, d := fr.branchN, f.depth()
	f.flush()
	f.a.TestSelf(creg, false)
	if cOwned {
		f.release(creg)
	}
	over := f.a.JccPlaceholder(condE)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, a)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	f.recordBrFold(over)
	return nil
}

func (f *fn) brOnNull(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	ref := f.materialize(f.popValue())
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	d := f.depth()
	f.flush()
	refSlot := f.allocSpillSlot()
	f.a.Store64(RSP, f.spillOff(refSlot), ref)
	f.a.TestSelf(ref, true)
	f.release(ref)
	over := f.a.JccPlaceholder(condNE)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchN)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	fallthroughRef := f.allocReg(0)
	f.a.Load64(fallthroughRef, RSP, f.spillOff(refSlot))
	result := f.pushReg(fallthroughRef, mtI64)
	markGCReference(result)
	return nil
}

func (f *fn) brOnNonNull(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	ref := f.materialize(f.popValue())
	result := f.pushReg(ref, mtI64)
	markGCReference(result)
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	allTypes := append([]machineType(nil), f.currentLogicalTypes()...)
	d := len(allTypes)
	refSlot := slotsOfTypes(allTypes) - 1
	f.flush()
	condition := f.allocReg(0)
	f.a.Load64(condition, RSP, f.spillOff(refSlot))
	f.a.TestSelf(condition, true)
	f.release(condition)
	over := f.a.JccPlaceholder(condE)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchN)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	// The reference is appended only to the taken branch payload. A null
	// fallthrough consumes it and retains any preceding label arguments.
	f.dropValue()
	return nil
}

// brOnCastResult consumes the helper's i32 match result while leaving the
// original reference at the top of the logical stack. Both the selected branch
// edge and fallthrough therefore retain the exact same 64-bit identity; only the
// validator-visible refinement differs.
func (f *fn) brOnCastResult(idx uint32, branchOnMatch bool) error {
	matched, owned := f.materializeRead(f.popValue())
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		if owned {
			f.release(matched)
		}
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	d := f.depth()
	f.flush()
	f.a.TestSelf(matched, false)
	if owned {
		f.release(matched)
	}
	skipCond := condE
	if !branchOnMatch {
		skipCond = condNE
	}
	over := f.a.JccPlaceholder(skipCond)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchN)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	f.recordBrFold(over)
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
		fr := &f.ctrl[len(f.ctrl)-1-int(labelIdx)]
		f.convergeBranchLocals(fr) // post-reconcile state records/no-op converges (no code, no flags)
		if fr.has(ctrlRegMerge1) {
			f.branchEdgeToMerge1(fr, d)
		} else {
			f.moveBranchValues(fr, d, fr.branchN)
		}
		f.branchJump(fr)
	}
	if len(labels) >= brTableJumpMin {
		f.hasJumpTableData = true
		compactIDs, uniqueN, compactStubAt := f.brTableCompactPlan(labels, def)
		// Jump table (P7): bounds-check the index, then one indirect jump through
		// a table of stub offsets — O(1) dispatch instead of a cmp/jne chain.
		// RAX/RDX are free after the flush (pinned locals never occupy scratch); the
		// dispatch below uses RAX as the table base and RDX as the target. The index
		// must therefore not live in RAX: LeaRipPlaceholder(RAX) would overwrite it
		// before the table load reads it, dispatching through a garbage address.
		// materialize() can place the index in RAX under high register pressure (few
		// neutral registers left), so relocate it off RAX here. (RDX is safe: the
		// table load consumes the index in the same instruction that overwrites it.)
		if ireg == RAX {
			safe := f.allocReg(maskOf(RAX))
			f.a.MovReg64(safe, ireg)
			f.pinned = f.pinned.remove(ireg)
			ireg = safe
			f.pinned = f.pinned.add(ireg)
		}
		f.stats.peep("br-table-jump")
		f.a.AluRI(cmpDigit, ireg, int32(len(labels)), false)
		defSite := f.a.JccPlaceholder(condAE) // idx >= n → default
		leaSite := f.a.LeaRipPlaceholder(RAX) // RAX = &table
		if compactIDs {
			f.stats.peep("br-table-compact")
			f.a.LoadIdx(RDX, RAX, ireg, 0, 1, false, true) // RDX = target ID
			f.a.LeaScaled(RDX, RDX, RDX, 2, 0)             // RDX = ID * 5
			f.a.LeaScaled(RDX, RAX, RDX, 0, int32(len(labels)))
		} else {
			f.a.ShiftImm(4, ireg, 2, false) // idx *= 4 (u32 entries)
			f.a.LoadIdx(RDX, RAX, ireg, 0, 4, true, true)
			f.a.AluRR(0x01, RDX, RAX, true) // target = table base + entry
		}
		f.a.JmpReg(RDX)
		tablePos := f.a.Len()
		f.a.PatchRel32(leaSite, tablePos)
		if compactIDs {
			for _, lbl := range labels {
				f.a.B = append(f.a.B, byte(compactStubAt[lbl]-1))
			}
			vectorPos := f.a.Len()
			f.sc.jumpTableFragments = append(f.sc.jumpTableFragments, jumpTableFragment{
				start: tablePos, end: vectorPos, kind: jumpTableFragmentIDs,
			})
			for range uniqueN {
				f.a.JmpPlaceholder()
			}
			for _, lbl := range labels {
				encodedID := compactStubAt[lbl]
				if encodedID <= 0 {
					continue
				}
				i := encodedID - 1
				p := f.a.Len()
				compactStubAt[lbl] = -p - 1
				vectorSite := vectorPos + 5*i + 1
				f.a.PatchRel32(vectorSite, p)
				f.a.KeepRel32Long(vectorSite)
				emitCase(lbl)
			}
			if encoded := compactStubAt[def]; encoded < 0 {
				f.a.PatchRel32(defSite, -encoded-1)
			} else {
				f.a.PatchRel32(defSite, f.a.Len())
				emitCase(def)
			}
			f.unreachable = true
			return nil
		}
		for range labels {
			f.a.B = append(f.a.B, 0, 0, 0, 0) // placeholder entries
		}
		f.sc.jumpTableFragments = append(f.sc.jumpTableFragments, jumpTableFragment{
			start: tablePos, end: f.a.Len(), kind: jumpTableFragmentDeltas,
		})
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
					f.a.PatchRel32(defSite, p)
				}
				emitCase(lbl)
			}
			if defIdx < 0 {
				f.a.PatchRel32(defSite, f.a.Len())
				emitCase(def)
			}
			f.unreachable = true
			return nil
		}
		// Branch labels are control-stack depths, so use one reusable dense scratch
		// table instead of allocating a hash map for every duplicate-heavy br_table.
		// Initialize only the labels used by this table; stale entries for other
		// depths are unreachable until a later table initializes them.
		stubAt := f.sc.brTableStubAt
		if cap(stubAt) < len(f.ctrl) {
			stubAt = make([]int, len(f.ctrl))
		} else {
			stubAt = stubAt[:len(f.ctrl)]
		}
		f.sc.brTableStubAt = stubAt
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
			f.a.PatchRel32(defSite, p)
		} else {
			f.a.PatchRel32(defSite, f.a.Len())
			emitCase(def)
		}
		f.unreachable = true
		return nil
	}
	for i, lbl := range labels {
		f.a.AluRI(cmpDigit, ireg, int32(i), false) // cmp ireg, i
		skip := f.a.JccPlaceholder(condNE)
		emitCase(lbl)
		f.a.PatchRel32(skip, f.a.Len())
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
		f.placeSingleResult() // result straight to RAX/XMM0; epilogue does not reload
		f.appendReturnSite(f.a.JmpPlaceholder())
		f.unreachable = true
		return nil
	}
	fr := &f.ctrl[0]
	a, d := fr.resultN, f.depth()
	f.flush()
	f.moveBranchValues(fr, d, a)
	f.appendReturnSite(f.a.JmpPlaceholder())
	f.unreachable = true
	return nil
}

// skipImmediates advances over a dead-code opcode's operands without emitting.
func skipImmediates(r *wasm.Reader, op byte) error {
	switch {
	case op == 0x08: // throw
		_, err := r.U32()
		return err
	case op == 0x0a: // throw_ref
		return nil
	case op == 0x10 || op == 0x12: // call / return_call
		_, err := r.U32()
		return err
	case op == 0x11 || op == 0x13: // call_indirect / return_call_indirect
		if _, err := r.U32(); err != nil {
			return err
		}
		_, err := r.U32()
		return err
	case op == 0x14 || op == 0x15: // call_ref / return_call_ref
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

// brTableJumpMin is the label count at which br_table switches from a linear
// cmp/jne chain to an indirect jump table.
const brTableJumpMin = 5

// brTableCompactPlan chooses byte target IDs plus five-byte direct-jump vector
// entries only when their exact data bytes beat the dense i32-offset table.
// The compact dispatch retains the same instruction count as the
// dense form but adds one predictable direct branch after the indirect jump.
func (f *fn) brTableCompactPlan(labels []uint32, def uint32) (bool, int, []int) {
	if !f.policy.CompactNative {
		return false, 0, nil
	}
	sc := f.sc
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
	uniqueN := 0
	for _, lbl := range labels {
		if stubAt[lbl] != 0 {
			continue
		}
		if uniqueN == 256 {
			return false, 0, nil
		}
		uniqueN++
		stubAt[lbl] = uniqueN
	}
	if len(labels)+5*uniqueN >= 4*len(labels) {
		return false, 0, nil
	}
	return true, uniqueN, stubAt
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
