package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// BoundedHostSegments proves that every native path reaches a direct imported
// call, return, or trap within a capped amount of scalar work. Imported calls
// are cut edges, not assumed-bounded native calls: the instance must separately
// prove that every import really parks into a Go host callback. Defined,
// indirect, tail, custom, memory, reference, SIMD, and helper calls fail closed.
// The proof covers every continuation after a host call as well as the entry.
func BoundedHostSegments(body []byte, imports uint32) bool {
	return boundedScalarHostCFG(body, imports, true)
}

// BoundedNativeHostBody proves a whole scalar body is bounded even when its
// imported calls do not yield to Go. Import edges remain in the CFG. Runtime
// admission must separately prove every import is a bounded native leaf.
func BoundedNativeHostBody(body []byte, imports uint32) bool {
	return boundedScalarHostCFG(body, imports, false)
}

func boundedScalarHostCFG(body []byte, imports uint32, cutHost bool) bool {
	if len(body) == 0 || len(body) > 384 {
		return false
	}
	type frame struct {
		op                    byte
		start, end, alternate int
	}
	type node struct {
		op      byte
		frame   int
		targets []int
	}
	frames := []frame{{op: 0xff, start: -1, end: -1, alternate: -1}}
	stack := []int{0}
	nodes := make([]node, 0, len(body))
	r := wasm.ReaderFrom(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || len(stack) == 0 {
			return false
		}
		n := node{op: op, frame: -1}
		var imm wasm.InstructionImmediate
		if op == 0x0e { // br_table: retain every possible target
			count, err := r.U32()
			if err != nil || count > uint32(len(body)) {
				return false
			}
			for i := uint32(0); i <= count; i++ {
				depth, err := r.U32()
				if err != nil || int(depth) >= len(stack) {
					return false
				}
				n.targets = append(n.targets, stack[len(stack)-1-int(depth)])
			}
		} else if wasm.ClassifyInstructionImmediateInto(&r, op, &imm) != nil {
			return false
		}
		switch {
		case op == 0x02 || op == 0x03 || op == 0x04:
			n.frame = len(frames)
			frames = append(frames, frame{op: op, start: len(nodes), end: -1, alternate: -1})
			stack = append(stack, n.frame)
		case op == 0x05:
			n.frame = stack[len(stack)-1]
			f := &frames[n.frame]
			if f.op != 0x04 || f.alternate != -1 {
				return false
			}
			f.alternate = len(nodes)
		case op == 0x0b:
			frames[stack[len(stack)-1]].end = len(nodes)
			stack = stack[:len(stack)-1]
		case op == 0x0c || op == 0x0d:
			if int(imm.Index) >= len(stack) {
				return false
			}
			n.targets = []int{stack[len(stack)-1-int(imm.Index)]}
		case op == 0x10:
			if imm.Index >= imports {
				return false
			}
		case op == 0x00 || op == 0x01 || op == 0x0e || op == 0x0f || op == 0x1a || op == 0x1b:
		case op >= 0x20 && op <= 0x22: // locals
		case op >= 0x41 && op <= 0xc4: // numeric constants, ALU, conversions
		default:
			return false
		}
		nodes = append(nodes, n)
	}
	if len(stack) != 0 {
		return false
	}
	// The selected CFG must be a DAG. Checking every node also
	// checks all post-host continuations, including ones unreachable at entry.
	color := make([]uint8, len(nodes))
	var visit func(int) bool
	visit = func(i int) bool {
		if i == len(nodes) {
			return true
		}
		if i < 0 || i > len(nodes) || color[i] == 1 {
			return false
		}
		if color[i] == 2 {
			return true
		}
		color[i] = 1
		n := nodes[i]
		edge := func(to int) bool { return visit(to) }
		switch n.op {
		case 0x00, 0x0f: // trap, return
		case 0x10:
			if !cutHost && !edge(i+1) {
				return false
			}
		case 0x04:
			f := frames[n.frame]
			other := f.end + 1
			if f.alternate >= 0 {
				other = f.alternate + 1
			}
			if !edge(i+1) || !edge(other) {
				return false
			}
		case 0x05:
			if !edge(frames[n.frame].end + 1) {
				return false
			}
		case 0x0c, 0x0d, 0x0e:
			for _, target := range n.targets {
				f := frames[target]
				to := f.end + 1
				if f.op == 0x03 {
					to = f.start + 1
				}
				if !edge(to) {
					return false
				}
			}
			if n.op == 0x0d && !edge(i+1) {
				return false
			}
		default:
			if !edge(i + 1) {
				return false
			}
		}
		color[i] = 2
		return true
	}
	for i := range nodes {
		if !visit(i) {
			return false
		}
	}
	return true
}
