//go:build arm64

package arm64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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

const (
	cfFunc ctrlKind = iota
	cfBlock
	cfLoop
	cfIf
)

// ctrlFrame is one open control construct (or the implicit function frame).
type ctrlFrame struct {
	kind            ctrlKind
	height          int // operand depth at the frame's result base
	paramN, resultN int
	branchN         int   // values transferred on a branch to this label
	loopStart       int   // cfLoop: backward target byte offset
	ends            []int // cfBlock/cfIf: forward B sites to patch to end
	condEnds        []int // cfBlock/cfIf: forward B.cond sites (imm19) to patch to end (empty-edge br_if fast path)
	elseSite        int   // cfIf: the false-edge B.cond site (to else/end), -1 once patched
	hasElse         bool
	entryUnreach    bool
	endReachable    bool
	regMerge1       bool        // single-result block/if: value lives in a register (mergeReg/mergeFReg) at edges, not a slot
	res0            machineType // first result's machine type (valid when resultN >= 1)
	baseTypes       []machineType
	paramTypes      []machineType
	resultTypes     []machineType

	// cfLoop only (P6.2 foundation): locals set anywhere in the loop body, and
	// whether the body grows memory — from a scan-ahead at the loop header. A local
	// base NOT in loopSetLocals is loop-invariant (a callee cannot touch a caller
	// local), so its bounds check is hoistable. nil for non-loops / unreachable.
	loopSetLocals map[uint32]bool
	loopHasGrow   bool
	// Loop-region allocation eligibility is collected in the same bounded scan
	// used by bounds hoisting. Promotion is enabled only once every exit edge is
	// modeled; these facts keep the eligibility decision one-pass and conservative.
	loopHasCall   bool
	loopHasNested bool
	loopHasTable  bool
	loopPins      []loopPin

	// Per-frame pinned-local merge agreement (convergeEdgeTo): branchState is the
	// recorded state at this frame's branch target (loop top for loops, the end
	// merge for blocks/ifs), fixed by the first edge; entryState is a cfIf
	// header snapshot — the else body's entry state, and the cond-false edge's
	// state for an if without else.
	branchState []locState
	entryState  []locState
	coldEdges   []coldEdge // deferred non-empty unlikely br_if edges targeting this frame
}

type coldEdge struct {
	site int
	code []byte
}

type loopPin struct {
	local int
	reg   Reg
}

// activateLoopPins is the deliberately narrow v1 region allocator. It borrows
// two caller-saved registers only for a simple call-free loop; slots remain the
// canonical representation outside the loop.
func (f *fn) activateLoopPins(fr *ctrlFrame) {
	if !loopRegionPinsEnabled || fr.kind != cfLoop || fr.loopHasCall || fr.loopHasNested || fr.loopHasTable {
		return
	}
	for _, r := range []Reg{X12, X13} {
		best := -1
		for x := 0; x < f.nLocals; x++ {
			if f.localType[x] != mtI32 && f.localType[x] != mtI64 || f.locals[x].reg != regNone || !fr.loopSetLocals[uint32(x)] {
				continue
			}
			already := false
			for _, p := range fr.loopPins {
				if p.local == x {
					already = true
					break
				}
			}
			if !already && best < 0 {
				best = x
			}
		}
		if best < 0 {
			break
		}
		fr.loopPins = append(fr.loopPins, loopPin{best, r})
		f.activeLoopPins = fr.loopPins // O(1) pinReg index; this is the only frame with pins
		f.pinnedLocalMask = f.pinnedLocalMask.add(r)
		if f.locals[best].state == lsConstZero {
			f.a.MovImm64(r, 0)
			f.locals[best].state = lsReg
		} else {
			f.ld64(r, SP, f.localOff(best))
			f.locals[best].state = lsStackReg
		}
	}
}

func (f *fn) storeLoopPinsLeaving(target int) {
	for i := len(f.ctrl) - 1; i > target; i-- {
		for _, p := range f.ctrl[i].loopPins {
			f.st64(SP, f.localOff(p.local), p.reg)
		}
	}
}

func (f *fn) releaseLoopPins(fr *ctrlFrame) {
	for _, p := range fr.loopPins {
		f.st64(SP, f.localOff(p.local), p.reg)
		f.pinnedLocalMask = f.pinnedLocalMask.remove(p.reg)
		f.locals[p.local].state = lsMem
	}
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
		panic("arm64: logical stack index out of range")
	}
	return slotsOfTypes(types[:logical])
}

