package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
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
	use, def   uint32 // tracked-root indexes, or noGCLiveIndex
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

type gcFrameLivenessExtra struct {
	words []uint64 // allocation-site words followed by native-call words
}

// gcFrameLocalLiveness computes architecture-independent exact backwards local
// liveness over the validated structured Wasm CFG. The low word for each site is
// returned directly. For functions wider than 64 collector locals, remaining
// words are appended to one flat site-major arena in extra.
// Small functions retain the one-word dataflow path; larger functions use one
// bounded nodes-by-words arena rather than per-node heap bitsets.
func gcFrameLocalLiveness(body []byte, indexes []uint32, callMasks *[]uint64, extra *gcFrameLivenessExtra) ([]uint64, error) {
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	return gcFrameLocalLivenessWithClassifier(body, indexes, callMasks, extra, &classifier)
}

func gcFrameLocalLivenessWithClassifier(body []byte, indexes []uint32, callMasks *[]uint64, extra *gcFrameLivenessExtra, classifier *wasm.ModuleInstructionClassifier) ([]uint64, error) {
	if len(indexes) > shared.GCFrameRootLimit {
		return nil, fmt.Errorf("GC local liveness tracks %d roots, limit %d", len(indexes), shared.GCFrameRootLimit)
	}
	bits := make(map[uint32]uint32, len(indexes))
	for i, index := range indexes {
		bits[index] = uint32(i)
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
		node := gcLiveNode{flow: gcLiveNext, use: noGCLiveIndex, def: noGCLiveIndex, index: noGCLiveIndex}
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
		} else if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return nil, err
		}

		switch imm.Kind {
		case wasm.InstrLocalGet:
			if bit, ok := bits[imm.Index]; ok {
				node.use = bit
			}
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			if bit, ok := bits[imm.Index]; ok {
				node.def = bit
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
	wordCount := (len(indexes) + 63) / 64
	if wordCount == 0 {
		wordCount = 1
	}
	liveIn := make([]uint64, len(nodes)*wordCount)
	changed := true
	for changed {
		changed = false
		for i := len(nodes) - 1; i >= 0; i-- {
			if !nodes[i].reachable {
				continue
			}
			base := i * wordCount
			for word := 0; word < wordCount; word++ {
				var in uint64
				for j := uint8(0); j < nodes[i].succN; j++ {
					in |= liveIn[int(nodes[i].succ[j])*wordCount+word]
				}
				if nodes[i].indexN != 0 {
					start := int(nodes[i].index)
					for _, succ := range branchTargets[start : start+int(nodes[i].indexN)] {
						in |= liveIn[int(succ)*wordCount+word]
					}
				}
				if nodes[i].def != noGCLiveIndex && int(nodes[i].def/64) == word {
					in &^= uint64(1) << uint(nodes[i].def%64)
				}
				if nodes[i].use != noGCLiveIndex && int(nodes[i].use/64) == word {
					in |= uint64(1) << uint(nodes[i].use%64)
				}
				if in != liveIn[base+word] {
					liveIn[base+word], changed = in, true
				}
			}
		}
	}
	liveMasks := make([]uint64, 0, allocationN)
	calls := make([]uint64, 0, nativeCallN)
	extraPerSite := wordCount - 1
	var extraWords []uint64
	if extraPerSite != 0 {
		extraWords = make([]uint64, (allocationN+nativeCallN)*extraPerSite)
	}
	allocationIndex, callIndex := 0, 0
	for i := range nodes {
		if !nodes[i].reachable {
			continue
		}
		words := liveIn[i*wordCount : (i+1)*wordCount]
		if nodes[i].allocation {
			liveMasks = append(liveMasks, words[0])
			copy(extraWords[allocationIndex*extraPerSite:], words[1:])
			allocationIndex++
		}
		if nodes[i].nativeCall {
			calls = append(calls, words[0])
			copy(extraWords[(allocationN+callIndex)*extraPerSite:], words[1:])
			callIndex++
		}
	}
	if callMasks != nil {
		*callMasks = calls
	}
	if extra != nil {
		extra.words = extraWords
	}
	return liveMasks, nil
}

func gcFrameBodyMayCollect(body []byte) bool {
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	return gcFrameBodyMayCollectWithClassifier(body, &classifier)
}

func gcFrameBodyMayCollectWithClassifier(body []byte, classifier *wasm.ModuleInstructionClassifier) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		var imm wasm.InstructionImmediate
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return true
		}
		switch imm.Kind {
		case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			return true
		}
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10:
				return true
			}
		}
	}
	return false
}

