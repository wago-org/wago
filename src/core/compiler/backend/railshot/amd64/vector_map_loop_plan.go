//go:build amd64

package amd64

import (
	"encoding/binary"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The bounded planner retains source-ordered FP loads, arithmetic and stores.
// It supports affine i32 recurrences and scalar f64 local versions.
// Acceptance alone does not authorize machine emission: range, trip-count,
// register ownership and exact exit-state qualification remain separate.
const (
	regionLoopMaxOps    = 64
	regionLoopMaxBytes  = 512
	regionLoopMaxLocals = 8
	regionLoopMaxLoads  = 8
)

// op zero is an affine i32 value, calculated modulo 2^32. A load's bits field
// instead holds its widened memory immediate, never folded into that value.
type regionLoopNode struct {
	bits                  uint64
	pos                   uint32
	coeff                 [regionLoopMaxLocals]uint32
	op, left, right, uses uint8
}

type regionLoopLocal struct {
	index          uint32
	typ            machineType
	step           uint32
	initial, value uint8
	written        bool
	order          uint8
}

type regionLoopStore struct {
	offset         uint64
	pos            uint32
	address, value uint8
}

type regionLoopLoad struct {
	stride      uint32
	node, bytes uint8
}

type regionLoopPlan struct {
	nodes                        [regionLoopMaxOps + 1]regionLoopNode
	locals                       [regionLoopMaxLocals]regionLoopLocal
	stores                       [2]regionLoopStore
	loads                        [regionLoopMaxLoads]regionLoopLoad
	events                       [regionLoopMaxOps]uint8
	eventN                       uint8
	endPC                        int
	nodeN, localN, storeN, loadN uint8
	counter, limit, predicate    uint8
	inputMask                    uint8
	adjacent                     bool
	exitHigh                     uint8
}

func (p *regionLoopPlan) add(n regionLoopNode) uint8 {
	if p.nodeN == regionLoopMaxOps {
		return 0
	}
	if n.op == 0 {
		n.bits = uint64(uint32(n.bits))
		n.uses = 0
		for i, c := range n.coeff {
			if c != 0 {
				n.uses |= 1 << i
			}
		}
	}
	p.nodeN++
	p.nodes[p.nodeN] = n
	return p.nodeN
}

func (p *regionLoopPlan) local(index uint32, typ machineType) (uint8, bool) {
	for i := uint8(0); i < p.localN; i++ {
		if p.locals[i].index == index {
			return i, true
		}
	}
	if p.localN == regionLoopMaxLocals || (typ != mtI32 && typ != mtF64) {
		return 0, false
	}
	i := p.localN
	n := regionLoopNode{}
	if typ == mtI32 {
		n.coeff[i] = 1
	} else {
		n.op, n.bits, n.uses = 0x20, uint64(i), 1<<i
	}
	id := p.add(n)
	if id == 0 {
		return 0, false
	}
	p.locals[i] = regionLoopLocal{index: index, typ: typ, initial: id, value: id}
	p.localN++
	return i, true
}

func (p *regionLoopPlan) integer(op byte, a, b uint8) uint8 {
	x, y := p.nodes[a], p.nodes[b]
	if x.op != 0 || y.op != 0 {
		return 0
	}
	switch op {
	case 0x6a, 0x6b:
		if op == 0x6b {
			y.bits = uint64(-uint32(y.bits))
			for i := range y.coeff {
				y.coeff[i] = -y.coeff[i]
			}
		}
		x.bits = uint64(uint32(x.bits) + uint32(y.bits))
		for i := range x.coeff {
			x.coeff[i] += y.coeff[i]
		}
	case 0x6c, 0x74:
		if op == 0x74 {
			if y.uses != 0 {
				return 0
			}
			y.bits = uint64(uint32(1) << (uint32(y.bits) & 31))
		} else if y.uses != 0 {
			x, y = y, x
		}
		if y.uses != 0 {
			return 0
		}
		x.bits = uint64(uint32(x.bits) * uint32(y.bits))
		for i := range x.coeff {
			x.coeff[i] *= uint32(y.bits)
		}
	default:
		return 0
	}
	return p.add(x)
}

func regionFloat(n regionLoopNode) bool {
	return n.op == 0x20 || n.op == 0x44 || n.op == 0x2b || n.op >= 0xa0 && n.op <= 0xa3
}

// regionLoopReader bounds immediate decoding too. Unsupported variable-size
// instructions (for example br_table) must not scan beyond the packet budget
// before the planner can reject their opcode.
func regionLoopReader(r wasm.Reader) wasm.Reader {
	size := r.BytesLeft()
	if size > regionLoopMaxBytes {
		size = regionLoopMaxBytes
	}
	data, _ := r.Bytes(size)
	return wasm.ReaderFrom(data)
}

// inspectRegionLoop reads the body after an empty loop block type. The caller's
// reader is unchanged, including on rejection. No allocations or unbounded scan.
func inspectRegionLoop(r wasm.Reader, types []machineType, classifier wasm.ModuleInstructionClassifier, p *regionLoopPlan) bool {
	*p = regionLoopPlan{}
	start := r.Offset()
	r = regionLoopReader(r)
	var stack [16]uint8
	depth := 0
	for step := 0; step < regionLoopMaxOps && r.Offset() < regionLoopMaxBytes; step++ {
		position := uint32(start + r.Offset())
		op, err := r.Byte()
		if err != nil {
			return false
		}
		look := r
		var imm wasm.InstructionImmediate
		if classifier.ClassifyInto(&r, op, &imm) != nil || r.Offset() > regionLoopMaxBytes {
			return false
		}
		var id uint8
		switch op {
		case 0x20, 0x21, 0x22:
			if uint64(imm.Index) >= uint64(len(types)) {
				return false
			}
			i, ok := p.local(imm.Index, types[imm.Index])
			if !ok {
				return false
			}
			if op == 0x20 {
				id = p.locals[i].value
			} else {
				if depth == 0 || (types[imm.Index] == mtI32 && p.nodes[stack[depth-1]].op != 0) || (types[imm.Index] == mtF64 && !regionFloat(p.nodes[stack[depth-1]])) {
					return false
				}
				p.locals[i].value, p.locals[i].written = stack[depth-1], true
				p.locals[i].order = uint8(step + 1)
				if op == 0x21 {
					depth--
				}
				continue
			}
		case 0x41:
			x, err := look.I32()
			if err != nil {
				return false
			}
			id = p.add(regionLoopNode{bits: uint64(uint32(x))})
		case 0x44:
			x, err := look.Bytes(8)
			if err != nil {
				return false
			}
			id = p.add(regionLoopNode{op: op, bits: binary.LittleEndian.Uint64(x)})
		case 0x2b:
			if depth == 0 || imm.MemIndex != 0 || imm.MemOffset > uint64(^uint32(0)) {
				return false
			}
			depth--
			a := stack[depth]
			if p.nodes[a].op != 0 {
				return false
			}
			id = p.add(regionLoopNode{op: op, left: a, bits: imm.MemOffset, uses: p.nodes[a].uses})
		case 0x39:
			if depth < 2 || p.storeN == 2 || imm.MemIndex != 0 || imm.MemOffset > uint64(^uint32(0)) {
				return false
			}
			depth -= 2
			a, b := stack[depth], stack[depth+1]
			if p.nodes[a].op != 0 || !regionFloat(p.nodes[b]) {
				return false
			}
			p.stores[p.storeN] = regionLoopStore{offset: imm.MemOffset, pos: position, address: a, value: b}
			p.events[p.eventN] = 0x80 | p.storeN
			p.eventN++
			p.storeN++
			continue
		case 0x6a, 0x6b, 0x6c, 0x74, 0xa0, 0xa1, 0xa2, 0xa3, 0x47:
			if depth < 2 {
				return false
			}
			depth -= 2
			a, b := stack[depth], stack[depth+1]
			if op >= 0xa0 && op <= 0xa3 {
				if !regionFloat(p.nodes[a]) || !regionFloat(p.nodes[b]) {
					return false
				}
				id = p.add(regionLoopNode{op: op, left: a, right: b, uses: p.nodes[a].uses | p.nodes[b].uses})
			} else if op == 0x47 {
				if p.nodes[a].op != 0 || p.nodes[b].op != 0 {
					return false
				}
				id = p.add(regionLoopNode{op: op, left: a, right: b, uses: p.nodes[a].uses | p.nodes[b].uses})
			} else {
				id = p.integer(op, a, b)
			}
		case 0x0d:
			if imm.Index != 0 || depth != 1 || p.storeN == 0 || p.nodes[stack[0]].op != 0x47 {
				return false
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b || r.Offset() > regionLoopMaxBytes {
				return false
			}
			p.endPC, p.predicate = start+r.Offset(), stack[0]
			return p.finish()
		default:
			return false
		}
		if id == 0 || depth == len(stack) {
			return false
		}
		if op != 0x20 {
			p.nodes[id].pos = position
		}
		if op == 0x2b || op == 0x44 || op >= 0xa0 && op <= 0xa3 {
			p.events[p.eventN] = id
			p.eventN++
		}
		stack[depth] = id
		depth++
	}
	return false
}

func (p *regionLoopPlan) stride(id uint8) uint32 {
	var stride uint32
	for i, c := range p.nodes[id].coeff {
		stride += c * p.locals[i].step
	}
	return stride
}

func (p *regionLoopPlan) finish() bool {
	var live uint8
	for _, store := range p.stores[:p.storeN] {
		live |= p.nodes[store.address].uses | p.nodes[store.value].uses
	}
	live |= p.nodes[p.predicate].uses
	for _, l := range p.locals[:p.localN] {
		if l.written {
			live |= p.nodes[l.value].uses
		}
	}
	p.inputMask = live
	var changed uint8
	for i := uint8(0); i < p.localN; i++ {
		l := &p.locals[i]
		if !l.written || live&(1<<i) == 0 {
			continue
		}
		if l.typ == mtF64 {
			continue
		}
		n := p.nodes[l.value]
		if n.op != 0 || n.uses != 1<<i || n.coeff[i] != 1 || n.bits == 0 || n.bits >= 1<<31 {
			return false
		}
		l.step = uint32(n.bits)
		changed |= 1 << i
	}
	pred := p.nodes[p.predicate]
	found := false
	for _, edges := range [2][2]uint8{{pred.left, pred.right}, {pred.right, pred.left}} {
		n, limit := p.nodes[edges[0]], p.nodes[edges[1]]
		if limit.uses&changed != 0 {
			continue
		}
		for i := uint8(0); i < p.localN; i++ {
			if p.locals[i].step != 0 && n.uses == 1<<i && n.coeff[i] == 1 && n.bits == uint64(p.locals[i].step) {
				if found {
					return false
				}
				p.counter, p.limit, found = i, edges[1], true
			}
		}
	}
	if !found {
		return false
	}
	step := p.locals[p.counter].step
	if step&(step-1) != 0 {
		return false
	}
	// Affine streams must move forward monotonically. Runtime guards prove that
	// each full range succeeds without memory32 wrap before taking the fast path.
	for _, store := range p.stores[:p.storeN] {
		if p.stride(store.address) >= 1<<31 {
			return false
		}
	}
	for id := uint8(1); id <= p.nodeN; id++ {
		n := p.nodes[id]
		if n.op != 0x2b {
			continue
		}
		if p.loadN == regionLoopMaxLoads {
			return false
		}
		stride := p.stride(n.left)
		if stride >= 1<<31 {
			return false
		}
		p.loads[p.loadN] = regionLoopLoad{node: id, bytes: 8, stride: stride}
		p.loadN++
	}
	return p.loadN > 0
}

// Independent lane admission excludes all loop-carried floating values.
// Address streams must advance by exactly one f64, or be read-only invariants.
func (p *regionLoopPlan) independentLanes() bool {
	for i, l := range p.locals[:p.localN] {
		if l.typ == mtF64 && l.written && p.inputMask&(1<<i) != 0 {
			return false
		}
	}
	for _, s := range p.stores[:p.storeN] {
		if p.stride(s.address) != 8 {
			return false
		}
	}
	for _, l := range p.loads[:p.loadN] {
		if l.stride != 0 && l.stride != 8 {
			return false
		}
	}
	return true
}
