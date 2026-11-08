//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type maxSumAddress struct {
	locals  [2]int
	n       int
	bias    uint32
	pointer int
	stride  uint32
}
type maxSumPlan struct {
	streams                  [2]maxSumAddress
	counter, limit, acc, dst int
	limitConst               uint32
	constantLimit            bool
	locals                   [32]shared.LoopLocal[machineType, int]
	localN                   int
}

// This candidate owns one original iteration: mutable source cells, immutable
// expression records and two ordered stores. No function graph is retained.
func (f *fn) inspectMaxSumLoop(source *wasm.Reader) (p maxSumPlan, ok bool) {
	var nodes [128]shared.LoopExpression
	var condition [128]int
	var entryRead [32]bool
	var stack [24]int
	var stores [2][2]int
	var loads [4]int
	n, depth, ns, nl := 0, 0, 0, 0
	spend := func() bool {
		if f.integerResidualWork >= integerResidualWorkLimit {
			return false
		}
		f.integerResidualWork++
		return true
	}
	add := func(op byte, a, b int, bits uint64, pc int) int {
		if n == len(nodes) || !spend() {
			return -1
		}
		i := n
		n++
		nodes[i] = shared.LoopExpression{Op: op, Type: wasm.I32, Left: a, Right: b, Bits: bits, PC: uint32(pc)}
		return i
	}
	push := func(v int) bool {
		if v < 0 || depth == len(stack) {
			return false
		}
		stack[depth] = v
		depth++
		return true
	}
	r := *source
	start := r.Offset()
	for ops := 0; ops < 192 && r.Offset()-start < 512 && spend(); ops++ {
		pc := r.Offset()
		op, err := r.Byte()
		if err != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			index, e := r.U32()
			if e != nil || index >= uint32(f.nLocals) || f.localType[index] != mtI32 {
				return p, false
			}
			slot := shared.FindLoopLocal(p.locals[:p.localN], index)
			if slot < 0 {
				if p.localN == len(p.locals) {
					return p, false
				}
				slot = p.localN
				p.localN++
				id := add(0x20, int(index), 0, 0, pc)
				if id < 0 {
					return p, false
				}
				p.locals[slot] = shared.LoopLocal[machineType, int]{Index: index, Type: mtI32, Initial: id, Value: id}
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
			v, e := r.I32()
			if e != nil || !push(add(op, 0, 0, uint64(uint32(v)), pc)) {
				return p, false
			}
		case 0x6a, 0x47, 0x48, 0x4a:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			if !push(add(op, stack[depth], stack[depth+1], 0, pc)) {
				return p, false
			}
		case 0x1b:
			if depth < 3 {
				return p, false
			}
			depth -= 3
			id := add(op, stack[depth], stack[depth+1], 0, pc)
			if id < 0 {
				return p, false
			}
			condition[id] = stack[depth+2]
			if !push(id) {
				return p, false
			}
		case 0x28, 0x36:
			mem, off, e := f.readMemArg(&r)
			if e != nil || mem != 0 || off != 0 {
				return p, false
			}
			if op == 0x28 {
				if depth == 0 || nl == len(loads) {
					return p, false
				}
				id := add(op, stack[depth-1], 0, 0, pc)
				if id < 0 {
					return p, false
				}
				stack[depth-1] = id
				loads[nl] = id
				nl++
			} else {
				if depth < 2 || ns == len(stores) {
					return p, false
				}
				depth -= 2
				stores[ns] = [2]int{stack[depth], stack[depth+1]}
				ns++
			}
		case 0x0d:
			label, e := r.U32()
			if e != nil || label != 0 || depth != 1 || ns != 2 || nl != 4 {
				return p, false
			}
			end, e := r.Byte()
			if e != nil || end != 0x0b {
				return p, false
			}
			// A positive two-element induction and an unchanged limit determine
			// the guarded prefix; all other termination cases use original code.
			c := nodes[stack[0]]
			if c.Op != 0x47 {
				return p, false
			}
			advance := func(id int) (local int, step uint32, yes bool) {
				v := nodes[id]
				if v.Op != 0x6a {
					return 0, 0, false
				}
				a, b := nodes[v.Left], nodes[v.Right]
				if a.Op != 0x20 {
					a, b = b, a
				}
				return a.Left, uint32(b.Bits), a.Op == 0x20 && b.Op == 0x41
			}
			inc, lim := c.Left, c.Right
			counter, step, yes := advance(inc)
			if !yes {
				inc, lim = lim, inc
				counter, step, yes = advance(inc)
			}
			if !yes || step != 2 {
				return p, false
			}
			cell := func(index int) *shared.LoopLocal[machineType, int] {
				s := shared.FindLoopLocal(p.locals[:p.localN], uint32(index))
				if s < 0 {
					return nil
				}
				return &p.locals[s]
			}
			cc := cell(counter)
			if cc == nil || cc.Value != inc {
				return p, false
			}
			p.counter = counter
			lv := nodes[lim]
			if lv.Op == 0x41 {
				p.constantLimit = true
				p.limitConst = uint32(lv.Bits)
			} else if lv.Op == 0x20 && !cell(lv.Left).Written {
				p.limit = lv.Left
			} else {
				return p, false
			}
			dst := nodes[stores[0][0]]
			if dst.Op != 0x20 || cell(dst.Left).Written || stores[0][0] != stores[1][0] {
				return p, false
			}
			p.dst = dst.Left
			maxPair := func(id int) (a, b int, yes bool) {
				v := nodes[id]
				if v.Op != 0x1b {
					return 0, 0, false
				}
				cmp := nodes[condition[id]]
				if cmp.Op == 0x4a && cmp.Left == v.Left && cmp.Right == v.Right || cmp.Op == 0x48 && cmp.Left == v.Right && cmp.Right == v.Left {
					return v.Left, v.Right, true
				}
				return 0, 0, false
			}
			a, b, yes := maxPair(stores[0][1])
			if !yes {
				return p, false
			}
			if nodes[a].Op != 0x20 {
				a, b = b, a
			}
			if nodes[a].Op != 0x20 || nodes[b].Op != 0x6a {
				return p, false
			}
			p.acc = nodes[a].Left
			if !cell(p.acc).Written || cell(p.acc).Value != stores[1][1] {
				return p, false
			}
			x, y, yes := maxPair(stores[1][1])
			if !yes {
				return p, false
			}
			if x != stores[0][1] {
				x, y = y, x
			}
			if x != stores[0][1] || nodes[y].Op != 0x6a {
				return p, false
			}
			var pairs [2][2]int
			for i, id := range [2]int{b, y} {
				v := nodes[id]
				pairs[i] = [2]int{v.Left, v.Right}
				for _, load := range pairs[i] {
					if nodes[load].Op != 0x28 {
						return p, false
					}
				}
			}
			var used [128]bool
			for _, pair := range pairs {
				for _, id := range pair {
					if used[id] {
						return p, false
					}
					used[id] = true
				}
			}
			for _, id := range loads {
				if !used[id] {
					return p, false
				}
			}
			address := func(id int) (out maxSumAddress, valid bool) {
				var walk func(int) bool
				walk = func(id int) bool {
					if !spend() {
						return false
					}
					v := nodes[id]
					switch v.Op {
					case 0x6a:
						return walk(v.Left) && walk(v.Right)
					case 0x41:
						out.bias += uint32(v.Bits)
						return true
					case 0x20:
						if out.n == len(out.locals) {
							return false
						}
						out.locals[out.n] = v.Left
						out.n++
						return true
					default:
						return false
					}
				}
				if !walk(nodes[id].Left) {
					return out, false
				}
				if out.n == 2 && out.locals[0] > out.locals[1] {
					out.locals[0], out.locals[1] = out.locals[1], out.locals[0]
				}
				out.pointer = -1
				for _, index := range out.locals[:out.n] {
					c := cell(index)
					if !c.Written {
						continue
					}
					local, step, yes := advance(c.Value)
					if out.pointer >= 0 || !yes || local != index || step < 2 || step > 1<<21 || step%2 != 0 || index == p.counter || index == p.acc {
						return out, false
					}
					out.pointer, out.stride = index, step/2
				}
				return out, out.pointer >= 0
			}
			var second [2]maxSumAddress
			for i := 0; i < 2; i++ {
				var yes bool
				p.streams[i], yes = address(pairs[0][i])
				if !yes {
					return p, false
				}
				second[i], yes = address(pairs[1][i])
				if !yes {
					return p, false
				}
			}
			matches := func(a, b maxSumAddress) bool {
				return a.n == b.n && a.locals == b.locals && a.pointer == b.pointer && a.stride == b.stride && b.bias-a.bias == a.stride
			}
			if !matches(p.streams[0], second[0]) || !matches(p.streams[1], second[1]) {
				second[0], second[1] = second[1], second[0]
				if !matches(p.streams[0], second[0]) || !matches(p.streams[1], second[1]) {
					return p, false
				}
			}
			if p.streams[0].stride != 4 && p.streams[1].stride != 4 {
				return p, false
			}
			for i, c := range p.locals[:p.localN] {
				if c.Written && entryRead[i] && int(c.Index) != p.counter && int(c.Index) != p.acc && int(c.Index) != p.streams[0].pointer && int(c.Index) != p.streams[1].pointer {
					return p, false
				}
			}
			return p, true
		default:
			return p, false
		}
	}
	return p, false
}
