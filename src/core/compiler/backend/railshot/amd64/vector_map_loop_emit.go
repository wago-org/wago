//go:build amd64

package amd64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
	"os"
)

// Bounded literal hoisting is enabled by default; zero permits diagnostic A/B.
var regionConstantHoistEnabled = os.Getenv("WAGO_AMD64_LOOP_CONSTANT_HOIST") != "0"

// Qualified bounded invariant prefixes are enabled by default; zero permits diagnostic A/B.
var regionInvariantPrefixEnabled = os.Getenv("WAGO_AMD64_REGION_INVARIANT_PREFIX") != "0"

// Exact adjacent outputs are the qualified default; zero permits diagnostic A/B.
var regionAdjacentEnabled = os.Getenv("WAGO_AMD64_ADJACENT_LOOP_PAIR") != "0"

var regionLoopMemForms = os.Getenv("WAGO_AMD64_VECTOR_MAP_MEMFORMS") != "0"

// Modular zero-counter loops use guarded trip counting and the original checked fallback.
var regionZeroCounterEnabled = os.Getenv("WAGO_AMD64_ZERO_COUNTER_LOOP") != "0"

// Pairing other whole scalar iterations remains an opt-in experiment.
var regionLoopEnabled = os.Getenv("WAGO_AMD64_VECTOR_MAP_LOOP") == "1"

// Diagnostic assertion for qualification only; ordinary timings leave it off.
var regionLoopTestFast = os.Getenv("WAGO_AMD64_VECTOR_MAP_ASSERT_FAST") == "1"

const regionLoopSlots = 28

type regionEntryLocal struct {
	reg   Reg
	state locState
}
type regionMemoryStream struct {
	address uint8
	offset  uint64
	stride  uint32
	width   uint8
	reg     Reg
	uses    uint8
	parent  uint8
	disp    int32
}
type regionLoopEmitter struct {
	f               *fn
	p               *regionLoopPlan
	gp              [4]Reg
	fp              [10]Reg
	slot            int
	fails           [64]int
	failN           int
	streams         [10]regionMemoryStream
	streamN         int
	loadStream      [regionLoopMaxOps + 1]uint8
	storeStream     [2]uint8
	allCached       bool
	folded          [regionLoopMaxOps + 1]bool
	memoryOperand   [regionLoopMaxOps + 1]uint8
	invariantPrefix uint8
	permanent       [regionLoopMaxOps + 1]bool
}

