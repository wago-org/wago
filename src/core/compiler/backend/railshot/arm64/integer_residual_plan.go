//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var integerResidualEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_RESIDUAL") != "1"

const integerResidualWorkLimit = 4096

type residualLinearTerm struct {
	node        int
	coefficient uint32
}
type residualMap struct {
	terms    [8]residualLinearTerm
	n        int
	constant uint32
	divisor  uint32
}
type integerResidualPlan struct {
	nodes                         [256]shared.LoopExpression
	deps                          [256]uint32
	locals                        [32]shared.LoopLocal[machineType, int]
	entryRead                     [32]bool
	maps                          [4]residualMap
	used                          [256]bool
	uses                          [256]uint16
	n, localN, mapN               int
	counter, seed, hash           int // dense candidate-local slots
	seedStep, multiplier, divisor uint32
	work, limit                   int
}

func (p *integerResidualPlan) spend() bool {
	if p.work >= p.limit {
		return false
	}
	p.work++
	return true
}
func (p *integerResidualPlan) add(op byte, a, b int, value uint64, pc int) int {
	if p.n == len(p.nodes) || !p.spend() {
		return -1
	}
	id := p.n
	p.n++
	p.nodes[id] = shared.LoopExpression{Op: op, Type: wasm.I32, Left: a, Right: b, Bits: value, PC: uint32(pc)}
	if op != 0x20 && op != 0x41 {
		p.deps[id] = p.deps[a] | p.deps[b]
	}
	return id
}
func (p *integerResidualPlan) slot(index uint32) int {
	return shared.FindLoopLocal(p.locals[:p.localN], index)
}

