package x64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Control flow: block / loop / if / else / end / br / br_if / br_table / return /
// unreachable. Ported from WARP's control-flow lowering, but using the canonical-
// slots reconciliation model (the same one backend/amd64 uses against this
// runtime): at every control boundary the operand stack is flushed to position-
// indexed frame slots, so all edges into a join agree on where each value lives.
// This trades WARP's RegisterCopyResolver register-shuffling for a simpler,
// proven scheme; register residency of locals is layered on separately.

var errBadLabel = fmt.Errorf("x64: br label out of range")

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
	roots := f.rootsBottomToTop()
	for i, root := range roots {
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slot == i {
			continue // already canonical
		}
		if root.kind == ekValue && root.st.kind == stLocalReg {
			f.a.Store64(RBP, f.spillOff(i), root.st.reg) // copy pinned local's value; never release
			continue
		}
		if root.kind == ekValue && root.st.typ.isFloat() {
			x := f.materializeF(root)
			f.a.FStoreDisp(RBP, f.spillOff(i), x, true) // 8B store
			f.releaseF(x)
			continue
		}
		r := f.materialize(root)
		f.a.Store64(RBP, f.spillOff(i), r)
		f.release(r)
	}
	f.setDepth(len(roots))
}

// setDepth resets the operand stack model to l canonical slot entries (slots
// 0..l-1) and frees all registers.
func (f *fn) setDepth(l int) {
	f.s.head.prev, f.s.head.next = f.s.head, f.s.head
	for i := 0; i < l; i++ {
		f.s.pushValue(storage{kind: stSlot, typ: mtI64, slot: i})
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
		f.a.Load64(RAX, RBP, f.spillOff(fromBase+i))
		f.a.Store64(RBP, f.spillOff(toBase+i), RAX)
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

// blockType decodes a block's parameter and result counts.
func (f *fn) blockType(r *wasm.Reader) (pN, rN int, err error) {
	b, ok := r.Peek()
	if !ok {
		return 0, 0, fmt.Errorf("eof in blocktype")
	}
	if b == 0x40 { // empty
		_, _ = r.Byte()
		return 0, 0, nil
	}
	if isValByte(b) {
		_, _ = r.Byte()
		return 0, 1, nil
	}
	x, e := r.I64()
	if e != nil {
		return 0, 0, e
	}
	ft, ok := f.m.TypeFunc(uint32(x))
	if x < 0 || !ok {
		return 0, 0, fmt.Errorf("bad blocktype index %d", x)
	}
	return len(ft.Params), len(ft.Results), nil
}

// branchJump emits the jump for a branch that targets frame fr.
func (f *fn) branchJump(fr *ctrlFrame) {
	switch fr.kind {
	case cfLoop:
		f.a.JmpBack(fr.loopStart)
	case cfFunc:
		f.retSites = append(f.retSites, f.a.JmpPlaceholder())
	default:
		fr.ends = append(fr.ends, f.a.JmpPlaceholder())
		fr.endReachable = true
	}
}

// --- control opcodes ---

func (f *fn) opBlock(r *wasm.Reader, op byte) error {
	pN, rN, err := f.blockType(r)
	if err != nil {
		return err
	}
	kind := cfBlock
	if op == 0x03 {
		kind = cfLoop
	} else if op == 0x04 {
		kind = cfIf
	}
	fr := ctrlFrame{kind: kind, paramN: pN, resultN: rN, elseSite: -1, entryUnreach: f.unreachable}
	if kind == cfLoop {
		fr.branchN = pN
	} else {
		fr.branchN = rN
	}
	if f.unreachable {
		f.ctrl = append(f.ctrl, fr)
		return nil
	}
	if kind == cfIf {
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
		f.flush()
		if kind == cfLoop {
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
		f.flush()
		fr.ends = append(fr.ends, f.a.JmpPlaceholder())
		fr.endReachable = true
	}
	f.a.PatchRel32(fr.elseSite, f.a.Len())
	fr.elseSite = -1
	fr.hasElse = true
	f.setDepth(fr.height + fr.paramN)
	return nil
}

func (f *fn) opEnd() error {
	fr := f.ctrl[len(f.ctrl)-1]
	f.ctrl = f.ctrl[:len(f.ctrl)-1]

	if fr.kind == cfFunc {
		if !f.unreachable {
			f.flush() // results land in slots [0, resultN)
		}
		return nil
	}

	fallthroughReachable := !f.unreachable
	if fallthroughReachable {
		f.flush() // results at [height, height+resultN)
	}
	// An if without else: the cond-false path reaches end with params == results.
	if fr.kind == cfIf && !fr.hasElse && !fr.entryUnreach {
		f.a.PatchRel32(fr.elseSite, f.a.Len())
		fr.endReachable = true
	}
	for _, site := range fr.ends {
		f.a.PatchRel32(site, f.a.Len())
	}
	endReachable := fallthroughReachable || fr.endReachable
	f.unreachable = !endReachable
	if endReachable {
		f.setDepth(fr.height + fr.resultN)
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
	// Fuse `<compare> br_if L` into CMP + conditional jump.
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
	a, base, d := fr.branchN, fr.height, f.depth()
	f.flush()
	if !conditional {
		f.moveSlots(d-a, base, a)
		f.branchJump(fr)
		f.unreachable = true
		return nil
	}
	f.a.TestSelf(creg, false)
	over := f.a.JccPlaceholder(condE)
	f.moveSlots(d-a, base, a)
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
	emitCase := func(labelIdx uint32) {
		fr := &f.ctrl[len(f.ctrl)-1-int(labelIdx)]
		f.moveSlots(d-fr.branchN, fr.height, fr.branchN)
		f.branchJump(fr)
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