func (e *regionLoopEmitter) off(i int) int32 { return e.f.spillOff(e.slot + i) }
func (e *regionLoopEmitter) fail(cc Cond)    { e.fails[e.failN] = e.f.a.JccPlaceholder(cc); e.failN++ }
func (e *regionLoopEmitter) affine(id uint8, dst, tmp Reg) {
	n := e.p.nodes[id]
	f := e.f
	f.a.MovImm32(dst, int32(uint32(n.bits)))
	for i, c := range n.coeff {
		if c != 0 {
			f.a.Load32(tmp, RSP, e.off(i))
			if c != 1 {
				f.a.ImulRI(tmp, int32(c), false)
			}
			f.a.Add32(dst, tmp)
		}
	}
}
func (e *regionLoopEmitter) stream(address uint8, offset uint64) uint8 {
	n := e.p.nodes[address]
	for i, s := range e.streams[:e.streamN] {
		a := e.p.nodes[s.address]
		if a.bits == n.bits && a.coeff == n.coeff && s.offset == offset {
			e.streams[i].uses++
			return uint8(i)
		}
	}
	i := e.streamN
	e.streamN++
	width := uint8(8)
	if e.p.adjacent && e.p.stride(address) != 0 {
		width = 16
	}
	e.streams[i] = regionMemoryStream{width: width, address: address, offset: offset, stride: e.p.stride(address), reg: regNone, uses: 1, parent: uint8(i)}
	return uint8(i)
}
func (e *regionLoopEmitter) rangeCheck(s *regionMemoryStream) {
	f := e.f
	a, b, c, d := e.gp[0], e.gp[1], e.gp[2], e.gp[3]
	e.affine(s.address, a, d)
	f.a.Load32(b, RSP, e.off(8))
	f.a.AluRI(aluTable[opSub].digit, b, 1, true)
	if s.stride == 0 {
		f.a.Xor32(b, b)
	} else if s.stride != 1 {
		f.a.ImulRI(b, int32(s.stride), true)
	}
	f.a.AluRI(aluTable[opAdd].digit, b, int32(s.width), true)
	f.a.MovReg64(c, a)
	f.a.Add64(c, b)
	f.a.MovImm64(d, 1<<32)
	f.a.Cmp64(c, d)
	e.fail(condA)
	if s.offset != 0 {
		f.a.MovImm64(d, s.offset)
		f.a.Add64(a, d)
		f.a.Add64(c, d)
	}
	f.a.AluRM(cmpRMcode, c, RBX, -bdCurBytes, true)
	e.fail(condA)
}
func (e *regionLoopEmitter) guards() {
	f, p := e.f, e.p
	a, b := e.gp[0], e.gp[1]
	for i, l := range p.locals[:p.localN] {
		d := f.locals[l.index]
		switch {
		case d.state == lsConstZero:
			f.a.Xor32(a, a)
		case d.reg != regNone && d.state != lsMem:
			if d.isFloat {
				f.a.MovXmmToGpr(a, d.reg, true)
			} else {
				f.a.MovRegReg32(a, d.reg)
			}
		default:
			if l.typ == mtF64 {
				f.a.Load64(a, RSP, f.localAddr(int(l.index)))
			} else {
				f.a.Load32(a, RSP, f.localAddr(int(l.index)))
			}
		}
		f.a.Store64(RSP, e.off(i), a)
	}
	e.affine(p.limit, a, b)
	f.a.Load32(b, RSP, e.off(int(p.counter)))
	if p.zeroTerminated {
		// finish proves the limit is literal zero and the positive step is a
		// power of two. Modular distance to zero gives the first terminating
		// iteration iff it is nonzero and divisible by that step. Zero entry
		// remains in the original do-while loop (it may execute 2^32 steps).
		f.a.TestSelf(b, false)
		e.fail(condE)
	} else {
		f.a.Cmp32(a, b)
		e.fail(condBE)
	}
	f.a.Sub32(a, b)
	step := p.locals[p.counter].step
	if step != 1 {
		f.a.TestImm(a, step-1, false)
		e.fail(condNE)
		f.a.ShiftImm(5, a, uint8(bits.TrailingZeros32(step)), false)
	}
	// Pair only complete iterations. Odd or zero/negative trip counts use the
	// original checked loop, including its original trap and exit semantics.
	if !p.adjacent && !p.scalar {
		f.a.TestImm(a, 1, false)
		e.fail(condNE)
	}
	f.a.Store64(RSP, e.off(8), a)
	for _, l := range p.loads[:p.loadN] {
		n := p.nodes[l.node]
		e.loadStream[l.node] = e.stream(n.left, n.bits)
	}
	for i, s := range p.stores[:p.storeN] {
		e.storeStream[i] = e.stream(s.address, s.offset)
	}
	for i := range e.streams[:e.streamN] {
		e.rangeCheck(&e.streams[i])
		f.a.Store64(RSP, e.off(9+i), a)
	}
}

// Equal affine streams with a constant separation can use one pointer. The
// runtime equality check makes modulo-i32 wrap explicit: range checks alone
// do not prove that two separately wrapped addresses have this relationship.
func (e *regionLoopEmitter) coalesceStreams() {
	for i := 1; i < e.streamN; i++ {
		s := &e.streams[i]
		a := e.p.nodes[s.address]
		for j := 0; j < i; j++ {
			b := e.streams[j]
			if b.parent != uint8(j) || b.stride != s.stride {
				continue
			}
			n := e.p.nodes[b.address]
			if a.coeff != n.coeff {
				continue
			}
			delta := int64(int32(uint32(a.bits)-uint32(n.bits))) + int64(s.offset) - int64(b.offset)
			if delta < -1<<31 || delta > 1<<31-1 {
				continue
			}
			x, y := e.gp[0], e.gp[1]
			e.f.a.Load64(x, RSP, e.off(9+i))
			e.f.a.Load64(y, RSP, e.off(9+j))
			if delta != 0 {
				e.f.a.AluRI(aluTable[opAdd].digit, y, int32(delta), true)
			}
			e.f.a.Cmp64(x, y)
			e.fail(condNE)
			s.parent, s.disp = uint8(j), int32(delta)
			e.streams[j].uses += s.uses
			break
		}
	}
}

