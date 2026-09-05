//go:build arm64

package arm64

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Control flow: block / loop / if / else / end / br / br_if / br_table / return /
// unreachable. Ported from WARP's control-flow lowering, but using the canonical-
// slots reconciliation model (the same one backend/railshot/arm64 uses against this
// runtime): at every control boundary the operand stack is flushed to position-
// indexed frame slots, so all edges into a join agree on where each value lives.
// This trades WARP's RegisterCopyResolver register-shuffling for a simpler,
// proven scheme; register residency of locals is layered on separately.
//
// This file is a mechanical arm64 twin of backend/railshot/amd64/control.go: the
// operand-stack canonicalization, control-frame bookkeeping and merge logic are
// architecture-neutral and port verbatim; only the leaf instruction lowering
// changes. Per the port contract, x86 EFLAGS cmp+Jcc fusion becomes CMP + B.cond
// (§4b), the br_table RIP-relative jump table becomes an ADR-based table + BR
// through the backend scratch registers (§4e), and every frame-slot load/store
// goes through the encodability-checked f.ld64/f.st64/f.fld/f.fst helpers off the
// SP base (§6.1/§6.7). Forward branch sites are patched with PatchBranch19 for the
// conditional (imm19, ±1 MiB) sites and PatchBranch26 for the unconditional (imm26,
// ±128 MiB) sites — the two-range split of §6.2 — chosen statically at each site.

var errBadLabel = fmt.Errorf("arm64: br label out of range")

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
	ctrlHasBaseGCRoots
	ctrlHasParamGCRoots
	ctrlHasResultGCRoots
)

// ctrlFrame is one open control construct (or the implicit function frame).
type ctrlFrame struct {
	kind       ctrlKind
	res0       machineType // first result's machine type (valid when resultN >= 1)
	flags      ctrlFlags
	mergeIndex uint32 // index+1 into scratch.ctrlMerges; zero has no cold merge state
	branchN    uint32 // values transferred on a branch to this label
	baseTypes  uint16 // fixed-arena start in low byte, count in high byte

	height          int // operand depth at the frame's result base
	paramN, resultN int
	controlSite     int           // cfLoop: backward target; cfIf: false-edge branch, -1 once patched
	types           []machineType // parameters followed by results; split by paramN/resultN
}

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

// ctrlFrameMerge contains feature-specific state needed only by frames that
// merge pinned locals, track GC roots, patch branches, hoist/pin loops, or defer cold edges.
// Keeping it in compact scratch removes pointer-rich fields from ordinary frames.
type ctrlFrameMerge struct {
	ends         []uint32 // overflow after two inline packed forward-end sites
	branchState  packedLocStates
	entryState   packedLocStates
	loopSetStart uint32 // loop: modified-local range; other frames: first packed end site
	loopSetMeta  uint32 // loop: count + known bit; other frames: second packed end site
	coldEdges    []coldEdge
	eh           *ctrlFrameEH
}

// ctrlFrameRoots is allocated as a depth-parallel sidecar only when exact GC
// root tracking reaches structured control. Scalar merges do not retain three
// scanned slice headers per reserved merge slot.
type ctrlFrameRoots struct {
	flags []bool // base, parameters, then results
}

const initialCtrlMergeCapacity = 16

const loopSetKnownBit = uint32(1 << 16)

func (m *ctrlFrameMerge) setLoopSet(start uint32, count uint16) {
	m.loopSetStart = start
	m.loopSetMeta = uint32(count) | loopSetKnownBit
}

func (m *ctrlFrameMerge) hasLoopSet() bool  { return m.loopSetMeta&loopSetKnownBit != 0 }
func (m *ctrlFrameMerge) loopSetCount() int { return int(uint16(m.loopSetMeta)) }

