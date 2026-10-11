//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Integer addition is associative modulo 2^32. A bounded proof recognizes
// contiguous signed-absolute-difference reductions without reordering effects.
type absReducePlan struct {
	counter, index, acc, baseA, baseB int
	biasA, biasB                      uint32
	stride                            dotNode
	lanes                             int
}

// Decode a small prefix before reserving symbolic scratch. Unsupported effects
// and prefixes without a signed shift retain ordinary scalar lowering.
func (f *fn) hasAbsReducePrefix(source *wasm.Reader) bool {
	scan := *source
	start := scan.Offset()
	last31 := false
	var imm wasm.InstructionImmediate
	for scan.Offset()-start < 128 {
		op, e := scan.Byte()
		if e != nil {
			return false
		}
		if op == 0x41 {
			v, e := scan.I32()
			if e != nil {
				return false
			}
			last31 = v == 31
			continue
		}
		if op == 0x75 && last31 {
			return true
		}
		last31 = false
		switch op {
		case 0x20, 0x21, 0x22, 0x28, 0x6a, 0x6b, 0x73, 0x75:
		default:
			return false
		}
		if f.classifier.ClassifyInto(&scan, op, &imm) != nil {
			return false
		}
	}
	return false
}

func (f *fn) inspectAbsReduceLoop(source *wasm.Reader) (p absReducePlan, ok bool) {
	if f.nLocals > 32 {
		return
	}
	for _, typ := range f.localType {
		if typ != mtI32 {
			return
		}
	}
	if !f.hasAbsReducePrefix(source) {
		return
	}
	return f.inspectAbsReduceBody(source)
}

