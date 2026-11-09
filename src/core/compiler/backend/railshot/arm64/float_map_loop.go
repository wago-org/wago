//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Nonzero-immediate maps retain a same-binary scalar control for qualification.
var floatMapScatterEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_SCATTER") != "1"

var floatMapOffsetsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_OFFSETS") != "1"

var floatMapLaneFPLoadsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_LANE_FP_LOADS") != "1"

var floatMapLaneOffsetsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_LANE_OFFSETS") != "1"

var floatMapInductionEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_INDUCTION") != "1"

var floatMapPacketsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_PACKETS") != "1"
var floatMapInvariantsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_INVARIANTS") != "1"
var floatMapLatchEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_LATCH") != "1"
var floatMapFourPacketsEnabled = os.Getenv("WAGO_ARM64_FLOAT_MAP_PACKET_COUNT") != "2"

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
	stride    uint32 // source byte stride divided by contiguous output stride; zero means one
	index     int    // source byte-index local plus one; zero keeps floating-map counter addressing
	bias      uint32
	offset    uint64 // widened memory immediate, added after dynamic i32 wrap
	invariant bool
}
type floatMapPlan struct {
	nodes                        [128]floatMapNode
	addresses                    [128]floatMapAddress
	invariant                    [128]bool
	locals                       [32]shared.LoopLocal[machineType, int]
	localN                       int
	counter, result, dest, count int
	step, limit, packet          uint32
	counterScale                 uint32 // physical source bytes per logical packet byte
	destOffset                   uint64
	wide                         bool
	integer                      bool
	normalizedInduction          bool
	work, workLimit              int
	counterStep, spanScale       uint32
	limitLocal                   int
	byteIndices                  uint32
	byteSteps                    [32]uint32
}

// Local identities stay in source coordinates; only the bounded cell table is
// dense. Expressions capture immutable node indices when a source local changes.
func (p *floatMapPlan) localSlot(index int) int {
	return shared.FindLoopLocal(p.locals[:p.localN], uint32(index))
}

func (p *floatMapPlan) localWritten(index int) bool {
	slot := p.localSlot(index)
	return slot >= 0 && p.locals[slot].Written
}