// Count scalar temporaries in the actual source event order. Initial FP locals
// have dedicated loop homes; all final versions remain live until the commit.
func (p *regionLoopPlan) fpUses() [regionLoopMaxOps + 1]uint8 {
	var uses [regionLoopMaxOps + 1]uint8
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 {
			uses[p.stores[event&0x7f].value]++
		} else {
			n := p.nodes[event]
			if n.op >= 0xa0 && n.op <= 0xa3 {
				uses[n.left]++
				uses[n.right]++
			}
		}
	}
	for _, l := range p.locals[:p.localN] {
		if l.written && l.typ == mtF64 {
			uses[l.value]++
		}
	}
	return uses
}

// A load may be delayed to its sole arithmetic consumer only across pure FP
// events. Moving an invariant scalar load into a packed operand would read the
// wrong second lane, so only moving streams are eligible. All range and alias
// guards remain mandatory. Source order is retained; left operands never commute.
func (p *regionLoopPlan) memoryForms(enabled bool) (folded [regionLoopMaxOps + 1]bool, memory [regionLoopMaxOps + 1]uint8) {
	if !enabled {
		return
	}
	uses := p.fpUses()
	var pending [regionLoopMaxOps + 1]bool
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 {
			clear(pending[:])
			continue
		}
		n := p.nodes[event]
		if n.op == 0x2b {
			pending[event] = p.stride(n.left) != 0
			continue
		}
		if n.op >= 0xa0 && n.op <= 0xa3 && pending[n.right] && uses[n.right] == 1 {
			folded[n.right] = true
			memory[event] = n.right
		}
	}
	return
}

