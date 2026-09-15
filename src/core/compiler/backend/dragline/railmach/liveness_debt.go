package railmach

import (
	"fmt"
	"slices"
)

// LivenessDebt describes interference created by representing a value with one
// contiguous interval even when CFG liveness contains holes. Positions use the
// same six-slot logical coordinate system as allocation intervals.
type LivenessDebt struct {
	ValuesWithHoles           uint32 `json:"values_with_holes"`
	TotalSegments             uint32 `json:"total_segments"`
	FalseLivePositions        uint64 `json:"false_live_positions"`
	FalseInterferencePairs    uint64 `json:"false_interference_pairs"`
	WeightedFalseInterference uint64 `json:"weighted_false_interference"`
	PeakCurrentGPR            uint32 `json:"peak_current_gpr"`
	PeakSegmentedGPR          uint32 `json:"peak_segmented_gpr"`
	PeakCurrentFPR            uint32 `json:"peak_current_fpr"`
	PeakSegmentedFPR          uint32 `json:"peak_segmented_fpr"`
}

type liveSegment struct {
	start uint32
	end   uint32
}

// MeasureLivenessDebt reconstructs block-aware liveness for diagnostics. It is
// intentionally separate from allocation: callers can quantify false
// interference before deciding whether segmented allocation is justified.
func MeasureLivenessDebt(f *Func, schedule *Schedule, allocation *GreedyAllocation) (LivenessDebt, error) {
	if f == nil || schedule == nil || allocation == nil || len(schedule.BlockRanges) != len(f.Blocks) || len(schedule.BlockOf) != len(f.Insts) {
		return LivenessDebt{}, fmt.Errorf("railmach: liveness debt requires a complete scheduled allocation")
	}
	segments := make([][]liveSegment, len(f.VRegs))
	preds := make([][]uint32, len(f.Blocks))
	for _, edge := range f.Edges {
		if int(edge.From) >= len(f.Blocks) || int(edge.To) >= len(f.Blocks) {
			return LivenessDebt{}, fmt.Errorf("railmach: liveness debt saw invalid edge %d -> %d", edge.From, edge.To)
		}
		preds[edge.To] = append(preds[edge.To], uint32(edge.From))
	}
	uses := make([][]uint32, len(f.VRegs))
	for instructionID := range f.Insts {
		block := uint32(schedule.BlockOf[instructionID])
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Reg != 0 && int(operand.Reg) < len(uses) {
				uses[operand.Reg] = appendUniqueBlock(uses[operand.Reg], block)
			}
		}
	}
	for _, transfer := range f.Transfers {
		if transfer.Src != 0 && int(transfer.Src) < len(uses) {
			uses[transfer.Src] = appendUniqueBlock(uses[transfer.Src], uint32(transfer.From))
		}
	}
	result := make([]bool, len(f.VRegs))
	for _, value := range f.Results {
		if value != 0 && int(value) < len(result) {
			result[value] = true
		}
	}
	var debt LivenessDebt
	for _, interval := range allocation.Intervals {
		if interval.Reg == 0 || int(interval.Reg) >= len(f.VRegs) || interval.End < interval.Start {
			return LivenessDebt{}, fmt.Errorf("railmach: liveness debt saw invalid interval %#v", interval)
		}
		// Function results have a synthetic use beyond the scheduled blocks.
		// Keep their conservative interval rather than manufacture a false hole.
		if result[interval.Reg] || len(uses[interval.Reg]) == 0 {
			segments[interval.Reg] = []liveSegment{{start: interval.Start, end: interval.End}}
			debt.TotalSegments++
			continue
		}
		definitionBlock := blockAtLogicalPosition(schedule, interval.Start)
		liveBlocks := make([]bool, len(f.Blocks))
		work := append([]uint32(nil), uses[interval.Reg]...)
		for len(work) != 0 {
			block := work[len(work)-1]
			work = work[:len(work)-1]
			if int(block) >= len(liveBlocks) || liveBlocks[block] {
				continue
			}
			liveBlocks[block] = true
			if block == definitionBlock {
				continue
			}
			work = append(work, preds[block]...)
		}
		liveBlocks[definitionBlock] = true
		var valueSegments []liveSegment
		for block, live := range liveBlocks {
			if !live {
				continue
			}
			range_ := schedule.BlockRanges[block]
			if range_.Count == 0 {
				continue
			}
			segment := liveSegment{start: range_.Start * 6, end: (range_.Start+range_.Count)*6 - 1}
			segment.start = max(segment.start, interval.Start)
			segment.end = min(segment.end, interval.End)
			if segment.start <= segment.end {
				valueSegments = append(valueSegments, segment)
			}
		}
		slices.SortFunc(valueSegments, func(a, b liveSegment) int { return int(a.start) - int(b.start) })
		valueSegments = mergeLiveSegments(valueSegments)
		if len(valueSegments) == 0 {
			valueSegments = append(valueSegments, liveSegment{start: interval.Start, end: interval.End})
		}
		segments[interval.Reg] = valueSegments
		debt.TotalSegments += uint32(len(valueSegments))
		truePositions := uint64(0)
		for _, segment := range valueSegments {
			truePositions += uint64(segment.end-segment.start) + 1
		}
		span := uint64(interval.End-interval.Start) + 1
		if truePositions < span {
			debt.FalseLivePositions += span - truePositions
			if len(valueSegments) > 1 {
				debt.ValuesWithHoles++
			}
		}
	}
	for left := range allocation.Intervals {
		a := allocation.Intervals[left]
		for right := left + 1; right < len(allocation.Intervals); right++ {
			b := allocation.Intervals[right]
			if b.Start > a.End || a.Bank != b.Bank || !intervalsOverlap(a, b) || liveSegmentsOverlap(segments[a.Reg], segments[b.Reg]) {
				continue
			}
			debt.FalseInterferencePairs++
			debt.WeightedFalseInterference += uint64(min(a.Weight, b.Weight))
		}
	}
	for position := uint32(0); position < uint32(len(f.Insts))*6; position++ {
		var current, segmented [2]uint32
		for _, interval := range allocation.Intervals {
			bank := 0
			if interval.Bank == BankFPR {
				bank = 1
			}
			if interval.Start <= position && position <= interval.End {
				current[bank]++
			}
			if liveSegmentsContain(segments[interval.Reg], position) {
				segmented[bank]++
			}
		}
		debt.PeakCurrentGPR = max(debt.PeakCurrentGPR, current[0])
		debt.PeakCurrentFPR = max(debt.PeakCurrentFPR, current[1])
		debt.PeakSegmentedGPR = max(debt.PeakSegmentedGPR, segmented[0])
		debt.PeakSegmentedFPR = max(debt.PeakSegmentedFPR, segmented[1])
	}
	return debt, nil
}