func (f *fn) currentLogicalTypes() []machineType { return f.logicalTypes(f.rootsBottomToTop()) }

func (f *fn) moveBranchValues(fr *ctrlFrame, d, a int) {
	types := f.currentLogicalTypes()
	fromSlot := slotOfLogicalTypes(types, d-a)
	toSlot := slotsOfTypes(fr.baseTypes)
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

// flush materializes every operand into canonical frame slots, condensing
// deferred nodes, then rebuilds the stack model as canonical slot entries with
// all registers freed. v128 values occupy two adjacent 8-byte slots.
func (f *fn) flush() {
	f.stats.addFlush()
	f.invalidateGlobalsCache() // the cached cell ptr must not span a call/control boundary
	f.invalidateBoundsCert()   // bounds facts are valid only within a straight-line region
	roots := f.rootsBottomToTop()
	types := f.tmpTypes[:0]
	slot := 0
	for _, root := range roots {
		typ := rootMachineType(root)
		f.stats.addFlushRoot(root.kind == ekDeferred)
		types = append(types, typ)
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slot == slot && root.st.typ == typ {
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
		if root.kind == ekValue && (root.st.kind == stLocalReg || root.st.kind == stGlobReg) {
			if root.st.typ.isFloat() {
				f.fst(SP, f.spillOff(slot), root.st.reg, true)
			} else {
				f.st64(SP, f.spillOff(slot), root.st.reg) // copy pinned local/global's value; never release
			}
			slot++
			continue
		}
		if root.kind == ekValue && root.st.typ.isFloat() {
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
	f.setDepthTypes(types)
}

// setDepth resets the operand stack model to l canonical scalar slot entries
// and frees all registers.
func (f *fn) setDepth(l int) {
	types := f.tmpTypes[:0]
	for i := 0; i < l; i++ {
		types = append(types, mtI64)
	}
	f.tmpTypes = types
	f.setDepthTypes(types)
}

func (f *fn) setDepthTypes(types []machineType) {
	f.s.head.prev, f.s.head.next = f.s.head, f.s.head
	slot := 0
	for _, typ := range types {
		f.pushValue(storage{kind: stSlot, typ: typ, slot: slot})
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
	case 0x7F, 0x7E, 0x7D, 0x7C, 0x7B, 0x70, 0x6F:
		return true
	}
	return false
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
	case 0x70, 0x6F:
		return mtI64
	}
	return mtNone
}

// blockType decodes a block's parameter and result types, plus the first
// result's machine type (res0; mtNone when resultN == 0).
func (f *fn) blockType(r *wasm.Reader) (params, results []machineType, res0 machineType, err error) {
	b, ok := r.Peek()
	if !ok {
		return nil, nil, mtNone, fmt.Errorf("eof in blocktype")
	}
	if b == 0x40 { // empty
		_, _ = r.Byte()
		return nil, nil, mtNone, nil
	}
	if isValByte(b) {
		_, _ = r.Byte()
		mt := valByteMT(b)
		return nil, []machineType{mt}, mt, nil
	}
	x, e := r.I64()
	if e != nil {
		return nil, nil, mtNone, e
	}
	ft, ok := f.m.TypeFunc(uint32(x))
	if x < 0 || !ok {
		return nil, nil, mtNone, fmt.Errorf("bad blocktype index %d", x)
	}
	r0 := mtNone
	if len(ft.Results) > 0 {
		r0 = mtOf(ft.Results[0])
	}
	return typesOfVals(ft.Params), typesOfVals(ft.Results), r0, nil
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
	f.convergeEdgeTo(&fr.branchState)
}

// branchJump emits the jump for a branch that targets frame fr.
func (f *fn) branchJump(fr *ctrlFrame) {
	switch fr.kind {
	case cfLoop:
		// Backward unconditional branch to the loop top (imm26, ±128 MiB): emit a B
		// placeholder and patch it immediately since the target is already known.
		f.a.PatchBranch26(f.a.Branch(), fr.loopStart)
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
		sc := f.scratchState()
		sc.retSites = append(sc.retSites, f.a.Branch())
	default:
		f.appendEndSite(&fr.ends, f.a.Branch())
		fr.endReachable = true
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
		if !f.a.PatchBranch19(site, fr.loopStart) {
			f.a.B = f.a.B[:site] // out of imm19 range: undo and let the caller fall back
			return false
		}
		return true
	case cfBlock, cfIf:
		f.appendEndSite(&fr.condEnds, f.a.Bcond(cc)) // patched to the block end (imm19)
		fr.endReachable = true
		return true
	}
	return false // cfFunc: the guarded form carries the singleRegResult load
}

// --- control opcodes ---

// scanLoopBody scans a loop body ahead from the reader's current position (the
// body start, just past the blocktype) to the matching `end`, recording the
// locals it sets and whether it grows memory, then restores the reader. Reuses
// skipImmediates for operand skipping; br_table (not covered there) is handled
// inline. Post-validation, so a decode error just ends the scan.
func scanLoopBody(r *wasm.Reader) (setLocals map[uint32]bool, hasGrow, hasCall, hasNested, hasTable bool) {
	start := r.Offset()
	setLocals = map[uint32]bool{}
	depth := 0
scan:
	for {
		op, err := r.Byte()
		if err != nil {
			break
		}
		switch op {
		case 0x02, 0x03, 0x04: // block / loop / if: skip blocktype, enter one level
			if _, err := r.S33(); err != nil {
				break scan
			}
			if op == 0x03 {
				hasNested = true
			}
			depth++
		case 0x10, 0x11: // call / call_indirect
			hasCall = true
			if err := skipImmediates(r, op); err != nil {
				break scan
			}
		case 0x0b: // end
			if depth == 0 {
				break scan
			}
			depth--
		case 0x21, 0x22: // local.set / local.tee
			idx, err := r.U32()
			if err != nil {
				break scan
			}
			setLocals[idx] = true
		case 0x40: // memory.grow
			if _, err := r.U32(); err != nil {
				break scan
			}
			hasGrow = true
		case 0x0e: // br_table: vec(labelidx) + default labelidx
			hasTable = true
			n, err := r.U32()
			if err != nil {
				break scan
			}
			if err := r.SkipU32N(n + 1); err != nil {
				break scan
			}
		default:
			if err := skipImmediates(r, op); err != nil {
				break scan
			}
		}
	}
	r.JumpTo(start)
	return
}

func (f *fn) opBlock(r *wasm.Reader, op byte) error {
	paramTypes, resultTypes, res0, err := f.blockType(r)
	if err != nil {
		return err
	}
	pN, rN := len(paramTypes), len(resultTypes)
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
	fr := ctrlFrame{kind: kind, paramN: pN, resultN: rN, elseSite: -1, entryUnreach: f.unreachable, res0: res0, paramTypes: paramTypes, resultTypes: resultTypes}
	if kind == cfLoop {
		fr.branchN = pN
	} else {
		fr.branchN = rN
	}
	// Phase 2/3: a block or if producing exactly one result (int → mergeReg, float
	// → mergeFReg) carries that value in a register across all its edges (fall-
	// through, else, br/br_if/br_table, and an if's cond-false passthrough) instead
	// of a frame slot. Excludes loops (params, back-edge) and multi-value.
	fr.regMerge1 = f.regMerge && (kind == cfBlock || kind == cfIf) && rN == 1 && res0 != mtNone && res0 != mtV128
	if kind == cfLoop && !f.unreachable {
		fr.loopSetLocals, fr.loopHasGrow, fr.loopHasCall, fr.loopHasNested, fr.loopHasTable = scanLoopBody(r) // P6.2 + region-pin foundation (reader restored)
		// P6.2 loop versioning: hoist invariant-base bounds checks out of the loop
		// via a precheck + fast/slow bodies. Explicit mode only (guard has no inline
		// check to elide) and not while already inside a versioned body.
		if loopPrecheckEnabled && f.memSizeReg != regNone && !f.inVersionedLoop {
			if cands, elidable, hasGrow := scanLoopHoistable(r); len(cands) > 0 && !hasGrow && elidable >= loopPrecheckMinChecks {
				if f.compileVersionedLoop(r, paramTypes, resultTypes, res0, cands) {
					return nil
				}
			}
		}
	}
	if f.unreachable {
		f.ctrl = append(f.ctrl, fr)
		return nil
	}
	if kind == cfIf {
		f.convergeEdgeTo(&fr.entryState) // header snapshot: else entry / cond-false edge state
		if isFusableCompare(f.s.back()) {
			cond := f.s.back()
			f.flushBelow(cond)
			cc := f.condenseToFlags(cond)
			fr.height = f.depth() - pN
			fr.baseTypes = append([]machineType(nil), f.currentLogicalTypes()[:fr.height]...)
			fr.elseSite = f.a.Bcond(invertCond(cc)) // to else/end when false
			f.ctrl = append(f.ctrl, fr)
			return nil
		}
		creg, cOwned := f.materializeRead(f.popValue()) // the test only reads: a pinned local needs no copy
		fr.height = f.depth() - pN
		fr.baseTypes = append([]machineType(nil), f.currentLogicalTypes()[:fr.height]...)
		f.flush()
		f.a.CmpImm32(creg, 0) // CMP creg, #0 — sets NZCV (no x86 test/flag side effect)
		if cOwned {
			f.release(creg)
		}
		fr.elseSite = f.a.Bcond(condE) // B.EQ else/end (branch when condition is zero)
	} else {
		fr.height = f.depth() - pN
		fr.baseTypes = append([]machineType(nil), f.currentLogicalTypes()[:fr.height]...)
		if kind == cfLoop {
			// Loop tops converge eagerly (all lsStackReg): hoists any post-call
			// reload OUT of the body — a lazy (lsMem) loop target would push the
			// reload into every iteration instead.
			f.reconcileLocals()
			f.convergeEdgeTo(&fr.branchState) // records the all-lsStackReg target
		}
		f.flush()
		if kind == cfLoop {
			f.a.Align16() // loop-top alignment: the pad runs on entry, not per iteration
			fr.loopStart = f.a.Len()
			f.emitInterruptCheck()
		}
	}
	f.ctrl = append(f.ctrl, fr)
	if kind == cfLoop && !f.unreachable {
		f.activateLoopPins(&f.ctrl[len(f.ctrl)-1])
	}
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
	f.realizeLocalRefs(x, baseOfValentBlock(cond))
	creg, cOwned := f.materializeRead(f.popValue())
	f.a.CmpImm32(creg, 0)
	if cOwned {
		f.release(creg)
	}
	toElse := f.a.Bcond(condE)
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

func (f *fn) opElse() error {
	fr := &f.ctrl[len(f.ctrl)-1]
	if fr.entryUnreach {
		return nil
	}
	if f.unreachable {
		f.unreachable = false // else edge is reachable (cond-false analogue)
	} else {
		// The then-branch jumps to the if's end — a merge edge like any br
		// (#68's root cause was skipping this). Converge to the end's recorded
		// state; as the chronologically first end edge it usually fixes it.
		f.convergeEdgeTo(&fr.branchState)
		if fr.regMerge1 {
			f.reconcileMerge1(fr) // then-branch result → mergeReg
		} else {
			f.flush()
		}
		f.appendEndSite(&fr.ends, f.a.Branch())
		fr.endReachable = true
	}
	f.a.PatchBranch19(fr.elseSite, f.a.Len()) // the false edge is a B.cond (imm19)
	fr.elseSite = -1
	fr.hasElse = true
	f.setDepthTypes(f.frameDepthTypes(fr.baseTypes, fr.paramTypes))
	// The else body is entered via the if's false edge: locals are exactly in the
	// header-snapshot state (no code).
	f.setLocalsState(fr.entryState)
	return nil
}

func (f *fn) opEnd() error {
	last := len(f.ctrl) - 1
	fr := f.ctrl[last]
	// ctrl backing is reused across functions. Clear the popped slot so its
	// variable-sized type and loop-analysis slices do not stay live in scratch.
	f.ctrl[last] = ctrlFrame{}
	f.ctrl = f.ctrl[:len(f.ctrl)-1]
	if len(fr.loopPins) != 0 {
		f.activeLoopPins = nil // this frame's pins leave scope with the pop
	}

	if fr.kind == cfFunc {
		if !f.unreachable {
			if f.singleRegResult {
				f.placeSingleResult() // fall-through return: result straight to X0/V0
			} else {
				f.flush() // results land in slots [0, resultN)
			}
		}
		if len(fr.coldEdges) != 0 {
			skip := -1
			if !f.unreachable {
				skip = f.a.Branch()
			}
			for i := range fr.coldEdges {
				f.a.PatchBranch19(fr.coldEdges[i].site, f.a.Len())
				f.a.B = append(f.a.B, fr.coldEdges[i].code...)
				f.branchJump(&fr) // branch from the cold edge to the shared epilogue
			}
			if skip != -1 {
				f.a.PatchBranch26(skip, f.a.Len())
			}
		}
		return nil
	}

	fallthroughReachable := !f.unreachable
	if fr.kind == cfLoop && fallthroughReachable {
		f.releaseLoopPins(&fr)
	}
	if fallthroughReachable {
		if fr.kind != cfLoop {
			// Merge edge: converge to the end's recorded state (or fix it).
			// A loop end is NOT a merge — br edges target the loop TOP — so the
			// fall-through's state simply flows out.
			f.convergeEdgeTo(&fr.branchState)
		}
		if fr.regMerge1 {
			f.reconcileMerge1(&fr) // result → mergeReg, operands below → slots
		} else {
			f.flush() // results at [height, height+resultN)
		}
	}
	// An if without else: the cond-false path reaches end with params == results.
	if fr.kind == cfIf && !fr.hasElse && !fr.entryUnreach {
		// The cond-false edge arrives in the header-snapshot state; if then-side
		// edges fixed a stronger end state (or a regMerge1 passthrough needs its
		// value in mergeReg), a stub on this edge converges it. The then
		// fall-through jumps over the stub.
		needLoads := false
		if f.usesCalls && fr.branchState != nil && fr.entryState != nil {
			for x := 0; x < f.nLocals; x++ {
				if _, _, ok := f.pinReg(x); ok && fr.branchState[x] == lsStackReg && fr.entryState[x] == lsMem {
					needLoads = true
					break
				}
			}
		}
		skip := -1
		if (fr.regMerge1 || needLoads) && fallthroughReachable {
			skip = f.a.Branch()
		}
		f.a.PatchBranch19(fr.elseSite, f.a.Len()) // the false edge is a B.cond (imm19)
		if fr.regMerge1 {
			slot := slotsOfTypes(fr.baseTypes)
			if fr.res0.isFloat() {
				f.fld(mergeFReg, SP, f.spillOff(slot), fr.res0 == mtF64) // passthrough → mergeFReg
			} else {
				f.ld64(mergeReg, SP, f.spillOff(slot)) // passthrough value → mergeReg
			}
		}
		// Converge the cond-false edge from the header snapshot into the end state
		// (records it when this is the only end edge).
		f.setLocalsState(fr.entryState)
		f.convergeEdgeTo(&fr.branchState)
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len()) // the skip is an unconditional B (imm26)
		}
		fr.endReachable = true
	}
	if fr.kind == cfLoop && len(fr.coldEdges) != 0 {
		skip := -1
		if fallthroughReachable {
			skip = f.a.Branch()
		}
		for i := range fr.coldEdges {
			f.a.PatchBranch19(fr.coldEdges[i].site, f.a.Len())
			f.a.B = append(f.a.B, fr.coldEdges[i].code...)
			f.a.PatchBranch26(f.a.Branch(), fr.loopStart)
		}
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len())
		}
	}
	// Emit deferred cold br_if edges immediately before this frame's target. A
	// hinted false path therefore falls through at its source; only the unlikely
	// true edge reaches these fragments. Each fragment branches to the target
	// below along with ordinary forward edges.
	if fr.kind != cfLoop && len(fr.coldEdges) != 0 {
		// A normal fall-through must not execute a cold reconciliation fragment.
		// Its skip and every cold-edge jump converge at the target below.
		skip := -1
		if fallthroughReachable {
			skip = f.a.Branch()
		}
		for i := range fr.coldEdges {
			f.a.PatchBranch19(fr.coldEdges[i].site, f.a.Len())
			f.a.B = append(f.a.B, fr.coldEdges[i].code...)
			fr.ends = append(fr.ends, f.a.Branch())
			fr.endReachable = true
		}
		if skip != -1 {
			f.a.PatchBranch26(skip, f.a.Len())
		}
	}
	for _, site := range fr.ends {
		f.a.PatchBranch26(site, f.a.Len()) // fr.ends are unconditional B sites (imm26)
	}
	for _, site := range fr.condEnds {
		f.a.PatchBranch19(site, f.a.Len()) // fr.condEnds are B.cond sites (imm19)
	}
	endReachable := fallthroughReachable || fr.endReachable
	f.unreachable = !endReachable
	if endReachable {
		if fr.kind != cfLoop {
			f.setLocalsState(fr.branchState) // merge: what every edge guaranteed
		}
		if fr.regMerge1 {
			// Every reaching edge left the result in the merge register (int→mergeReg,
			// float→mergeFReg) and the operands below in canonical slots [0, height).
			f.setDepthTypes(fr.baseTypes)
			if fr.res0.isFloat() {
				f.pushFReg(mergeFReg, fr.res0)
			} else {
				f.pushReg(mergeReg, fr.res0)
			}
		} else {
			f.setDepthTypes(f.frameDepthTypes(fr.baseTypes, fr.resultTypes))
		}
	}
	// The popped frame no longer owns these temporary buffers. Recycle them for
	// later frames at the same or a shallower nesting depth.
	f.freeLocStateBuf(fr.branchState)
	f.freeLocStateBuf(fr.entryState)
	f.freeEndsBuf(fr.ends)
	f.freeEndsBuf(fr.condEnds)
	return nil
}