type ctrlFrameEH struct {
	// cfTry only: one fixed native-stack handler record plus an ordered catch
	// dispatch table. Scalar exceptions carry at most two payload words; reference
	// catches copy those words into a fixed rooted exception slot before exposing
	// its stable frame-relative address.
	catches     []ehCatchClause
	targetSite  uint32
	recordIndex uint8
	refResults  [3]bool
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
	if fr.mergeIndex == 0 || int(fr.mergeIndex) > len(f.scratchState().ctrlRoots) {
		return nil
	}
	return &f.scratchState().ctrlRoots[fr.mergeIndex-1]
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
	f.ctrl = append(f.ctrl, *fr)
}

func (f *fn) frameBranchState(fr *ctrlFrame) packedLocStates {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge.branchState
	}
	return nil
}

func (f *fn) frameEntryState(fr *ctrlFrame) packedLocStates {
	if merge := f.ctrlMerge(fr); merge != nil {
		return merge.entryState
	}
	return nil
}

func (f *fn) setFrameBranchState(fr *ctrlFrame, state packedLocStates) {
	if state != nil {
		f.ensureCtrlMerge(fr).branchState = state
	}
}

func (f *fn) setFrameEntryState(fr *ctrlFrame, state packedLocStates) {
	if state != nil {
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
		f.a.PatchBranch19(site, f.a.Len())
	} else {
		f.a.PatchBranch26(site, f.a.Len())
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

func (f *fn) ensureFrameGCRootFlags(fr *ctrlFrame) []bool {
	roots := f.ensureCtrlRoots(fr)
	n := fr.height + fr.paramN + fr.resultN
	if len(roots.flags) < n {
		flags := make([]bool, n)
		copy(flags, roots.flags)
		roots.flags = flags
	}
	return roots.flags[:n]
}

func (f *fn) setFrameResultGCRoot(fr *ctrlFrame, index int) {
	flags := f.ensureFrameGCRootFlags(fr)
	flags[fr.height+fr.paramN+index] = true
	fr.set(ctrlHasResultGCRoots, true)
}

func (f *fn) frameLoopSetLocals(fr *ctrlFrame) []uint16 {
	if cold := f.ctrlMerge(fr); cold != nil && cold.hasLoopSet() {
		start := int(cold.loopSetStart)
		return f.loopSetLocals[start : start+cold.loopSetCount()]
	}
	return nil
}

func loopSetsLocal(locals []uint16, index uint32) bool {
	if index > uint32(^uint16(0)) {
		return false
	}
	_, ok := slices.BinarySearch(locals, uint16(index))
	return ok
}

func (f *fn) frameColdEdges(fr *ctrlFrame) []coldEdge {
	if cold := f.ctrlMerge(fr); cold != nil {
		return cold.coldEdges
	}
	return nil
}

func (f *fn) appendFrameColdEdge(fr *ctrlFrame, edge coldEdge) {
	cold := f.ensureCtrlMerge(fr)
	cold.coldEdges = append(cold.coldEdges, edge)
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

type ehCatchClause struct {
	tag         uint32
	frame       uint32
	matchSite   uint32
	kind        wasm.CatchKind
	scalarN     uint8
	payloadN    uint8
	rootIndex   uint8
	payloadType [3]machineType
}

type coldEdge struct {
	site int
	code []byte
}

// --- operand-stack canonicalization ---

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

// flushSuffix canonicalizes the stack and returns its logical types plus the
// physical slot where the last n logical operands begin. Logical depth and slot
// depth differ whenever values below the suffix include v128.
func (f *fn) flushSuffix(n int) ([]machineType, int) {
	f.flush()
	types := f.currentLogicalTypes()
	return types, slotOfLogicalTypes(types, len(types)-n)
}

func (f *fn) dropFlushedSuffix(types []machineType, n int) {
	f.setDepthTypes(types[:len(types)-n])
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
	f.stats.addFlush()
	f.invalidateGlobalsCache() // the cached cell ptr must not span a call/control boundary
	f.invalidateBoundsCert()   // bounds facts are valid only within a straight-line region
	roots := f.rootsBottomToTop()
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
	f.tmpTypes = types
	f.setDepthTypesWithGCRoots(types, gcRoots)
}

// flushWideStack stages unusually wide operand stacks in a disjoint frame range
// before copying them to canonical slots. This avoids overwriting earlier
// allocation spills while the one-pass canonicalizer is still reloading them.
func (f *fn) flushWideStack(roots []*elem, gcRoots []bool) bool {
	const wideFlushSlots = 64

	types := f.tmpFlushTypes[:0]
	total := 0
	for _, root := range roots {
		typ := rootMachineType(root)
		types = append(types, typ)
		total += typ.stackSlots()
	}
	f.tmpFlushTypes = types
	if total <= wideFlushSlots {
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

func (f *fn) setDepthTypes(types []machineType) {
	f.setDepthTypesWithGCRoots(types, nil)
}

func (f *fn) setDepthTypesWithGCRoots(types []machineType, gcRoots []bool) {
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
	if fromBase == toBase {
		return
	}
	for i := 0; i < n; i++ {
		f.ld64(X0, SP, f.spillOff(fromBase+i))
		f.st64(SP, f.spillOff(toBase+i), X0)
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
			f.a.FmovReg(0, x, f.resultF64) // -> V0
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
	f.convergeFrameBranchState(fr)
}

// branchJump emits the jump for a branch that targets frame fr.
func (f *fn) branchJump(fr *ctrlFrame) {
	switch fr.kind {
	case cfLoop:
		// Backward unconditional branch to the loop top (imm26, ±128 MiB): emit a B
		// placeholder and patch it immediately since the target is already known.
		f.a.PatchBranch26(f.a.Branch(), fr.controlSite)
	case cfFunc:
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

const pollFreeLoopPhaseMaxLocals = 16

// alignLoopHeader keeps poll-free loop bodies in the same half of a 32-byte
// fetch block that the former four-instruction cooperative poll selected. The
// padding precedes loopStart, so it executes only on initial entry and never on
// a backedge. Large, loop-dense functions skip it: their extra code footprint
// costs more than preserving the fetch phase.
func (f *fn) alignLoopHeader() {
	loopAlign := f.policy.LoopAlignLog2
	if loopAlign == 0 {
		loopAlign = 4
	}
	f.alignCode(loopAlign)
	if loopAlign >= 4 && !f.interruptible && f.nLocals <= pollFreeLoopPhaseMaxLocals {
		f.a.Nop4()
	}
}

// --- control opcodes ---

// scanLoopSetLocals scans a loop body ahead from the reader's current position
// (the body start, just past the blocktype) to the matching `end`, recording the
// locals it sets. The module-aware classifier keeps mixed memory-width
// immediates synchronized. Any unexpected decode failure returns no proof.
func scanLoopSetLocals(r *wasm.Reader, classifier wasm.ModuleInstructionClassifier, dst []uint16) (setLocals []uint16) {
	start := r.Offset()
	defer func() { _ = r.JumpTo(start) }()
	base := len(dst)
	setLocals = dst
	if setLocals == nil {
		setLocals = []uint16{}
	}
	depth := 0
	var imm wasm.InstructionImmediate
	for {
		op, err := r.Byte()
		if err != nil {
			return nil
		}
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return nil
		}
		switch imm.Kind {
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			if imm.Index > uint32(^uint16(0)) {
				return nil
			}
			setLocals = append(setLocals, uint16(imm.Index))
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x0b:
			if depth == 0 {
				modified := setLocals[base:]
				slices.Sort(modified)
				modified = slices.Compact(modified)
				setLocals = setLocals[:base+len(modified)]
				return
			}
			depth--
		}
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
		if done, err := f.trySimpleIfLocalSet(r); done || err != nil {
			return err
		}
	}
	fr := ctrlFrame{kind: kind, paramN: pN, resultN: rN, controlSite: -1, res0: res0, types: frameTypes}
	fr.set(ctrlEntryUnreachable, f.unreachable)
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
	if kind == cfLoop && !f.unreachable && f.stats != nil {
		base := len(f.loopSetLocals)
		setLocals := scanLoopSetLocals(r, f.classifier, f.loopSetLocals)
		if setLocals != nil {
			f.loopSetLocals = setLocals
			cold := f.ensureCtrlMerge(&fr)
			cold.setLoopSet(uint32(base), uint16(len(setLocals)-base))
		}
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
			// Loop tops converge eagerly (all lsStackReg): hoists any post-call
			// reload OUT of the body — a lazy (lsMem) loop target would push the
			// reload into every iteration instead.
			f.reconcileLocals()
			f.convergeFrameBranchState(&fr) // records the all-lsStackReg target
		}
		f.flush()
		if kind == cfLoop {
			f.alignLoopHeader()
			fr.controlSite = f.a.Len()
			f.emitInterruptCheck()
		}
	}
	f.pushCtrl(&fr)
	return nil
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
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert()
	}
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
	f.a.PatchBranch19(toElse, f.a.Len())
	if !f.aluImm3(elseArm.op, dest, dest, elseArm.imm, false) {
		panic("arm64: prechecked if arm immediate became unencodable")
	}
	f.a.PatchBranch26(toEnd, f.a.Len())
	f.markLocalDirty(x)
	f.stats.peep("if-local-sink")
	return true, nil
}

const (
	ehRecordSlots    = 7
	ehRootSlots      = 3
	maxEHTryRecords  = 4
	maxEHRootRecords = 4
	maxEHCatches     = 8
	ehPrevOff        = 0
	ehSavedSPOff     = 8
	ehTagOff         = 16
	ehPayload0Off    = 24
	ehPayload1Off    = 32
	ehTargetOff      = 40
	ehSavedLinMemOff = 48
	offEHTagDirPtr   = abi.EHTagDirPtrOffset
	ehReg            = X22
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
	fr := ctrlFrame{kind: cfTry, paramN: pN, resultN: rN, branchN: uint32(rN), controlSite: -1, res0: res0, types: frameTypes}
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
			clause.scalarN = uint8(len(ft.Params))
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
				clause.rootIndex = uint8(f.ehRootCount)
				f.ehRootCount++
				clause.payloadType[clause.payloadN] = mtI64
				clause.payloadN++
			}
		case wasm.CatchAll:
		case wasm.CatchAllRef:
			if f.ehRootCount >= maxEHRootRecords {
				return fmt.Errorf("bounded exception handling supports at most %d rooted exception values per function", maxEHRootRecords)
			}
			clause.rootIndex = uint8(f.ehRootCount)
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
		frame := len(f.ctrl) - 1 - int(label)
		if frame < 0 {
			return errBadLabel
		}
		clause.frame = uint32(frame)
		if f.ctrl[frame].branchArity() != int(clause.payloadN) {
			return fmt.Errorf("bounded exception handler payload arity mismatch")
		}
		if kind == wasm.CatchRef || kind == wasm.CatchAllRef {
			f.ensureFrameEH(&f.ctrl[frame]).refResults[clause.payloadN-1] = true
		}
		f.ctrl[frame].set(ctrlRegMerge1, false)
		eh.catches = append(eh.catches, clause)
	}
	fr.height = f.depth() - fr.paramN
	f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
	if f.unreachable {
		f.pushCtrl(&fr)
		return nil
	}
	if f.ehTryDepth >= maxEHTryRecords {
		return fmt.Errorf("bounded exception handling supports at most %d nested try_table records", maxEHTryRecords)
	}
	eh.recordIndex = uint8(f.ehTryDepth)
	f.ehTryDepth++
	f.reconcileLocals()
	f.flush()
	for i := range eh.catches {
		clause := &eh.catches[i]
		if clause.kind != wasm.CatchRef && clause.kind != wasm.CatchAllRef {
			continue
		}
		rootOff := f.ehRootOff(int(clause.rootIndex))
		for word := int32(0); word < ehRootSlots*8; word += 8 {
			f.st64(SP, rootOff+word, ZR)
		}
		f.stats.peep("eh-root-init")
	}
	recordOff := f.ehRecordOff(int(eh.recordIndex))
	f.leaDisp(X16, SP, recordOff, true)
	f.st64(X16, ehPrevOff, ehReg)
	f.a.AddImm64(X17, SP, 0)
	f.st64(X16, ehSavedSPOff, X17)
	eh.targetSite = uint32(f.a.Adr(X17))
	f.recordPCRelative(int(eh.targetSite))
	f.st64(X16, ehTargetOff, X17)
	f.st64(X16, ehSavedLinMemOff, linMemReg)
	f.a.MovReg64(ehReg, X16)
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
	noHandler := f.zeroBranch(ehReg, true, true)
	f.ld64(X16, linMemReg, -int32(offEHTagDirPtr))
	f.ld64(X16, X16, int32(tag*8))
	f.st64(ehReg, ehTagOff, X16)
	base := len(types) - len(ft.Params)
	for i := range ft.Params {
		slot := slotOfLogicalTypes(types, base+i)
		f.ld64(X16, SP, f.spillOff(slot))
		off := int32(ehPayload0Off)
		if i == 1 {
			off = ehPayload1Off
		}
		f.st64(ehReg, off, X16)
	}
	f.ld64(X17, ehReg, ehSavedSPOff)
	f.ld64(X16, ehReg, ehTargetOff)
	f.a.AddImm64(SP, X17, 0)
	f.a.Br(X16)
	f.a.PatchBranch19(noHandler, f.a.Len())
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
	f.ld64(X16, SP, f.spillOff(refSlot))
	f.trapIfZero(X16, true, true, trapNullReference)
	noHandler := f.zeroBranch(ehReg, true, true)
	for _, off := range [...]int32{0, 8, 16} {
		f.ld64(X17, X16, off)
		f.st64(ehReg, ehTagOff+off, X17)
	}
	f.ld64(X17, ehReg, ehSavedSPOff)
	f.ld64(X16, ehReg, ehTargetOff)
	f.a.AddImm64(SP, X17, 0)
	f.a.Br(X16)
	f.a.PatchBranch19(noHandler, f.a.Len())
	f.trapAlways(trapUnhandledException)
	f.unreachable = true
	return nil
}

func (f *fn) emitEHCatchRoute(fr *ctrlFrame, clause *ehCatchClause, recordOff int32) {
	target := &f.ctrl[int(clause.frame)]
	f.ld64(ehReg, SP, recordOff+ehPrevOff)

	rootOff := int32(0)
	if clause.kind == wasm.CatchRef || clause.kind == wasm.CatchAllRef {
		rootOff = f.ehRootOff(int(clause.rootIndex))
		for _, off := range [...]int32{ehTagOff, ehPayload0Off, ehPayload1Off} {
			f.ld64(X16, SP, recordOff+off)
			f.st64(SP, rootOff+off-ehTagOff, X16)
		}
	}

	loadPayload := func(reg Reg, i int) {
		if i == int(clause.scalarN) {
			f.leaDisp(reg, SP, rootOff, true)
			return
		}
		off := recordOff + ehPayload0Off
		if i == 1 {
			off = recordOff + ehPayload1Off
		}
		f.ld64(reg, SP, off)
	}
	if target.has(ctrlRegMerge1) && clause.payloadN == 1 {
		if clause.scalarN == 0 {
			f.leaDisp(mergeReg, SP, rootOff, true)
		} else if clause.payloadType[0].isFloat() {
			off := recordOff + ehPayload0Off
			f.fld(mergeFReg, SP, off, clause.payloadType[0] == mtF64)
		} else {
			loadPayload(mergeReg, 0)
		}
	} else {
		toSlot := slotsOfTypes(f.frameBaseTypes(target))
		for i := 0; i < int(clause.payloadN); i++ {
			loadPayload(X16, i)
			f.st64(SP, f.spillOff(toSlot+i), X16)
		}
	}

	var savedState [16]locState
	var savedLocal [16]int
	savedN := 0
	if f.usesCalls {
		for x := range f.locals {
			if _, _, ok := f.pinReg(x); !ok {
				continue
			}
			if savedN == len(savedState) {
				panic("arm64: too many pinned locals in EH route")
			}
			savedLocal[savedN] = x
			savedState[savedN] = f.locals[x].state
			savedN++
			f.locals[x].state = lsMem
		}
	}
	f.convergeBranchLocals(target)
	f.branchJump(target)
	for i := 0; i < savedN; i++ {
		f.locals[savedLocal[i]].state = savedState[i]
	}
}

func (f *fn) emitEHHandler(fr *ctrlFrame) {
	eh := f.ensureFrameEH(fr)
	recordOff := f.ehRecordOff(int(eh.recordIndex))
	handlerPos := f.a.Len()
	if !f.a.PatchAdr(int(eh.targetSite), handlerPos) {
		panic("arm64: exception handler ADR out of range")
	}
	f.ld64(linMemReg, SP, recordOff+ehSavedLinMemOff)
	if f.memSizeReg != regNone {
		f.ld64(f.memSizeReg, linMemReg, -bdCurBytes)
	}
	f.deriveModuleGlobals()
	f.derivePinnedGlobals()

	dispatchN := len(eh.catches)
	for i := range eh.catches {
		clause := &eh.catches[i]
		if clause.kind == wasm.CatchAll || clause.kind == wasm.CatchAllRef {
			clause.matchSite = uint32(f.a.Branch())
			dispatchN = i + 1
			break
		}
		f.ld64(X16, SP, recordOff+ehTagOff)
		f.ld64(X17, linMemReg, -int32(offEHTagDirPtr))
		f.ld64(X17, X17, int32(clause.tag*8))
		f.cmpRR(X16, X17, true)
		clause.matchSite = uint32(f.a.Bcond(condE))
	}

	f.leaDisp(X16, SP, recordOff, true)
	f.ld64(X17, X16, ehPrevOff)
	noPrevious := f.zeroBranch(X17, true, true)
	for _, off := range [...]int32{ehTagOff, ehPayload0Off, ehPayload1Off} {
		f.ld64(X9, X16, off)
		f.st64(X17, off, X9)
	}
	f.a.MovReg64(ehReg, X17)
	f.ld64(X16, X17, ehTargetOff)
	f.ld64(X17, X17, ehSavedSPOff)
	f.a.AddImm64(SP, X17, 0)
	f.a.Br(X16)
	f.a.PatchBranch19(noPrevious, f.a.Len())
	f.trapAlways(trapUnhandledException)

	for i := 0; i < dispatchN; i++ {
		clause := &eh.catches[i]
		if clause.kind == wasm.CatchAll || clause.kind == wasm.CatchAllRef {
			f.a.PatchBranch26(int(clause.matchSite), f.a.Len())
		} else {
			f.a.PatchBranch19(int(clause.matchSite), f.a.Len())
		}
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
		f.convergeFrameBranchState(fr)
		if fr.has(ctrlRegMerge1) {
			f.reconcileMerge1(fr) // then-branch result → mergeReg
		} else {
			f.flush()
		}
		f.appendFrameEnd(fr, f.a.Branch(), false)
		fr.set(ctrlEndReachable, true)
	}
	f.a.PatchBranch19(fr.controlSite, f.a.Len()) // the false edge is a B.cond (imm19)
	fr.controlSite = -1
	fr.set(ctrlHasElse, true)
	f.setDepthTypesWithGCRoots(f.frameDepthTypesForFrame(fr, true), frameGCRootFlags(f.frameBaseGCRoots(fr), f.frameParamGCRoots(fr)))
	// The else body is entered via the if's false edge: locals are exactly in the
	// header-snapshot state (no code).
	f.setLocalsState(f.frameEntryState(fr))
	return nil
}

func (f *fn) opEnd(r *wasm.Reader) error {
	last := len(f.ctrl) - 1
	fr := f.ctrl[last]
	branchState := f.frameBranchState(&fr)
	entryState := f.frameEntryState(&fr)
	baseGCRoots := f.frameBaseGCRoots(&fr)
	paramGCRoots := f.frameParamGCRoots(&fr)
	resultGCRoots := f.frameResultGCRoots(&fr)
	firstEnd, secondEnd, ends := f.frameEndSites(&fr)
	coldEdges := f.frameColdEdges(&fr)
	// ctrl backing is reused across functions. Clear the popped slot so its
	// variable-sized type and loop-analysis slices do not stay live in scratch.
	f.ctrl[last] = ctrlFrame{mergeIndex: fr.mergeIndex}
	f.ctrl = f.ctrl[:len(f.ctrl)-1]

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
				f.a.PatchBranch19(coldEdges[i].site, f.a.Len())
				f.a.B = append(f.a.B, coldEdges[i].code...)
				f.branchJump(&fr) // branch from the cold edge to the shared epilogue
			}
			if skip != -1 {
				f.a.PatchBranch26(skip, f.a.Len())
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
			deadGP, deadFP := f.planForwardMergeDeadLocals(r, branchState, nil)
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
		if f.usesCalls && branchState != nil && entryState != nil {
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
		f.a.PatchBranch19(fr.controlSite, f.a.Len()) // the false edge is a B.cond (imm19)
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
		f.setLocalsState(entryState)
		f.convergeFrameBranchStateWithDead(&fr, deadGP, deadFP)
		branchState = f.frameBranchState(&fr)
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len()) // the skip is an unconditional B (imm26)
		}
		fr.set(ctrlEndReachable, true)
	}
	if fr.kind == cfLoop && len(coldEdges) != 0 {
		skip := -1
		if fallthroughReachable {
			skip = f.a.Branch()
		}
		for i := range coldEdges {
			f.a.PatchBranch19(coldEdges[i].site, f.a.Len())
			f.a.B = append(f.a.B, coldEdges[i].code...)
			f.a.PatchBranch26(f.a.Branch(), fr.controlSite)
		}
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len())
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
			f.a.PatchBranch19(coldEdges[i].site, f.a.Len())
			f.a.B = append(f.a.B, coldEdges[i].code...)
			f.appendFrameEnd(&fr, f.a.Branch(), false)
			firstEnd, secondEnd, ends = f.frameEndSites(&fr)
			fr.set(ctrlEndReachable, true)
		}
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len())
		}
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
		recordOff := f.ehRecordOff(int(f.ensureFrameEH(&fr).recordIndex))
		if endReachable {
			f.ld64(ehReg, SP, recordOff+ehPrevOff)
		}
		skip := -1
		if endReachable {
			skip = f.a.Branch()
		}
		f.emitEHHandler(&fr)
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len())
		}
		f.ehTryDepth--
	}
	// The popped frame no longer owns these temporary buffers. Recycle them for
	// later frames at the same or a shallower nesting depth.
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
	a, d := fr.branchArity(), f.depth()
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
	f.convergeBranchLocals(fr)
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
	if f.a.Len() == mark {
		if f.opt(optZeroBranch) && f.policy.CompactNative {
			f.a.B = f.a.B[:testAt]
			if f.opt(optBranchFold) && f.zeroBranchJump(fr, creg) {
				f.stats.peep("zero-branch")
				return nil
			}
			over := f.a.Cbz32(creg)
			f.branchJump(fr)
			f.a.PatchBranch19(over, f.a.Len())
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
		f.a.PatchBranch19(over, f.a.Len())
		return nil
	}
	if f.branchHintUnlikely {
		// Emit the edge into a temporary assembler. It contains only the
		// position-independent local/value reconciliation bytes; its final jump
		// is emitted when the target frame closes.
		edge := append([]byte(nil), f.a.B[mark:]...)
		f.a.B = f.a.B[:mark]
		site := f.a.Bcond(condNE)
		f.appendFrameColdEdge(fr, coldEdge{site: site, code: edge})
		return nil
	}
	// Non-empty edge: the edge is already emitted at [mark:]; insert the skip guard
	// before it by relocating the (position-independent) edge bytes up one word.
	f.edgeScratch = append(f.edgeScratch[:0], f.a.B[mark:]...)
	f.a.B = f.a.B[:mark]
	over := f.a.Bcond(condE) // skip the edge when the condition is false (== 0)
	f.a.B = append(f.a.B, f.edgeScratch...)
	f.branchJump(fr)
	f.a.PatchBranch19(over, f.a.Len()) // `over` is a B.cond (imm19)
	return nil
}