func (f *fn) inspectAbsReduceBody(source *wasm.Reader) (p absReducePlan, ok bool) {
	var nodes [256]dotNode
	var values [32]int
	var written, initialRead [32]bool
	var stack [24]int
	n, depth := f.nLocals, 0
	for i := 0; i < n; i++ {
		nodes[i] = dotNode{op: 0x20, a: i}
		values[i] = i
	}
	makeNode := func(v dotNode) int {
		if n == len(nodes) {
			return -1
		}
		nodes[n] = v
		n++
		return n - 1
	}
	push := func(x int) bool {
		if x < 0 || depth == len(stack) {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	scan := *source
	start := scan.Offset()
	for scan.Offset()-start < 1024 {
		op, e := scan.Byte()
		if e != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			x, e := scan.U32()
			if e != nil || x >= uint32(f.nLocals) {
				return p, false
			}
			if op == 0x20 {
				if values[x] == int(x) {
					initialRead[x] = true
				}
				if !push(values[x]) {
					return p, false
				}
			} else {
				if depth == 0 {
					return p, false
				}
				values[x] = stack[depth-1]
				written[x] = true
				if op == 0x21 {
					depth--
				}
			}
		case 0x41:
			v, e := scan.I32()
			if e != nil || !push(makeNode(dotNode{op: op, v: uint32(v)})) {
				return p, false
			}
		case 0x6a, 0x6b, 0x73, 0x75:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			if !push(makeNode(dotNode{op: op, a: stack[depth], b: stack[depth+1]})) {
				return p, false
			}
		case 0x28:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || off != 0 || depth == 0 {
				return p, false
			}
			x := makeNode(dotNode{op: op, a: stack[depth-1]})
			if x < 0 {
				return p, false
			}
			stack[depth-1] = x
		case 0x0d:
			label, e := scan.U32()
			if e != nil || label != 0 || depth != 1 {
				return p, false
			}
			end, e := scan.Byte()
			if e != nil || end != 0x0b {
				return p, false
			}
			inc := nodes[stack[0]]
			if inc.op != 0x6a {
				return p, false
			}
			local, step := nodes[inc.a], nodes[inc.b]
			if local.op != 0x20 {
				local, step = step, local
			}
			if local.op != 0x20 || step.op != 0x41 || step.v != 0xffffffff || values[local.a] != stack[0] {
				return p, false
			}
			p.counter = local.a
			return proveAbsReduce(nodes[:n], values[:f.nLocals], written[:f.nLocals], initialRead[:f.nLocals], p)
		default:
			return p, false
		}
	}
	return p, false
}

func proveAbsReduce(nodes []dotNode, values []int, written, initialRead []bool, p absReducePlan) (absReducePlan, bool) {
	var pairs [8][2]int
	for acc := range values {
		if !written[acc] || !initialRead[acc] || acc == p.counter {
			continue
		}
		count, accReads, visits := 0, 0, 0
		var sum func(int) bool
		sum = func(x int) bool {
			visits++
			if visits > 256 {
				return false
			}
			v := nodes[x]
			if v.op == 0x20 && v.a == acc {
				accReads++
				return true
			}
			if v.op == 0x6a {
				return sum(v.a) && sum(v.b)
			}
			if v.op != 0x6b || count == len(pairs) {
				return false
			}
			xor, sign := nodes[v.a], nodes[v.b]
			if xor.op != 0x73 || sign.op != 0x75 || nodes[sign.b].op != 0x41 || nodes[sign.b].v != 31 {
				return false
			}
			diff, signRef := xor.a, xor.b
			if signRef != v.b {
				diff, signRef = signRef, diff
			}
			if signRef != v.b || diff != sign.a || nodes[diff].op != 0x6b {
				return false
			}
			a, b := nodes[diff].a, nodes[diff].b
			if nodes[a].op != 0x28 || nodes[b].op != 0x28 {
				return false
			}
			pairs[count] = [2]int{a, b}
			count++
			return true
		}
		if !sum(values[acc]) || accReads != 1 || count != 4 && count != 8 {
			continue
		}
		for index := range values {
			if !written[index] || !initialRead[index] || index == acc || index == p.counter {
				continue
			}
			advance := nodes[values[index]]
			if advance.op != 0x6a {
				continue
			}
			idx, stride := nodes[advance.a], nodes[advance.b]
			if idx.op != 0x20 || idx.a != index {
				idx, stride = stride, idx
			}
			if idx.op != 0x20 || idx.a != index || stride.op != 0x41 && (stride.op != 0x20 || written[stride.a]) {
				continue
			}
			valid := true
			for i := range values {
				if written[i] && initialRead[i] && i != acc && i != index && i != p.counter {
					valid = false
				}
			}
			if !valid {
				continue
			}
			var offsets [8][2]uint32
			var bases [2]int
			for i := 0; i < count && valid; i++ {
				for j := 0; j < 2; j++ {
					base, off, ok := affineDotAddress(nodes, nodes[pairs[i][j]].a, index)
					if !ok || written[base] {
						valid = false
						break
					}
					if i == 0 {
						bases[j] = base
					} else if bases[j] != base {
						valid = false
						break
					}
					offsets[i][j] = off
				}
			}
			if !valid {
				continue
			}
			mins := offsets[0]
			for i := 1; i < count; i++ {
				for j := 0; j < 2; j++ {
					if offsets[i][j] < mins[j] {
						mins[j] = offsets[i][j]
					}
				}
			}
			var seen uint32
			for i := 0; i < count; i++ {
				a, b := offsets[i][0]-mins[0], offsets[i][1]-mins[1]
				if a != b || a%4 != 0 || a/4 >= uint32(count) || seen&(1<<(a/4)) != 0 {
					valid = false
					break
				}
				seen |= 1 << (a / 4)
			}
			// Every source memory effect must belong to the proven reduction.
			for x, v := range nodes {
				if v.op == 0x28 {
					used := false
					for i := 0; i < count; i++ {
						if pairs[i][0] == x || pairs[i][1] == x {
							used = true
						}
					}
					if !used {
						valid = false
					}
				}
			}
			if valid {
				p.acc, p.index, p.baseA, p.baseB, p.biasA, p.biasB, p.stride, p.lanes = acc, index, bases[0], bases[1], mins[0], mins[1], stride, count
				return p, true
			}
		}
	}
	return p, false
}

func (f *fn) emitAbsReduceLoop(source *wasm.Reader) {
	if !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	p, ok := f.inspectAbsReduceLoop(source)
	if !ok {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	var regs [6]Reg
	nr := 0
	alloc := func() Reg {
		x := f.allocRegOrNone(0)
		if x != regNone {
			f.pinned = f.pinned.add(x)
			regs[nr] = x
			nr++
		}
		return x
	}
	carrier := func(local int) Reg {
		if reg, _, pin := f.pinReg(local); pin {
			return reg
		}
		return alloc()
	}
	counter, index, acc := carrier(p.counter), carrier(p.index), carrier(p.acc)
	a, b := alloc(), alloc()
	size := f.memSizeReg
	if size == regNone {
		size = alloc()
	}
	defer func() {
		f.pinned = oldPinned
		f.fpinned = oldFPinned
		for i := 0; i < nr; i++ {
			f.release(regs[i])
		}
	}()
	if counter == regNone || index == regNone || acc == regNone || a == regNone || b == regNone || size == regNone {
		return
	}
	available := 0
	blocked := f.blockedFRegs(0)
	for _, reg := range fpAllocRegs {
		if !blocked.has(reg) && f.fregUser[reg] == nil {
			available++
		}
	}
	if available < 2 {
		return
	}
	read := func(local int, dst Reg) {
		if reg, _, pin := f.pinReg(local); pin {
			f.recoverLocal(local)
			f.a.MovReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld32(dst, SP, f.localOff(local))
		}
	}
	read(p.counter, counter)
	read(p.index, index)
	read(p.acc, acc)
	// Recover all invariant carriers before any edge can bypass their reload.
	read(p.baseA, a)
	read(p.baseB, b)
	if p.stride.op == 0x20 {
		read(p.stride.a, X16)
	}
	if f.memSizeReg == regNone {
		f.ld64(size, linMemReg, -int32(bdCurBytes))
	}
	va := f.allocFReg(0)
	f.fpinned = f.fpinned.add(va)
	vb := f.allocFReg(0)
	f.fpinned = f.fpinned.add(vb)
	loop := f.a.Len()
	f.cmpImm(counter, 1, false)
	done := f.a.Bcond(condBE)
	read(p.baseA, a)
	read(p.baseB, b)
	f.a.Add32(a, a, index)
	f.a.MovImm64(X16, uint64(p.biasA))
	f.a.Add32(a, a, X16)
	f.a.Add32(b, b, index)
	f.a.MovImm64(X16, uint64(p.biasB))
	f.a.Add32(b, b, X16)
	f.a.AddImm64(X16, a, uint32(4*p.lanes))
	f.cmpRR(X16, size, true)
	badA := f.a.Bcond(condA)
	f.a.AddImm64(X16, b, uint32(4*p.lanes))
	f.cmpRR(X16, size, true)
	badB := f.a.Bcond(condA)
	f.a.Add64(a, a, linMemReg)
	f.a.Add64(b, b, linMemReg)
	for off := int32(0); off < int32(4*p.lanes); off += 16 {
		f.a.LdrQ(va, a, off)
		f.a.LdrQ(vb, b, off)
		f.a.NeonSubS(va, va, vb)
		f.a.NeonAbsS(va, va)
		f.a.NeonAddvS(va, va)
		f.a.FmovToGpr(X16, va, false)
		f.a.Add32(acc, acc, X16)
	}
	if p.stride.op == 0x20 {
		read(p.stride.a, X16)
	} else {
		f.a.MovImm64(X16, uint64(p.stride.v))
	}
	f.a.Add32(index, index, X16)
	f.a.SubImm32(counter, counter, 1)
	f.patchBranch26(f.a.Branch(), loop)
	for _, site := range []int{done, badA, badB} {
		f.patchBranch19(site, f.a.Len())
	}
	for _, state := range []struct {
		local int
		reg   Reg
	}{{p.counter, counter}, {p.index, index}, {p.acc, acc}} {
		if reg, _, pin := f.pinReg(state.local); pin {
			f.a.MovReg32(reg, state.reg)
			f.markLocalDirty(state.local)
		} else {
			f.st64(SP, f.localOff(state.local), state.reg)
			f.locals[state.local].state = lsMem
		}
		f.setFactsForLocal(state.local, 0)
	}
	f.releaseF(va)
	f.releaseF(vb)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("abs-reduce-loop-vector")
}
