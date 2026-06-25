package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Trap codes (must match jit.TrapCode / WARP's vb::TrapCode, which the engine reads).
const (
	trapUnreachable = 1
	trapMemOOB      = 3
	trapIndirectOOB = 5
	trapIndirectSig = 6
	trapDivZero     = 9
	trapDivOverflow = 10
)

type ckKind uint8

const (
	ckFunc ckKind = iota
	ckBlock
	ckLoop
	ckIf
)

// Branch targets use canonical slots so all edges into a join agree on state.
type cframe struct {
	kind            ckKind
	height          int // operand depth at the result base
	paramN, resultN int
	branchN         int   // values transferred on a branch to this label
	loopStart       int   // ckLoop: backward target PC
	ends            []int // ckBlock/ckIf: forward jmp sites to patch to end
	elseSite        int   // ckIf: the jz site (to else/end), -1 once patched
	hasElse         bool
	entryUnreach    bool
	endReachable    bool
}

func isValByte(b byte) bool {
	switch b {
	case 0x7F, 0x7E, 0x7D, 0x7C, 0x7B, 0x70, 0x6F:
		return true
	}
	return false
}

func (g *cg) blockType(r *wasm.Reader) (pN, rN int, err error) {
	b, ok := r.Peek()
	if !ok {
		return 0, 0, fmt.Errorf("eof in blocktype")
	}
	if b == 0x40 {
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
	if x < 0 || int(x) >= len(g.m.Types) {
		return 0, 0, fmt.Errorf("bad blocktype index %d", x)
	}
	ft := &g.m.Types[x]
	return len(ft.Params), len(ft.Results), nil
}

func skipBlockType(r *wasm.Reader) error {
	b, ok := r.Peek()
	if !ok {
		return fmt.Errorf("eof in blocktype")
	}
	if b == 0x40 || isValByte(b) {
		_, _ = r.Byte()
		return nil
	}
	_, e := r.I64()
	return e
}

// flush materializes the operand stack into canonical slots.
func (g *cg) flush() {
	for i := range g.st {
		e := g.st[i]
		switch e.kind {
		case vReg:
			if e.fp {
				g.a.FStoreDisp(RBP, g.slotOff(i), e.reg, true)
			} else {
				g.a.Store64(RBP, g.slotOff(i), e.reg)
			}
		case vConst:
			if e.wide {
				g.a.MovImm64(RSI, uint64(e.cval))
			} else {
				g.a.MovImm32(RSI, int32(e.cval)) // zero-extends RSI
			}
			g.a.Store64(RBP, g.slotOff(i), RSI)
		case vLocal:
			g.a.Load64(RSI, RBP, g.localOff(e.local))
			g.a.Store64(RBP, g.slotOff(i), RSI)
		case vSpill:
			if e.slot != i {
				g.a.Load64(RSI, RBP, g.slotOff(e.slot))
				g.a.Store64(RBP, g.slotOff(i), RSI)
			}
		}
		g.st[i] = ventry{kind: vSpill, slot: i}
	}
	for i := range g.busy {
		g.busy[i] = false
		g.fbusy[i] = false
	}
}

func (g *cg) setDepth(l int) {
	g.st = g.st[:0]
	for i := 0; i < l; i++ {
		g.st = append(g.st, ventry{kind: vSpill, slot: i})
	}
	if l > g.maxDepth {
		g.maxDepth = l
	}
}

func (g *cg) moveSlots(fromBase, toBase, n int) {
	if fromBase == toBase {
		return
	}
	for i := 0; i < n; i++ {
		g.a.Load64(RSI, RBP, g.slotOff(fromBase+i))
		g.a.Store64(RBP, g.slotOff(toBase+i), RSI)
	}
}

// emitTrap returns without clearing the trap slot.
func (g *cg) emitTrap(code uint32) {
	g.a.Load64(RSI, RBP, -24) // trap ptr
	g.a.StoreImm32Mem(RSI, 0, int32(code))
	g.a.Leave()
	g.a.Ret()
}

func (g *cg) branchJump(f *cframe) {
	switch f.kind {
	case ckLoop:
		g.a.JmpBack(f.loopStart)
	case ckFunc:
		g.retSites = append(g.retSites, g.a.JmpPlaceholder())
	default:
		f.ends = append(f.ends, g.a.JmpPlaceholder())
		f.endReachable = true
	}
}

// opBlock starts block/loop/if frames and records their canonical stack height.
func (g *cg) opBlock(r *wasm.Reader, op byte) error {
	pN, rN, err := g.blockType(r)
	if err != nil {
		return err
	}
	kind := ckBlock
	if op == 0x03 {
		kind = ckLoop
	} else if op == 0x04 {
		kind = ckIf
	}
	f := cframe{kind: kind, paramN: pN, resultN: rN, elseSite: -1, entryUnreach: g.unreachable}
	if kind == ckLoop {
		f.branchN = pN
	} else {
		f.branchN = rN
	}
	if g.unreachable {
		g.ctrl = append(g.ctrl, f)
		return nil
	}
	if kind == ckIf {
		cond := g.pop()
		creg := g.materialize(cond)
		f.height = len(g.st) - pN
		g.flush()
		g.a.TestSelf(creg, false)
		f.elseSite = g.a.JccPlaceholder(CondE) // jz else/end
	} else {
		f.height = len(g.st) - pN
		g.flush()
		if kind == ckLoop {
			f.loopStart = g.a.Len()
		}
	}
	g.ctrl = append(g.ctrl, f)
	return nil
}

// opElse closes the then edge, patches the false edge, and opens the else edge.
func (g *cg) opElse() error {
	f := &g.ctrl[len(g.ctrl)-1]
	if f.entryUnreach {
		return nil // whole if is unreachable; else stays unreachable
	}
	if g.unreachable {
		// then-branch diverged; else is reachable (cond-false analogue path).
		g.unreachable = false
	} else {
		// then-branch falls through to end; canonicalize its results and skip else.
		g.flush()
		f.ends = append(f.ends, g.a.JmpPlaceholder())
		f.endReachable = true
	}
	g.a.PatchRel32(f.elseSite, g.a.Len())
	f.elseSite = -1
	f.hasElse = true
	g.setDepth(f.height + f.paramN)
	return nil
}

// opEnd closes the current frame and patches any forward branches to this join.
func (g *cg) opEnd() error {
	f := g.ctrl[len(g.ctrl)-1]
	g.ctrl = g.ctrl[:len(g.ctrl)-1]

	if f.kind == ckFunc {
		if !g.unreachable {
			g.flush() // results land in slots [0, resultN)
		}
		return nil
	}

	fallthroughReachable := !g.unreachable
	if fallthroughReachable {
		g.flush() // results at [height, height+resultN)
	}
	// an if without else: the cond-false path reaches end with params==results.
	if f.kind == ckIf && !f.hasElse && !f.entryUnreach {
		g.a.PatchRel32(f.elseSite, g.a.Len())
		f.endReachable = true
	}
	for _, site := range f.ends {
		g.a.PatchRel32(site, g.a.Len())
	}
	endReachable := fallthroughReachable || f.endReachable
	g.unreachable = !endReachable
	if endReachable {
		g.setDepth(f.height + f.resultN)
	}
	return nil
}

// opBr moves branch values into the target frame slots before jumping.
func (g *cg) opBr(r *wasm.Reader, conditional bool) error {
	if g.unreachable {
		if _, err := r.U32(); err != nil { // label
			return err
		}
		return nil
	}
	var creg Reg
	if conditional {
		cond := g.pop()
		creg = g.materialize(cond)
	}
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(g.ctrl) - 1 - int(idx)
	if fi < 0 {
		return fmt.Errorf("br label out of range")
	}
	f := &g.ctrl[fi]
	a, base, d := f.branchN, f.height, len(g.st)
	g.flush()
	if !conditional {
		g.moveSlots(d-a, base, a)
		g.branchJump(f)
		g.unreachable = true
		return nil
	}
	g.a.TestSelf(creg, false)
	over := g.a.JccPlaceholder(CondE)
	g.moveSlots(d-a, base, a)
	g.branchJump(f)
	g.a.PatchRel32(over, g.a.Len())
	return nil
}

// opBrTable emits a small linear dispatch over labels plus the default target.
func (g *cg) opBrTable(r *wasm.Reader) error {
	if g.unreachable {
		n, err := r.U32()
		if err != nil {
			return err
		}
		for i := uint32(0); i <= n; i++ { // n labels + default
			if _, err := r.U32(); err != nil {
				return err
			}
		}
		return nil
	}
	idxE := g.pop()
	ireg := g.materialize(idxE)
	n, err := r.U32()
	if err != nil {
		return err
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
	d := len(g.st)
	g.flush()
	emitCase := func(labelIdx uint32) {
		f := &g.ctrl[len(g.ctrl)-1-int(labelIdx)]
		g.moveSlots(d-f.branchN, f.height, f.branchN)
		g.branchJump(f)
	}
	for i, lbl := range labels {
		g.a.AluRI(7, ireg, int32(i), false) // cmp ireg, i
		skip := g.a.JccPlaceholder(CondNE)
		emitCase(lbl)
		g.a.PatchRel32(skip, g.a.Len())
	}
	emitCase(def)
	g.unreachable = true
	return nil
}

func (g *cg) opReturn() error {
	if g.unreachable {
		return nil
	}
	f := &g.ctrl[0]
	a, d := f.resultN, len(g.st)
	g.flush()
	g.moveSlots(d-a, 0, a)
	g.retSites = append(g.retSites, g.a.JmpPlaceholder())
	g.unreachable = true
	return nil
}

// skipImmediates advances over dead-code operands without emitting code.
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
		_, err := r.Byte()
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
	case op == 0x1C: // select t
		n, err := r.U32()
		if err != nil {
			return err
		}
		return r.Step(int(n))
	case op == 0xD0: // ref.null
		_, err := r.Byte()
		return err
	case op == 0xD2: // ref.func
		_, err := r.U32()
		return err
	case op == 0xFC:
		sub, err := r.U32()
		if err != nil {
			return err
		}
		switch sub {
		case 8: // memory.init
			if _, err := r.U32(); err != nil {
				return err
			}
			_, err := r.Byte()
			return err
		case 9: // data.drop
			_, err := r.U32()
			return err
		case 10: // memory.copy
			if _, err := r.Byte(); err != nil {
				return err
			}
			_, err := r.Byte()
			return err
		case 11: // memory.fill
			_, err := r.Byte()
			return err
		}
		return nil
	default:
		return nil // no immediates
	}
}
