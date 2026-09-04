package wago

import (
	"fmt"
	"math/bits"
	"slices"

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

const maxGCFrameLivenessArenaBytes = 64 << 20

const maxGCFrameBranchTargets = maxGCFrameLivenessArenaBytes / 4

const maxGCFrameLivenessWorkWords = maxGCFrameLivenessArenaBytes / 8

// Four conservative local roots and a 512 root-byte budget bound additional
// guest retention and serialized offsets while avoiding a pointer-rich CFG and
// backwards dataflow for common narrow-root functions. A root-free collecting
// function has no retention cost and always uses the cheap site-counting path.
// Fixed EH payload roots are accounted independently and remain always live.
const (
	gcFrameConservativeLocalLimit    = 4
	gcFrameConservativeRootByteLimit = 512
)

func gcFramePreferConservativeMasks(localRoots, bodyBytes int) bool {
	if localRoots < 0 || bodyBytes < 0 {
		return false
	}
	if localRoots == 0 {
		return true
	}
	return localRoots <= gcFrameConservativeLocalLimit && bodyBytes <= gcFrameConservativeRootByteLimit/localRoots
}

func gcFrameLivenessArenaFits(nodes, words int) bool {
	return nodes >= 0 && words > 0 && (nodes == 0 || words <= maxGCFrameLivenessArenaBytes/8/nodes)
}

func gcFrameLivenessWorkFits(nodes, branchEdges, words int) bool {
	units := nodes + branchEdges
	return nodes >= 0 && branchEdges >= 0 && units >= nodes && words > 0 &&
		(units == 0 || words <= maxGCFrameLivenessWorkWords/units)
}

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

type gcFrameLiveMasks struct {
	words        []uint64 // site-major: allocation sites, then native calls
	allocationN  int
	callN        int
	wordsPerSite int
}

func gcFrameLiveMaskArenaFits(allocationN, callN, wordsPerSite int) bool {
	if allocationN < 0 || callN < 0 || wordsPerSite <= 0 || allocationN > int(^uint(0)>>1)-callN {
		return false
	}
	sites := allocationN + callN
	return sites == 0 || wordsPerSite <= maxGCFrameLivenessArenaBytes/8/sites
}

func newGCFrameLiveMasks(allocationN, callN, wordsPerSite int) gcFrameLiveMasks {
	if !gcFrameLiveMaskArenaFits(allocationN, callN, wordsPerSite) {
		return gcFrameLiveMasks{}
	}
	return gcFrameLiveMasks{
		words:        make([]uint64, (allocationN+callN)*wordsPerSite),
		allocationN:  allocationN,
		callN:        callN,
		wordsPerSite: wordsPerSite,
	}
}

func (m gcFrameLiveMasks) site(site int) []uint64 {
	start := site * m.wordsPerSite
	return m.words[start : start+m.wordsPerSite]
}

// gcFrameCompactLiveLocals removes collector locals that are dead at every
// collecting site and returns the maximum population live at any one site.
// Masks remain site-major and exact; the returned local slice preserves its
// original frame order.
// Keep this large compile-time helper out of newGCFrameRootPlan. TinyGo's size
// optimizer otherwise inlines it and grows the minimal runtime substantially.
//
//go:noinline
func gcFrameCompactLiveLocalsArena(locals []shared.GCFrameLocal, masks gcFrameLiveMasks) ([]shared.GCFrameLocal, gcFrameLiveMasks, int, error) {
	wordCount := (len(locals) + 63) / 64
	if wordCount == 0 {
		wordCount = 1
	}
	totalSites := masks.allocationN + masks.callN
	if masks.wordsPerSite != wordCount || len(masks.words) != totalSites*wordCount {
		return nil, gcFrameLiveMasks{}, 0, fmt.Errorf("GC local liveness mask arena has %d words at width %d, want %d at width %d", len(masks.words), masks.wordsPerSite, totalSites*wordCount, wordCount)
	}
	union := make([]uint64, wordCount)
	maximum := 0
	for site := 0; site < totalSites; site++ {
		live := 0
		for word, value := range masks.site(site) {
			union[word] |= value
			live += bits.OnesCount64(value)
		}
		if live > maximum {
			maximum = live
		}
	}
	// Every set bit in a site belongs to the union. Retain a direct old-to-new
	// mapping so reconstruction visits arena words and live bits rather than
	// rescanning the complete retained union at every site.
	remap := make([]uint32, len(locals))
	kept := 0
	for root := range locals {
		if union[root/64]&(uint64(1)<<uint(root%64)) != 0 {
			remap[root] = uint32(kept)
			kept++
		}
	}
	compactedLocals := make([]shared.GCFrameLocal, kept)
	compacted := 0
	for root := range locals {
		if union[root/64]&(uint64(1)<<uint(root%64)) != 0 {
			compactedLocals[compacted] = locals[root]
			compacted++
		}
	}
	newWordCount := (kept + 63) / 64
	if newWordCount == 0 {
		newWordCount = 1
	}
	compactedMasks := masks
	if newWordCount != wordCount || kept != len(locals) && wordCount > 1 {
		if !gcFrameLiveMaskArenaFits(masks.allocationN, masks.callN, newWordCount) {
			return nil, gcFrameLiveMasks{}, 0, fmt.Errorf("compacted GC local liveness mask arena exceeds %d-byte implementation limit", maxGCFrameLivenessArenaBytes)
		}
		compactedMasks = newGCFrameLiveMasks(masks.allocationN, masks.callN, newWordCount)
	}
	for site := 0; site < totalSites; site++ {
		oldWords := masks.site(site)
		var newLow uint64
		for word := 0; word < wordCount; word++ {
			value := oldWords[word]
			for value != 0 {
				bit := bits.TrailingZeros64(value)
				value &= value - 1
				oldRoot := word*64 + bit
				if oldRoot >= len(remap) { // Ignore unused padding bits in the final word.
					continue
				}
				root := int(remap[oldRoot])
				if root < 64 {
					newLow |= uint64(1) << uint(root)
				} else {
					compactedMasks.site(site)[root/64] |= uint64(1) << uint(root%64)
				}
			}
		}
		compactedMasks.site(site)[0] = newLow
	}
	return compactedLocals, compactedMasks, maximum, nil
}

// gcFrameLocalLiveness computes architecture-independent exact backwards local
// liveness over the validated structured Wasm CFG. Every site occupies one or
// more words in a single pointer-free site-major arena.
// Small functions retain the one-word dataflow path; larger functions use one
// bounded nodes-by-words arena rather than per-node heap bitsets. The tracked
// population may exceed the final per-site root limit; compaction and admission
// apply that limit after this exact analysis.
func gcFrameLocalLivenessArena(body []byte, locals []shared.GCFrameLocal) (gcFrameLiveMasks, error) {
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	return gcFrameLocalLivenessArenaWithClassifier(body, locals, &classifier)
}

func gcFrameLocalLivenessArenaWithClassifier(body []byte, locals []shared.GCFrameLocal, classifier *wasm.ModuleInstructionClassifier) (gcFrameLiveMasks, error) {
	if len(locals) > shared.GCFrameTrackedLocalLimit {
		return gcFrameLiveMasks{}, fmt.Errorf("GC local liveness tracks %d locals, limit %d", len(locals), shared.GCFrameTrackedLocalLimit)
	}
	bits := make(map[uint32]uint32, len(locals))
	for i, local := range locals {
		bits[local.Index] = uint32(i)
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
			return gcFrameLiveMasks{}, err
		}
		node := gcLiveNode{flow: gcLiveNext, use: noGCLiveIndex, def: noGCLiveIndex, index: noGCLiveIndex}
		var imm wasm.InstructionImmediate
		if op == 0x0e { // br_table keeps every target, unlike the cheap classifier.
			n, err := r.U32()
			if err != nil {
				return gcFrameLiveMasks{}, err
			}
			count := uint64(n) + 1 // the vector plus its default target
			if count > uint64(^uint32(0)) || uint64(len(branchTargets))+count > uint64(^uint32(0)) {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness br_table target count exceeds implementation limit")
			}
			if count > uint64(maxGCFrameBranchTargets) || uint64(len(branchTargets))+count > uint64(maxGCFrameBranchTargets) {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness br_table target arena exceeds %d-byte implementation limit", maxGCFrameLivenessArenaBytes)
			}
			node.flow = gcLiveBrTable
			node.index = uint32(len(branchTargets))
			node.indexN = uint32(count)
			for i := uint64(0); i < count; i++ {
				depth, err := r.U32()
				if err != nil {
					return gcFrameLiveMasks{}, err
				}
				target, ok := gcLiveBranchFrame(stack, depth)
				if !ok {
					return gcFrameLiveMasks{}, fmt.Errorf("GC liveness br_table depth %d is out of range", depth)
				}
				branchTargets = append(branchTargets, uint32(target))
			}
			imm.Kind = wasm.InstrBrTable
		} else if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return gcFrameLiveMasks{}, err
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
		keep := node.use != noGCLiveIndex || node.def != noGCLiveIndex || node.nativeCall || node.flow == gcLiveBrTable
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10: // struct.new*, array.new*
				node.allocation = true
				keep = true
			}
		}

		nodeIndex := len(nodes)
		switch op {
		case 0x02, 0x03, 0x04: // block, loop, if
			keep = true
			frames = append(frames, gcLiveFrame{loop: op == 0x03, header: nodeIndex, elseNode: -1, endNode: -1})
			node.index = uint32(len(frames) - 1)
			stack = append(stack, int(node.index))
			if op == 0x04 {
				node.flow = gcLiveIf
			}
		case 0x05: // else
			keep = true
			if len(stack) <= 1 {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness else without if")
			}
			top := stack[len(stack)-1]
			frames[top].elseNode = nodeIndex
			node.index, node.flow = uint32(top), gcLiveElse
		case 0x0b: // end
			keep = true
			if len(stack) == 0 {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness end without frame")
			}
			top := stack[len(stack)-1]
			frames[top].endNode = nodeIndex
			stack = stack[:len(stack)-1]
		case 0x0c: // br
			keep = true
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness br depth %d is out of range", imm.Index)
			}
			node.flow, node.index = gcLiveBr, uint32(target)
		case 0x0d: // br_if
			keep = true
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness br_if depth %d is out of range", imm.Index)
			}
			node.flow, node.index = gcLiveBrIf, uint32(target)
		case 0x00, 0x0f: // unreachable, return
			keep = true
			node.flow = gcLiveStop
		}
		switch imm.Kind {
		case wasm.InstrBrOnNull, wasm.InstrBrOnNonNull, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
			keep = true
			target, ok := gcLiveBranchFrame(stack, imm.Index)
			if !ok {
				return gcFrameLiveMasks{}, fmt.Errorf("GC liveness reference branch depth %d is out of range", imm.Index)
			}
			node.flow, node.index = gcLiveBrIf, uint32(target)
		case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			keep = true
			node.flow = gcLiveStop
		}
		if !keep {
			continue
		}
		if node.nativeCall {
			nativeCallN++
		}
		if node.allocation {
			allocationN++
		}
		nodes = append(nodes, node)
	}
	if len(stack) != 0 || len(nodes) == 0 || frames[0].endNode < 0 {
		return gcFrameLiveMasks{}, fmt.Errorf("GC liveness body has unterminated control frames")
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
	branchEdgeN := 0
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
				return gcFrameLiveMasks{}, err
			}
		case gcLiveBrIf:
			addNext()
			if err := addTarget(int(nodes[i].index)); err != nil {
				return gcFrameLiveMasks{}, err
			}
		case gcLiveBrTable:
			start := int(nodes[i].index)
			end := start + int(nodes[i].indexN)
			targets := branchTargets[start:end]
			succN := 0
			for _, frameIndex := range targets {
				target, ok, err := resolveTarget(int(frameIndex))
				if err != nil {
					return gcFrameLiveMasks{}, err
				}
				if ok {
					targets[succN] = uint32(target)
					succN++
				}
			}
			// The liveness equation unions successors, so duplicate destinations
			// carry no information. Compact them once before bitmap dataflow to
			// keep repeated br_table labels from multiplying work by local width.
			targets = targets[:succN]
			slices.Sort(targets)
			targets = slices.Compact(targets)
			nodes[i].indexN = uint32(len(targets))
			branchEdgeN += len(targets)
		case gcLiveStop:
		}
	}
	wordCount := (len(locals) + 63) / 64
	if wordCount == 0 {
		wordCount = 1
	}
	if !gcFrameLivenessArenaFits(len(nodes), wordCount) {
		return gcFrameLiveMasks{}, fmt.Errorf("GC local liveness arena exceeds %d-byte implementation limit", maxGCFrameLivenessArenaBytes)
	}
	if !gcFrameLivenessWorkFits(len(nodes), branchEdgeN, wordCount) {
		return gcFrameLiveMasks{}, fmt.Errorf("GC local liveness graph exceeds %d bitmap-word implementation limit", maxGCFrameLivenessWorkWords)
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
	allocationN, nativeCallN = 0, 0
	for i := range nodes {
		if nodes[i].reachable {
			if nodes[i].allocation {
				allocationN++
			}
			if nodes[i].nativeCall {
				nativeCallN++
			}
		}
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
	if !gcFrameLiveMaskArenaFits(allocationN, nativeCallN, wordCount) {
		return gcFrameLiveMasks{}, fmt.Errorf("GC local liveness mask arena exceeds %d-byte implementation limit", maxGCFrameLivenessArenaBytes)
	}
	masks := newGCFrameLiveMasks(allocationN, nativeCallN, wordCount)
	allocationIndex, callIndex := 0, 0
	for i := range nodes {
		if !nodes[i].reachable {
			continue
		}
		words := liveIn[i*wordCount : (i+1)*wordCount]
		if nodes[i].allocation {
			copy(masks.site(allocationIndex), words)
			allocationIndex++
		}
		if nodes[i].nativeCall {
			copy(masks.site(allocationN+callIndex), words)
			callIndex++
		}
	}
	return masks, nil
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
		if gcFrameInstructionMayAllocate(op, imm) {
			return true
		}
	}
	return false
}

