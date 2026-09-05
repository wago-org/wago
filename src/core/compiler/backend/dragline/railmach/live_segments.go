package railmach

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// CanUseSegmentedLiveness reports whether the staged allocator can model this
// function without needing loop-aware transfer pricing.
func CanUseSegmentedLiveness(f *Func) bool {
	if f == nil || len(f.Blocks) < 2 {
		return false
	}
	for _, block := range f.Blocks {
		if block.Flags&railssa.BlockLoopHeader != 0 {
			return false
		}
	}
	return true
}

// buildLiveSegments records only intervals with real CFG-layout holes. The
// conservative Start/End span remains available for ordering and cost, while
// allocation conflict checks use this sparse slab when SegmentCount is nonzero.
func buildLiveSegments(f *Func, schedule *Schedule, allocation *Allocation) error {
	if !CanUseSegmentedLiveness(f) || len(allocation.Intervals) == 0 {
		// Loop-aware holes need to price transfers and allocation changes on hot
		// backedges. Stage 7B proves the one-location model on acyclic CFGs before
		// true hot/cold split products are introduced.
		return nil
	}
	scratch := &allocation.scratch
	blockAt := resize(scratch.segmentBlockAt, len(f.Insts)+1)
	for block := range f.Blocks {
		start, end := blockScheduleStart(f, schedule, uint32(block)), blockScheduleEnd(f, schedule, uint32(block))
		if start > uint32(len(f.Insts)) || end > uint32(len(f.Insts)) || start > end {
			return fmt.Errorf("railmach: live segments saw invalid block %d range %d..%d", block, start, end)
		}
		for ordinal := start; ordinal < end; ordinal++ {
			blockAt[ordinal] = uint32(block)
		}
		if end == uint32(len(f.Insts)) {
			blockAt[end] = uint32(block)
		}
	}
	eligible := resize(scratch.segmentEligible, len(f.VRegs))
	eligibleCount := 0
	for _, interval := range allocation.Intervals {
		startOrdinal, endOrdinal := min(interval.Start/6, uint32(len(f.Insts))), min(interval.End/6, uint32(len(f.Insts)))
		// Loop nesting raises block weight by eight. Keep this first segmented
		// allocation slice on cold/control-flow ranges: sharing a location across
		// hot holes can trade fewer spills for extra physical copies, and that
		// choice needs the calibrated cost model planned for true split products.
		if interval.Weight < 8 && blockAt[startOrdinal] != blockAt[endOrdinal] {
			eligible[interval.Reg] = true
			eligibleCount++
		}
	}
	if eligibleCount == 0 {
		scratch.segmentBlockAt, scratch.segmentEligible = blockAt, eligible
		return nil
	}
	predOffsets := resize(scratch.segmentPredOff, len(f.Blocks)+1)
	for _, edge := range f.Edges {
		if int(edge.From) >= len(f.Blocks) || int(edge.To) >= len(f.Blocks) {
			return fmt.Errorf("railmach: live segments saw invalid edge %d -> %d", edge.From, edge.To)
		}
		predOffsets[edge.To+1]++
	}
	for block := 1; block < len(predOffsets); block++ {
		predOffsets[block] += predOffsets[block-1]
	}
	preds := resize(scratch.segmentPreds, len(f.Edges))
	cursor := resize(scratch.segmentSeen, len(f.Blocks))
	copy(cursor, predOffsets[:len(f.Blocks)])
	for _, edge := range f.Edges {
		preds[cursor[edge.To]] = uint32(edge.From)
		cursor[edge.To]++
	}

	useHead := resize(scratch.segmentUseHead, len(f.VRegs))
	useNext := scratch.segmentUseNext[:0]
	useAt := scratch.segmentUseAt[:0]
	appendUse := func(reg VReg, block railssa.BlockID) {
		if reg == 0 || int(reg) >= len(useHead) || !eligible[reg] || int(block) >= len(f.Blocks) {
			return
		}
		useAt = append(useAt, uint32(block))
		useNext = append(useNext, useHead[reg])
		useHead[reg] = uint32(len(useAt))
	}
	for instructionID, instruction := range f.Insts {
		block := scheduleInstructionBlock(f, schedule, uint32(instructionID))
		if instruction.Op == 0 || int(block) >= len(f.Blocks) {
			return fmt.Errorf("railmach: live segments saw invalid instruction block %d", block)
		}
		if (instruction.Op == wasm.InstrBrOnCast || instruction.Op == wasm.InstrBrOnCastFail) && instruction.Result != 0 {
			appendUse(instruction.Result, block)
		}
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Flags&OperandColdRemat != 0 {
				eligible[operand.Reg] = false
				continue
			}
			appendUse(operand.Reg, block)
		}
	}
	for _, transfer := range f.Transfers {
		appendUse(transfer.Src, transfer.From)
	}
	for _, result := range f.Results {
		if result != 0 && int(result) < len(eligible) {
			eligible[result] = false
		}
	}

	seen := resize(cursor, len(f.Blocks))
	work := scratch.segmentWork[:0]
	liveBlocks := scratch.segmentBlocks[:0]
	segments := allocation.LiveSegments[:0]
	ranges := allocation.LiveSegmentRanges[:0]
	generation := uint32(0)
	// Candidate schedules rebuild the same block liveness in different logical
	// coordinates. Bound predecessor traversal to linear work so large, branchy
	// functions retain their conservative spans instead of multiplying compile
	// latency by values x edges. Completed ranges remain exact and independently
	// verified; the budget never manufactures a partial live range.
	workLimit := max(2048, len(f.Insts)*4+len(f.Edges)*2)
	workUsed := 0
	exhausted := false
	for intervalIndex := range allocation.Intervals {
		interval := &allocation.Intervals[intervalIndex]
		interval.Flags &^= liveIntervalSegmented
		if !eligible[interval.Reg] || useHead[interval.Reg] == 0 {
			continue
		}
		generation++
		if generation == 0 {
			clear(seen)
			generation = 1
		}
		work = work[:0]
		liveBlocks = liveBlocks[:0]
		for use := useHead[interval.Reg]; use != 0; use = useNext[use-1] {
			work = append(work, useAt[use-1])
		}
		definitionBlock := blockAt[min(interval.Start/6, uint32(len(f.Insts)))]
		for len(work) != 0 {
			if workUsed >= workLimit {
				exhausted = true
				break
			}
			block := work[len(work)-1]
			work = work[:len(work)-1]
			workUsed++
			if seen[block] == generation {
				continue
			}
			seen[block] = generation
			liveBlocks = append(liveBlocks, block)
			if block == definitionBlock {
				continue
			}
			predecessors := preds[predOffsets[block]:predOffsets[block+1]]
			if workUsed+len(predecessors) > workLimit {
				exhausted = true
				break
			}
			workUsed += len(predecessors)
			for _, predecessor := range predecessors {
				work = append(work, predecessor)
			}
		}
		if exhausted {
			break
		}
		if seen[definitionBlock] != generation {
			seen[definitionBlock] = generation
			liveBlocks = append(liveBlocks, definitionBlock)
		}
		slices.Sort(liveBlocks)
		start := len(segments)
		for _, block := range liveBlocks {
			blockStart := blockScheduleStart(f, schedule, block) * 6
			blockEnd := blockScheduleEnd(f, schedule, block) * 6
			segment := LiveSegment{Start: max(blockStart, interval.Start), End: min(blockEnd, interval.End)}
			if segment.Start > segment.End {
				continue
			}
			if len(segments) != start && segment.Start <= segments[len(segments)-1].End+1 {
				segments[len(segments)-1].End = max(segments[len(segments)-1].End, segment.End)
			} else {
				segments = append(segments, segment)
			}
		}
		count := len(segments) - start
		if count < 2 || segments[start].Start != interval.Start || segments[len(segments)-1].End != interval.End {
			segments = segments[:start]
			continue
		}
		if count > int(^uint16(0)) {
			return &BudgetError{Resource: "segments per live interval", Required: uint64(count), Limit: uint64(^uint16(0))}
		}
		if uint64(start) > uint64(^uint32(0)) {
			return &BudgetError{Resource: "live segment slab", Required: uint64(start) + uint64(count), Limit: uint64(^uint32(0)) + 1}
		}
		interval.Flags |= liveIntervalSegmented
		ranges = append(ranges, LiveSegmentRange{Reg: interval.Reg, SegmentStart: uint32(start), SegmentCount: uint16(count)})
	}
	// Keep sparse ranges ordered by vreg for logarithmic lookup, then repack the
	// segment slab in the same order so verification can prove exact ownership
	// without another dense index.
	slices.SortFunc(ranges, func(a, b LiveSegmentRange) int { return int(a.Reg) - int(b.Reg) })
	repacked := scratch.segmentRepack[:0]
	for index := range ranges {
		range_ := &ranges[index]
		start := int(range_.SegmentStart)
		end := start + int(range_.SegmentCount)
		range_.SegmentStart = uint32(len(repacked))
		repacked = append(repacked, segments[start:end]...)
	}
	rangeAt := resize(scratch.segmentRangeAt, len(f.VRegs))
	for index, range_ := range ranges {
		rangeAt[range_.Reg] = uint32(index) + 1
	}
	allocation.LiveSegments = repacked
	allocation.LiveSegmentRanges = ranges
	scratch.segmentRepack = segments[:0]
	scratch.segmentPredOff, scratch.segmentPreds = predOffsets, preds
	scratch.segmentUseHead, scratch.segmentUseNext, scratch.segmentUseAt = useHead, useNext, useAt
	scratch.segmentSeen, scratch.segmentWork, scratch.segmentBlocks = seen, work, liveBlocks
	scratch.segmentBlockAt, scratch.segmentEligible = blockAt, eligible
	scratch.segmentRangeAt = rangeAt
	return nil
}

func allocationLiveRangeCrossesPositions(allocation *Allocation, interval LiveInterval, positions []uint32) bool {
	index, _ := slices.BinarySearch(positions, interval.Start+1)
	if interval.Flags&liveIntervalSegmented == 0 {
		return index < len(positions) && positions[index] < interval.End
	}
	for _, position := range positions[index:] {
		if position >= interval.End {
			break
		}
		if position > interval.Start && position < interval.End && allocationLiveRangeContains(allocation, interval, position) {
			return true
		}
	}
	return false
}
