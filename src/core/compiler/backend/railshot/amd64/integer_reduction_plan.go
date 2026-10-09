//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// This packet has no memory operations, calls, effects, or trapping arithmetic.
// i32 maps use the low dword of each qword. Reductions add modulo 2^64.
const (
	integerReductionMaxOps    = 128
	integerReductionMaxNodes  = 64
	integerReductionMaxBytes  = 768
	integerReductionMaxLocals = 12
)

type integerReductionNode struct {
	bits            uint64
	deps            uint16
	op, left, right uint8
	typ             machineType
	u32             bool // An i64 value proved to be in [0, UINT32_MAX].
}

type integerReductionLocal struct {
	index                          uint32
	initial, value                 uint8
	step                           uint32
	written, recurrence, reduction bool
}

type integerReductionPlan struct {
	nodes                                         [integerReductionMaxNodes + 1]integerReductionNode
	locals                                        [integerReductionMaxLocals]integerReductionLocal
	live, permanent                               [integerReductionMaxNodes + 1]bool
	register                                      [integerReductionMaxNodes + 1]uint8
	nodeN, localN, counter, reductions, registers uint8
}

func (p *integerReductionPlan) add(n integerReductionNode) uint8 {
	if n.op == 0x41 || n.op == 0x42 {
		for id := uint8(1); id <= p.nodeN; id++ {
			if p.nodes[id].op == n.op && p.nodes[id].bits == n.bits {
				return id
			}
		}
	}
	if p.nodeN == integerReductionMaxNodes {
		return 0
	}
	p.nodeN++
	p.nodes[p.nodeN] = n
	return p.nodeN
}

func (p *integerReductionPlan) local(index uint32, typ machineType) (uint8, bool) {
	for i := uint8(0); i < p.localN; i++ {
		if p.locals[i].index == index {
			return i, true
		}
	}
	if p.localN == integerReductionMaxLocals || (typ != mtI32 && typ != mtI64) {
		return 0, false
	}
	i := p.localN
	id := p.add(integerReductionNode{op: 0x20, bits: uint64(i), deps: 1 << i, typ: typ})
	if id == 0 {
		return 0, false
	}
	p.locals[i] = integerReductionLocal{index: index, initial: id, value: id}
	p.localN++
	return i, true
}

func inspectIntegerReductionLoop(r wasm.Reader, types []machineType, p *integerReductionPlan) bool {
	*p = integerReductionPlan{}
	n := r.BytesLeft()
	if n > integerReductionMaxBytes {
		n = integerReductionMaxBytes
	}
	data, _ := r.Bytes(n)
	r = wasm.ReaderFrom(data)
	var stack [16]uint8
	depth := 0
	for count := 0; count < integerReductionMaxOps; count++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		var id uint8
		switch op {
		case 0x20, 0x21, 0x22:
			index, err := r.U32()
			if err != nil || uint64(index) >= uint64(len(types)) {
				return false
			}
			i, ok := p.local(index, types[index])
			if !ok {
				return false
			}
			if op == 0x20 {
				id = p.locals[i].value
			} else {
				if depth == 0 || p.nodes[stack[depth-1]].typ != types[index] {
					return false
				}
				p.locals[i].value, p.locals[i].written = stack[depth-1], true
				if op == 0x21 {
					depth--
				}
				continue
			}
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return false
			}
			id = p.add(integerReductionNode{op: op, bits: uint64(uint32(v)), typ: mtI32})
		case 0x42:
			v, err := r.I64()
			if err != nil {
				return false
			}
			id = p.add(integerReductionNode{op: op, bits: uint64(v), typ: mtI64, u32: uint64(v) <= uint64(^uint32(0))})
		case 0xac, 0xad:
			if depth == 0 {
				return false
			}
			depth--
			a := stack[depth]
			if p.nodes[a].typ != mtI32 {
				return false
			}
			id = p.add(integerReductionNode{op: op, left: a, typ: mtI64, deps: p.nodes[a].deps, u32: op == 0xad})
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x7c, 0x7e:
			if depth < 2 {
				return false
			}
			depth -= 2
			a, b := stack[depth], stack[depth+1]
			x, y := p.nodes[a], p.nodes[b]
			typ := mtI32
			if op == 0x7c || op == 0x7e {
				typ = mtI64
			}
			if x.typ != typ || y.typ != typ {
				return false
			}
			if op >= 0x74 && op <= 0x76 && y.op != 0x41 {
				return false
			}
			if op == 0x7e && (!x.u32 || !y.u32) {
				return false
			}
			id = p.add(integerReductionNode{op: op, left: a, right: b, typ: typ, deps: x.deps | y.deps})
		case 0x0d:
			label, err := r.U32()
			if err != nil || label != 0 || depth != 1 {
				return false
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b {
				return false
			}
			return p.finish(stack[0])
		default:
			return false
		}
		if id == 0 || depth == len(stack) {
			return false
		}
		stack[depth] = id
		depth++
	}
	return false
}

