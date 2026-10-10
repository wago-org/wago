//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var dotLoopEnabled = os.Getenv("WAGO_ARM64_NO_DOT_LOOP") != "1"

// A bounded symbolic scan proves a read-only modular i32 dot reduction. No
// expression, local, memory or control effect outside this grammar is admitted.
type dotNode struct {
	op   byte
	a, b int
	v    uint32
}
type dotAlias struct {
	local, base int
	offset      uint32
}
type dotLoopPlan struct {
	counter, acc, baseA, baseB int
	step, limit, biasA, biasB  uint32
	aliases                    [8]dotAlias
	nAliases                   int
}

func (f *fn) inspectDotLoop(r *wasm.Reader) (plan dotLoopPlan, ok bool) {
	if f.nLocals > 32 {
		return
	}
	for _, typ := range f.localType {
		if typ != mtI32 {
			return
		}
	}
	scan := *r
	start := scan.Offset()
	var nodes [128]dotNode
	var stack [24]int
	var values [32]int
	var written [32]bool
	n, depth := f.nLocals, 0
	for i := 0; i < n; i++ {
		nodes[i] = dotNode{op: 0x20, a: i}
		values[i] = i
	}
	push := func(x int) bool {
		if depth == len(stack) {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	makeNode := func(x dotNode) int {
		if n == len(nodes) {
			return -1
		}
		nodes[n] = x
		n++
		return n - 1
	}
	for scan.Offset()-start < 384 {
		op, err := scan.Byte()
		if err != nil {
			return plan, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			x, err := scan.U32()
			if err != nil || x >= uint32(f.nLocals) {
				return plan, false
			}
			if op == 0x20 {
				if !push(values[x]) {
					return plan, false
				}
				continue
			}
			if depth == 0 {
				return plan, false
			}
			values[x] = stack[depth-1]
			written[x] = true
			if op == 0x21 {
				depth--
			}
		case 0x41:
			v, err := scan.I32()
			if err != nil {
				return plan, false
			}
			x := makeNode(dotNode{op: op, v: uint32(v)})
			if x < 0 || !push(x) {
				return plan, false
			}
		case 0x6a, 0x6b, 0x6c, 0x47:
			if depth < 2 {
				return plan, false
			}
			depth -= 2
			x := makeNode(dotNode{op: op, a: stack[depth], b: stack[depth+1]})
			if x < 0 || !push(x) {
				return plan, false
			}
		case 0x28:
			mem, off, err := f.readMemArg(&scan)
			if err != nil || mem != 0 || off > 1<<20 || depth == 0 {
				return plan, false
			}
			x := makeNode(dotNode{op: op, a: stack[depth-1], v: uint32(off)})
			if x < 0 {
				return plan, false
			}
			stack[depth-1] = x
		case 0x0d:
			label, err := scan.U32()
			if err != nil || label != 0 || depth != 1 {
				return plan, false
			}
			end, err := scan.Byte()
			if err != nil || end != 0x0b {
				return plan, false
			}
			condition := nodes[stack[0]]
			if condition.op != 0x47 {
				return plan, false
			}
			limit, increment := condition.b, condition.a
			if nodes[limit].op != 0x41 {
				limit, increment = increment, limit
			}
			if nodes[limit].op != 0x41 || nodes[increment].op != 0x6a {
				return plan, false
			}
			inc := nodes[increment]
			local, stride := inc.a, inc.b
			if nodes[local].op != 0x20 {
				local, stride = stride, local
			}
			if nodes[local].op != 0x20 || nodes[stride].op != 0x41 {
				return plan, false
			}
			plan.counter, plan.step, plan.limit = nodes[local].a, nodes[stride].v, nodes[limit].v
			if !written[plan.counter] || values[plan.counter] != increment || (plan.step != 4 && plan.step != 8 && plan.step != 16) {
				return plan, false
			}
			return proveDotLoop(nodes[:n], values[:f.nLocals], written[:f.nLocals], plan)
		default:
			return plan, false
		}
	}
	return plan, false
}

// affineDotAddress flattens only addition of two distinct original locals and
// nonnegative constants. The range guard later excludes modular address wrap.
func affineDotAddress(nodes []dotNode, x int, counter int) (base int, offset uint32, ok bool) {
	var locals [2]int
	n := 0
	visits := 0
	var visit func(int) bool
	visit = func(x int) bool {
		visits++
		if visits > 128 {
			return false
		}
		v := nodes[x]
		switch v.op {
		case 0x20:
			if n == 2 {
				return false
			}
			locals[n] = v.a
			n++
			return true
		case 0x41:
			if v.v > 1<<20 || uint64(offset)+uint64(v.v) > 1<<20 {
				return false
			}
			offset += v.v
			return true
		case 0x6a:
			return visit(v.a) && visit(v.b)
		}
		return false
	}
	if !visit(x) || n != 2 || locals[0] == locals[1] {
		return 0, 0, false
	}
	if locals[0] == counter {
		return locals[1], offset, true
	}
	if locals[1] == counter {
		return locals[0], offset, true
	}
	return 0, 0, false
}

func proveDotLoop(nodes []dotNode, values []int, written []bool, plan dotLoopPlan) (dotLoopPlan, bool) {
	load := func(x int) (base int, offset, bias uint32, ok bool) {
		v := nodes[x]
		if v.op == 0x6a || v.op == 0x6b {
			left, right := v.a, v.b
			if v.op == 0x6a && nodes[left].op == 0x41 {
				left, right = right, left
			}
			if nodes[right].op != 0x41 {
				return
			}
			bias = nodes[right].v
			if v.op == 0x6b {
				bias = -bias
			}
			v = nodes[left]
		}
		if v.op != 0x28 {
			return
		}
		base, offset, ok = affineDotAddress(nodes, v.a, plan.counter)
		if !ok || uint64(offset)+uint64(v.v) >= uint64(plan.step) {
			return 0, 0, 0, false
		}
		offset += v.v
		return
	}
	for acc := range values {
		if !written[acc] || acc == plan.counter {
			continue
		}
		var products [4]int
		count, identities := 0, 0
		visits := 0
		var flatten func(int) bool
		flatten = func(x int) bool {
			visits++
			if visits > 128 {
				return false
			}
			v := nodes[x]
			if v.op == 0x6a {
				return flatten(v.a) && flatten(v.b)
			}
			if v.op == 0x20 && v.a == acc {
				identities++
				return identities == 1
			}
			if v.op == 0x6c && count < len(products) {
				products[count] = x
				count++
				return true
			}
			return false
		}
		if !flatten(values[acc]) || identities != 1 || uint32(count*4) != plan.step {
			continue
		}
		seen := uint32(0)
		valid := true
		for i := 0; i < count; i++ {
			p := nodes[products[i]]
			a, oa, ba, oka := load(p.a)
			b, ob, bb, okb := load(p.b)
			if i == 0 {
				plan.baseA, plan.baseB, plan.biasA, plan.biasB = a, b, ba, bb
			}
			if (a != plan.baseA || ba != plan.biasA) && a == plan.baseB && b == plan.baseA {
				a, b, oa, ob, ba, bb = b, a, ob, oa, bb, ba
			}
			if !oka || !okb || a != plan.baseA || b != plan.baseB || ba != plan.biasA || bb != plan.biasB || oa != ob || oa%4 != 0 || seen&(1<<(oa/4)) != 0 {
				valid = false
				break
			}
			seen |= 1 << (oa / 4)
		}
		if !valid || seen != (1<<count)-1 || written[plan.baseA] || written[plan.baseB] {
			continue
		}
		plan.acc = acc
		plan.nAliases = 0
		for local, changed := range written {
			if !changed || local == acc || local == plan.counter {
				continue
			}
			base, offset, yes := affineDotAddress(nodes, values[local], plan.counter)
			if !yes || (base != plan.baseA && base != plan.baseB) || offset >= plan.step || plan.nAliases == len(plan.aliases) {
				valid = false
				break
			}
			plan.aliases[plan.nAliases] = dotAlias{local, base, offset}
			plan.nAliases++
		}
		if valid {
			return plan, true
		}
	}
	return plan, false
}

type dotLoopState struct {
	plan           dotLoopPlan
	depth, endSite int
}

// emitDotLoop precedes the scalar loop's backedge target. Failed range/trip
// guards fall into the original body without changing source locals or memory;
// successful vector execution skips that body and joins after its void end.
// Pinned values are written only to their registers, preserving frame elision.
// Other final locals use canonical slots. Both paths therefore satisfy the
// scalar lowering's final local-storage state, with value/range facts cleared.
func (f *fn) emitDotLoop(r *wasm.Reader) {
	if !f.opt(optDotLoopVector) || f.dotLoop != nil || f.usesCalls || f.interruptible || f.moduleEH ||
		f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 ||
		f.depth() != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	plan, ok := f.inspectDotLoop(r)
	if !ok {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	alloc := func() Reg {
		x := f.allocRegOrNone(0)
		if x != regNone {
			f.pinned = f.pinned.add(x)
		}
		return x
	}
	a, b, counter, accumulator := alloc(), alloc(), alloc(), alloc()
	if a == regNone || b == regNone || counter == regNone || accumulator == regNone {
		f.pinned = oldPinned
		for _, x := range []Reg{a, b, counter, accumulator} {
			if x != regNone {
				f.release(x)
			}
		}
		return
	}
	compareLimit := func() {
		limit := int64(int32(plan.limit))
		if f.fitsAddSubImmediate(limit) {
			f.cmpImmS(counter, limit, false)
		} else {
			f.a.MovImm64(X16, uint64(plan.limit))
			f.cmpRR(counter, X16, false)
		}
	}
	read := func(local int, dst Reg) {
		if reg, _, pinned := f.pinReg(local); pinned {
			f.recoverLocal(local)
			f.a.MovReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld32(dst, SP, f.localOff(local))
		}
	}
	read(plan.baseA, a)
	read(plan.baseB, b)
	read(plan.counter, counter)
	read(plan.acc, accumulator)
	var fallback [6]int
	nFallback := 0
	guard := func(cc Cond) { fallback[nFallback] = f.a.Bcond(cc); nFallback++ }
	compareLimit()
	guard(condAE)
	f.a.MovImm64(X16, uint64(plan.limit))
	f.a.Sub32(X16, X16, counter)
	f.a.TstImm32(X16, 15)
	guard(condNE)
	mb := f.memSizeReg
	if mb == regNone {
		mb = X17
		f.ld64(mb, linMemReg, -int32(bdCurBytes))
	}
	for _, base := range []Reg{a, b} {
		f.a.MovImm64(X16, uint64(plan.limit))
		f.a.Add64(X16, base, X16)
		f.cmpRR(X16, mb, true)
		guard(condA)
		// The memory can be 4 GiB; end==4 GiB remains a valid nonwrapping range.
		f.a.MovImm64(X17, 1<<32)
		f.cmpRR(X16, X17, true)
		guard(condA)
		if mb == X17 {
			f.ld64(mb, linMemReg, -int32(bdCurBytes))
		}
	}
	vec := func() Reg { x := f.allocFReg(0); f.fpinned = f.fpinned.add(x); return x }
	sum, va, vb, biasA, biasB := vec(), vec(), vec(), vec(), vec()
	f.a.NeonEor16b(sum, sum, sum)
	if plan.biasA != 0 {
		f.a.MovImm64(X16, uint64(plan.biasA))
		f.a.NeonDupGprS(biasA, X16)
	}
	if plan.biasB != 0 {
		f.a.MovImm64(X16, uint64(plan.biasB))
		f.a.NeonDupGprS(biasB, X16)
	}
	f.a.Add64(a, a, counter)
	f.a.Add64(a, a, linMemReg)
	f.a.Add64(b, b, counter)
	f.a.Add64(b, b, linMemReg)
	loop := f.a.Len()
	f.a.LdrQ(va, a, 0)
	f.a.LdrQ(vb, b, 0)
	if plan.biasA != 0 {
		f.a.NeonAddS(va, va, biasA)
	}
	if plan.biasB != 0 {
		f.a.NeonAddS(vb, vb, biasB)
	}
	f.a.NeonMulS(va, va, vb)
	f.a.NeonAddS(sum, sum, va)
	f.a.AddImm64(a, a, 16)
	f.a.AddImm64(b, b, 16)
	f.a.AddImm32(counter, counter, 16)
	compareLimit()
	f.patchBranch19(f.a.Bcond(condNE), loop)
	f.a.NeonAddvS(sum, sum)
	f.a.NeonUmovS(X16, sum, 0)
	f.a.Add32(accumulator, accumulator, X16)
	write := func(local int, src Reg) {
		if reg, _, pinned := f.pinReg(local); pinned {
			f.a.MovReg32(reg, src)
		} else {
			f.st64(SP, f.localOff(local), src)
		}
	}
	write(plan.counter, counter)
	write(plan.acc, accumulator)
	for i := 0; i < plan.nAliases; i++ {
		alias := plan.aliases[i]
		base := a
		if alias.base == plan.baseB {
			base = b
		}
		f.a.Sub64(X16, base, linMemReg)
		f.a.SubImm32(X16, X16, plan.step-alias.offset)
		write(alias.local, X16)
	}
	f.dotLoop = &dotLoopState{plan: plan, depth: len(f.ctrl) + 1, endSite: f.a.Branch()}
	for i := 0; i < nFallback; i++ {
		f.patchBranch19(fallback[i], f.a.Len())
	}
	f.pinned, f.fpinned = oldPinned, oldFPinned
	for _, x := range []Reg{a, b, counter, accumulator} {
		f.release(x)
	}
	for _, x := range []Reg{sum, va, vb, biasA, biasB} {
		f.releaseF(x)
	}
	f.stats.peep("dot-loop-vector")
}

func (f *fn) finishDotLoop(depth int) {
	state := f.dotLoop
	if state == nil || state.depth != depth {
		return
	}
	f.patchBranch26(state.endSite, f.a.Len())
	f.setFactsForLocal(state.plan.counter, 0)
	f.setFactsForLocal(state.plan.acc, 0)
	for i := 0; i < state.plan.nAliases; i++ {
		f.setFactsForLocal(state.plan.aliases[i].local, 0)
	}
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.dotLoop = nil
}