func (p *regionLoopPlan) scratchNeed() int {
	return p.scratchNeedFolded([regionLoopMaxOps + 1]bool{})
}
func (p *regionLoopPlan) scratchNeedFolded(folded [regionLoopMaxOps + 1]bool) int {
	return p.scratchNeedPermanent(folded, [regionLoopMaxOps + 1]bool{})
}
func (p *regionLoopPlan) scratchNeedPermanent(folded [regionLoopMaxOps + 1]bool, permanent [regionLoopMaxOps + 1]bool) int {
	uses := p.fpUses()
	homes, live, peak := 0, 0, 0
	for _, l := range p.locals[:p.localN] {
		if l.typ == mtF64 && uses[l.initial] != 0 {
			homes++
		}
	}
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 {
			id := p.stores[event&0x7f].value
			uses[id]--
			if uses[id] == 0 && p.nodes[id].op != 0x20 && !permanent[id] {
				live--
			}
			continue
		}
		if folded[event] {
			continue
		}
		n := p.nodes[event]
		reuse := n.op >= 0xa0 && n.op <= 0xa3 && uses[n.left] == 1 && p.nodes[n.left].op != 0x20 && !permanent[n.left]
		if !reuse {
			live++
			if live > peak {
				peak = live
			}
		}
		if n.op >= 0xa0 && n.op <= 0xa3 {
			for _, id := range []uint8{n.left, n.right} {
				uses[id]--
				if uses[id] == 0 && p.nodes[id].op != 0x20 && !permanent[id] && !folded[id] && !(reuse && id == n.left) {
					live--
				}
			}
		}
		if uses[event] == 0 {
			live--
		}
	}
	return homes + peak
}
func (e *regionLoopEmitter) prepareRegisterStreams(entry *[256]regionEntryLocal) {
	f, p := e.f, e.p
	var occupied regMask
	for _, r := range e.gp {
		occupied = occupied.add(r)
	}
	occupied = occupied.union(f.pinned).union(f.reserved)
	// Invariant direct local pointers can reuse their existing canonical i32 pin.
	for i, s := range e.streams[:e.streamN] {
		if s.parent != uint8(i) {
			continue
		}
		n := p.nodes[s.address]
		if s.stride != 0 || s.offset != 0 || n.bits != 0 {
			continue
		}
		for at, c := range n.coeff {
			if c != 1 || n.uses != 1<<at || p.locals[at].written {
				continue
			}
			l := p.locals[at]
			old := entry[l.index]
			d := f.locals[l.index]
			if old.reg != regNone && old.reg == d.reg && !occupied.has(old.reg) && f.regUser[old.reg] == nil {
				e.streams[i].reg = old.reg
				occupied = occupied.add(old.reg)
			}
		}
	}
	// Use otherwise free registers, then pins whose values the loop replaces.
	for pass := 0; pass < 2; pass++ {
		for _, r := range gpAlloc {
			if occupied.has(r) || f.regUser[r] != nil {
				continue
			}
			allowed := !f.pinnedLocalMask.has(r)
			if pass == 1 {
				for _, l := range p.locals[:p.localN] {
					if l.written && l.typ == mtI32 && entry[l.index].reg == r {
						allowed = true
					}
				}
			}
			if !allowed {
				continue
			}
			at := -1
			for i, s := range e.streams[:e.streamN] {
				if s.parent == uint8(i) && s.reg == regNone && (at < 0 || s.uses > e.streams[at].uses) {
					at = i
				}
			}
			if at < 0 {
				break
			}
			e.streams[at].reg = r
			occupied = occupied.add(r)
		}
	}
	// Guard temporaries have no hot-path owner when every address can stay in
	// a register. Reuse them only as an all-or-nothing contract: uncached
	// streams still require gp[0], gp[2], and gp[3] for indexed reconstruction.
	missing := 0
	for i, s := range e.streams[:e.streamN] {
		if s.parent == uint8(i) && s.reg == regNone {
			missing++
		}
	}
	if missing <= 3 {
		available := [3]Reg{e.gp[2], e.gp[3], e.gp[0]}
		at := 0
		for i := range e.streams[:e.streamN] {
			if e.streams[i].parent == uint8(i) && e.streams[i].reg == regNone {
				e.streams[i].reg = available[at]
				at++
			}
		}
		e.allCached = true
	}

}
func (e *regionLoopEmitter) address(at uint8) Reg {
	s := e.streams[e.streams[at].parent]
	if s.reg != regNone {
		return s.reg
	}
	f := e.f
	out, tmp := e.gp[2], e.gp[3]
	f.a.Load64(out, RSP, e.off(9+int(at)))
	if s.stride != 0 {
		f.a.MovReg64(tmp, e.gp[0])
		if s.stride != 1 {
			f.a.ImulRI(tmp, int32(s.stride), true)
		}
		f.a.Add64(out, tmp)
	}
	return out
}
func (e *regionLoopEmitter) body() {
	f, p := e.f, e.p
	uses := p.fpUses()
	initialUses := uses
	// Guards prove every load safe on this path. A single-use load may remain
	// a memory operand across pure FP events, but never across a store. Only
	// right-hand moving loads can fold; operand order is never exchanged.
	folded, memoryOperand := e.folded, e.memoryOperand
	// AVX packed memory forms and explicit loads retain the guarded pair width.

	var regs [regionLoopMaxOps + 1]Reg
	var scratch regMask
	homes := 0
	for _, l := range p.locals[:p.localN] {
		if l.typ == mtF64 && uses[l.initial] != 0 {
			regs[l.initial] = e.fp[homes]
			f.a.FLoadDisp(regs[l.initial], RSP, e.off(int(p.nodes[l.initial].bits)), true)
			if !p.scalar {
				f.a.SseRR(0x66, 0x14, regs[l.initial], regs[l.initial], false)
			}
			homes++
		}
	}
	for _, r := range e.fp[homes:p.scratchNeedPermanent(folded, e.permanent)] {
		scratch = scratch.add(r)
	}
	allocate := func() Reg {
		for r := Reg(0); r < 16; r++ {
			if scratch.has(r) {
				scratch = scratch.remove(r)
				return r
			}
		}
		panic("region FP demand underestimated")
	}
	release := func(id uint8, keep Reg) {
		uses[id]--
		if uses[id] == 0 && p.nodes[id].op != 0x20 && !e.permanent[id] && regs[id] != keep {
			scratch = scratch.add(regs[id])
		}
	}
	for i, s := range e.streams[:e.streamN] {
		if s.parent == uint8(i) && s.reg != regNone {
			f.a.Load64(s.reg, RSP, e.off(9+i))
		}
	}
	if !e.allCached {
		f.a.Xor32(e.gp[0], e.gp[0])
	}
	f.a.Load32(e.gp[1], RSP, e.off(8))
	if !p.adjacent && !p.scalar {
		f.a.ShiftImm(5, e.gp[1], 1, false)
	}
	top := 0
	oldPC := f.wasmPC
	for at, event := range p.events[:p.eventN] {
		if at == int(e.invariantPrefix) {
			f.a.AlignLoop()
			top = f.a.Len()
		}
		if event&0x80 != 0 {
			at := event & 0x7f
			s := p.stores[at]
			f.wasmPC = f.tracePCBase + s.pos
			previous := f.enterProfileInstruction()
			var ea Reg
			if !p.scalar || p.reductionLoad[at] == 0 {
				ea = e.address(e.storeStream[at])
			}
			if p.scalar {
				if id := p.reductionLoad[at]; id != 0 {
					f.a.FMov(regs[id], regs[s.value], true)
				} else {
					f.a.FStoreIdx(RBX, ea, regs[s.value], e.streams[e.storeStream[at]].disp, true)
				}
			} else {
				f.mov128StoreIdx(RBX, ea, regs[s.value], e.streams[e.storeStream[at]].disp)
			}
			release(s.value, regNone)
			f.switchProfileOrigin(previous)
			continue
		}
		n := p.nodes[event]
		if folded[event] {
			continue
		}
		f.wasmPC = f.tracePCBase + n.pos
		previous := f.enterProfileInstruction()
		switch n.op {
		case 0x44:
			regs[event] = allocate()
			var raw [8]byte
			binary.LittleEndian.PutUint64(raw[:], n.bits)
			site := f.a.MovsRipPlaceholder(regs[event], true)
			f.recordConst(raw[:], site)
			if !p.scalar {
				f.a.SseRR(0x66, 0x14, regs[event], regs[event], false)
			}
		case 0x2b:
			regs[event] = allocate()
			ea := e.address(e.loadStream[event])
			if p.scalar || p.stride(n.left) == 0 {
				f.a.FLoadIdx(regs[event], RBX, ea, e.streams[e.loadStream[event]].disp, true)
				if !p.scalar {
					f.a.SseRR(0x66, 0x14, regs[event], regs[event], false)
				}
			} else {
				f.mov128LoadIdx(regs[event], RBX, ea, e.streams[e.loadStream[event]].disp)
			}
		default:
			source := n.left
			memory := memoryOperand[event]
			out := regNone
			if uses[source] == 1 && p.nodes[source].op != 0x20 && !e.permanent[source] {
				out = regs[source]
			} else {
				out = allocate()
			}
			var opcode byte
			switch n.op {
			case 0xa0:
				opcode = 0x58
			case 0xa1:
				opcode = 0x5c
			case 0xa2:
				opcode = 0x59
			case 0xa3:
				opcode = 0x5e
			default:
				panic("invalid region arithmetic")
			}
			if memory != 0 {
				ea := e.address(e.loadStream[memory])
				f.a.VFPackedMemIdx(opcode, out, regs[source], RBX, ea, e.streams[e.loadStream[memory]].disp, true)
				f.stats.peep("region-loop-fold-load")
				uses[memory]--
				release(source, out)
			} else {
				if out != regs[n.left] {
					if p.scalar {
						f.a.FMov(out, regs[n.left], true)
					} else {
						f.mov128(out, regs[n.left])
					}
				}
				prefix := byte(0x66)
				if p.scalar {
					prefix = 0xf2
				}
				f.a.SseRR(prefix, opcode, out, regs[n.right], false)
				release(n.right, out)
				release(n.left, out)
			}
			regs[event] = out
		}
		f.switchProfileOrigin(previous)
		if uses[event] == 0 {
			scratch = scratch.add(regs[event])
		}
	}
	// Arithmetic/constant final versions own registers distinct from the initial
	// loop homes. Keep the recurrence entirely in XMM registers.
	for _, l := range p.locals[:p.localN] {
		if l.written && l.typ == mtF64 && initialUses[l.initial] != 0 {
			f.a.FMov(regs[l.initial], regs[l.value], true)
		}
	}
	for i, s := range e.streams[:e.streamN] {
		if s.parent == uint8(i) && s.reg != regNone && s.stride != 0 {
			f.a.AluRI(aluTable[opAdd].digit, s.reg, int32(p.iterationsPerVector()*s.stride), true)
		}
	}
	if !e.allCached {
		f.a.AluRI(aluTable[opAdd].digit, e.gp[0], int32(p.iterationsPerVector()), true)
	}
	f.a.AluRI(aluTable[opSub].digit, e.gp[1], 1, false)
	again := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(again, top)
	// Publish the final invariant cells before exit-register reconciliation.
	if p.scalar {
		for i, id := range p.reductionLoad {
			if id != 0 {
				ea := e.address(e.storeStream[i])
				f.a.FStoreIdx(RBX, ea, regs[id], e.streams[e.storeStream[i]].disp, true)
			}
		}
	}
	// Only the final iteration publishes private outputs for exit reconciliation.
	for i, l := range p.locals[:p.localN] {
		if l.written && l.typ == mtF64 {
			if p.adjacent {
				// Use the final two scratch slots to publish either lane without
				// mutating a packed value shared by multiple scalar locals.
				f.mov128StoreDisp(RSP, e.off(26), regs[l.value])
				lane := int32(0)
				if p.exitHigh&(1<<i) != 0 {
					lane = 8
				}
				f.a.Load64(e.gp[0], RSP, e.off(26)+lane)
				f.a.Store64(RSP, e.off(19+i), e.gp[0])
			} else {
				// The last scalar iteration is the upper lane of the last pair.
				if !p.scalar {
					f.a.SseRR(0, 0x12, regs[l.value], regs[l.value], false)
				}
				f.a.FStoreDisp(RSP, e.off(19+i), regs[l.value], true)
			}
		}
	}
	// Integer definitions are affine in the input image of the final iteration.
	for i, l := range p.locals[:p.localN] {
		if l.step != 0 {
			f.a.Load32(e.gp[0], RSP, e.off(i))
			f.a.Load32(e.gp[1], RSP, e.off(8))
			f.a.AluRI(aluTable[opSub].digit, e.gp[1], 1, false)
			if l.step != 1 {
				f.a.ImulRI(e.gp[1], int32(l.step), false)
			}
			f.a.Add32(e.gp[0], e.gp[1])
			f.a.Store32(RSP, e.off(i), e.gp[0])
		}
	}
	for i, l := range p.locals[:p.localN] {
		if l.written && l.typ == mtI32 {
			e.affine(l.value, e.gp[0], e.gp[1])
			f.a.Store64(RSP, e.off(19+i), e.gp[0])
		}
	}
	for order := 1; order <= regionLoopMaxOps; order++ {
		for i, l := range p.locals[:p.localN] {
			if !l.written || int(l.order) != order {
				continue
			}
			d := f.locals[l.index]
			if d.state == lsConstZero {
				continue
			}
			if l.typ == mtF64 {
				f.a.FLoadDisp(e.fp[0], RSP, e.off(19+i), true)
				if d.reg == regNone || d.state != lsReg {
					f.a.FStoreDisp(RSP, f.localAddr(int(l.index)), e.fp[0], true)
				}
				if d.reg != regNone && d.state != lsMem {
					f.a.FMov(d.reg, e.fp[0], true)
				}
			} else {
				f.a.Load32(e.gp[0], RSP, e.off(19+i))
				if d.reg == regNone || d.state != lsReg {
					f.a.Store32(RSP, f.localAddr(int(l.index)), e.gp[0])
				}
				if d.reg != regNone && d.state != lsMem {
					f.a.MovRegReg32(d.reg, e.gp[0])
				}
			}
		}
	}
	f.wasmPC = oldPC
}