func (p *integerReductionPlan) finish(predicate uint8) bool {
	var reductions uint16
	counter := -1
	for i := range p.locals[:p.localN] {
		l := &p.locals[i]
		if !l.written {
			continue
		}
		n := p.nodes[l.value]
		if n.typ == mtI32 && n.op == 0x6a {
			a, b := n.left, n.right
			if b == l.initial {
				a, b = b, a
			}
			if a == l.initial && p.nodes[b].op == 0x41 {
				l.step, l.recurrence = uint32(p.nodes[b].bits), true
				if l.value == predicate && l.step == ^uint32(0) {
					counter = i
				}
			}
		} else if n.typ == mtI64 && n.op == 0x7c {
			a, b := n.left, n.right
			if b == l.initial {
				a, b = b, a
			}
			if a == l.initial && p.nodes[b].deps&(1<<i) == 0 {
				l.reduction = true
				reductions |= 1 << i
				p.reductions++
			}
		}
	}
	if counter < 0 || p.reductions == 0 || p.reductions > 4 {
		return false
	}
	p.counter = uint8(counter)
	// Each reduction reads its old value only in its one final add. Maps may
	// read invariant inputs and affine i32 induction values, but no other old
	// version of a written local. This proves lane independence.
	var allowed uint16
	for i, l := range p.locals[:p.localN] {
		if !l.written || l.recurrence && i != counter {
			allowed |= 1 << i
		}
	}
	for i, l := range p.locals[:p.localN] {
		if !l.written || i == counter {
			continue
		}
		n := p.nodes[l.value]
		if l.reduction {
			a := n.left
			if a == l.initial {
				a = n.right
			}
			if p.nodes[a].deps & ^allowed != 0 {
				return false
			}
		} else if n.deps & ^allowed != 0 {
			return false
		}
		// Induction input homes advance at the end of each vector pair. A
		// direct local copy aliases that home and would expose the next pair's
		// input at exit. Keep these loops scalar until copies have separate homes.
		if !l.reduction && n.op == 0x20 && p.locals[n.bits].recurrence {
			return false
		}
		p.live[l.value] = true
	}
	for id := p.nodeN; id > 0; id-- {
		if !p.live[id] {
			continue
		}
		n := p.nodes[id]
		if n.deps&reductions != 0 && n.op != 0x20 {
			ok := false
			for _, l := range p.locals[:p.localN] {
				if l.reduction && l.value == id {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		if n.left != 0 {
			p.live[n.left] = true
		}
		if n.right != 0 && !(n.op >= 0x74 && n.op <= 0x76) {
			p.live[n.right] = true
		}
	}
	return p.allocate()
}

// Allocate the entire packet before emission. Constants and input versions
// have fixed homes. All other values use source-order last-use reuse. Reserve
// one additional XMM register for sign extension and exit lane extraction.
func (p *integerReductionPlan) allocate() bool {
	var uses [integerReductionMaxNodes + 1]uint8
	for id := uint8(1); id <= p.nodeN; id++ {
		if !p.live[id] {
			continue
		}
		n := p.nodes[id]
		if n.left != 0 {
			uses[n.left]++
		}
		if n.right != 0 && !(n.op >= 0x74 && n.op <= 0x76) {
			uses[n.right]++
		}
	}
	for i, l := range p.locals[:p.localN] {
		if l.written && uint8(i) != p.counter {
			uses[l.value]++
		}
	}
	var free uint16 = 0x7fff
	alloc := func() uint8 {
		for r := uint8(0); r < 15; r++ {
			if free&(1<<r) != 0 {
				free &^= 1 << r
				if r+1 > p.registers {
					p.registers = r + 1
				}
				return r
			}
		}
		return 0xff
	}
	for id := uint8(1); id <= p.nodeN; id++ {
		if p.live[id] && (p.nodes[id].op == 0x20 || p.nodes[id].op == 0x41 || p.nodes[id].op == 0x42) {
			p.permanent[id] = true
			p.register[id] = alloc()
			if p.register[id] == 0xff {
				return false
			}
		}
	}
	for id := uint8(1); id <= p.nodeN; id++ {
		if !p.live[id] || p.permanent[id] {
			continue
		}
		n := p.nodes[id]
		out := uint8(0xff)
		for _, l := range p.locals[:p.localN] {
			if l.reduction && l.value == id {
				out = p.register[l.initial]
				p.permanent[id] = true
			}
		}
		if out == 0xff && uses[n.left] == 1 && !p.permanent[n.left] {
			out = p.register[n.left]
		}
		if out == 0xff {
			out = alloc()
			if out == 0xff {
				return false
			}
		}
		p.register[id] = out
		for operand, source := range [2]uint8{n.left, n.right} {
			if source == 0 || operand == 1 && n.op >= 0x74 && n.op <= 0x76 {
				continue
			}
			uses[source]--
			if uses[source] == 0 && !p.permanent[source] && p.register[source] != out {
				free |= 1 << p.register[source]
			}
		}
		if uses[id] == 0 && !p.permanent[id] {
			free |= 1 << out
		}
	}
	p.registers++
	return true
}
