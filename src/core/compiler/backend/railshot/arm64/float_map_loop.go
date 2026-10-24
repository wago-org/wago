//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A bounded symbolic proof admits only independent scalar floating-point maps.
// The vector prefix preserves the expression tree and leaves an original scalar
// packet to restore every observable local. All range/alias failures fall back.
type floatMapNode struct {
	op   byte
	a, b int
	bits uint64
}
type floatMapAddress struct {
	base      int
	bias      uint32
	invariant bool
}
type floatMapPlan struct {
	nodes                        [128]floatMapNode
	addresses                    [128]floatMapAddress
	values                       [32]int
	written                      [32]bool
	localType                    [32]machineType
	counter, result, dest, count int
	step, limit                  uint32
	wide                         bool
}

func (f *fn) inspectFloatMapLoop(source *wasm.Reader) (p floatMapPlan, ok bool) {
	if f.nLocals > 32 {
		return
	}
	scan := *source
	start := scan.Offset()
	var stack [24]int
	var initialRead [32]bool
	var stores [2][2]int
	depth, ns, n := 0, 0, f.nLocals
	for i := 0; i < n; i++ {
		p.nodes[i] = floatMapNode{op: 0x20, a: i}
		p.values[i] = i
		p.localType[i] = f.localType[i]
	}
	push := func(x int) bool {
		if depth == len(stack) || x < 0 {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	node := func(v floatMapNode) int {
		if n == len(p.nodes) {
			return -1
		}
		p.nodes[n] = v
		n++
		return n - 1
	}
	for scan.Offset()-start < 512 {
		op, err := scan.Byte()
		if err != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			x, e := scan.U32()
			if e != nil || x >= uint32(f.nLocals) {
				return p, false
			}
			if op == 0x20 {
				if p.values[x] == int(x) {
					initialRead[x] = true
				}
				if !push(p.values[x]) {
					return p, false
				}
			} else {
				if depth == 0 {
					return p, false
				}
				p.values[x] = stack[depth-1]
				p.written[x] = true
				if op == 0x21 {
					depth--
				}
			}
		case 0x41, 0x43, 0x44:
			var bits uint64
			if op == 0x41 {
				v, e := scan.I32()
				err = e
				bits = uint64(uint32(v))
			} else if op == 0x43 {
				v, e := scan.LEU32()
				err = e
				bits = uint64(v)
			} else {
				bits, err = scan.LEU64()
			}
			if err != nil || !push(node(floatMapNode{op: op, bits: bits})) {
				return p, false
			}
		case 0x6a, 0x47, 0x92, 0x93, 0x94, 0x95, 0xa0, 0xa1, 0xa2, 0xa3:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			if !push(node(floatMapNode{op: op, a: stack[depth], b: stack[depth+1]})) {
				return p, false
			}
		case 0x2a, 0x2b:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || off != 0 || depth == 0 {
				return p, false
			}
			x := node(floatMapNode{op: op, a: stack[depth-1]})
			if x < 0 {
				return p, false
			}
			stack[depth-1] = x
		case 0x38, 0x39:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || off != 0 || depth < 2 || ns == len(stores) {
				return p, false
			}
			depth -= 2
			stores[ns] = [2]int{stack[depth], stack[depth+1]}
			ns++
			if ns == 1 {
				p.wide = op == 0x39
			} else if p.wide != (op == 0x39) {
				return p, false
			}
		case 0x0d:
			label, e := scan.U32()
			if e != nil || label != 0 || depth != 1 || ns == 0 {
				return p, false
			}
			end, e := scan.Byte()
			if e != nil || end != 0x0b {
				return p, false
			}
			increment := stack[0]
			if condition := p.nodes[increment]; condition.op == 0x47 {
				increment = condition.a
				limit := p.nodes[condition.b]
				if limit.op != 0x41 {
					increment = condition.b
					limit = p.nodes[condition.a]
				}
				if limit.op != 0x41 {
					return p, false
				}
				p.limit = uint32(limit.bits)
			}
			inc := p.nodes[increment]
			if inc.op != 0x6a {
				return p, false
			}
			local, stride := p.nodes[inc.a], p.nodes[inc.b]
			if local.op != 0x20 {
				local, stride = stride, local
			}
			size := uint32(4)
			if p.wide {
				size = 8
			}
			if local.op != 0x20 || f.localType[local.a] != mtI32 || stride.op != 0x41 || stride.bits != uint64(size)*uint64(ns) {
				return p, false
			}
			p.counter, p.step = local.a, uint32(stride.bits)
			if p.values[p.counter] != increment {
				return p, false
			}
			for i := 0; i < f.nLocals; i++ {
				if p.written[i] && initialRead[i] && i != p.counter {
					return p, false
				}
			}
			p.count, p.dest, p.result = n, stores[0][0], stores[0][1]
			if !p.proveAddresses(f.localType) {
				return p, false
			}
			if ns == 2 {
				visits := 0
				a, valid := p.affine(stores[1][0], f.localType, &visits)
				if !valid {
					return p, false
				}
				p.addresses[stores[1][0]] = a
			}
			var used [128]bool
			var mark func(int)
			mark = func(x int) {
				if used[x] {
					return
				}
				used[x] = true
				v := p.nodes[x]
				if v.op == 0x92 || v.op == 0x93 || v.op == 0x94 || v.op == 0x95 || v.op == 0xa0 || v.op == 0xa1 || v.op == 0xa2 || v.op == 0xa3 {
					mark(v.a)
					mark(v.b)
				}
			}
			for i := 0; i < ns; i++ {
				mark(stores[i][1])
			}
			for i := 0; i < n; i++ {
				if (p.nodes[i].op == 0x2a || p.nodes[i].op == 0x2b) && !used[i] {
					return p, false
				}
			}
			if ns == 2 && (!p.sameMap(stores[0][1], stores[1][1], size, 0) || !p.nextAddress(stores[0][0], stores[1][0], size)) {
				return p, false
			}
			return p, p.validExpression(p.result, 0)
		default:
			return p, false
		}
	}
	return p, false
}

func (p *floatMapPlan) affine(x int, types []machineType, visits *int) (floatMapAddress, bool) {
	(*visits)++
	if *visits > 128 {
		return floatMapAddress{}, false
	}
	v := p.nodes[x]
	if v.op == 0x20 && types[v.a] == mtI32 && v.a != p.counter && !p.written[v.a] {
		return floatMapAddress{base: v.a, invariant: true}, true
	}
	if v.op != 0x6a {
		return floatMapAddress{}, false
	}
	// Flatten addition modulo 2^32. Exactly one invariant base and one counter
	// are required; the runtime guard excludes wrap across the entire prefix.
	var base, counter, n int
	var bias uint32
	var walk func(int) bool
	walk = func(x int) bool {
		n++
		if n > 128 {
			return false
		}
		v := p.nodes[x]
		switch v.op {
		case 0x41:
			bias += uint32(v.bits)
		case 0x20:
			if types[v.a] != mtI32 {
				return false
			}
			if v.a == p.counter {
				counter++
			} else {
				if base != 0 || p.written[v.a] {
					return false
				}
				base = v.a + 1
			}
		case 0x6a:
			return walk(v.a) && walk(v.b)
		default:
			return false
		}
		return true
	}
	if !walk(x) || base == 0 || counter > 1 || bias > 1<<20 {
		return floatMapAddress{}, false
	}
	return floatMapAddress{base: base - 1, bias: bias, invariant: counter == 0}, true
}
func (p *floatMapPlan) proveAddresses(types []machineType) bool {
	visits := 0
	addr, ok := p.affine(p.dest, types, &visits)
	if !ok || addr.invariant {
		return false
	}
	p.addresses[p.dest] = addr
	for i := 0; i < p.count; i++ {
		if p.nodes[i].op == 0x2a || p.nodes[i].op == 0x2b {
			visits = 0
			a, ok := p.affine(p.nodes[i].a, types, &visits)
			if !ok {
				return false
			}
			p.addresses[i] = a
		}
	}
	return true
}
func (p *floatMapPlan) validExpression(x, depth int) bool {
	if depth > 8 {
		return false
	}
	v := p.nodes[x]
	if v.op == 0x20 {
		return !p.written[v.a] && p.localType[v.a] == mapFloatType(p.wide)
	}
	if v.op == 0x2b || v.op == 0x44 {
		return p.wide
	}
	if v.op == 0x2a || v.op == 0x43 {
		return !p.wide
	}
	if p.wide && v.op >= 0xa0 && v.op <= 0xa3 || !p.wide && v.op >= 0x92 && v.op <= 0x95 {
		return p.validExpression(v.a, depth+1) && p.validExpression(v.b, depth+1)
	}
	return false
}
func (p *floatMapPlan) nextAddress(x, y int, step uint32) bool {
	a, b := p.addresses[x], p.addresses[y]
	if a.invariant || b.invariant {
		return a.invariant && b.invariant && a.base == b.base && a.bias == b.bias
	}
	return a.base == b.base && uint64(a.bias)+uint64(step) == uint64(b.bias)
}
func (p *floatMapPlan) sameMap(x, y int, step uint32, depth int) bool {
	if depth > 8 {
		return false
	}
	a, b := p.nodes[x], p.nodes[y]
	if a.op != b.op {
		return false
	}
	if a.op == 0x2a || a.op == 0x2b {
		return p.nextAddress(x, y, step)
	}
	if a.op == 0x20 {
		return a.a == b.a
	}
	if a.op == 0x43 || a.op == 0x44 {
		return a.bits == b.bits
	}
	return p.sameMap(a.a, b.a, step, depth+1) && p.sameMap(a.b, b.b, step, depth+1)
}

func (f *fn) emitFloatMapLoop(source *wasm.Reader) {
	if !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	if f.nLocals > 32 || !f.hasFloatMapPrefix(source) {
		return
	}
	f.emitFloatMapBody(source)
}

// Every admitted map needs a typed float producer. Decode the prefix without
// reserving the large symbolic frame for ordinary integer/control loops.
func (f *fn) hasFloatMapPrefix(source *wasm.Reader) bool {
	scan := *source
	start := scan.Offset()
	var imm wasm.InstructionImmediate
	for scan.Offset()-start < 512 {
		op, e := scan.Byte()
		if e != nil {
			return false
		}
		switch op {
		case 0x2a, 0x2b, 0x43, 0x44:
			return true
		case 0x20:
			local, e := scan.U32()
			if e != nil || local >= uint32(f.nLocals) {
				return false
			}
			if f.localType[local].isFloat() {
				return true
			}
		case 0x21, 0x22, 0x41, 0x6a, 0x47:
			if f.classifier.ClassifyInto(&scan, op, &imm) != nil {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func (f *fn) emitFloatMapBody(source *wasm.Reader) {
	p, ok := f.inspectFloatMapLoop(source)
	if !ok {
		return
	}
	type group struct {
		base     int
		min, max uint32
		reg      Reg
	}
	var groups [3]group
	ng := 0
	add := func(a floatMapAddress) bool {
		for i := 0; i < ng; i++ {
			g := &groups[i]
			if g.base == a.base {
				if a.bias < g.min {
					g.min = a.bias
				}
				if a.bias > g.max {
					g.max = a.bias
				}
				return true
			}
		}
		if ng == len(groups) {
			return false
		}
		groups[ng] = group{base: a.base, min: a.bias, max: a.bias}
		ng++
		return true
	}
	var loads [16]int
	nl := 0
	var coefficients uint32
	var invariant [2]int
	nInvariant := 0
	var collect func(int) bool
	collect = func(x int) bool {
		v := p.nodes[x]
		if v.op == 0x2a || v.op == 0x2b {
			if p.addresses[x].invariant {
				for i := 0; i < nInvariant; i++ {
					if invariant[i] == x {
						return true
					}
				}
				if nInvariant == len(invariant) {
					return false
				}
				invariant[nInvariant] = x
				nInvariant++
				return true
			}
			if nl == len(loads) || !add(p.addresses[x]) {
				return false
			}
			loads[nl] = x
			nl++
			return true
		}
		if v.op == 0x20 {
			coefficients |= 1 << v.a
			return true
		}
		if v.op == 0x43 || v.op == 0x44 {
			return true
		}
		return collect(v.a) && collect(v.b)
	}
	if !add(p.addresses[p.dest]) || !collect(p.result) {
		return
	}
	var countConst func(int) int
	countConst = func(x int) int {
		v := p.nodes[x]
		if v.op == 0x20 || v.op == 0x43 || v.op == 0x44 {
			return 1
		}
		if v.op == 0x2a || v.op == 0x2b {
			if p.addresses[x].invariant {
				return 1
			}
			return 0
		}
		return countConst(v.a) + countConst(v.b)
	}
	if countConst(p.result) > 2 {
		return
	}
	// Reserve enough FP scratch for the deepest expression before emitting code.
	// Whole-function pins and outer immutable caches remain unavailable.
	available := 0
	blocked := f.blockedFRegs(0)
	for _, reg := range fpAllocRegs {
		if !blocked.has(reg) && f.fregUser[reg] == nil {
			available++
		}
	}
	var pressure func(int) int
	pressure = func(x int) int {
		v := p.nodes[x]
		if v.op == 0x2a || v.op == 0x2b || v.op == 0x20 || v.op == 0x43 || v.op == 0x44 {
			return 1
		}
		a, b := pressure(v.a), pressure(v.b)+1
		if b > a {
			return b
		}
		return a
	}
	if available < countConst(p.result)+pressure(p.result)+1 {
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
	for i := 0; i < ng; i++ {
		groups[i].reg = alloc()
	}
	counter, span, size := alloc(), alloc(), alloc()
	defer func() {
		f.pinned = oldPinned
		f.fpinned = oldFPinned
		for i := 0; i < nr; i++ {
			f.release(regs[i])
		}
	}()
	if counter == regNone || span == regNone || size == regNone {
		return
	}
	for i := 0; i < ng; i++ {
		if groups[i].reg == regNone {
			return
		}
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
	for local := 0; local < f.nLocals; local++ {
		if coefficients&(1<<local) != 0 {
			f.recoverLocal(local)
		}
	}
	for i := 0; i < nInvariant; i++ {
		f.recoverLocal(p.addresses[invariant[i]].base)
	}
	// Recover every borrowed local before any guard can bypass its reload.
	for i := 0; i < ng; i++ {
		read(groups[i].base, groups[i].reg)
	}
	var guards [64]int
	nGuards := 0
	guard := func(cc Cond) { guards[nGuards] = f.a.Bcond(cc); nGuards++ }
	f.a.MovImm64(span, uint64(p.limit))
	f.a.Sub32(span, span, counter)
	f.cmpImm(span, 16+p.step, false)
	guard(condB)
	f.a.TstImm32(span, p.step-1)
	guard(condNE)
	// Reject nonterminating modular trips: zero-limit countdowns must start
	// negative; positive fixed limits must be ahead of the unsigned counter.
	if p.limit == 0 {
		f.cmpImm(counter, 0, false)
		guard(condGE)
	} else {
		f.a.MovImm64(X16, uint64(p.limit))
		f.cmpRR(counter, X16, false)
		guard(condAE)
	}
	f.ld64(size, linMemReg, -int32(bdCurBytes))
	for i := 0; i < ng; i++ {
		g := &groups[i]
		f.a.Add32(g.reg, g.reg, counter)
		f.a.MovImm64(X16, uint64(g.min))
		f.a.Add32(g.reg, g.reg, X16)
		f.a.Add64(X16, g.reg, span)
		f.a.MovImm64(X17, uint64(g.max-g.min))
		f.a.Add64(X16, X16, X17)
		f.cmpRR(X16, size, true)
		guard(condA)
		f.a.MovImm64(X17, 1<<32)
		f.cmpRR(X16, X17, true)
		guard(condA)
	}
	locate := func(a floatMapAddress) (Reg, uint32) {
		for i := 0; i < ng; i++ {
			if groups[i].base == a.base {
				return groups[i].reg, a.bias - groups[i].min
			}
		}
		panic("arm64: missing float map address")
	}
	d, dOff := locate(p.addresses[p.dest])
	// An invariant memory coefficient must stay unchanged for the whole map.
	// Invalid words and even byte overlaps with the output retain scalar loads.
	for i := 0; i < nInvariant; i++ {
		iv := p.addresses[invariant[i]]
		read(iv.base, X16)
		f.a.MovImm64(X17, uint64(iv.bias))
		f.a.Add32(X16, X16, X17)
		width := uint32(4)
		if p.wide {
			width = 8
		}
		f.a.AddImm64(X17, X16, width)
		f.cmpRR(X17, size, true)
		guard(condA)
		f.a.MovImm64(X17, uint64(dOff))
		f.a.Add64(X17, d, X17)
		f.a.Sub64(X16, X16, X17)
		f.a.AddImm64(X16, X16, width-1)
		f.a.AddImm64(X17, span, width-1)
		f.cmpRR(X16, X17, true)
		guard(condB)
	}
	// Every input range must be disjoint from the output or exactly equal to it.
	// This also excludes inter-iteration stencil dependencies and byte overlaps.
	for i := 0; i < nl; i++ {
		s, off := locate(p.addresses[loads[i]])
		f.a.MovImm64(X16, uint64(dOff))
		f.a.Add64(X16, d, X16)
		f.a.MovImm64(X17, uint64(off))
		f.a.Add64(X17, s, X17)
		f.cmpRR(X16, X17, true)
		equal := f.a.Bcond(condE)
		above := f.a.Bcond(condA)
		f.a.Sub64(X16, X17, X16)
		f.cmpRR(X16, span, true)
		guard(condB)
		skip := f.a.Branch()
		f.patchBranch19(above, f.a.Len())
		f.a.Sub64(X16, X16, X17)
		f.cmpRR(X16, span, true)
		guard(condB)
		f.patchBranch19(equal, f.a.Len())
		f.patchBranch26(skip, f.a.Len())
	}
	for i := 0; i < ng; i++ {
		f.a.Add64(groups[i].reg, groups[i].reg, linMemReg)
	}
	var constants [8]struct {
		node int
		reg  Reg
	}
	nc := 0
	var vectors [24]Reg
	nv := 0
	vec := func() Reg { x := f.allocFReg(0); f.fpinned = f.fpinned.add(x); vectors[nv] = x; nv++; return x }
	var preload func(int)
	preload = func(x int) {
		v := p.nodes[x]
		if v.op == 0x20 || v.op == 0x43 || v.op == 0x44 || (v.op == 0x2a || v.op == 0x2b) && p.addresses[x].invariant {
			for i := 0; i < nc; i++ {
				if constants[i].node == x {
					return
				}
			}
			r := vec()
			if v.op == 0x20 {
				if reg, _, pin := f.pinReg(v.a); pin {
					f.a.FmovReg(r, reg, p.wide)
				} else if f.locals[v.a].state == lsConstZero {
					f.a.FmovFromGpr(r, ZR, p.wide)
				} else {
					f.a.LdrF(r, SP, f.localOff(v.a), p.wide)
				}
			} else if v.op == 0x2a || v.op == 0x2b {
				iv := p.addresses[x]
				read(iv.base, X16)
				f.a.MovImm64(X17, uint64(iv.bias))
				f.a.Add32(X16, X16, X17)
				f.a.Add64(X16, X16, linMemReg)
				f.a.LdrF(r, X16, 0, p.wide)
			} else {
				f.loadFConst(r, storage{kind: stConst, typ: mapFloatType(p.wide), cval: int64(v.bits)})
			}
			if p.wide {
				f.a.NeonDupD(r, r)
			} else {
				f.a.NeonDupS(r, r)
			}
			constants[nc].node = x
			constants[nc].reg = r
			nc++
			return
		}
		if v.op != 0x2a && v.op != 0x2b {
			preload(v.a)
			preload(v.b)
		}
	}
	// The bounded expression has at most eight levels. Count constants before
	// emitting anything that would need an unbounded register reservation.
	preload(p.result)
	var block int32
	var emit func(int) (Reg, bool)
	emit = func(x int) (Reg, bool) {
		v := p.nodes[x]
		if v.op == 0x20 || v.op == 0x43 || v.op == 0x44 || (v.op == 0x2a || v.op == 0x2b) && p.addresses[x].invariant {
			for i := 0; i < nc; i++ {
				if constants[i].node == x {
					return constants[i].reg, false
				}
			}
			panic("arm64: missing float map constant")
		}
		if v.op == 0x2a || v.op == 0x2b {
			base, off := locate(p.addresses[x])
			r := f.allocFReg(0)
			f.fpinned = f.fpinned.add(r)
			f.a.LdrQ(r, base, int32(off)+block)
			return r, true
		}
		a, ownA := emit(v.a)
		b, ownB := emit(v.b)
		out := a
		if !ownA {
			out = f.allocFReg(0)
			f.fpinned = f.fpinned.add(out)
		}
		switch v.op {
		case 0x92, 0xa0:
			f.a.NeonFadd(out, a, b, p.wide)
		case 0x93, 0xa1:
			f.a.NeonFsub(out, a, b, p.wide)
		case 0x94, 0xa2:
			f.a.NeonFmul(out, a, b, p.wide)
		case 0x95, 0xa3:
			f.a.NeonFdiv(out, a, b, p.wide)
		}
		if ownB {
			f.fpinned = f.fpinned.remove(b)
			f.releaseF(b)
		}
		return out, true
	}
	packet := func(width uint32, target int) {
		for block = 0; block < int32(width); block += 16 {
			value, owned := emit(p.result)
			f.a.StrQ(d, int32(dOff)+block, value)
			if owned {
				f.fpinned = f.fpinned.remove(value)
				f.releaseF(value)
			}
		}
		for i := 0; i < ng; i++ {
			f.a.AddImm64(groups[i].reg, groups[i].reg, width)
		}
		f.a.AddImm32(counter, counter, width)
		f.a.SubImm32(span, span, width)
		f.patchBranch26(f.a.Branch(), target)
	}
	// Four wider packets repay the extra code. Short trips use the compact
	// loop directly, and the original scalar packet remains observable at exit.
	f.cmpImm(span, 4*32+p.step, false)
	short := f.a.Bcond(condB)
	wideLoop := f.a.Len()
	f.cmpImm(span, 32+p.step, false)
	tail := f.a.Bcond(condB)
	packet(32, wideLoop)
	f.patchBranch19(short, f.a.Len())
	f.patchBranch19(tail, f.a.Len())
	loop := f.a.Len()
	f.cmpImm(span, 16+p.step, false)
	done := f.a.Bcond(condB)
	packet(16, loop)
	f.stats.peep("float-map-loop-unroll")
	f.patchBranch19(done, f.a.Len())
	if reg, _, pin := f.pinReg(p.counter); pin {
		f.a.MovReg32(reg, counter)
		f.markLocalDirty(p.counter)
	} else {
		f.st64(SP, f.localOff(p.counter), counter)
		f.locals[p.counter].state = lsMem
	}
	for i := 0; i < nGuards; i++ {
		f.patchBranch19(guards[i], f.a.Len())
	}
	for i := 0; i < nv; i++ {
		f.releaseF(vectors[i])
	}
	f.setFactsForLocal(p.counter, 0)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("float-map-loop-vector")
}
func mapFloatType(wide bool) machineType {
	if wide {
		return mtF64
	}
	return mtF32
}