func (f *fn) tryRegionLoop(r *wasm.Reader) (bool, error) {
	if f.unreachable || f.depth() > 4 || f.moduleEH || f.localBase != 0 || f.interruptible || f.threadedMemory0 || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.nLocals > 256 || f.m.MemCount() == 0 || f.memoryAddr64(0) {
		return false, nil
	}
	look := *r
	bt, err := look.Byte()
	if err != nil || bt != 0x40 {
		return false, nil
	}
	var p regionLoopPlan
	if !inspectRegionLoop(look, f.localType, f.classifier, &p) {
		return false, nil
	}
	if p.independentLanes() {
		if !regionLoopEnabled && !(regionZeroCounterEnabled && p.zeroTerminated) {
			return false, nil
		}
	} else if !regionAdjacentEnabled || !p.packAdjacentOutputs() {
		if !scalarMemoryRecurrenceEnabled || !p.scalarMemoryRecurrence() {
			return false, nil
		}
	}
	// Scalar home commits below require every new FP version to own its result.
	// Cross-home copies need a separate parallel-copy contract.
	for _, l := range p.locals[:p.localN] {
		if l.written && l.typ == mtF64 && p.nodes[l.value].op == 0x20 && l.value != l.initial {
			return false, nil
		}
	}
	step := p.locals[p.counter].step
	memoryForms := !p.scalar && regionLoopMemForms && f.cpuHas(shared.AMD64AVX)
	var prefix uint8
	var permanent [regionLoopMaxOps + 1]bool
	constantPrefix := false
	if p.scalar {
		prefix, permanent = p.prepareMemoryRecurrence()
	} else {
		prefix, permanent = p.hoistConstants(regionConstantHoistEnabled, regionInvariantPrefixEnabled, memoryForms)
		constantPrefix = prefix != 0
		if prefix == 0 {
			prefix, permanent = p.invariantPrefix(regionInvariantPrefixEnabled)
		}
	}
	folded, memory := p.memoryForms(memoryForms)
	need := p.scratchNeedPermanent(folded, permanent)
	if step&(step-1) != 0 || need > 10 {
		return false, nil
	}
	prefixDepth := f.depth()
	depth := len(f.ctrl)
	if err := f.opBlock(r, 0x03); err != nil {
		return true, err
	}
	e := regionLoopEmitter{f: f, p: &p, folded: folded, memoryOperand: memory, invariantPrefix: prefix, permanent: permanent}
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	n := 0
	for _, reg := range gpAlloc {
		if !block.has(reg) && f.regUser[reg] == nil {
			e.gp[n] = reg
			n++
			if n == 4 {
				break
			}
		}
	}
	if n != 4 {
		f.stats.peep("region-loop-no-gp")
		return true, nil
	}
	entryF := f.fpinned.union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	var entry [256]regionEntryLocal
	for i := 0; i < f.nLocals; i++ {
		entry[i] = regionEntryLocal{f.locals[i].reg, f.locals[i].state}
	}
	reserved, pinned := f.reserved, f.pinned
	memSize, memLease := f.memSizeReg, f.memSizeRegionalLease
	e.slot = f.allocSpillSlots(regionLoopSlots)
	e.guards()
	e.aliasGuards()
	e.coalesceStreams()
	toFast := f.a.JmpPlaceholder()
	if f.compactLoopAlign32 {
		f.a.AlignLoop32()
	} else {
		f.a.AlignLoop()
	}
	fallback := f.a.Len()
	for _, site := range e.fails[:e.failN] {
		f.a.PatchRel32(site, fallback)
	}
	f.ctrl[len(f.ctrl)-1].controlSite = fallback
	if regionLoopTestFast {
		f.trapAlways(trapUnreachable)
	}
	if err := f.bodyLoop(r, depth); err != nil {
		return true, err
	}
	toDone := f.a.JmpPlaceholder()
	f.a.PatchRel32(toFast, f.a.Len())
	valid := !f.unreachable && f.depth() == prefixDepth && f.s.canonicalSlots && f.reserved == reserved && f.pinned == pinned && f.memSizeReg == memSize && f.memSizeRegionalLease == memLease
	for _, reg := range e.gp {
		if f.regUser[reg] != nil || f.pinnedLocalMask.has(reg) {
			valid = false
		}
	}
	mask := entryF.union(f.fpinned).union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	n = 0
	for reg := Reg(0); reg < 16; reg++ {
		if !mask.has(reg) && f.fregUser[reg] == nil && n < need {
			e.fp[n] = reg
			n++
		}
	}
	if n != need {
		valid = false
	}
	var written [256]bool
	for _, l := range p.locals[:p.localN] {
		if l.written {
			written[l.index] = true
		}
	}
	for _, x := range f.pinnedLocals {
		if !written[x] && entry[x].reg != regNone && entry[x].reg != f.locals[x].reg {
			valid = false
		}
	}
	if valid {
		e.prepareRegisterStreams(&entry)
		if e.allCached {
			f.stats.peep("region-loop-all-addresses-cached")
		}
		// Loop-pin exchange can restore an old, unmodified owner on exit. Its
		// home was established by the common entry. Other unchanged pins remain
		// in their original registers, which the scalar path never borrows.
		for _, x := range f.pinnedLocals {
			if written[x] {
				continue
			}
			d, old := f.locals[x], entry[x]
			if old.state == lsConstZero {
				if d.isFloat {
					f.a.SseRR(0x66, 0x57, d.reg, d.reg, false)
				} else {
					f.a.Xor32(d.reg, d.reg)
				}
			} else if old.reg == regNone || old.state == lsMem {
				if d.isFloat {
					f.a.FLoadDisp(d.reg, RSP, f.localAddr(x), f.localType[x] == mtF64)
				} else {
					f.loadFrameInt(d.reg, f.localAddr(x), f.localType[x])
				}
			}
			if (d.state == lsMem || d.state == lsStackReg) && (old.state == lsReg || old.state == lsConstZero) {
				f.storeLocalReg(x, d.reg, d.isFloat)
			}
		}
		e.body()
		if prefix != 0 {
			if constantPrefix {
				f.stats.peep("region-loop-constant-hoist")
			}
			f.stats.peep("region-loop-invariant-prefix")
		}
		if p.scalar {
			f.stats.peep("region-loop-scalar-memory-recurrence")
		}
		f.stats.peep("region-loop-fast")
	} else {
		f.a.JmpBack(fallback)
		f.stats.peep("region-loop-exit-reject")
	}
	f.a.PatchRel32(toDone, f.a.Len())
	return true, nil
}