// branchToFrame emits an unconditional branch edge to control frame fi: converge
// pinned locals, flush operands, move the branched values into the frame's
// canonical slots (or merge register), and jump. Shared by opBr's unconditional
// path and opReturn's inlined-callee routing. The caller sets f.unreachable.
func (f *fn) branchToFrame(fi int) {
	fr := &f.ctrl[fi]
	f.storeLoopPinsLeaving(fi)
	f.convergeBranchLocals(fr)
	a, d := fr.branchN, f.depth()
	f.flush()
	if fr.regMerge1 {
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
	a, d := fr.branchN, f.depth()
	f.flush()
	f.a.CmpImm32(creg, 0) // CMP creg, #0
	if cOwned {
		f.release(creg)
	}
	// Emit the edge (loop-pin stores + value moves) first and measure it. The edge
	// helpers emit only straight-line, position-independent LDR/STR/MOV — no
	// branches or PC-relative ops — so the bytes can be relocated freely below.
	mark := f.a.Len()
	f.storeLoopPinsLeaving(fi)
	if fr.regMerge1 {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, a)
	}
	if f.a.Len() == mark {
		// Empty edge: one conditional branch straight to the target (taken when the
		// condition holds, != 0), with no skip branch and no padding NOP.
		if branchFoldEnabled && f.condBranchJump(fr, condNE) {
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
		fr.coldEdges = append(fr.coldEdges, coldEdge{site: site, code: edge})
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
		if fr.regMerge1 {
			f.branchEdgeToMerge1(fr, d)
		} else {
			f.moveBranchValues(fr, d, fr.branchN)
		}
		f.branchJump(fr)
	}
	if len(labels) >= brTableJumpMin {
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
		defSite := f.a.Bcond(condAE)                  // idx >= n → default (B.cond, imm19)
		adrSite := f.a.Adr(X16)                       // X16 = &table (PC-relative ADR, patched)
		f.a.LslImm(ireg, ireg, 2, false)              // idx *= 4 (u32 entries)
		f.a.LoadIdx(X17, X16, ireg, 0, 4, true, true) // X17 = (i32)table[idx]
		f.a.Add64(X17, X16, X17)                      // target = table base + entry
		f.a.Br(X17)
		tablePos := f.a.Len()
		f.a.PatchAdr(adrSite, tablePos)
		for range labels {
			f.a.B = append(f.a.B, 0, 0, 0, 0) // placeholder entries
		}
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
		stubAt := map[uint32]int{}
		stub := func(lbl uint32) int {
			if p, ok := stubAt[lbl]; ok {
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
		if p, ok := stubAt[def]; ok {
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
		sc := f.scratchState()
		sc.retSites = append(sc.retSites, f.a.Branch())
		f.unreachable = true
		return nil
	}
	fr := &f.ctrl[0]
	a, d := fr.resultN, f.depth()
	f.flush()
	f.moveBranchValues(fr, d, a)
	sc := f.scratchState()
	sc.retSites = append(sc.retSites, f.a.Branch())
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

// brTableJumpMin is the label count at which br_table switches from a linear
// cmp/jne chain to an indirect jump table.
const brTableJumpMin = 5

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
