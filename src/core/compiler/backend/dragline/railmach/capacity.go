package railmach

import "unsafe"

// PipelineCapacityBytes reports all reusable backing storage owned by the
// native RailMach planning products, including private allocator and verifier
// scratch. Shared embedded Allocation storage is counted exactly once.
func PipelineCapacityBytes(f *Func, selection *SelectionPlan, dag *DependencyDAG, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit, postRA *PostRAPlan, remat *RematPlan, layout *BlockLayout) uint64 {
	bytes := CapacityBytes(f)
	if selection != nil {
		bytes += capacityBytes(selection.Selections) + capacityBytes(selection.Forms) + capacityBytes(selection.Combinations) + capacityBytes(selection.AddressFolds) + capacityBytes(selection.useCount) + capacityBytes(selection.soleConsumer) + capacityBytes(selection.verifyUseCount)
	}
	if dag != nil {
		bytes += capacityBytes(dag.Offsets) + capacityBytes(dag.Dependencies) + capacityBytes(dag.scratch) + capacityBytes(dag.verifySeen) + capacityBytes(dag.definition) + capacityBytes(dag.defined)
	}
	if schedule != nil {
		bytes += capacityBytes(schedule.Order) + capacityBytes(schedule.BlockRanges) + capacityBytes(schedule.BlockOf) + capacityBytes(schedule.remaining) + capacityBytes(schedule.sinkBefore) + capacityBytes(schedule.sinkProducer) + capacityBytes(schedule.lateBefore) + capacityBytes(schedule.lateProducer) + capacityBytes(schedule.fusionBefore) + capacityBytes(schedule.fusionSource) + capacityBytes(schedule.verifyPosition) + capacityBytes(schedule.verifySeen) + capacityBytes(schedule.uses) + capacityBytes(schedule.blockCandidates) + capacityBytes(schedule.pressureSpecial)
	}
	if allocation != nil {
		bytes += capacityBytes(allocation.Locations) + capacityBytes(allocation.Intervals) + capacityBytes(allocation.FixedMoves) + capacityBytes(allocation.InstructionPositions)
		scratch := &allocation.scratch
		bytes += capacityBytes(scratch.starts) + capacityBytes(scratch.ends) + capacityBytes(scratch.weights) + capacityBytes(scratch.used) + capacityBytes(scratch.callPositions) + capacityBytes(scratch.fixedAt) + capacityBytes(scratch.fixedConflict) + capacityBytes(scratch.affinitySource) + capacityBytes(scratch.affinityWeight) + capacityBytes(scratch.gprActive) + capacityBytes(scratch.fprActive) + capacityBytes(scratch.spillActive) + capacityBytes(scratch.spillFree) + capacityBytes(scratch.verifySeen) + capacityBytes(scratch.positionSeen)
		bytes += capacityBytes(allocation.SpillSets) + capacityBytes(allocation.SpillMembers) + capacityBytes(allocation.Fragments) + capacityBytes(allocation.priorityIntervals) + capacityBytes(allocation.callPositions) + capacityBytes(allocation.candidateVictims) + capacityBytes(allocation.bestVictims) + capacityBytes(allocation.verifySpillSeen) + capacityBytes(allocation.occupantNext) + capacityBytes(allocation.intervalByReg) + capacityBytes(allocation.regionalStates) + capacityBytes(allocation.regionalSegments)
	}
	if exit != nil {
		bytes += capacityBytes(exit.Moves) + capacityBytes(exit.EdgeMoves) + capacityBytes(exit.FixedMoves) + capacityBytes(exit.FixedPoints) + capacityBytes(exit.fixedScratch) + capacityBytes(exit.pending) + capacityBytes(exit.predSuccs) + capacityBytes(exit.succPreds)
	}
	if postRA != nil {
		bytes += capacityBytes(postRA.Rewrites) + capacityBytes(postRA.position) + capacityBytes(postRA.seen) + capacityBytes(postRA.uses)
	}
	if remat != nil {
		bytes += capacityBytes(remat.Decisions)
	}
	if layout != nil {
		bytes += capacityBytes(layout.Order) + capacityBytes(layout.Position)
	}
	return bytes
}

func capacityBytes[T any](values []T) uint64 {
	var value T
	return uint64(cap(values)) * uint64(unsafe.Sizeof(value))
}
