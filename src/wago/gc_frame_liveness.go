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

const noGCLiveIndex = ^uint32(0)

type gcLiveNode struct {
	use, def   uint64
	index      uint32 // control frame, branch frame, or br_table target-arena start
	indexN     uint32 // br_table target count
	succ       [2]uint32
	flow       gcLiveFlow
	succN      uint8
	allocation bool
	nativeCall bool
	reachable  bool
}

func (n *gcLiveNode) addSucc(index int) {
	if n.succN >= uint8(len(n.succ)) || index < 0 || uint64(index) > uint64(^uint32(0)) {
		panic("GC liveness node has invalid fixed successor")
	}
	n.succ[n.succN] = uint32(index)
	n.succN++
}

type gcLiveFrame struct {
	loop     bool
	header   int
	elseNode int
	endNode  int
}

// gcFrameLocalLiveness computes architecture-independent exact backwards local
// liveness over the validated structured Wasm CFG. It returns allocation-site
// masks and writes native-call masks to callMasks, sharing one decode and
// dataflow pass. Only the at-most-64 collector-reference locals in indexes
// participate, so each state is one uint64 and loops converge without per-node
// heap bitsets.
func gcFrameLocalLiveness(body []byte, indexes []uint32, callMasks *[]uint64) ([]uint64, error) {
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
	branchTargets := make([]uint32, 0)
	allocationN, nativeCallN := 0, 0
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		node := gcLiveNode{flow: gcLiveNext, index: noGCLiveIndex}
		var imm wasm.InstructionImmediate
		if op == 0x0e { // br_table keeps every target, unlike the cheap classifier.
			n, err := r.U32()
			if err != nil {
				return nil, err
			}
			count := uint64(n) + 1 // the vector plus its default target
			if count > uint64(^uint32(0)) || uint64(len(branchTargets))+count > uint64(^uint32(0)) {
				return nil, fmt.Errorf("GC liveness br_table target count exceeds implementation limit")
			}
			node.flow = gcLiveBrTable
			node.index = uint32(len(branchTargets))
			node.indexN = uint32(count)
			for i := uint64(0); i < count; i++ {
				depth, err := r.U32()
				if err != nil {
					return nil, err
				}
				target, ok := gcLiveBranchFrame(stack, depth)
				if !ok {
					return nil, fmt.Errorf("GC liveness br_table depth %d is out of range", depth)
				}
				branchTargets = append(branchTargets, uint32(target))
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
		if node.nativeCall {
			nativeCallN++
		}
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10: // struct.new*, array.new*
				node.allocation = true
				allocationN++
			}
		}

		nodeIndex := len(nodes)
		switch op {
		case 0x02, 0x03, 0x04: // block, loop, if
			frames = append(frames, gcLiveFrame{loop: op == 0x03, header: nodeIndex, elseNode: -1, endNode: -1})
			node.index = uint32(len(frames) - 1)
			stack = append(stack, int(node.index))
			if op == 0x04 {
				node.flow = gcLiveIf
			}
		case 0x05: // else
			if len(stack) <= 1 {
				return nil, fmt.Errorf("GC liveness else without if")
			}
			top := stack[len(stack)-1]
			frames[top].elseNode = nodeIndex
			node.index, node.flow = uint32(top), gcLiveElse
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
			node.flow, node.index = gcLiveBr, uint32(target)
		case 0x0d: // br_if
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return nil, fmt.Errorf("GC liveness br_if depth %d is out of range", imm.Index)
			}
			node.flow, node.index = gcLiveBrIf, uint32(target)
		case 0x00, 0x0f: // unreachable, return
			node.flow = gcLiveStop
		}
		switch imm.Kind {
		case wasm.InstrBrOnNull, wasm.InstrBrOnNonNull, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return nil, fmt.Errorf("GC liveness reference branch depth %d is out of range", imm.Index)
			}
			node.flow, node.index = gcLiveBrIf, uint32(target)
		case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			node.flow = gcLiveStop
		}
		nodes = append(nodes, node)
	}
	if len(stack) != 0 || len(nodes) == 0 || frames[0].endNode < 0 {
		return nil, fmt.Errorf("GC liveness body has unterminated control frames")
	}

	resolveTarget := func(frameIndex int) (int, bool, error) {
		if frameIndex < 0 || frameIndex >= len(frames) || frames[frameIndex].endNode < 0 {
			return 0, false, fmt.Errorf("GC liveness branch frame %d is unresolved", frameIndex)
		}
		target := frames[frameIndex].endNode + 1
		if frames[frameIndex].loop {
			target = frames[frameIndex].header + 1
		}
		return target, target < len(nodes), nil
	}
	for i := range nodes {
		next := i + 1
		addNext := func() {
			if next < len(nodes) {
				nodes[i].addSucc(next)
			}
		}
		addTarget := func(frameIndex int) error {
			target, ok, err := resolveTarget(frameIndex)
			if err != nil {
				return err
			}
			if ok {
				nodes[i].addSucc(target)
			}
			return nil
		}
		switch nodes[i].flow {
		case gcLiveNext:
			addNext()
		case gcLiveIf:
			addNext()
			frame := frames[int(nodes[i].index)]
			if frame.elseNode >= 0 {
				nodes[i].addSucc(frame.elseNode + 1)
			} else if frame.endNode+1 < len(nodes) {
				nodes[i].addSucc(frame.endNode + 1)
			}
		case gcLiveElse:
			frame := frames[int(nodes[i].index)]
			if frame.endNode+1 < len(nodes) {
				nodes[i].addSucc(frame.endNode + 1)
			}
		case gcLiveBr:
			if err := addTarget(int(nodes[i].index)); err != nil {
				return nil, err
			}
		case gcLiveBrIf:
			addNext()
			if err := addTarget(int(nodes[i].index)); err != nil {
				return nil, err
			}
		case gcLiveBrTable:
			start := int(nodes[i].index)
			end := start + int(nodes[i].indexN)
			targets := branchTargets[start:end]
			succN := 0
			for _, frameIndex := range targets {
				target, ok, err := resolveTarget(int(frameIndex))
				if err != nil {
					return nil, err
				}
				if ok {
					targets[succN] = uint32(target)
					succN++
				}
			}
			nodes[i].indexN = uint32(succN)
		case gcLiveStop:
		}
	}

	work := []int{0}
	for len(work) != 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		if i < 0 || i >= len(nodes) || nodes[i].reachable {
			continue
		}
		nodes[i].reachable = true
		for j := uint8(0); j < nodes[i].succN; j++ {
			work = append(work, int(nodes[i].succ[j]))
		}
		if nodes[i].indexN != 0 {
			start := int(nodes[i].index)
			for _, succ := range branchTargets[start : start+int(nodes[i].indexN)] {
				work = append(work, int(succ))
			}
		}
	}
	liveIn := make([]uint64, len(nodes))
	changed := true
	for changed {
		changed = false
		for i := len(nodes) - 1; i >= 0; i-- {
			if !nodes[i].reachable {
				continue
			}
			var out uint64
			for j := uint8(0); j < nodes[i].succN; j++ {
				out |= liveIn[int(nodes[i].succ[j])]
			}
			if nodes[i].indexN != 0 {
				start := int(nodes[i].index)
				for _, succ := range branchTargets[start : start+int(nodes[i].indexN)] {
					out |= liveIn[int(succ)]
				}
			}
			in := nodes[i].use | (out &^ nodes[i].def)
			if in != liveIn[i] {
				liveIn[i], changed = in, true
			}
		}
	}
	liveMasks := make([]uint64, 0, allocationN)
	calls := make([]uint64, 0, nativeCallN)
	for i := range nodes {
		if !nodes[i].reachable {
			continue
		}
		if nodes[i].allocation {
			liveMasks = append(liveMasks, liveIn[i])
		}
		if nodes[i].nativeCall {
			calls = append(calls, liveIn[i])
		}
	}
	if callMasks != nil {
		*callMasks = calls
	}
	return liveMasks, nil
}

func gcLiveBranchFrame(stack []int, depth uint32) (int, bool) {
	if uint64(depth) >= uint64(len(stack)) {
		return 0, false
	}
	return stack[len(stack)-1-int(depth)], true
}