func appendUniqueBlock(blocks []uint32, block uint32) []uint32 {
	for _, existing := range blocks {
		if existing == block {
			return blocks
		}
	}
	return append(blocks, block)
}

func blockAtLogicalPosition(schedule *Schedule, position uint32) uint32 {
	ordinal := position / 6
	for block, range_ := range schedule.BlockRanges {
		if ordinal >= range_.Start && ordinal < range_.Start+range_.Count {
			return uint32(block)
		}
	}
	return 0
}

func mergeLiveSegments(segments []liveSegment) []liveSegment {
	merged := segments[:0]
	for _, segment := range segments {
		if len(merged) == 0 || segment.start > merged[len(merged)-1].end+1 {
			merged = append(merged, segment)
			continue
		}
		merged[len(merged)-1].end = max(merged[len(merged)-1].end, segment.end)
	}
	return merged
}

func liveSegmentsOverlap(a, b []liveSegment) bool {
	left, right := 0, 0
	for left < len(a) && right < len(b) {
		if a[left].end < b[right].start {
			left++
		} else if b[right].end < a[left].start {
			right++
		} else {
			return true
		}
	}
	return false
}

func liveSegmentsContain(segments []liveSegment, position uint32) bool {
	for _, segment := range segments {
		if position < segment.start {
			return false
		}
		if position <= segment.end {
			return true
		}
	}
	return false
}
