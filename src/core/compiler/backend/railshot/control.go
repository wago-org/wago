package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
	ends            []int // cfBlock/cfIf: forward jmp sites to patch to end
	elseSite        int   // cfIf: the jz site (to else/end), -1 once patched
	hasElse         bool
	entryUnreach    bool
	endReachable    bool
	regMerge1       bool        // single-result block/if: value lives in a register (mergeReg/mergeFReg) at edges, not a slot
	res0            machineType // first result's machine type (valid when resultN >= 1)

	// Per-frame pinned-local merge agreement (convergeEdgeTo): branchState is the
	// recorded state at this frame's branch target (loop top for loops, the end
	// merge for blocks/ifs), fixed by the first edge; entryState is a cfIf
	// header snapshot — the else body's entry state, and the cond-false edge's
	// state for an if without else.
	branchState []locState
	entryState  []locState
}

// --- operand-stack canonicalization ---

// depth returns the number of logical operands (valent-block roots) on the stack.
func (f *fn) depth() int {
	n := 0
	for cur := f.s.head.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		n++
	}
	return n
}

// rootsBottomToTop returns the logical operands in bottom-to-top order.
func (f *fn) rootsBottomToTop() []*elem {
	var rs []*elem
	for cur := f.s.head.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		rs = append(rs, cur)
	}
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return rs
}

// flush materializes every operand into its canonical frame slot (position i →
// spillOff(i)), condensing deferred nodes, then rebuilds the stack model as a run
// of canonical slot entries with all registers freed.
func (f *fn) flush() {
	f.invalidateGlobalsCache() // the cached cell ptr must not span a call/control boundary
	roots := f.rootsBottomToTop()
	for i, root := range roots {
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slot == i {
			continue // already canonical
		}
		if root.kind == ekValue && root.st.kind == stLocalReg {
			if root.st.typ.isFloat() {
				f.a.FStoreDisp(RSP, f.spillOff(i), root.st.reg, true)
			} else {
				f.a.Store64(RSP, f.spillOff(i), root.st.reg) // copy pinned local's value; never release
			}
			continue
		}
		if root.kind == ekValue && root.st.typ.isFloat() {
			x := f.materializeF(root)
			f.a.FStoreDisp(RSP, f.spillOff(i), x, true) // 8B store
			f.releaseF(x)
			continue
		}
		r := f.materialize(root)
		f.a.Store64(RSP, f.spillOff(i), r)
		f.release(r)
	}
	f.setDepth(len(roots))
}

// setDepth resets the operand stack model to l canonical slot entries (slots
// 0..l-1) and frees all registers.
func (f *fn) setDepth(l int) {
	f.s.head.prev, f.s.head.next = f.s.head, f.s.head
	f.refs = nil
	for i := 0; i < l; i++ {
		f.pushValue(storage{kind: stSlot, typ: mtI64, slot: i})
	}
	if l > f.maxSpill {
		f.maxSpill = l
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
	case 0x7F, 0x7E, 0x7D, 0x7C, 0x7B, 0x70, 0x6F:
		return true
	}
	return false
}

// valByteMT maps a value-type byte to its machine type (mtNone if not scalar).
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
	}
	return mtNone
}

// blockType decodes a block's parameter and result counts, plus the first
// result's machine type (res0; mtNone when resultN == 0).
func (f *fn) blockType(r *wasm.Reader) (pN, rN int, res0 machineType, err error) {
	b, ok := r.Peek()
	if !ok {
		return 0, 0, mtNone, fmt.Errorf("eof in blocktype")
	}
	if b == 0x40 { // empty
		_, _ = r.Byte()
		return 0, 0, mtNone, nil
	}
	if isValByte(b) {
		_, _ = r.Byte()
		return 0, 1, valByteMT(b), nil
	}
	x, e := r.I64()
	if e != nil {
		return 0, 0, mtNone, e
	}
	ft, ok := f.m.TypeFunc(uint32(x))
	if x < 0 || !ok {
		return 0, 0, mtNone, fmt.Errorf("bad blocktype index %d", x)
	}
	r0 := mtNone
	if len(ft.Results) > 0 {
		r0 = mtOf(ft.Results[0])
	}
	return len(ft.Params), len(ft.Results), r0, nil
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
	if fr.res0.isFloat() {
		f.a.FLoadDisp(mergeFReg, RSP, f.spillOff(d-1), fr.res0 == mtF64)
	} else {
		f.a.Load64(mergeReg, RSP, f.spillOff(d-1))
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
		f.retSites = append(f.retSites, f.a.JmpPlaceholder())
	default:
		fr.ends = append(fr.ends, f.a.JmpPlaceholder())
		fr.endReachable = true
	}
}

// --- control opcodes ---