// All source reads capture an owned expression index. Replacing a mutable cell
// cannot change earlier reads. No reader, allocator, or native state is changed.
func (f *fn) inspectIntegerResidual(source *wasm.Reader) (p integerResidualPlan, ok bool) {
	p.limit = integerResidualWorkLimit - int(f.integerResidualWork)
	defer func() {
		f.integerResidualWork = uint16(min(integerResidualWorkLimit, int(f.integerResidualWork)+p.work))
	}()
	r := *source
	start := r.Offset()
	var stack [24]int
	depth := 0
	push := func(v int) bool {
		if v < 0 || depth == len(stack) {
			return false
		}
		stack[depth] = v
		depth++
		return true
	}
	for ops := 0; ops < 256 && r.Offset()-start < 512 && p.spend(); ops++ {
		pc := r.Offset()
		op, e := r.Byte()
		if e != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			index, e := r.U32()
			if e != nil || index >= uint32(f.nLocals) || f.localType[index] != mtI32 {
				return p, false
			}
			slot := p.slot(index)
			if slot < 0 {
				if p.localN == len(p.locals) {
					return p, false
				}
				slot = p.localN
				id := p.add(0x20, int(index), 0, 0, pc)
				if id < 0 {
					return p, false
				}
				p.deps[id] = uint32(1) << slot
				p.locals[slot] = shared.LoopLocal[machineType, int]{Index: index, Type: mtI32, Initial: id, Value: id}
				p.localN++
			}
			cell := &p.locals[slot]
			if op == 0x20 {
				if cell.Value == cell.Initial {
					p.entryRead[slot] = true
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
			v, e := r.I32()
			if e != nil || !push(p.add(op, 0, 0, uint64(uint32(v)), pc)) {
				return p, false
			}
		case 0x6a, 0x6b, 0x6c, 0x6d, 0x71, 0x72, 0x73, 0x74, 0x76, 0x47:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			a, b := stack[depth], stack[depth+1]
			// Even overwritten/unused divisions must be nontrapping. Shift counts are
			// constants so the target recipe preserves Wasm's exact masked count.
			if op == 0x6d && (p.nodes[b].Op != 0x41 || int32(p.nodes[b].Bits) <= 0) {
				return p, false
			}
			if (op == 0x74 || op == 0x76) && p.nodes[b].Op != 0x41 {
				return p, false
			}
			if !push(p.add(op, a, b, 0, pc)) {
				return p, false
			}
		case 0x0d:
			label, e := r.U32()
			if e != nil || label != 0 || depth != 1 {
				return p, false
			}
			end, e := r.Byte()
			if e != nil || end != 0x0b || r.Offset()-start > 512 {
				return p, false
			}
			ok = p.finish(stack[0])
			return p, ok
		default:
			return p, false
		}
	}
	return p, false
}
func (p *integerResidualPlan) increment(root, initial int) (uint32, bool) {
	n := p.nodes[root]
	if n.Op != 0x6a && n.Op != 0x6b {
		return 0, false
	}
	a, b := n.Left, n.Right
	if n.Op == 0x6a && p.nodes[a].Op == 0x41 {
		a, b = b, a
	}
	if a != initial || p.nodes[b].Op != 0x41 {
		return 0, false
	}
	delta := uint32(p.nodes[b].Bits)
	if n.Op == 0x6b {
		delta = -delta
	}
	return delta, true
}
func (p *integerResidualPlan) finish(condition int) bool {
	p.counter, p.seed, p.hash = -1, -1, -1
	if n := p.nodes[condition]; n.Op == 0x47 {
		if p.nodes[n.Right].Op == 0x41 && p.nodes[n.Right].Bits == 0 {
			condition = n.Left
		} else if p.nodes[n.Left].Op == 0x41 && p.nodes[n.Left].Bits == 0 {
			condition = n.Right
		} else {
			return false
		}
	}
	for i, c := range p.locals[:p.localN] {
		if !p.spend() {
			return false
		}
		if c.Written && c.Value == condition {
			if d, ok := p.increment(c.Value, c.Initial); ok && d == ^uint32(0) {
				p.counter = i
			}
		}
	}
	if p.counter < 0 {
		return false
	}
	for i, c := range p.locals[:p.localN] {
		if !c.Written || !p.entryRead[i] || i == p.counter {
			continue
		}
		if d, ok := p.increment(c.Value, c.Initial); ok && d != 0 {
			if p.seed >= 0 {
				return false
			}
			p.seed, p.seedStep = i, d
		} else {
			if p.hash >= 0 {
				return false
			}
			p.hash = i
		}
	}
	if p.seed < 0 || p.hash < 0 {
		return false
	}
	root := p.locals[p.hash].Value
	initial := p.locals[p.hash].Initial
	bit := uint32(1) << p.hash
	var payload [4]int
	for root != initial {
		if !p.spend() || p.mapN == len(payload) {
			return false
		}
		n := p.nodes[root]
		if n.Op != 0x6c {
			return false
		}
		a, b := n.Left, n.Right
		if p.nodes[a].Op == 0x41 {
			a, b = b, a
		}
		if p.nodes[b].Op != 0x41 {
			return false
		}
		c := uint32(p.nodes[b].Bits)
		if c <= 1 || p.mapN > 0 && p.multiplier != c {
			return false
		}
		p.multiplier = c
		x := p.nodes[a]
		if x.Op != 0x73 {
			return false
		}
		a, b = x.Left, x.Right
		if p.deps[a]&bit == 0 {
			a, b = b, a
		}
		if p.deps[a]&bit == 0 || p.deps[b]&bit != 0 {
			return false
		}
		payload[p.mapN] = b
		p.mapN++
		root = a
	}
	// Peel order is outside-in; scalar residual execution must be inside-out.
	for i := 0; i < p.mapN; i++ {
		root = payload[p.mapN-1-i]
		m := &p.maps[i]
		m.divisor = 1
		if n := p.nodes[root]; n.Op == 0x6d {
			m.divisor = uint32(p.nodes[n.Right].Bits)
			root = n.Left
			if m.divisor != 1 {
				if p.divisor != 0 && p.divisor != m.divisor {
					return false
				}
				p.divisor = m.divisor
			}
		}
		if !p.linear(m, root, 1) {
			return false
		}
		for _, t := range m.terms[:m.n] {
			if t.coefficient != 0 {
				if p.deps[t.node]&(uint32(1)<<p.counter|uint32(1)<<p.hash) != 0 || !p.mark(t.node) {
					return false
				}
				p.uses[t.node]++
			}
		}
	}
	if p.mapN == 0 {
		return false
	}
	for i, c := range p.locals[:p.localN] {
		if c.Written && p.entryRead[i] && i != p.counter && i != p.seed && i != p.hash {
			return false
		}
	}
	return p.work <= p.limit
}
func (p *integerResidualPlan) linear(out *residualMap, x int, scale uint32) bool {
	if !p.spend() {
		return false
	}
	n := p.nodes[x]
	switch n.Op {
	case 0x41:
		out.constant += uint32(n.Bits) * scale
		return true
	case 0x6a:
		return p.linear(out, n.Left, scale) && p.linear(out, n.Right, scale)
	case 0x6b:
		return p.linear(out, n.Left, scale) && p.linear(out, n.Right, -scale)
	case 0x6c:
		a, b := n.Left, n.Right
		if p.nodes[a].Op == 0x41 {
			a, b = b, a
		}
		if p.nodes[b].Op == 0x41 {
			return p.linear(out, a, scale*uint32(p.nodes[b].Bits))
		}
	case 0x74:
		return p.linear(out, n.Left, scale<<(uint32(p.nodes[n.Right].Bits)&31))
	}
	for i := 0; i < out.n; i++ {
		if out.terms[i].node == x {
			out.terms[i].coefficient += scale
			return true
		}
	}
	if out.n == len(out.terms) {
		return false
	}
	out.terms[out.n] = residualLinearTerm{x, scale}
	out.n++
	return true
}
func (p *integerResidualPlan) mark(x int) bool {
	if !p.spend() {
		return false
	}
	if p.used[x] {
		return true
	}
	p.used[x] = true
	n := p.nodes[x]
	switch n.Op {
	case 0x20:
		return true
	case 0x41:
		return true
	case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x76:
		if !p.mark(n.Left) {
			return false
		}
		p.uses[n.Left]++
		if n.Op != 0x74 && n.Op != 0x76 {
			if !p.mark(n.Right) {
				return false
			}
			p.uses[n.Right]++
		}
		return true
	}
	return false
}