func (f *fn) inspectFloatMapLoop(source *wasm.Reader) (p floatMapPlan, ok bool) {
	p.integer = f.integerLoadMapProbe
	p.normalizedInduction = p.integer
	p.spanScale = 1
	p.counterScale = 1
	p.limitLocal = -1
	p.workLimit = integerLoadMapWorkLimit - int(f.integerLoadMapWork)
	scan := *source
	start := scan.Offset()
	var stack [24]int
	var initialRead [32]bool
	var stores [8]shared.LoopStore[int]
	depth, ns, n := 0, 0, 0
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
	local := func(index uint32) int {
		if slot := shared.FindLoopLocal(p.locals[:p.localN], index); slot >= 0 {
			return slot
		}
		if p.localN == len(p.locals) {
			return -1
		}
		id := node(floatMapNode{op: 0x20, a: int(index)})
		if id < 0 {
			return -1
		}
		slot := p.localN
		p.locals[slot] = shared.LoopLocal[machineType, int]{Index: index, Type: f.localType[index], Initial: id, Value: id}
		p.localN++
		return slot
	}
	for scan.Offset()-start < 512 {
		if !p.spend() {
			return p, false
		}
		pos := scan.Offset()
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
			slot := local(x)
			if slot < 0 {
				return p, false
			}
			cell := &p.locals[slot]
			if op == 0x20 {
				if cell.Value == cell.Initial {
					initialRead[slot] = true
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
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x47, 0x92, 0x93, 0x94, 0x95, 0xa0, 0xa1, 0xa2, 0xa3:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			if !push(node(floatMapNode{op: op, a: stack[depth], b: stack[depth+1]})) {
				return p, false
			}
		case 0x28, 0x2a, 0x2b:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || (off > 1<<20 || off != 0 && !floatMapOffsetsEnabled) || depth == 0 {
				return p, false
			}
			x := node(floatMapNode{op: op, a: stack[depth-1], bits: off})
			if x < 0 {
				return p, false
			}
			stack[depth-1] = x
		case 0x36, 0x38, 0x39:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || (off > 1<<20 || off != 0 && !floatMapOffsetsEnabled) || depth < 2 || (ns == len(stores) || ns == 2 && !floatMapPacketsEnabled) {
				return p, false
			}
			depth -= 2
			stores[ns] = shared.LoopStore[int]{Address: stack[depth], Value: stack[depth+1], Offset: off, PC: uint32(pos)}
			ns++
			if p.integer != (op == 0x36) {
				return p, false
			}
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
			if p.integer {
				if !p.finishIntegerInduction(stack[0], ns, initialRead[:]) {
					return p, false
				}
			} else if !p.finishFloatInduction(stack[0], ns, initialRead[:]) {
				if !floatMapInductionEnabled {
					return p, false
				}
				p.normalizedInduction = true
				if !p.finishIntegerInduction(stack[0], ns, initialRead[:]) {
					return p, false
				}
			}
			size := uint32(4)
			if p.wide {
				size = 8
			}
			p.count, p.dest, p.result = n, stores[0].Address, stores[0].Value
			p.destOffset = stores[0].Offset
			if !p.proveAddresses(f.localType) || p.addresses[p.dest].stride > 1 && p.counterScale <= 1 {
				return p, false
			}
			for i := 1; i < ns; i++ {
				visits := 0
				a, valid := p.affine(stores[i].Address, f.localType, &visits)
				if !valid {
					return p, false
				}
				a.offset = stores[i].Offset
				p.addresses[stores[i].Address] = a
			}
			if p.normalizedInduction && !p.integer {
				strided := false
				for i := 0; i < n; i++ {
					strided = strided || p.load(p.nodes[i].op) && p.addresses[i].stride > 1
				}
				if !strided {
					return p, false
				}
			}
			var used [128]bool
			var mark func(int)
			mark = func(x int) {
				if !p.spend() {
					return
				}
				if used[x] {
					return
				}
				used[x] = true
				v := p.nodes[x]
				if p.binary(v.op) {
					mark(v.a)
					mark(v.b)
				}
			}
			for i := 0; i < ns; i++ {
				mark(stores[i].Value)
			}
			for i := 0; i < n; i++ {
				if (p.nodes[i].op == 0x28 || p.nodes[i].op == 0x2a || p.nodes[i].op == 0x2b) && !used[i] {
					return p, false
				}
			}
			for i := 1; i < ns; i++ {
				distance := uint32(i) * size
				if !p.sameMap(stores[0].Value, stores[i].Value, distance, 0) || !p.nextAddress(stores[0].Address, stores[i].Address, distance) {
					return p, false
				}
			}
			if !p.validExpression(p.result, 0) {
				return p, false
			}
			if floatMapInvariantsEnabled {
				p.labelInvariants()
			}
			return p, true
		default:
			return p, false
		}
	}
	return p, false
}

func (p *floatMapPlan) affine(x int, types []machineType, visits *int) (floatMapAddress, bool) {
	if p.normalizedInduction {
		return p.integerAffine(x)
	}
	if !p.spend() {
		return floatMapAddress{}, false
	}
	(*visits)++
	if *visits > 128 {
		return floatMapAddress{}, false
	}
	v := p.nodes[x]
	if v.op == 0x20 && types[v.a] == mtI32 && v.a != p.counter && !p.localWritten(v.a) {
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
		if !p.spend() {
			return false
		}
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
				if base != 0 || p.localWritten(v.a) {
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
	stride := uint32(0)
	if counter != 0 && p.counterScale > 1 {
		stride = p.counterScale
	}
	return floatMapAddress{base: base - 1, bias: bias, stride: stride, invariant: counter == 0}, true
}
func (p *floatMapPlan) proveAddresses(types []machineType) bool {
	visits := 0
	addr, ok := p.affine(p.dest, types, &visits)
	if !ok || addr.invariant {
		return false
	}
	addr.offset = p.destOffset
	p.addresses[p.dest] = addr
	for i := 0; i < p.count; i++ {
		if p.load(p.nodes[i].op) {
			visits = 0
			a, ok := p.affine(p.nodes[i].a, types, &visits)
			if !ok {
				return false
			}
			a.offset = p.nodes[i].bits
			p.addresses[i] = a
		}
	}
	return true
}
func (p *floatMapPlan) validExpression(x, depth int) bool {
	if depth > 8 || !p.spend() {
		return false
	}
	v := p.nodes[x]
	if v.op == 0x20 {
		return !p.localWritten(v.a) && p.locals[p.localSlot(v.a)].Type == p.valueType()
	}
	if p.integer {
		if v.op == 0x28 || v.op == 0x41 {
			return true
		}
		if p.binary(v.op) {
			return p.validExpression(v.a, depth+1) && p.validExpression(v.b, depth+1)
		}
		return false
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

// Nodes are owned by one candidate and constructed after their operands.
// Memory invariance is usable only after the same guarded disjointness proof
// that protects the existing invariant-load coefficients.
func (p *floatMapPlan) labelInvariants() {
	for i := 0; i < p.count; i++ {
		v := p.nodes[i]
		switch v.op {
		case 0x41, 0x43, 0x44:
			p.invariant[i] = true
		case 0x20:
			p.invariant[i] = !p.localWritten(v.a) && p.locals[p.localSlot(v.a)].Type == p.valueType()
		case 0x28, 0x2a, 0x2b:
			p.invariant[i] = p.addresses[i].invariant
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x92, 0x93, 0x94, 0x95, 0xa0, 0xa1, 0xa2, 0xa3:
			p.invariant[i] = p.invariant[v.a] && p.invariant[v.b]
		}
	}
}

func (p *floatMapPlan) nextAddress(x, y int, step uint32) bool {
	a, b := p.addresses[x], p.addresses[y]
	if a.invariant || b.invariant {
		return a.invariant && b.invariant && a.base == b.base && a.index == b.index && a.bias == b.bias && a.offset == b.offset
	}
	return a.base == b.base && a.index == b.index && a.stride == b.stride && a.offset == b.offset && uint64(a.bias)+uint64(step)*uint64(max(a.stride, 1)) == uint64(b.bias)
}
func (p *floatMapPlan) sameMap(x, y int, step uint32, depth int) bool {
	if depth > 8 || !p.spend() {
		return false
	}
	a, b := p.nodes[x], p.nodes[y]
	if a.op != b.op {
		return false
	}
	if p.load(a.op) {
		return p.nextAddress(x, y, step)
	}
	if a.op == 0x20 {
		return a.a == b.a
	}
	if p.constant(a.op) {
		return a.bits == b.bits
	}
	return p.sameMap(a.a, b.a, step, depth+1) && p.sameMap(a.b, b.b, step, depth+1)
}

func (f *fn) emitFloatMapLoop(source *wasm.Reader) {
	if !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	if !f.hasFloatMapPrefix(source) {
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
	defer func() {
		if p.integer {
			f.integerLoadMapWork = uint16(min(integerLoadMapWorkLimit, int(f.integerLoadMapWork)+p.work))
		}
	}()
	if !ok {
		return
	}
	// Deeply nested copy-only loops often move a handful of words, and even
	// the cheap runtime admission check can dominate them. Keep their original
	// scalar path until a lower-overhead entry recipe is available. Arithmetic
	// maps and shallow copies retain the normal guarded trip policy.
	if p.integer && p.limitLocal >= 0 && p.nodes[p.result].op == 0x28 {
		parents := 0
		for i := len(f.ctrl) - 1; i >= 0; i-- {
			if !p.spend() {
				return
			}
			if f.ctrl[i].kind == cfLoop {
				parents++
				if parents == 2 {
					f.stats.peep("integer-load-map-nested-copy-reject")
					return
				}
			}
		}
	}
	minimumSpan := p.packet + p.step
	if p.normalizedInduction {
		minimumSpan = max(minimumSpan, 128)
		// The guarded positive-limit path requires counter < limit, so its
		// span cannot exceed limit, even when the entry value is unknown.
		if p.limitLocal < 0 && p.limit != 0 && uint64(p.limit)*uint64(p.spanScale) < uint64(minimumSpan) {
			return
		}
	}
	type group struct {
		base   int
		index  int
		stride uint32
		min    uint32
		max    uint64
		reg    Reg
	}
	var groups [3]group
	ng := 0
	add := func(a floatMapAddress) bool {
		for i := 0; i < ng; i++ {
			g := &groups[i]
			if g.base == a.base && g.index == a.index {
				if a.bias < g.min {
					g.min = a.bias
				}
				if end := uint64(a.bias) + a.offset; end > g.max {
					g.max = end
				}
				return true
			}
		}
		if ng == len(groups) {
			return false
		}
		groups[ng] = group{base: a.base, index: a.index, stride: a.stride, min: a.bias, max: uint64(a.bias) + a.offset}
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
		if !p.spend() {
			return false
		}
		v := p.nodes[x]
		if p.load(v.op) {
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
			coefficients |= 1 << p.localSlot(v.a)
			return true
		}
		if p.constant(v.op) {
			return true
		}
		return collect(v.a) && collect(v.b)
	}
	if !add(p.addresses[p.dest]) || !collect(p.result) {
		return
	}
	// Each carried byte index must have a guarded stream from which its next
	// source value can be recovered. Other carried state stays on scalar code.
	if p.normalizedInduction {
		var covered uint32
		for _, g := range groups[:ng] {
			if g.index > 0 {
				covered |= 1 << p.localSlot(g.index-1)
			}
		}
		if p.byteIndices&^covered != 0 {
			return
		}
	}
	if floatMapPacketDependenceEnabled && p.hasShortForwardDependence(loads[:nl]) {
		return
	}
	var countConst func(int) int
	countConst = func(x int) int {
		if !p.spend() {
			return 1000
		}
		v := p.nodes[x]
		if p.invariant[x] || v.op == 0x20 || p.constant(v.op) {
			return 1
		}
		if p.load(v.op) {
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
		if !p.spend() {
			return 1000
		}
		v := p.nodes[x]
		if p.load(v.op) || v.op == 0x20 || p.constant(v.op) {
			return 1
		}
		a, b := pressure(v.a), pressure(v.b)+1
		if b > a {
			return b
		}
		return a
	}
	if available < countConst(p.result)+pressure(p.result)+1 || p.normalizedInduction && p.work > p.workLimit {
		return
	}
	read := func(local int, dst Reg) {
		if local < 0 {
			f.a.MovImm64(dst, 0)
			return
		}
		if reg, _, pin := f.pinReg(local); pin {
			f.recoverLocal(local)
			f.a.MovReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld32(dst, SP, f.localOff(local))
		}
	}

	if p.normalizedInduction {
		// Admission must not evict an original stack value merely to execute a
		// scalar fallback. These are leases from the existing allocator's free
		// pool; the unchanged scalar loop retains its original owned locations.
		free := 0
		blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
		for _, reg := range gpAlloc {
			if f.regUser[reg] == nil && !blocked.has(reg) {
				free++
			}
		}
		if free < ng+3 {
			f.stats.peep("integer-load-map-gp-pressure-reject")
			return
		}
		// Recover all borrowed pins on both paths before a check can skip setup.
		f.recoverLocal(p.counter)
		if p.limitLocal >= 0 {
			f.recoverLocal(p.limitLocal)
		}
		for slot, cell := range p.locals[:p.localN] {
			if coefficients&(1<<slot) != 0 {
				f.recoverLocal(int(cell.Index))
			}
		}
		for _, x := range invariant[:nInvariant] {
			if p.addresses[x].base >= 0 {
				f.recoverLocal(p.addresses[x].base)
			}
		}
		for _, g := range groups[:ng] {
			if g.base >= 0 {
				f.recoverLocal(g.base)
			}
			if g.index > 0 {
				f.recoverLocal(g.index - 1)
			}
		}
		// Compare in source-counter units before leasing/initializing streams,
		// computing widened byte spans or checking ranges. A wrapped negative
		// difference may pass here but is rejected by the full eligibility proof.
		read(p.counter, X17)
		if p.limitLocal >= 0 {
			read(p.limitLocal, X16)
		} else {
			f.a.MovImm64(X16, uint64(p.limit))
		}
		f.a.Sub32(X17, X16, X17)
		f.cmpImm(X17, (minimumSpan+p.spanScale-1)/p.spanScale, false)
		scalar := f.a.Bcond(condB)
		defer func() { f.patchBranch19(scalar, f.a.Len()) }()
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

	read(p.counter, counter)
	if p.normalizedInduction && p.limitLocal >= 0 {
		f.recoverLocal(p.limitLocal)
	}
	for slot, cell := range p.locals[:p.localN] {
		if coefficients&(1<<slot) != 0 {
			f.recoverLocal(int(cell.Index))
		}
	}
	for i := 0; i < nInvariant; i++ {
		if base := p.addresses[invariant[i]].base; base >= 0 {
			f.recoverLocal(base)
		}
	}
	// Recover every borrowed local before any guard can bypass its reload.
	for i := 0; i < ng; i++ {
		if p.normalizedInduction {
			if groups[i].base >= 0 {
				f.recoverLocal(groups[i].base)
			}
			if groups[i].index > 0 {
				f.recoverLocal(groups[i].index - 1)
			}
		} else {
			read(groups[i].base, groups[i].reg)
		}
	}
	var guards [64]int
	nGuards := 0
	guard := func(cc Cond) { guards[nGuards] = f.a.Bcond(cc); nGuards++ }
	if p.counterScale > 1 {
		// A source counter in another residue class must retain the original
		// modular loop. Normalize only an exact multiple of the physical scale.
		f.a.MovImm64(X17, uint64(p.counterScale))
		f.a.Udiv32(span, counter, X17)
		f.a.Msub32(X16, span, X17, counter)
		f.cmpImm(X16, 0, false)
		guard(condNE)
		f.a.MovReg32(counter, span)
	}
	if p.normalizedInduction && p.limitLocal >= 0 {
		read(p.limitLocal, X16)
		f.cmpRR(counter, X16, false)
		guard(condAE)
		f.a.Sub32(span, X16, counter)
	} else {
		f.a.MovImm64(span, uint64(p.limit))
		f.a.Sub32(span, span, counter)
	}
	if p.spanScale > 1 {
		shift := uint8(0)
		for scale := p.spanScale; scale > 1; scale >>= 1 {
			shift++
		}
		f.a.LslImm(span, span, shift, false)
		f.a.MovImm64(X16, 1<<32-1)
		f.cmpRR(span, X16, true)
		guard(condA)
	}
	f.cmpImm(span, minimumSpan, false)
	guard(condB)
	if p.normalizedInduction {
		for i := 0; i < ng; i++ {
			read(groups[i].base, groups[i].reg)
		}
	}
	if p.step&(p.step-1) == 0 {
		f.a.TstImm32(span, p.step-1)
	} else {
		// A source packet can contain an odd number of scalar lanes. A
		// nontrapping remainder guard retains the original modular loop when
		// its fixed limit is not reachable before wrapping.
		f.a.MovImm64(X17, uint64(p.step))
		f.a.Udiv32(X16, span, X17)
		f.a.Msub32(X16, X16, X17, span)
		f.cmpImm(X16, 0, false)
	}
	guard(condNE)
	// Reject nonterminating modular trips: zero-limit countdowns must start
	// negative; positive fixed limits must be ahead of the unsigned counter.
	if p.normalizedInduction && p.limitLocal >= 0 {
		// Dynamic limits were checked before computing their widened byte span.
	} else if p.limit == 0 {
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
		if !p.normalizedInduction || g.index == p.counter+1 {
			if p.counterScale > 1 {
				f.a.MovImm32(X17, int32(p.counterScale))
				f.a.Mul32(X16, counter, X17)
				f.a.Add32(g.reg, g.reg, X16)
			} else {
				f.a.Add32(g.reg, g.reg, counter)
			}
		} else if g.index > 0 {
			read(g.index-1, X16)
			f.a.Add32(g.reg, g.reg, X16)
		}
		f.floatMapOffset(g.reg, g.reg, uint64(g.min), false, X16)
		if g.stride > 1 {
			f.a.MovImm64(X17, uint64(g.stride))
			f.a.Mul64(X16, span, X17)
			f.a.Add64(X16, g.reg, X16)
		} else {
			f.a.Add64(X16, g.reg, span)
		}
		f.floatMapOffset(X16, X16, g.max-uint64(g.min), true, X17)
		f.cmpRR(X16, size, true)
		guard(condA)
		f.a.MovImm64(X17, 1<<32)
		f.cmpRR(X16, X17, true)
		guard(condA)
	}
	locate := func(a floatMapAddress) (Reg, uint32) {
		for i := 0; i < ng; i++ {
			if groups[i].base == a.base && groups[i].index == a.index {
				return groups[i].reg, uint32(uint64(a.bias) - uint64(groups[i].min) + a.offset)
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
		f.floatMapOffset(X16, X16, uint64(iv.bias), false, X17)
		if iv.offset != 0 {
			f.floatMapOffset(X16, X16, iv.offset, true, X17)
		}
		width := uint32(4)
		if p.wide {
			width = 8
		}
		f.a.AddImm64(X17, X16, width)
		f.cmpRR(X17, size, true)
		guard(condA)
		f.floatMapOffset(X17, d, uint64(dOff), true, X17)
		f.a.Sub64(X16, X16, X17)
		f.a.AddImm64(X16, X16, width-1)
		if p.counterScale > 1 {
			f.a.MovImm64(X17, uint64(p.counterScale))
			f.a.Mul64(X17, span, X17)
			f.a.AddImm64(X17, X17, width-1)
		} else {
			f.a.AddImm64(X17, span, width-1)
		}
		f.cmpRR(X16, X17, true)
		guard(condB)
	}
	// Scalar stores and native Q stores both advance in source order. Only
	// forward dependences within one Q group need scalar execution; longer
	// dependences remain ordered between groups. Invariant memory above still
	// requires whole-loop disjointness because its value is broadcast.
	var checked [16]struct {
		base   Reg
		offset uint32
	}
	nChecked := 0
	for i := 0; i < nl; i++ {
		s, off := locate(p.addresses[loads[i]])
		if p.counterScale > 1 {
			width := uint32(4)
			if p.wide {
				width = 8
			}
			f.floatMapScatterAlias(d, s, dOff, off, span, size, p.counterScale, width, guard)
			continue
		}
		if stride := p.addresses[loads[i]].stride; stride > 1 {
			// A gather changes the lane address spacing. Require disjoint whole
			// source/output ranges rather than reusing the unit-stride proof.
			f.floatMapOffset(X16, d, uint64(dOff), true, X16)
			f.floatMapOffset(X17, s, uint64(off), true, X17)
			f.cmpRR(X16, X17, true)
			above := f.a.Bcond(condA)
			f.a.Sub64(X16, X17, X16)
			f.cmpRR(X16, span, true)
			guard(condB)
			skip := f.a.Branch()
			f.patchBranch19(above, f.a.Len())
			f.a.Sub64(X16, X16, X17)
			f.a.MovImm64(X17, uint64(stride))
			f.a.Mul64(X17, span, X17)
			f.cmpRR(X16, X17, true)
			guard(condB)
			f.patchBranch26(skip, f.a.Len())
			continue
		}
		if floatMapGuardSelectionEnabled {
			duplicate := false
			for _, a := range checked[:nChecked] {
				if a.base == s && a.offset == off {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			checked[nChecked].base, checked[nChecked].offset = s, off
			nChecked++
			// The range checks above exclude address wrap. Equal symbolic
			// bases then differ by this exact widened byte distance.
			if s == d {
				if floatMapPacketDependenceEnabled {
					continue
				}
				if off != dOff {
					distance := off - dOff
					if off < dOff {
						distance = dOff - off
					}
					f.cmpImm(span, distance, false)
					guard(condA)
				}
				continue
			}
		}
		f.floatMapOffset(X16, d, uint64(dOff), true, X16)
		f.floatMapOffset(X17, s, uint64(off), true, X17)
		if floatMapPacketDependenceEnabled {
			if floatMapDirectionGuardEnabled {
				// Equal or backward output addresses introduce no forward
				// dependence. Keep their cheap branch around the subtraction.
				f.cmpRR(X16, X17, true)
				safe := f.a.Bcond(condBE)
				f.a.Sub64(X16, X16, X17)
				f.cmpImm(X16, floatMapVectorBytes, true)
				guard(condB)
				f.patchBranch19(safe, f.a.Len())
			} else {
				// Same proof in a branchless direction test, retained as a
				// same-binary cost control for qualification.
				f.a.Sub64(X16, X16, X17)
				f.a.SubImm64(X16, X16, 1)
				f.cmpImm(X16, floatMapVectorBytes-1, true)
				guard(condB)
			}
			continue
		}
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

	emitOp := func(op byte, out, a, b Reg) {
		switch op {
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
		case 0x92, 0xa0:
			f.a.NeonFadd(out, a, b, p.wide)
		case 0x93, 0xa1:
			f.a.NeonFsub(out, a, b, p.wide)
		case 0x94, 0xa2:
			f.a.NeonFmul(out, a, b, p.wide)
		case 0x95, 0xa3:
			f.a.NeonFdiv(out, a, b, p.wide)
		}
	}
	releaseTemp := func(r Reg) { f.fpinned = f.fpinned.remove(r); f.releaseF(r) }
	var invariantValue func(int) Reg
	invariantValue = func(x int) Reg {
		v := p.nodes[x]
		if p.binary(v.op) {
			a := invariantValue(v.a)
			b := invariantValue(v.b)
			emitOp(v.op, a, a, b)
			releaseTemp(b)
			return a
		}
		r := f.allocFReg(0)
		f.fpinned = f.fpinned.add(r)

		if v.op == 0x20 && p.integer {
			read(v.a, X16)
			f.a.FmovFromGpr(r, X16, false)
		} else if v.op == 0x20 {
			if reg, _, pin := f.pinReg(v.a); pin {
				f.a.FmovReg(r, reg, p.wide)
			} else if f.locals[v.a].state == lsConstZero {
				f.a.FmovFromGpr(r, ZR, p.wide)
			} else {
				f.a.LdrF(r, SP, f.localOff(v.a), p.wide)
			}
		} else if p.load(v.op) {
			iv := p.addresses[x]
			read(iv.base, X16)
			f.floatMapOffset(X16, X16, uint64(iv.bias), false, X17)
			if iv.offset != 0 {
				f.floatMapOffset(X16, X16, iv.offset, true, X17)
			}
			f.a.Add64(X16, X16, linMemReg)
			f.a.LdrF(r, X16, 0, p.wide)
		} else if p.integer {
			f.a.MovImm64(X16, uint64(uint32(v.bits)))
			f.a.FmovFromGpr(r, X16, false)
		} else {
			f.loadFConst(r, storage{kind: stConst, typ: mapFloatType(p.wide), cval: int64(v.bits)})
		}
		if p.wide {
			f.a.NeonDupD(r, r)
		} else {
			f.a.NeonDupS(r, r)
		}
		return r
	}
	var preload func(int)
	preload = func(x int) {
		v := p.nodes[x]
		if p.invariant[x] || v.op == 0x20 || p.constant(v.op) || (p.load(v.op)) && p.addresses[x].invariant {
			for i := 0; i < nc; i++ {
				if constants[i].node == x {
					return
				}
			}
			r := invariantValue(x)
			vectors[nv] = r
			nv++
			constants[nc].node = x
			constants[nc].reg = r
			nc++
			return
		}
		if !p.load(v.op) {
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
		if p.invariant[x] || v.op == 0x20 || p.constant(v.op) || (p.load(v.op)) && p.addresses[x].invariant {
			for i := 0; i < nc; i++ {
				if constants[i].node == x {
					return constants[i].reg, false
				}
			}
			panic("arm64: missing float map constant")
		}
		if p.load(v.op) {
			base, off := locate(p.addresses[x])
			r := f.allocFReg(0)
			f.fpinned = f.fpinned.add(r)
			if stride := p.addresses[x].stride; stride > 1 {
				size := uint32(4)
				if p.wide {
					size = 8
				}
				for lane := uint32(0); lane < 16/size; lane++ {
					address := uint64(off) + (uint64(block)+uint64(lane*size))*uint64(stride)
					if lane == 0 && floatMapLaneFPLoadsEnabled {
						// The first scalar lane initializes the vector directly;
						// later lane inserts never need its temporary GP copy.
						if floatMapLaneOffsetsEnabled && address%uint64(size) == 0 && address/uint64(size) <= 0xfff {
							f.a.LdrF(r, base, int32(address), p.wide)
						} else {
							f.floatMapOffset(X17, base, address, true, X16)
							f.a.LdrF(r, X17, 0, p.wide)
						}
						continue
					}
					if floatMapLaneOffsetsEnabled && address%uint64(size) == 0 && address/uint64(size) <= 0xfff {
						// The guarded exact-width load can use its native scaled
						// displacement without materializing a second address.
						if p.wide {
							f.a.Load64(X16, base, uint32(address))
						} else {
							f.a.Load32(X16, base, uint32(address))
						}
					} else {
						f.floatMapOffset(X17, base, address, true, X16)
						if p.wide {
							f.ld64(X16, X17, 0)
						} else {
							f.ld32(X16, X17, 0)
						}
					}
					if lane == 0 {
						f.a.FmovFromGpr(r, X16, p.wide)
					} else if p.wide {
						f.a.NeonInsD(r, X16, byte(lane))
					} else {
						f.a.NeonInsS(r, X16, byte(lane))
					}
				}
			} else {
				f.a.LdrQ(r, base, int32(off)+block)
			}
			return r, true
		}
		a, ownA := emit(v.a)
		b, ownB := emit(v.b)
		out := a
		if !ownA {
			out = f.allocFReg(0)
			f.fpinned = f.fpinned.add(out)
		}
		emitOp(v.op, out, a, b)
		if ownB {
			f.fpinned = f.fpinned.remove(b)
			f.releaseF(b)
		}
		return out, true
	}
	packet := func(width uint32) {
		for block = 0; block < int32(width); block += floatMapVectorBytes {
			value, owned := emit(p.result)
			if p.counterScale > 1 {
				width := uint32(4)
				if p.wide {
					width = 8
				}
				for lane := uint32(0); lane < 16/width; lane++ {
					address := uint64(dOff) + (uint64(block)+uint64(lane*width))*uint64(p.counterScale)
					base, disp := d, int32(0)
					if address%uint64(width) == 0 && address/uint64(width) <= 0xfff {
						disp = int32(address)
					} else {
						f.floatMapOffset(X17, d, address, true, X16)
						base = X17
					}
					if lane == 0 {
						f.a.StrF(base, disp, value, p.wide)
					} else if p.wide {
						f.a.NeonUmovD(X16, value, byte(lane))
						f.a.Store64(X16, base, uint32(disp))
					} else {
						f.a.NeonUmovS(X16, value, byte(lane))
						f.a.Store32(X16, base, uint32(disp))
					}
				}
			} else {
				f.a.StrQ(d, int32(dOff)+block, value)
			}
			if owned {
				f.fpinned = f.fpinned.remove(value)
				f.releaseF(value)
			}
		}
		for i := 0; i < ng; i++ {
			if groups[i].stride > 1 {
				f.floatMapOffset(groups[i].reg, groups[i].reg, uint64(width)*uint64(groups[i].stride), true, X16)
			} else {
				f.a.AddImm64(groups[i].reg, groups[i].reg, width)
			}
		}
		f.a.AddImm32(counter, counter, width/p.spanScale)
		f.a.SubImm32(span, span, width)
	}
	// Choose a small native packet menu independently of the source unroll.
	// Larger source packets already have a bounded replicated body.
	wideWidth := 2 * p.packet
	if floatMapFourPacketsEnabled && p.packet == floatMapVectorBytes {
		wideWidth = 4 * p.packet
	}
	f.cmpImm(span, 4*wideWidth+p.step, false)
	short := f.a.Bcond(condB)
	if floatMapLatchEnabled {
		wideLoop := f.a.Len()
		packet(wideWidth)
		f.cmpImm(span, wideWidth+p.step, false)
		f.patchBranch19(f.a.Bcond(condAE), wideLoop)
		f.patchBranch19(short, f.a.Len())
		f.cmpImm(span, p.packet+p.step, false)
		done := f.a.Bcond(condB)
		compactLoop := f.a.Len()
		packet(p.packet)
		f.cmpImm(span, p.packet+p.step, false)
		f.patchBranch19(f.a.Bcond(condAE), compactLoop)
		f.patchBranch19(done, f.a.Len())
	} else {
		wideLoop := f.a.Len()
		f.cmpImm(span, wideWidth+p.step, false)
		tail := f.a.Bcond(condB)
		packet(wideWidth)
		f.patchBranch26(f.a.Branch(), wideLoop)
		f.patchBranch19(short, f.a.Len())
		f.patchBranch19(tail, f.a.Len())
		loop := f.a.Len()
		f.cmpImm(span, p.packet+p.step, false)
		done := f.a.Bcond(condB)
		packet(p.packet)
		f.patchBranch26(f.a.Branch(), loop)
		f.patchBranch19(done, f.a.Len())
	}
	f.stats.peep("float-map-loop-unroll")
	if p.normalizedInduction {
		var published uint32
		for _, g := range groups[:ng] {
			if g.index == 0 {
				continue
			}
			local := g.index - 1
			slot := p.localSlot(local)
			bit := uint32(1) << slot
			if p.byteIndices&bit == 0 || published&bit != 0 {
				continue
			}
			published |= bit
			// g.reg is hostMemory + wrap(base + oldIndex + min) + bytesDone.
			// Range guards excluded wrap during the prefix. Recover the exact
			// modular source index before the original scalar packet resumes.
			f.a.Sub64(X16, g.reg, linMemReg)
			f.a.MovImm64(X17, uint64(g.min))
			f.a.Sub32(X16, X16, X17)
			read(g.base, X17)
			f.a.Sub32(X16, X16, X17)
			if reg, _, pin := f.pinReg(local); pin {
				f.a.MovReg32(reg, X16)
				f.markLocalDirty(local)
			} else {
				f.st64(SP, f.localOff(local), X16)
				f.locals[local].state = lsMem
			}
			f.setFactsForLocal(local, 0)
		}
	}
	if p.counterScale > 1 {
		f.a.MovImm32(X16, int32(p.counterScale))
		f.a.Mul32(counter, counter, X16)
		f.stats.peep("float-map-scatter-vector")
	}
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
	if p.integer {
		f.stats.peep("integer-load-map-vector")
	} else {
		f.stats.peep("float-map-loop-vector")
	}
}
func mapFloatType(wide bool) machineType {
	if wide {
		return mtF64
	}
	return mtF32
}

// Each native packet must end on both a vector boundary and an original
// source-iteration boundary. This leaves every final changed local to the
// original scalar packet, including loops unrolled by an odd number of lanes.
func floatMapPacketWidth(step uint32) uint32 {
	if step == 0 || step > 64 {
		return 0
	}
	a, b := uint32(floatMapVectorBytes), step
	for b != 0 {
		a, b = b, a%b
	}
	width := floatMapVectorBytes / a * step
	// Bound duplicated native recipes independently of the source scan budget.
	if width > 80 {
		return 0
	}
	return width
}

// Preserve the original fixed byte-counter recipe before trying broader induction.
func (p *floatMapPlan) finishFloatInduction(condition, ns int, initialRead []bool) bool {
	increment := condition
	if condition := p.nodes[increment]; condition.op == 0x47 {
		increment = condition.a
		limit := p.nodes[condition.b]
		if limit.op != 0x41 {
			increment = condition.b
			limit = p.nodes[condition.a]
		}
		if limit.op != 0x41 {
			return false
		}
		p.limit = uint32(limit.bits)
	}
	inc := p.nodes[increment]
	if inc.op != 0x6a {
		return false
	}
	local, stride := p.nodes[inc.a], p.nodes[inc.b]
	if local.op != 0x20 {
		local, stride = stride, local
	}
	size := uint32(4)
	if p.wide {
		size = 8
	}
	if local.op != 0x20 || p.locals[p.localSlot(local.a)].Type != mtI32 || stride.op != 0x41 {
		return false
	}
	step := size * uint32(ns)
	scale := uint32(1)
	if stride.bits != uint64(step) {
		if !floatMapScatterEnabled || step == 0 || stride.bits > 1<<24 || stride.bits%uint64(step) != 0 {
			return false
		}
		scale = uint32(stride.bits / uint64(step))
		if scale <= 1 || scale > 1<<16 || p.limit == 0 || p.limit%scale != 0 {
			return false
		}
	}
	p.counter, p.step = local.a, step
	p.packet = floatMapPacketWidth(p.step)
	if p.packet == 0 {
		return false
	}
	if p.locals[p.localSlot(p.counter)].Value != increment {
		return false
	}
	for i, cell := range p.locals[:p.localN] {
		if cell.Written && initialRead[i] && int(cell.Index) != p.counter {
			return false
		}
	}
	p.counterScale = scale
	p.limit /= scale
	return true
}
