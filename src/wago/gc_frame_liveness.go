package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type gcLiveFlow uint8

const (
	gcLiveNext gcLiveFlow = iota
	gcLiveIf
	gcLiveElse
	gcLiveBr
	gcLiveBrIf
	gcLiveBrTable
	gcLiveStop
)

type gcLiveNode struct {
	flow       gcLiveFlow
	frame      int
	targets    []int
	use, def   uint64
	allocation bool
	nativeCall bool
	succ       []int
}

type gcLiveFrame struct {
	loop     bool
	header   int
	elseNode int
	endNode  int
}

// gcFrameLocalLiveness computes architecture-independent exact backwards local liveness over the
// validated structured Wasm CFG. Only the at-most-64 collector-reference locals
// in indexes participate, so each dataflow state is one uint64 and loops converge
// without per-node heap bitsets.
func gcFrameLocalLiveness(body []byte, indexes []uint32, calls bool) ([]uint64, error) {
	if len(indexes) > 64 {
		return nil, fmt.Errorf("GC local liveness tracks %d roots, limit 64", len(indexes))
	}
	bits := make(map[uint32]uint, len(indexes))
	for i, index := range indexes {
		bits[index] = uint(i)
	}

	r := wasm.NewReader(body)
	frames := []gcLiveFrame{{header: -1, elseNode: -1, endNode: -1}}
	stack := []int{0}
	nodes := make([]gcLiveNode, 0, len(body)/2+1)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		node := gcLiveNode{flow: gcLiveNext, frame: -1}
		var imm wasm.InstructionImmediate
		if op == 0x0e { // br_table keeps every target, unlike the cheap classifier.
			n, err := r.U32()
			if err != nil {
				return nil, err
			}
			node.flow = gcLiveBrTable
			node.targets = make([]int, 0, int(n)+1)
			for i := uint32(0); i <= n; i++ {
				depth, err := r.U32()
				if err != nil {
					return nil, err
				}
				target, ok := gcLiveBranchFrame(stack, depth)
				if !ok {
					return nil, fmt.Errorf("GC liveness br_table depth %d is out of range", depth)
				}
				node.targets = append(node.targets, target)
			}
			imm.Kind = wasm.InstrBrTable
		} else if err := wasm.ClassifyInstructionImmediateInto(r, op, &imm); err != nil {
			return nil, err
		}

		switch imm.Kind {
		case wasm.InstrLocalGet:
			if bit, ok := bits[imm.Index]; ok {
				node.use = uint64(1) << bit
			}
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			if bit, ok := bits[imm.Index]; ok {
				node.def = uint64(1) << bit
			}
		}
		node.nativeCall = imm.Kind == wasm.InstrCall || imm.Kind == wasm.InstrCallIndirect || imm.Kind == wasm.InstrCallRef
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10: // struct.new*, array.new*
				node.allocation = true
			}
		}

		nodeIndex := len(nodes)
		switch op {
		case 0x02, 0x03, 0x04: // block, loop, if
			frames = append(frames, gcLiveFrame{loop: op == 0x03, header: nodeIndex, elseNode: -1, endNode: -1})
			node.frame = len(frames) - 1
			stack = append(stack, node.frame)
			if op == 0x04 {
				node.flow = gcLiveIf
			}
		case 0x05: // else
			if len(stack) <= 1 {
				return nil, fmt.Errorf("GC liveness else without if")
			}
			top := stack[len(stack)-1]
			frames[top].elseNode = nodeIndex
			node.frame, node.flow = top, gcLiveElse
		case 0x0b: // end
			if len(stack) == 0 {
				return nil, fmt.Errorf("GC liveness end without frame")
			}
			top := stack[len(stack)-1]
			frames[top].endNode = nodeIndex
			stack = stack[:len(stack)-1]
		case 0x0c: // br
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return nil, fmt.Errorf("GC liveness br depth %d is out of range", imm.Index)
			}
			node.flow, node.targets = gcLiveBr, []int{target}
		case 0x0d: // br_if
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return nil, fmt.Errorf("GC liveness br_if depth %d is out of range", imm.Index)
			}
			node.flow, node.targets = gcLiveBrIf, []int{target}
		case 0x00, 0x0f: // unreachable, return
			node.flow = gcLiveStop
		}
		switch imm.Kind {
		case wasm.InstrBrOnNull, wasm.InstrBrOnNonNull, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return nil, fmt.Errorf("GC liveness reference branch depth %d is out of range", imm.Index)
			}
			node.flow, node.targets = gcLiveBrIf, []int{target}
		case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			node.flow = gcLiveStop
		}
		nodes = append(nodes, node)
	}
	if len(stack) != 0 || len(nodes) == 0 || frames[0].endNode < 0 {
		return nil, fmt.Errorf("GC liveness body has unterminated control frames")
	}

	for i := range nodes {
		next := i + 1
		addNext := func() {
			if next < len(nodes) {
				nodes[i].succ = append(nodes[i].succ, next)
			}
		}
		addTarget := func(frameIndex int) error {
			if frameIndex < 0 || frameIndex >= len(frames) || frames[frameIndex].endNode < 0 {
				return fmt.Errorf("GC liveness branch frame %d is unresolved", frameIndex)
			}
			target := frames[frameIndex].endNode + 1
			if frames[frameIndex].loop {
				target = frames[frameIndex].header + 1
			}
			if target < len(nodes) {
				nodes[i].succ = append(nodes[i].succ, target)
			}
			return nil
		}
		switch nodes[i].flow {
		case gcLiveNext:
			addNext()
		case gcLiveIf:
			addNext()
			frame := frames[nodes[i].frame]
			if frame.elseNode >= 0 {
				nodes[i].succ = append(nodes[i].succ, frame.elseNode+1)
			} else if frame.endNode+1 < len(nodes) {
				nodes[i].succ = append(nodes[i].succ, frame.endNode+1)
			}
		case gcLiveElse:
			frame := frames[nodes[i].frame]
			if frame.endNode+1 < len(nodes) {
				nodes[i].succ = append(nodes[i].succ, frame.endNode+1)
			}
		case gcLiveBr:
			if err := addTarget(nodes[i].targets[0]); err != nil {
				return nil, err
			}
		case gcLiveBrIf:
			addNext()
			if err := addTarget(nodes[i].targets[0]); err != nil {
				return nil, err
			}
		case gcLiveBrTable:
			for _, target := range nodes[i].targets {
				if err := addTarget(target); err != nil {
					return nil, err
				}
			}
		case gcLiveStop:
		}
	}

	reachable := make([]bool, len(nodes))
	work := []int{0}
	for len(work) != 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		if i < 0 || i >= len(nodes) || reachable[i] {
			continue
		}
		reachable[i] = true
		work = append(work, nodes[i].succ...)
	}
	liveIn, liveOut := make([]uint64, len(nodes)), make([]uint64, len(nodes))
	changed := true
	for changed {
		changed = false
		for i := len(nodes) - 1; i >= 0; i-- {
			if !reachable[i] {
				continue
			}
			var out uint64
			for _, succ := range nodes[i].succ {
				out |= liveIn[succ]
			}
			in := nodes[i].use | (out &^ nodes[i].def)
			if out != liveOut[i] || in != liveIn[i] {
				liveOut[i], liveIn[i], changed = out, in, true
			}
		}
	}
	masks := make([]uint64, 0)
	for i := range nodes {
		if reachable[i] && ((!calls && nodes[i].allocation) || (calls && nodes[i].nativeCall)) {
			masks = append(masks, liveIn[i])
		}
	}
	return masks, nil
}

func gcLiveBranchFrame(stack []int, depth uint32) (int, bool) {
	if uint64(depth) >= uint64(len(stack)) {
		return 0, false
	}
	return stack[len(stack)-1-int(depth)], true
}