func (f *fn) brOnNull(r *wasm.Reader) error {
	value := f.popValue()
	gcRoot := value.st.hasGCRoot()
	ref := f.materialize(value)
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	d := f.depth()
	f.flush()
	refSlot := f.allocSpillSlot()
	f.st64(SP, f.spillOff(refSlot), ref)
	f.release(ref)
	over := f.zeroBranch(ref, true, false)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchArity())
	}
	f.branchJump(fr)
	f.a.PatchBranch19(over, f.a.Len())
	fallthroughRef := f.allocReg(0)
	f.ld64(fallthroughRef, SP, f.spillOff(refSlot))
	f.pushReg(fallthroughRef, mtI64).st.setGCRoot(gcRoot)
	return nil
}

func (f *fn) brOnNonNull(r *wasm.Reader) error {
	value := f.popValue()
	gcRoot := value.st.hasGCRoot()
	ref := f.materialize(value)
	f.pushReg(ref, mtI64).st.setGCRoot(gcRoot)
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	allTypes := append([]machineType(nil), f.currentLogicalTypes()...)
	d := len(allTypes)
	refSlot := slotsOfTypes(allTypes) - 1
	f.flush()
	condition := f.allocReg(0)
	f.ld64(condition, SP, f.spillOff(refSlot))
	f.release(condition)
	over := f.zeroBranch(condition, true, true)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchArity())
	}
	f.branchJump(fr)
	f.a.PatchBranch19(over, f.a.Len())
	_ = f.popValue()
	return nil
}