func gcFrameInstructionMayAllocate(op byte, imm wasm.InstructionImmediate) bool {
	if op != 0xfb {
		return false
	}
	switch imm.Subopcode {
	case 0, 1, 6, 7, 8, 9, 10:
		return true
	default:
		return false
	}
}

func gcFrameBodyMayAllocateWithClassifier(body []byte, classifier *wasm.ModuleInstructionClassifier) bool {
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
		if gcFrameInstructionMayAllocate(op, imm) {
			return true
		}
	}
	return false
}

func gcFrameAllLiveMasksArena(body []byte, localRoots int) (gcFrameLiveMasks, error) {
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	return gcFrameAllLiveMasksArenaWithClassifier(body, localRoots, &classifier)
}

func gcFrameAllLiveMasksArenaWithClassifier(body []byte, localRoots int, classifier *wasm.ModuleInstructionClassifier) (gcFrameLiveMasks, error) {
	if localRoots < 0 || localRoots > shared.GCFrameTrackedLocalLimit {
		return gcFrameLiveMasks{}, fmt.Errorf("GC conservative liveness tracks %d locals, representation limit %d", localRoots, shared.GCFrameTrackedLocalLimit)
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
		return gcFrameLiveMasks{}, err
	}
	if !gcFrameLiveMaskArenaFits(allocationN, callN, wordCount) {
		return gcFrameLiveMasks{}, fmt.Errorf("GC conservative liveness mask arena exceeds %d-byte implementation limit", maxGCFrameLivenessArenaBytes)
	}
	masks := newGCFrameLiveMasks(allocationN, callN, wordCount)
	allocationIndex, callIndex := 0, 0
	appendMask := func(site int) {
		copy(masks.site(site), words)
	}
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return gcFrameLiveMasks{}, readErr
		}
		var imm wasm.InstructionImmediate
		if readErr := classifier.ClassifyInto(r, op, &imm); readErr != nil {
			return gcFrameLiveMasks{}, readErr
		}
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10:
				appendMask(allocationIndex)
				allocationIndex++
			}
		}
		if imm.Kind == wasm.InstrCall || imm.Kind == wasm.InstrCallIndirect || imm.Kind == wasm.InstrCallRef {
			appendMask(allocationN + callIndex)
			callIndex++
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
