//go:build arm64

package arm64

import (
	"math/bits"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var pureReduceEnabled = os.Getenv("WAGO_ARM64_NO_PURE_REDUCE") != "1"

// The bounded scan expands local tees into a DAG. Eligible bodies have one
// modular i32 induction, a countdown exit, and independent additive i64
// reductions. Widened products require a proven 32-bit result.
type pureReduction struct{ local, term int }
type pureReducePlan struct {
	nodes                         [128]dotNode
	values                        [32]int
	written                       uint32
	n, locals, counter, induction int
	step                          uint32
	reductions                    [4]pureReduction
	nReductions                   int
}
type pureReduceJoin struct {
	depth, site int
	written     uint32
}

func (f *fn) inspectPureReduce(r *wasm.Reader) (p pureReducePlan, ok bool) {
	if f.nLocals > 32 {
		return
	}
	p.locals, p.n = f.nLocals, f.nLocals
	for i, typ := range f.localType {
		if typ != mtI32 && typ != mtI64 {
			return p, false
		}
		p.nodes[i] = dotNode{op: 0x20, a: i}
		p.values[i] = i
	}
	scan := *r
	start := scan.Offset()
	var stack [24]int
	depth := 0
	push := func(x int) bool {
		if depth == len(stack) {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	node := func(v dotNode) int {
		if p.n == len(p.nodes) {
			return -1
		}
		p.nodes[p.n] = v
		p.n++
		return p.n - 1
	}
	for scan.Offset()-start < 512 {
		op, err := scan.Byte()
		if err != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			x, err := scan.U32()
			if err != nil || int(x) >= p.locals {
				return p, false
			}
			if op == 0x20 {
				if !push(p.values[x]) {
					return p, false
				}
				continue
			}
			if depth == 0 {
				return p, false
			}
			p.values[x] = stack[depth-1]
			p.written |= 1 << x
			if op == 0x21 {
				depth--
			}
		case 0x41:
			v, err := scan.I32()
			if err != nil {
				return p, false
			}
			x := node(dotNode{op: op, v: uint32(v)})
			if x < 0 || !push(x) {
				return p, false
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x76, 0x7c, 0x7e:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			x := node(dotNode{op: op, a: stack[depth], b: stack[depth+1]})
			if x < 0 || !push(x) {
				return p, false
			}
		case 0xad:
			if depth == 0 {
				return p, false
			}
			x := node(dotNode{op: op, a: stack[depth-1]})
			if x < 0 {
				return p, false
			}
			stack[depth-1] = x
		case 0x0d:
			label, err := scan.U32()
			if err != nil || label != 0 || depth != 1 {
				return p, false
			}
			end, err := scan.Byte()
			if err != nil || end != 0x0b {
				return p, false
			}
			p.counter = -1
			for local := 0; local < p.locals; local++ {
				if p.written&(1<<local) == 0 || f.localType[local] != mtI32 {
					continue
				}
				v := p.nodes[p.values[local]]
				if v.op != 0x6a && v.op != 0x6b {
					continue
				}
				a, b := v.a, v.b
				if v.op == 0x6a && p.nodes[a].op == 0x41 {
					a, b = b, a
				}
				if p.nodes[a].op != 0x20 || p.nodes[a].a != local || p.nodes[b].op != 0x41 {
					continue
				}
				step := p.nodes[b].v
				if v.op == 0x6b {
					step = -step
				}
				if step == 0xffffffff && p.values[local] == stack[0] {
					p.counter = local
				}
			}
			if p.counter < 0 {
				return p, false
			}
			p.induction = -1
			for local := 0; local < p.locals; local++ {
				if local == p.counter || p.written&(1<<local) == 0 || f.localType[local] != mtI32 {
					continue
				}
				v := p.nodes[p.values[local]]
				if v.op != 0x6a && v.op != 0x6b {
					continue
				}
				a, b := v.a, v.b
				if v.op == 0x6a && p.nodes[a].op == 0x41 {
					a, b = b, a
				}
				if p.nodes[a].op != 0x20 || p.nodes[a].a != local || p.nodes[b].op != 0x41 {
					continue
				}
				if p.induction >= 0 {
					return p, false
				}
				p.induction = local
				p.step = p.nodes[b].v
				if v.op == 0x6b {
					p.step = -p.step
				}
			}
			if p.induction < 0 {
				return p, false
			}
			return f.provePureReduce(p)
		default:
			return p, false
		}
	}
	return p, false
}

func (f *fn) provePureReduce(p pureReducePlan) (pureReducePlan, bool) {
	var valid [128]bool
	var bound [128]uint32
	for i := 0; i < p.n; i++ {
		v := p.nodes[i]
		switch v.op {
		case 0x20:
			valid[i] = f.localType[v.a] == mtI32 && v.a != p.counter && (v.a == p.induction || p.written&(1<<v.a) == 0)
			bound[i] = ^uint32(0)
		case 0x41:
			valid[i] = true
			bound[i] = v.v
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73:
			if !valid[v.a] || !valid[v.b] {
				continue
			}
			valid[i] = true
			bound[i] = ^uint32(0)
			if v.op == 0x71 {
				bound[i] = min(bound[v.a], bound[v.b])
			}
			if v.op == 0x72 || v.op == 0x73 {
				width := bits.Len32(bound[v.a] | bound[v.b])
				if width < 32 {
					bound[i] = (1 << width) - 1
				}
			}
			if v.op == 0x6a && uint64(bound[v.a])+uint64(bound[v.b]) <= 0xffffffff {
				bound[i] = bound[v.a] + bound[v.b]
			}
			if v.op == 0x6c && uint64(bound[v.a])*uint64(bound[v.b]) <= 0xffffffff {
				bound[i] = bound[v.a] * bound[v.b]
			}
		case 0x74, 0x76:
			if !valid[v.a] || p.nodes[v.b].op != 0x41 {
				continue
			}
			valid[i] = true
			bound[i] = ^uint32(0)
			if v.op == 0x76 {
				bound[i] = bound[v.a] >> (p.nodes[v.b].v & 31)
			}
		case 0xad:
			if valid[v.a] {
				valid[i] = true
				bound[i] = bound[v.a]
			}
		case 0x7e:
			if p.nodes[v.a].op != 0xad || p.nodes[v.b].op != 0xad || !valid[v.a] || !valid[v.b] || uint64(bound[v.a])*uint64(bound[v.b]) > 0xffffffff {
				continue
			}
			valid[i] = true
			bound[i] = bound[v.a] * bound[v.b]
		}
	}
	for local := 0; local < p.locals; local++ {
		if p.written&(1<<local) == 0 || local == p.counter || local == p.induction {
			continue
		}
		x := p.values[local]
		v := p.nodes[x]
		if f.localType[local] == mtI64 && v.op == 0x7c {
			a, b := v.a, v.b
			if p.nodes[b].op == 0x20 && p.nodes[b].a == local {
				a, b = b, a
			}
			if p.nodes[a].op != 0x20 || p.nodes[a].a != local || !valid[b] || p.nReductions == len(p.reductions) {
				return p, false
			}
			p.reductions[p.nReductions] = pureReduction{local, b}
			p.nReductions++
		} else if !valid[x] {
			return p, false
		}
	}
	if p.nReductions == 0 {
		return p, false
	}
	// Canonicalize widening nodes after proving full-precision products. The
	// emitted 32-bit terms are widened by pairwise addition into 64-bit lanes.
	for i := 0; i < p.n; i++ {
		v := &p.nodes[i]
		if v.op == 0x7e && valid[i] {
			v.op = 0x6c
			v.a = p.nodes[v.a].a
			v.b = p.nodes[v.b].a
		}
	}
	for i := 0; i < p.nReductions; i++ {
		x := p.reductions[i].term
		if p.nodes[x].op == 0xad {
			p.reductions[i].term = p.nodes[x].a
		}
	}
	for local := 0; local < p.locals; local++ {
		x := p.values[local]
		if p.nodes[x].op == 0xad {
			p.values[local] = p.nodes[x].a
		}
	}
	return p, true
}

type pureVectorLayout struct {
	regs         [128]Reg
	refs         [128]int
	reachable    [128]bool
	acc          [4]Reg
	pair, stride Reg
	mask         regMask
}

// Preflight physical vector assignments before emission. Invariants remain
// resident; DAG temporaries release at their last use; final source-local roots
// stay live for lane extraction. Excess pressure declines without state changes.
func (f *fn) planPureVectors(p *pureReducePlan) (v pureVectorLayout, ok bool) {
	for i := range v.regs {
		v.regs[i] = regNone
	}
	var visit func(int)
	visit = func(x int) {
		if v.reachable[x] {
			return
		}
		v.reachable[x] = true
		n := p.nodes[x]
		if n.op != 0x20 && n.op != 0x41 {
			v.refs[n.a]++
			visit(n.a)
			if n.op != 0x74 && n.op != 0x76 {
				v.refs[n.b]++
				visit(n.b)
			}
		}
	}
	root := func(x int) { v.refs[x]++; visit(x) }
	root(p.induction)
	for i := 0; i < p.nReductions; i++ {
		root(p.reductions[i].term)
	}
	for local := 0; local < p.locals; local++ {
		if p.written&(1<<local) == 0 || local == p.counter || local == p.induction {
			continue
		}
		isAcc := false
		for i := 0; i < p.nReductions; i++ {
			if p.reductions[i].local == local {
				isAcc = true
			}
		}
		if !isAcc {
			root(p.values[local])
		}
	}
	inuse := f.blockedFRegs(0)
	alloc := func() Reg {
		for _, r := range fpAllocRegs {
			if !inuse.has(r) && f.fregUser[r] == nil {
				inuse = inuse.add(r)
				v.mask = v.mask.add(r)
				return r
			}
		}
		return regNone
	}
	v.pair, v.stride = alloc(), alloc()
	if v.pair == regNone || v.stride == regNone {
		return v, false
	}
	for i := 0; i < p.nReductions; i++ {
		v.acc[i] = alloc()
		if v.acc[i] == regNone {
			return v, false
		}
	}
	for i := 0; i < p.n; i++ {
		if !v.reachable[i] {
			continue
		}
		n := p.nodes[i]
		if n.op != 0x20 && n.op != 0x41 {
			continue
		}
		if n.op == 0x41 {
			for j := 0; j < i; j++ {
				if v.reachable[j] && p.nodes[j].op == 0x41 && p.nodes[j].v == n.v {
					v.regs[i] = v.regs[j]
					break
				}
			}
		}
		if v.regs[i] == regNone {
			v.regs[i] = alloc()
			if v.regs[i] == regNone {
				return v, false
			}
		}
	}
	refs := v.refs
	consume := func(x int) {
		refs[x]--
		if refs[x] == 0 && p.nodes[x].op != 0x20 && p.nodes[x].op != 0x41 {
			inuse = inuse.remove(v.regs[x])
		}
	}
	for i := 0; i < p.n; i++ {
		if !v.reachable[i] {
			continue
		}
		n := p.nodes[i]
		if n.op == 0x20 || n.op == 0x41 {
			continue
		}
		v.regs[i] = alloc()
		if v.regs[i] == regNone {
			return v, false
		}
		consume(n.a)
		if n.op != 0x74 && n.op != 0x76 {
			consume(n.b)
		}
	}
	return v, true
}

// Process groups of four, merge 64-bit lane sums, and restore every modified
// scalar local. Up to three remaining iterations enter the unchanged scalar
// body. Scalar backedges skip the vector prefix and its initialization.
func (f *fn) emitPureReduce(r *wasm.Reader) {
	if !f.opt(optPureReduceVector) || f.pureReduce != nil || f.dotLoop != nil || f.usesCalls || f.interruptible || f.moduleEH || f.preserveCallerPins || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.depth() != 0 {
		return
	}
	if f.nLocals > 32 {
		return
	}
	wide := false
	for _, typ := range f.localType {
		if typ != mtI32 && typ != mtI64 {
			return
		}
		if typ == mtI64 {
			wide = true
		}
	}
	if !wide {
		return
	}
	p, ok := f.inspectPureReduce(r)
	if !ok {
		return
	}
	v, ok := f.planPureVectors(&p)
	if !ok {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	count := f.allocRegOrNone(0)
	if count == regNone {
		return
	}
	f.pinned = f.pinned.add(count)
	read := func(local int, dst Reg) {
		if reg, _, pin := f.pinReg(local); pin {
			f.recoverLocal(local)
			if f.localType[local] == mtI64 {
				f.a.MovReg64(dst, reg)
			} else {
				f.a.MovReg32(dst, reg)
			}
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld64(dst, SP, f.localOff(local))
		}
	}
	write := func(local int, src Reg) {
		if reg, _, pin := f.pinReg(local); pin {
			if f.localType[local] == mtI64 {
				f.a.MovReg64(reg, src)
			} else {
				f.a.MovReg32(reg, src)
			}
		} else {
			f.st64(SP, f.localOff(local), src)
		}
	}
	for local := 0; local < p.locals; local++ {
		if (p.written&(1<<local) != 0 || v.reachable[local]) && f.locals[local].state == lsConstZero {
			_, _, pinned := f.pinReg(local)
			f.materializeZeroLocal(local, !pinned)
		}
	}
	read(p.counter, count)
	f.a.CmpImm32(count, 4)
	toScalar := f.a.Bcond(condB)
	f.fpinned = f.fpinned.union(v.mask)
	for i := 0; i < p.n; i++ {
		if !v.reachable[i] {
			continue
		}
		n := p.nodes[i]
		if n.op != 0x20 && n.op != 0x41 {
			continue
		}
		if n.op == 0x41 {
			duplicate := false
			for j := 0; j < i; j++ {
				if v.reachable[j] && v.regs[j] == v.regs[i] {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			f.a.MovImm64(X16, uint64(n.v))
		} else {
			read(n.a, X16)
		}
		f.a.NeonDupGprS(v.regs[i], X16)
		if i == p.induction {
			step := int64(int32(p.step))
			if step > 4095 || step < -4095 {
				f.a.MovImm64(X17, uint64(p.step))
			}
			for lane := byte(1); lane < 4; lane++ {
				if step >= 0 && step <= 4095 {
					f.a.AddImm32(X16, X16, uint32(step))
				} else if step < 0 && step >= -4095 {
					f.a.SubImm32(X16, X16, uint32(-step))
				} else {
					f.a.Add32(X16, X16, X17)
				}
				f.a.NeonInsS(v.regs[i], X16, lane)
			}
		}
	}
	f.a.MovImm64(X16, uint64(p.step*4))
	f.a.NeonDupGprS(v.stride, X16)
	for i := 0; i < p.nReductions; i++ {
		f.a.NeonEor16b(v.acc[i], v.acc[i], v.acc[i])
	}
	loop := f.a.Len()
	for i := 0; i < p.n; i++ {
		if !v.reachable[i] {
			continue
		}
		n := p.nodes[i]
		dst, a, b := v.regs[i], v.regs[n.a], v.regs[n.b]
		switch n.op {
		case 0x6a:
			f.a.NeonAddS(dst, a, b)
		case 0x6b:
			f.a.NeonSubS(dst, a, b)
		case 0x6c:
			f.a.NeonMulS(dst, a, b)
		case 0x71:
			f.a.NeonAnd16b(dst, a, b)
		case 0x72:
			f.a.NeonOrr16b(dst, a, b)
		case 0x73:
			f.a.NeonEor16b(dst, a, b)
		case 0x74:
			shift := uint8(p.nodes[n.b].v & 31)
			if shift == 0 {
				f.a.NeonOrr16b(dst, a, a)
			} else {
				f.a.NeonShlS(dst, a, shift)
			}
		case 0x76:
			shift := uint8(p.nodes[n.b].v & 31)
			if shift == 0 {
				f.a.NeonOrr16b(dst, a, a)
			} else {
				f.a.NeonUshrS(dst, a, shift)
			}
		}
	}
	for i := 0; i < p.nReductions; i++ {
		f.a.NeonUaddlpDfromS(v.pair, v.regs[p.reductions[i].term])
		f.a.NeonAddD(v.acc[i], v.acc[i], v.pair)
	}
	f.a.NeonAddS(v.regs[p.induction], v.regs[p.induction], v.stride)
	f.a.SubImm32(count, count, 4)
	f.a.CmpImm32(count, 4)
	f.patchBranch19(f.a.Bcond(condAE), loop)
	for i := 0; i < p.nReductions; i++ {
		f.a.NeonUmovD(X16, v.acc[i], 0)
		f.a.NeonUmovD(X17, v.acc[i], 1)
		f.a.Add64(X16, X16, X17)
		read(p.reductions[i].local, X17)
		f.a.Add64(X16, X16, X17)
		write(p.reductions[i].local, X16)
	}
	for local := 0; local < p.locals; local++ {
		if p.written&(1<<local) == 0 {
			continue
		}
		isAcc := false
		for i := 0; i < p.nReductions; i++ {
			if p.reductions[i].local == local {
				isAcc = true
			}
		}
		if isAcc {
			continue
		}
		if local == p.counter {
			write(local, count)
			continue
		}
		if local == p.induction {
			f.a.NeonUmovS(X16, v.regs[p.induction], 0)
		} else if p.values[local] == p.induction {
			f.a.NeonUmovS(X16, v.regs[p.induction], 0)
			f.a.MovImm64(X17, uint64(p.step))
			f.a.Sub32(X16, X16, X17)
		} else {
			f.a.NeonUmovS(X16, v.regs[p.values[local]], 3)
		}
		write(local, X16)
	}
	finished := f.a.Cbz32(count)
	f.patchBranch19(toScalar, f.a.Len())
	f.pinned, f.fpinned = oldPinned, oldFPinned
	f.release(count)
	for local := 0; local < p.locals; local++ {
		if p.written&(1<<local) != 0 {
			f.setFactsForLocal(local, 0)
			if _, _, pin := f.pinReg(local); pin {
				f.locals[local].state = lsReg
			} else {
				f.locals[local].state = lsMem
			}
		}
	}
	f.pureReduce = &pureReduceJoin{depth: len(f.ctrl) + 1, site: finished, written: p.written}
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("pure-reduce-vector")
}

func (f *fn) finishPureReduce(depth int) {
	state := f.pureReduce
	if state == nil || state.depth != depth {
		return
	}
	f.patchBranch19(state.site, f.a.Len())
	for local := 0; local < f.nLocals; local++ {
		if state.written&(1<<local) != 0 {
			f.setFactsForLocal(local, 0)
		}
	}
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.pureReduce = nil
}
