//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var integerWordMapEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_WORD_MAP") != "1"

const integerWordMapWorkLimit = 4096

// One flat iteration, with mutable source cells and owned exact-i32 nodes.
// Initially only pure maps without loads are admitted: the full output range
// can be guarded without alias assumptions or speculative trapping arithmetic.
type integerWordMapPlan struct {
	nodes                       [128]shared.LoopExpression
	locals                      [32]shared.LoopLocal[machineType, int]
	stores                      [4]shared.LoopStore[int]
	n, localN, storeN           int
	index, pointer, limit, root int
	bias                        uint32
	offset                      uint64
	used                        [128]bool
	uses                        [128]uint8
	proofWork, proofLimit       int
}

// Ordinary loops avoid the large candidate frame. All failed probes, including
// the screen, are charged to one finite per-function work counter.
func (f *fn) hasIntegerWordMapPrefix(source *wasm.Reader) bool {
	r := *source
	var imm wasm.InstructionImmediate
	for i := 0; i < 24 && f.integerWordMapWork < integerWordMapWorkLimit; i++ {
		f.integerWordMapWork++
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x6c, 0x71, 0x72, 0x73, 0x74, 0x76:
			return true
		case 0x20, 0x21, 0x22, 0x41, 0x6a, 0x6b:
			if f.classifier.ClassifyInto(&r, op, &imm) != nil {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func (f *fn) inspectIntegerWordMap(source *wasm.Reader) (p integerWordMapPlan, ok bool) {
	defer func() {
		f.integerWordMapWork = uint16(min(integerWordMapWorkLimit, int(f.integerWordMapWork)+p.proofWork))
	}()
	r := *source
	start := r.Offset()
	var stack [24]int
	var entryRead [32]bool
	depth := 0
	add := func(op byte, a, b int, bits uint64, pc int) int {
		if p.n == len(p.nodes) {
			return -1
		}
		x := p.n
		p.n++
		p.nodes[x] = shared.LoopExpression{Op: op, Type: wasm.I32, Left: a, Right: b, Bits: bits, PC: uint32(pc)}
		return x
	}
	push := func(x int) bool {
		if x < 0 || depth == len(stack) {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	for ops := 0; ops < 128 && r.Offset()-start < 512 && f.integerWordMapWork < integerWordMapWorkLimit; ops++ {
		f.integerWordMapWork++
		pc := r.Offset()
		op, err := r.Byte()
		if err != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			x, err := r.U32()
			if err != nil || x >= uint32(f.nLocals) || f.localType[x] != mtI32 {
				return p, false
			}
			slot := shared.FindLoopLocal(p.locals[:p.localN], x)
			if slot < 0 {
				if p.localN == len(p.locals) {
					return p, false
				}
				id := add(0x20, int(x), 0, 0, pc)
				if id < 0 {
					return p, false
				}
				slot = p.localN
				p.localN++
				p.locals[slot] = shared.LoopLocal[machineType, int]{Index: x, Type: mtI32, Initial: id, Value: id}
			}
			cell := &p.locals[slot]
			if op == 0x20 {
				if cell.Value == cell.Initial {
					entryRead[slot] = true
				}
				if !push(cell.Value) {
					return p, false
				}
			} else {
				if depth == 0 {
					return p, false
				}
				cell.Assign(stack[depth-1], 0)
				if op == 0x21 {
					depth--
				}
			}
		case 0x41:
			x, err := r.I32()
			if err != nil || !push(add(op, 0, 0, uint64(uint32(x)), pc)) {
				return p, false
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x76, 0x47, 0x49:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			if !push(add(op, stack[depth], stack[depth+1], 0, pc)) {
				return p, false
			}
		case 0x36:
			mem, offset, err := f.readMemArg(&r)
			if err != nil || mem != 0 || offset > 1<<20 || depth < 2 || p.storeN == len(p.stores) {
				return p, false
			}
			depth -= 2
			p.stores[p.storeN] = shared.LoopStore[int]{Address: stack[depth], Value: stack[depth+1], Offset: offset, PC: uint32(pc)}
			p.storeN++
		case 0x0d:
			label, err := r.U32()
			if err != nil || label != 0 || depth != 1 || p.storeN == 0 || p.storeN == 3 {
				return p, false
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b || r.Offset()-start > 512 {
				return p, false
			}
			condition := p.nodes[stack[0]]
			if condition.Op != 0x47 && condition.Op != 0x49 {
				return p, false
			}
			p.proofLimit = integerWordMapWorkLimit - int(f.integerWordMapWork)
			// Match the positive index update on either side of !=, but only
			// on the left of the unsigned ordered comparison.
			for _, cell := range p.locals[:p.localN] {
				if !cell.Written {
					continue
				}
				step, valid := p.increment(cell.Value, cell.Initial)
				if !valid || step != uint32(p.storeN) {
					continue
				}
				limit := condition.Right
				if condition.Left != cell.Value {
					if condition.Op != 0x47 || condition.Right != cell.Value {
						continue
					}
					limit = condition.Left
				}
				p.index, p.limit = int(cell.Index), limit
				if !p.invariantLeaf(limit) {
					continue
				}
				if p.finish(entryRead) {
					return p, true
				}
			}
			return p, false
		default:
			return p, false
		}
	}
	return p, false
}

func (p *integerWordMapPlan) local(index int) *shared.LoopLocal[machineType, int] {
	i := shared.FindLoopLocal(p.locals[:p.localN], uint32(index))
	if i < 0 {
		return nil
	}
	return &p.locals[i]
}

func (p *integerWordMapPlan) increment(x, initial int) (uint32, bool) {
	n := p.nodes[x]
	if n.Op != 0x6a {
		return 0, false
	}
	a, b := n.Left, n.Right
	if a != initial {
		a, b = b, a
	}
	if a != initial || p.nodes[b].Op != 0x41 {
		return 0, false
	}
	return uint32(p.nodes[b].Bits), true
}

func (p *integerWordMapPlan) invariantLeaf(x int) bool {
	n := p.nodes[x]
	return n.Op == 0x41 || n.Op == 0x20 && !p.local(n.Left).Written
}

// Flatten only addition/subtraction in their exact modular i32 domain. There
// is one chosen source-local input; widened memory offsets stay separate.
func (p *integerWordMapPlan) affine(x, local int, visits *int) (coefficient, bias uint32, ok bool) {
	*visits++
	p.proofWork++
	if *visits > 128 || p.proofWork > p.proofLimit {
		return 0, 0, false
	}
	n := p.nodes[x]
	switch n.Op {
	case 0x41:
		return 0, uint32(n.Bits), true
	case 0x20:
		return 1, 0, n.Left == local
	case 0x6a, 0x6b:
		a, b, ok := p.affine(n.Left, local, visits)
		if !ok {
			return 0, 0, false
		}
		c, d, ok := p.affine(n.Right, local, visits)
		if !ok {
			return 0, 0, false
		}
		if n.Op == 0x6b {
			return a - c, b - d, true
		}
		return a + c, b + d, true
	}
	return 0, 0, false
}

func (p *integerWordMapPlan) same(x, y int, delta uint32, visits *int) bool {
	*visits++
	p.proofWork++
	if *visits > 512 || p.proofWork > p.proofLimit {
		return false
	}
	a, b := p.nodes[x], p.nodes[y]
	av, bv := 0, 0
	ac, ab, ao := p.affine(x, p.index, &av)
	bc, bb, bo := p.affine(y, p.index, &bv)
	if ao && bo {
		return ac == bc && ab+ac*delta == bb
	}
	if a.Op != b.Op {
		return false
	}
	if a.Op == 0x20 {
		return a.Left == b.Left
	}
	if a.Op == 0x41 {
		return a.Bits == b.Bits
	}
	return p.same(a.Left, b.Left, delta, visits) && p.same(a.Right, b.Right, delta, visits)
}

func (p *integerWordMapPlan) mark(x int, depth int) bool {
	p.proofWork++
	if depth > 12 || p.proofWork > p.proofLimit {
		return false
	}
	if p.used[x] {
		return true
	}
	p.used[x] = true
	n := p.nodes[x]
	switch n.Op {
	case 0x41:
		return true
	case 0x20:
		return n.Left == p.index || !p.local(n.Left).Written
	case 0x74, 0x76:
		if p.nodes[n.Right].Op != 0x41 {
			return false
		}
		p.uses[n.Left]++
		return p.mark(n.Left, depth+1)
	case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73:
		p.uses[n.Left]++
		p.uses[n.Right]++
		return p.mark(n.Left, depth+1) && p.mark(n.Right, depth+1)
	}
	return false
}

func (p *integerWordMapPlan) finish(entryRead [32]bool) bool {
	for _, cell := range p.locals[:p.localN] {
		if !cell.Written || int(cell.Index) == p.index {
			continue
		}
		step, ok := p.increment(cell.Value, cell.Initial)
		if !ok || step != uint32(4*p.storeN) {
			continue
		}
		p.pointer = int(cell.Index)
		bias := uint32(0xffffffff)
		var offsets [4]uint32
		valid := true
		for i, s := range p.stores[:p.storeN] {
			visits := 0
			c, b, ok := p.affine(s.Address, p.pointer, &visits)
			if !ok || c != 1 || b > 1<<20 || s.Offset != p.stores[0].Offset {
				valid = false
				break
			}
			offsets[i] = b
			if b < bias {
				bias = b
				p.root = s.Value
			}
		}
		if !valid {
			continue
		}
		var lanes uint8
		for i, s := range p.stores[:p.storeN] {
			d := offsets[i] - bias
			visits := 0
			if d%4 != 0 || d/4 >= uint32(p.storeN) || lanes&(1<<(d/4)) != 0 || !p.same(p.root, s.Value, d/4, &visits) {
				valid = false
				break
			}
			lanes |= 1 << (d / 4)
		}
		if !valid {
			continue
		}
		for i, other := range p.locals[:p.localN] {
			if other.Written && entryRead[i] && int(other.Index) != p.index && int(other.Index) != p.pointer {
				valid = false
			}
		}
		if !valid {
			continue
		}
		p.used = [128]bool{}
		p.uses = [128]uint8{}
		if !p.mark(p.root, 0) {
			continue
		}
		p.uses[p.root]++
		p.bias, p.offset = bias, p.stores[0].Offset
		return true
	}
	return false
}

func (f *fn) emitIntegerWordMap(source *wasm.Reader) {
	if !integerWordMapEnabled || !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.memoryAddr64(0) || f.threadedMemory0 || !f.hasIntegerWordMapPrefix(source) {
		return
	}
	f.emitIntegerWordMapBody(source)
}

func (f *fn) emitIntegerWordMapBody(source *wasm.Reader) {
	p, ok := f.inspectIntegerWordMap(source)
	if !ok {
		return
	}
	f.emitIntegerWordMapPlan(&p)
}

func (f *fn) emitIntegerWordMapPlan(p *integerWordMapPlan) {
	// Simulate the fixed topological schedule before leasing native scratch.
	uses := p.uses
	live, peak, constants := 0, 0, 0
	for i, n := range p.nodes[:p.n] {
		if !p.used[i] {
			continue
		}
		if p.invariantLeaf(i) {
			constants++
			continue
		}
		live++
		if live > peak {
			peak = live
		}
		if n.Op != 0x20 {
			uses[n.Left]--
			if uses[n.Left] == 0 && !p.invariantLeaf(n.Left) {
				live--
			}
			if n.Op != 0x74 && n.Op != 0x76 {
				uses[n.Right]--
				if uses[n.Right] == 0 && !p.invariantLeaf(n.Right) {
					live--
				}
			}
		}
	}
	if constants > 8 {
		return
	}
	available := 0
	blocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !blocked.has(r) && f.fregUser[r] == nil {
			available++
		}
	}
	if available < constants+peak+1 {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	var gp [4]Reg
	for i := range gp {
		gp[i] = regNone
	}
	defer func() {
		f.pinned, f.fpinned = oldPinned, oldFPinned
		for _, r := range gp {
			if r != regNone {
				f.release(r)
			}
		}
	}()
	for i := range gp {
		gp[i] = f.allocRegOrNone(0)
		if gp[i] == regNone {
			return
		}
		f.pinned = f.pinned.add(gp[i])
	}
	pointer, index, remaining, size := gp[0], gp[1], gp[2], gp[3]
	read := func(local int, r Reg) {
		if reg, _, pin := f.pinReg(local); pin {
			f.recoverLocal(local)
			f.a.MovReg32(r, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(r, 0)
		} else {
			f.ld32(r, SP, f.localOff(local))
		}
	}
	// Recover all homes before a runtime guard can bypass an invariant reload.
	for _, cell := range p.locals[:p.localN] {
		f.recoverLocal(int(cell.Index))
	}
	for _, local := range []int{p.index, p.pointer} {
		if f.locals[local].state == lsConstZero {
			f.materializeZeroLocal(local, true)
		}
	}
	read(p.index, index)
	read(p.pointer, pointer)
	limit := p.nodes[p.limit]
	if limit.Op == 0x41 {
		f.a.MovImm64(remaining, limit.Bits)
	} else {
		read(limit.Left, remaining)
	}
	var guards [8]int
	ng := 0
	guard := func(c Cond) { guards[ng] = f.a.Bcond(c); ng++ }
	f.cmpRR(index, remaining, false)
	guard(condAE)
	f.a.Sub32(remaining, remaining, index)
	f.cmpImm(remaining, uint32(4+p.storeN), false)
	guard(condB)
	if p.storeN != 1 {
		f.a.TstImm32(remaining, uint32(p.storeN-1))
		guard(condNE)
	}
	f.floatMapOffset(pointer, pointer, uint64(p.bias), false, X16)
	f.a.LslImm64(X16, remaining, 2)
	f.a.Add64(X16, pointer, X16)
	if p.offset != 0 {
		f.floatMapOffset(X16, X16, p.offset, true, X17)
	}
	f.ld64(size, linMemReg, -int32(bdCurBytes))
	f.cmpRR(X16, size, true)
	guard(condA)
	f.a.MovImm64(X17, 1<<32)
	f.cmpRR(X16, X17, true)
	guard(condA)
	if p.offset != 0 {
		f.floatMapOffset(pointer, pointer, p.offset, true, X16)
	}
	f.a.Add64(pointer, pointer, linMemReg)
	var values [128]Reg
	var persistent [9]Reg
	np := 0
	alloc := func() Reg { r := f.allocFReg(0); f.fpinned = f.fpinned.add(r); return r }
	release := func(r Reg) { f.fpinned = f.fpinned.remove(r); f.releaseF(r) }
	for i, n := range p.nodes[:p.n] {
		if !p.used[i] || !p.invariantLeaf(i) {
			continue
		}
		r := alloc()
		values[i] = r
		persistent[np] = r
		np++
		if n.Op == 0x41 {
			f.a.MovImm64(X16, n.Bits)
		} else {
			read(n.Left, X16)
		}
		f.a.NeonDupGprS(r, X16)
	}
	lanes := alloc()
	persistent[np] = lanes
	np++
	for i := byte(0); i < 4; i++ {
		f.a.MovImm64(X16, uint64(i))
		f.a.NeonInsS(lanes, X16, i)
	}
	loop := f.a.Len()
	uses = p.uses
	for i, n := range p.nodes[:p.n] {
		if !p.used[i] || p.invariantLeaf(i) {
			continue
		}
		out := alloc()
		values[i] = out
		if n.Op == 0x20 {
			f.a.NeonDupGprS(out, index)
			f.a.NeonAddS(out, out, lanes)
			continue
		}
		a, b := values[n.Left], values[n.Right]
		switch n.Op {
		case 0x6a:
			f.a.NeonAddS(out, a, b)
		case 0x6b:
			f.a.NeonSubS(out, a, b)
		case 0x6c:
			f.a.NeonMulS(out, a, b)
		case 0x71:
			f.a.NeonAnd16b(out, a, b)
		case 0x72:
			f.a.NeonOrr16b(out, a, b)
		case 0x73:
			f.a.NeonEor16b(out, a, b)
		case 0x74, 0x76:
			shift := uint8(p.nodes[n.Right].Bits & 31)
			if shift == 0 {
				f.a.NeonMov16b(out, a)
			} else if n.Op == 0x74 {
				f.a.NeonShlS(out, a, shift)
			} else {
				f.a.NeonUshrS(out, a, shift)
			}
		}
		uses[n.Left]--
		if uses[n.Left] == 0 && !p.invariantLeaf(n.Left) {
			release(a)
		}
		if n.Op != 0x74 && n.Op != 0x76 {
			uses[n.Right]--
			if uses[n.Right] == 0 && !p.invariantLeaf(n.Right) {
				release(b)
			}
		}
	}
	f.a.StrQ(pointer, 0, values[p.root])
	if !p.invariantLeaf(p.root) {
		release(values[p.root])
	}
	f.a.AddImm64(pointer, pointer, 16)
	f.a.AddImm32(index, index, 4)
	f.a.SubImm32(remaining, remaining, 4)
	f.cmpImm(remaining, uint32(4+p.storeN), false)
	f.patchBranch19(f.a.Bcond(condAE), loop)
	// Leave at least one original source iteration. It publishes every changed
	// temporary and index/pointer home without a function liveness analysis.
	publish := func(local int, value Reg) {
		if reg, _, pin := f.pinReg(local); pin {
			f.a.MovReg32(reg, value)
			f.markLocalDirty(local)
		} else {
			f.st64(SP, f.localOff(local), value)
			f.locals[local].state = lsMem
		}
	}
	publish(p.index, index)
	f.a.Sub64(X16, pointer, linMemReg)
	f.a.MovImm64(X17, uint64(p.bias)+p.offset)
	f.a.Sub32(X16, X16, X17)
	publish(p.pointer, X16)
	for i := 0; i < ng; i++ {
		f.patchBranch19(guards[i], f.a.Len())
	}
	for _, r := range persistent[:np] {
		release(r)
	}
	f.setFactsForLocal(p.index, 0)
	f.setFactsForLocal(p.pointer, 0)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("integer-word-map-vector")
}