// brOnCastResult consumes the helper's i32 match result while leaving the
// original reference at the top of the logical stack. Both edges retain the
// same compact reference identity; validation supplies only the refined type.
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
	if owned {
		f.release(matched)
	}
	skipCond := condE
	if !branchOnMatch {
		skipCond = condNE
	}
	over := f.zeroBranch(matched, false, skipCond == condE)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchArity())
	}
	f.branchJump(fr)
	f.a.PatchBranch19(over, f.a.Len())
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
			f.moveBranchValues(fr, d, fr.branchArity())
		}
		f.branchJump(fr)
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
				f.a.PatchBranch26(vectorPos+4*i, p)
				emitCase(lbl)
			}
			if encoded := compactStubAt[def]; encoded < 0 {
				f.a.PatchBranch19(defSite, -encoded-1)
			} else {
				f.a.PatchBranch19(defSite, f.a.Len())
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
					f.a.PatchBranch19(defSite, p)
				}
				emitCase(lbl)
			}
			if defIdx < 0 {
				f.a.PatchBranch19(defSite, f.a.Len())
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
			f.a.PatchBranch19(defSite, p)
		} else {
			f.a.PatchBranch19(defSite, f.a.Len())
			emitCase(def)
		}
		f.unreachable = true
		return nil
	}
	for i, lbl := range labels {
		f.a.CmpImm32(ireg, uint32(i)) // cmp ireg, i (i < brTableJumpMin, always fits imm12)
		skip := f.a.Bcond(condNE)
		emitCase(lbl)
		f.a.PatchBranch19(skip, f.a.Len()) // `skip` is a B.cond (imm19)
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

func skipImmediatesWithMemory64(r *wasm.Reader, op byte, memory64 bool) error {
	if memory64 {
		return wasm.SkipInstructionImmediateWithMemarg64(r, op, true)
	}
	return skipImmediates(r, op)
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

// brTableJumpMin is the label count at which br_table switches from a linear
// cmp/jne chain to an indirect jump table.
const brTableJumpMin = 5

func align4(n int) int { return (n + 3) &^ 3 }

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
