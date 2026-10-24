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

const maxBoundedHostBodyBytes = 384

type boundedHostFrame struct {
	start, end, alternate int16
	op                    byte
}

type boundedHostNode struct {
	targetStart, targetCount uint16
	frame                    int16
	op                       byte
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
	if len(body) == 0 || len(body) > maxBoundedHostBodyBytes {
		return false
	}
	// Every retained item corresponds to at least one input byte. Fixed compact
	// arrays therefore cover the complete admitted domain without per-function
	// heap growth. Wide br_table vectors consume input bytes too, so one shared
	// target array has the same bound.
	var frameStorage [maxBoundedHostBodyBytes + 1]boundedHostFrame
	var nodeStorage [maxBoundedHostBodyBytes]boundedHostNode
	var stackStorage [maxBoundedHostBodyBytes + 1]int16
	var targetStorage [maxBoundedHostBodyBytes]int16
	frames, frameN := frameStorage[:], 1
	nodes, nodeN := nodeStorage[:], 0
	stack, stackN := stackStorage[:], 1
	targets, targetN := targetStorage[:], 0
	frames[0] = boundedHostFrame{op: 0xff, start: -1, end: -1, alternate: -1}
	stack[0] = 0
	r := wasm.ReaderFrom(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || stackN == 0 || nodeN == len(nodes) {
			return false
		}
		n := boundedHostNode{op: op, frame: -1, targetStart: uint16(targetN)}
		var imm wasm.InstructionImmediate
		if op == 0x0e { // br_table: retain every possible target
			count, err := r.U32()
			if err != nil || count >= uint32(len(targets)-targetN) {
				return false
			}
			for i := uint32(0); i <= count; i++ {
				depth, err := r.U32()
				if err != nil || int(depth) >= stackN {
					return false
				}
				targets[targetN] = stack[stackN-1-int(depth)]
				targetN++
				n.targetCount++
			}
		} else if wasm.ClassifyInstructionImmediateInto(&r, op, &imm) != nil {
			return false
		}
		switch {
		case op == 0x02 || op == 0x03 || op == 0x04:
			if frameN == len(frames) || stackN == len(stack) {
				return false
			}
			n.frame = int16(frameN)
			frames[frameN] = boundedHostFrame{op: op, start: int16(nodeN), end: -1, alternate: -1}
			frameN++
			stack[stackN] = n.frame
			stackN++
		case op == 0x05:
			n.frame = stack[stackN-1]
			f := &frames[int(n.frame)]
			if f.op != 0x04 || f.alternate != -1 {
				return false
			}
			f.alternate = int16(nodeN)
		case op == 0x0b:
			frames[int(stack[stackN-1])].end = int16(nodeN)
			stackN--
		case op == 0x0c || op == 0x0d:
			if int(imm.Index) >= stackN || targetN == len(targets) {
				return false
			}
			targets[targetN] = stack[stackN-1-int(imm.Index)]
			targetN++
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
		nodes[nodeN] = n
		nodeN++
	}
	if stackN != 0 {
		return false
	}
	// The selected CFG must be a DAG. Checking every node also
	// checks all post-host continuations, including ones unreachable at entry.
	var colorStorage [maxBoundedHostBodyBytes]uint8
	nodeView := nodes[:nodeN]
	color := colorStorage[:nodeN]
	for i := range nodeView {
		if !visitBoundedHostCFG(i, nodeView, frames[:frameN], targets[:targetN], color, cutHost) {
			return false
		}
	}
	return true
}

func visitBoundedHostCFG(i int, nodes []boundedHostNode, frames []boundedHostFrame, targets []int16, color []uint8, cutHost bool) bool {
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
	edge := func(to int) bool { return visitBoundedHostCFG(to, nodes, frames, targets, color, cutHost) }
	switch n.op {
	case 0x00, 0x0f: // trap, return
	case 0x10:
		if !cutHost && !edge(i+1) {
			return false
		}
	case 0x04:
		f := frames[int(n.frame)]
		other := int(f.end) + 1
		if f.alternate >= 0 {
			other = int(f.alternate) + 1
		}
		if !edge(i+1) || !edge(other) {
			return false
		}
	case 0x05:
		if !edge(int(frames[int(n.frame)].end) + 1) {
			return false
		}
	case 0x0c, 0x0d, 0x0e:
		end := int(n.targetStart + n.targetCount)
		for _, target := range targets[int(n.targetStart):end] {
			f := frames[int(target)]
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