func (f *fn) opBlock(r *wasm.Reader, op byte) error {
	pN, rN, res0, err := f.blockType(r)
	if err != nil {
		return err
	}
	kind := cfBlock
	if op == 0x03 {
		kind = cfLoop
	} else if op == 0x04 {
		kind = cfIf
	}
	fr := ctrlFrame{kind: kind, paramN: pN, resultN: rN, elseSite: -1, entryUnreach: f.unreachable, res0: res0}
	if kind == cfLoop {
		fr.branchN = pN
	} else {
		fr.branchN = rN
	}
	// Phase 2/3: a block or if producing exactly one result (int → mergeReg, float
	// → mergeFReg) carries that value in a register across all its edges (fall-
	// through, else, br/br_if/br_table, and an if's cond-false passthrough) instead
	// of a frame slot. Excludes loops (params, back-edge) and multi-value.
	fr.regMerge1 = f.regMerge && (kind == cfBlock || kind == cfIf) && rN == 1 && res0 != mtNone
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
			fr.elseSite = f.a.JccPlaceholder(invertCond(cc)) // to else/end when false
			f.ctrl = append(f.ctrl, fr)
			return nil
		}
		creg := f.materialize(f.popValue())
		fr.height = f.depth() - pN
		f.flush()
		f.a.TestSelf(creg, false)
		fr.elseSite = f.a.JccPlaceholder(condE) // jz else/end
	} else {
		fr.height = f.depth() - pN
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
		}
	}
	f.ctrl = append(f.ctrl, fr)
	return nil
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
		fr.ends = append(fr.ends, f.a.JmpPlaceholder())
		fr.endReachable = true
	}
	f.a.PatchRel32(fr.elseSite, f.a.Len())
	fr.elseSite = -1
	fr.hasElse = true
	f.setDepth(fr.height + fr.paramN)
	// The else body is entered via the if's false edge: locals are exactly in the
	// header-snapshot state (no code).
	f.setLocalsState(fr.entryState)
	return nil
}

func (f *fn) opEnd() error {
	fr := f.ctrl[len(f.ctrl)-1]
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
			skip = f.a.JmpPlaceholder()
		}
		f.a.PatchRel32(fr.elseSite, f.a.Len())
		if fr.regMerge1 {
			if fr.res0.isFloat() {
				f.a.FLoadDisp(mergeFReg, RSP, f.spillOff(fr.height), fr.res0 == mtF64) // passthrough → mergeFReg
			} else {
				f.a.Load64(mergeReg, RSP, f.spillOff(fr.height)) // passthrough value → mergeReg
			}
		}
		// Converge the cond-false edge from the header snapshot into the end state
		// (records it when this is the only end edge).
		f.setLocalsState(fr.entryState)
		f.convergeEdgeTo(&fr.branchState)
		if skip != -1 {
			f.a.PatchRel32(skip, f.a.Len())
		}
		fr.endReachable = true
	}
	for _, site := range fr.ends {
		f.a.PatchRel32(site, f.a.Len())
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
			f.setDepth(fr.height)
			if fr.res0.isFloat() {
				f.pushFReg(mergeFReg, fr.res0)
			} else {
				f.pushReg(mergeReg, fr.res0)
			}
		} else {
			f.setDepth(fr.height + fr.resultN)
		}
	}
	return nil
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
	if conditional {
		creg = f.materialize(f.popValue())
	}
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
	a, base, d := fr.branchN, fr.height, f.depth()
	f.flush()
	if !conditional {
		if fr.regMerge1 {
			f.branchEdgeToMerge1(fr, d)
		} else {
			f.moveSlots(d-a, base, a)
		}
		f.branchJump(fr)
		f.unreachable = true
		return nil
	}
	f.a.TestSelf(creg, false)
	over := f.a.JccPlaceholder(condE)
	if fr.regMerge1 {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveSlots(d-a, base, a)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
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
	labels := make([]uint32, n)
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
			f.moveSlots(d-fr.branchN, fr.height, fr.branchN)
		}
		f.branchJump(fr)
	}
	if len(labels) >= brTableJumpMin {
		// Jump table (P7): bounds-check the index, then one indirect jump through
		// a table of stub offsets — O(1) dispatch instead of a cmp/jne chain.
		// RAX/RDX are free after the flush; case stubs are deduplicated per label.
		f.a.AluRI(cmpDigit, ireg, int32(len(labels)), false)
		defSite := f.a.JccPlaceholder(condAE) // idx >= n → default
		leaSite := f.a.LeaRipPlaceholder(RAX) // RAX = &table
		f.a.ShiftImm(4, ireg, 2, false)       // idx *= 4 (u32 entries)
		f.a.LoadIdx(RDX, RAX, ireg, 0, 4, true, true)
		f.a.AluRR(0x01, RDX, RAX, true) // target = table base + entry
		f.a.JmpReg(RDX)
		tablePos := f.a.Len()
		f.a.PatchRel32(leaSite, tablePos)
		for range labels {
			f.a.B = append(f.a.B, 0, 0, 0, 0) // placeholder entries
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
	if f.singleRegResult {
		f.placeSingleResult() // result straight to RAX/XMM0; epilogue does not reload
		f.retSites = append(f.retSites, f.a.JmpPlaceholder())
		f.unreachable = true
		return nil
	}
	fr := &f.ctrl[0]
	a, d := fr.resultN, f.depth()
	f.flush()
	f.moveSlots(d-a, 0, a)
	f.retSites = append(f.retSites, f.a.JmpPlaceholder())
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
	case op >= 0x20 && op <= 0x24: // local.*/global.*
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
	case op == 0xfc: // misc prefix: sub-opcode + its own immediates
		sub, err := r.U32()
		if err != nil {
			return err
		}
		switch sub {
		case 8: // memory.init: dataidx + memidx
			if _, err := r.U32(); err != nil {
				return err
			}
			_, err = r.U32()
			return err
		case 9, 13: // data.drop / elem.drop: one index
			_, err := r.U32()
			return err
		case 10: // memory.copy: two memidx
			if _, err := r.U32(); err != nil {
				return err
			}
			_, err = r.U32()
			return err
		case 11: // memory.fill: memidx
			_, err := r.U32()
			return err
		}
		return nil
	}
	return nil
}

// brTableJumpMin is the label count at which br_table switches from a linear
// cmp/jne chain to an indirect jump table.
const brTableJumpMin = 5
