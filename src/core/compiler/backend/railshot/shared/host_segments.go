package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// BoundedHostSegments proves that every native path reaches a direct imported
// call, return, or trap within a capped amount of scalar work. Imported calls
// are cut edges, not assumed-bounded native calls: the instance must separately
// prove that every import really parks into a Go host callback. Defined,
// indirect, tail, custom, memory, reference, SIMD, and helper calls fail closed.
// The proof covers every continuation after a host call as well as the entry.
func BoundedHostSegments(body []byte, imports uint32) bool {
	return boundedScalarHostCFG(body, imports, true, hostSegmentExtension{})
}

// BoundedNativeHostBody proves a whole scalar body is bounded even when its
// imported calls do not yield to Go. Import edges remain in the CFG. Runtime
// admission must separately prove every import is a bounded native leaf.
func BoundedNativeHostBody(body []byte, imports uint32) bool {
	return boundedScalarHostCFG(body, imports, false, hostSegmentExtension{})
}

type hostSegmentExtension struct {
	fixedSharedMemory bool
	yieldingLocals    uint64
}

// BoundedFixedSharedHostSegments additionally admits i32 load/store/size on
// memory zero. The caller must prove that it is a fixed-size defined shared
// memory with explicit bounds checks. yieldingLocals names local callees already
// proved bounded and guaranteed to enter Go before a normal return. The second
// result conservatively proves that guarantee for this function's entry too.
func BoundedFixedSharedHostSegments(body []byte, imports uint32, yieldingLocals uint64) (bool, bool) {
	ext := hostSegmentExtension{fixedSharedMemory: true, yieldingLocals: yieldingLocals}
	if !boundedScalarHostCFG(body, imports, true, ext) {
		return false, false
	}
	r := wasm.ReaderFrom(body)
	depth := 0
	for r.HasNext() {
		op, err := r.Byte()
		var imm wasm.InstructionImmediate
		if err != nil || wasm.ClassifyInstructionImmediateInto(&r, op, &imm) != nil {
			return true, false
		}
		switch op {
		case 0x02, 0x03:
			depth++
		case 0x0b:
			if depth == 0 {
				return true, false
			}
			depth--
		case 0x10:
			return true, imm.Index < imports || imm.Index-imports < 64 && yieldingLocals&(uint64(1)<<(imm.Index-imports)) != 0
		case 0x00, 0x04, 0x05, 0x0c, 0x0d, 0x0e, 0x0f:
			// A branch or early exit before the first yield needs a stronger
			// entry-path analysis; do not use it to cut a caller's call edge.
			return true, false
		}
	}
	return true, false
}

func boundedScalarHostCFG(body []byte, imports uint32, cutHost bool, ext hostSegmentExtension) bool {
	if len(body) == 0 || len(body) > 384 {
		return false
	}
	type frame struct {
		op                    byte
		start, end, alternate int16
	}
	type node struct {
		op                       byte
		frame                    int16
		targetStart, targetCount uint16
	}
	// Every node, frame and branch target consumes at least one byte in the
	// bounded source body. Keep compact scratch on the stack; rejected bodies
	// must not allocate a body-sized heap graph for each scheduler proof.
	var frameStorage [385]frame
	var stackStorage [385]int16
	var nodeStorage [384]node
	var targetStorage [384]int16
	frames := frameStorage[:1]
	frames[0] = frame{op: 0xff, start: -1, end: -1, alternate: -1}
	stack := stackStorage[:1]
	nodes := nodeStorage[:0]
	targets := targetStorage[:0]
	r := wasm.ReaderFrom(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || len(stack) == 0 {
			return false
		}
		n := node{op: op, frame: -1, targetStart: uint16(len(targets))}
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
				targets = append(targets, stack[len(stack)-1-int(depth)])
				n.targetCount++
			}
		} else if wasm.ClassifyInstructionImmediateInto(&r, op, &imm) != nil {
			return false
		}
		switch {
		case op == 0x02 || op == 0x03 || op == 0x04:
			n.frame = int16(len(frames))
			frames = append(frames, frame{op: op, start: int16(len(nodes)), end: -1, alternate: -1})
			stack = append(stack, n.frame)
		case op == 0x05:
			n.frame = stack[len(stack)-1]
			f := &frames[n.frame]
			if f.op != 0x04 || f.alternate != -1 {
				return false
			}
			f.alternate = int16(len(nodes))
		case op == 0x0b:
			frames[stack[len(stack)-1]].end = int16(len(nodes))
			stack = stack[:len(stack)-1]
		case op == 0x0c || op == 0x0d:
			if int(imm.Index) >= len(stack) {
				return false
			}
			targets = append(targets, stack[len(stack)-1-int(imm.Index)])
			n.targetCount = 1
		case op == 0x10:
			if imm.Index >= imports {
				local := imm.Index - imports
				if !cutHost || local >= 64 || ext.yieldingLocals&(uint64(1)<<local) == 0 {
					return false
				}
			}
		case ext.fixedSharedMemory && (op == 0x28 || op == 0x36):
			if imm.MemIndex != 0 || imm.MemAlign > 2 {
				return false
			}
		case ext.fixedSharedMemory && op == 0x3f:
			if imm.Index != 0 {
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
	var color [384]uint8
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
			other := int(f.end) + 1
			if f.alternate >= 0 {
				other = int(f.alternate) + 1
			}
			if !edge(i+1) || !edge(other) {
				return false
			}
		case 0x05:
			if !edge(int(frames[n.frame].end) + 1) {
				return false
			}
		case 0x0c, 0x0d, 0x0e:
			for _, target := range targets[n.targetStart : n.targetStart+n.targetCount] {
				f := frames[target]
				to := int(f.end) + 1
				if f.op == 0x03 {
					to = int(f.start) + 1
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