func gcFrameAllLiveMasks(body []byte, localRoots int, extra *gcFrameLivenessExtra) (allocations, calls []uint64, err error) {
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	return gcFrameAllLiveMasksWithClassifier(body, localRoots, extra, &classifier)
}

func gcFrameAllLiveMasksWithClassifier(body []byte, localRoots int, extra *gcFrameLivenessExtra, classifier *wasm.ModuleInstructionClassifier) (allocations, calls []uint64, err error) {
	if localRoots < 0 || localRoots > shared.GCFrameRootLimit {
		return nil, nil, fmt.Errorf("GC conservative liveness tracks %d roots, limit %d", localRoots, shared.GCFrameRootLimit)
	}
	wordCount := (localRoots + 63) / 64
	if wordCount == 0 {
		wordCount = 1
	}
	words := make([]uint64, wordCount)
	for i := range words {
		words[i] = ^uint64(0)
	}
	if remain := uint(localRoots % 64); remain != 0 {
		words[len(words)-1] = uint64(1)<<remain - 1
	} else if localRoots == 0 {
		words[0] = 0
	}
	countSites := func() (allocationN, callN int, err error) {
		r := wasm.NewReader(body)
		for r.HasNext() {
			op, readErr := r.Byte()
			if readErr != nil {
				return 0, 0, readErr
			}
			var imm wasm.InstructionImmediate
			if readErr := classifier.ClassifyInto(r, op, &imm); readErr != nil {
				return 0, 0, readErr
			}
			if op == 0xfb {
				switch imm.Subopcode {
				case 0, 1, 6, 7, 8, 9, 10:
					allocationN++
				}
			}
			if imm.Kind == wasm.InstrCall || imm.Kind == wasm.InstrCallIndirect || imm.Kind == wasm.InstrCallRef {
				callN++
			}
		}
		return allocationN, callN, nil
	}
	allocationN, callN, err := countSites()
	if err != nil {
		return nil, nil, err
	}
	extraPerSite := wordCount - 1
	var extraWords []uint64
	if extraPerSite != 0 {
		extraWords = make([]uint64, (allocationN+callN)*extraPerSite)
	}
	allocationIndex, callIndex := 0, 0
	appendMask := func(low *[]uint64, site int) {
		*low = append(*low, words[0])
		copy(extraWords[site*extraPerSite:], words[1:])
	}
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return nil, nil, readErr
		}
		var imm wasm.InstructionImmediate
		if readErr := classifier.ClassifyInto(r, op, &imm); readErr != nil {
			return nil, nil, readErr
		}
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10:
				appendMask(&allocations, allocationIndex)
				allocationIndex++
			}
		}
		if imm.Kind == wasm.InstrCall || imm.Kind == wasm.InstrCallIndirect || imm.Kind == wasm.InstrCallRef {
			appendMask(&calls, allocationN+callIndex)
			callIndex++
		}
	}
	if extra != nil {
		extra.words = extraWords
	}
	return allocations, calls, nil
}

func gcLiveBranchFrame(stack []int, depth uint32) (int, bool) {
	if uint64(depth) >= uint64(len(stack)) {
		return 0, false
	}
	return stack[len(stack)-1-int(depth)], true
}