// The two scalar iterations may be grouped only when each destination range
// is disjoint from other streams, or represents the exact same moving stream.
// A read-only invariant must be disjoint: broadcasting it cannot observe a
// preceding iteration's write. All ranges were proved in-bounds first.
func (e *regionLoopEmitter) aliasGuards() {
	f, p := e.f, e.p
	a, b, c, d := e.gp[0], e.gp[1], e.gp[2], e.gp[3]
	for _, destination := range e.storeStream[:p.storeN] {
		s := e.streams[destination]
		for i, t := range e.streams[:e.streamN] {
			if i == int(destination) {
				continue
			}
			f.a.Load64(a, RSP, e.off(9+int(destination)))
			f.a.Load64(c, RSP, e.off(9+i))
			equal := -1
			if t.stride == s.stride && !(p.scalar && s.stride == 0) {
				f.a.Cmp64(a, c)
				equal = f.a.JccPlaceholder(condE)
			}
			f.a.Load32(b, RSP, e.off(8))
			f.a.AluRI(aluTable[opSub].digit, b, 1, true)
			f.a.ImulRI(b, int32(s.stride), true)
			f.a.AluRI(aluTable[opAdd].digit, b, int32(s.width), true)
			f.a.Add64(b, a)
			f.a.Cmp64(b, c)
			before := f.a.JccPlaceholder(condBE)
			if t.stride == 0 {
				f.a.MovImm32(d, int32(t.width))
			} else {
				f.a.Load32(d, RSP, e.off(8))
				f.a.AluRI(aluTable[opSub].digit, d, 1, true)
				f.a.ImulRI(d, int32(t.stride), true)
				f.a.AluRI(aluTable[opAdd].digit, d, int32(t.width), true)
			}
			f.a.Add64(d, c)
			f.a.Cmp64(d, a)
			e.fail(condA)
			if equal >= 0 {
				f.a.PatchRel32(equal, f.a.Len())
			}
			f.a.PatchRel32(before, f.a.Len())
		}
	}
}
