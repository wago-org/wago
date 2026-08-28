package railmach

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type LocationKind uint8

const (
	LocationInvalid LocationKind = iota
	LocationRegister
	LocationSpill
	LocationRematerialize
)

type Location struct {
	Kind  LocationKind
	Bank  Bank
	Index uint16
}

type LiveInterval struct {
	Reg    VReg
	Start  uint32
	End    uint32
	Weight uint32
	Bank   Bank
	_      [3]byte
}

type FixedMove struct {
	Position uint32
	Reg      VReg
	Bank     Bank
	Physical uint8
	_        uint16
}

type Allocation struct {
	Locations            []Location
	Intervals            []LiveInterval
	FixedMoves           []FixedMove
	InstructionPositions []uint32
	FrameBytes           uint32
	SpillSlots           uint16

	scratch linearQScratch
}

type activeInterval struct {
	interval LiveInterval
	physical uint16
}

type spillInterval struct {
	end  uint32
	slot uint16
}

// linearQScratch retains the dense liveness tables and bounded worklists used
// by repeated schedule candidates. It is deliberately owned by the allocation
// product so verification remains independent of allocator-private state.
type linearQScratch struct {
	starts         []uint32
	ends           []uint32
	weights        []uint32
	used           []bool
	callPositions  []uint32
	fixedAt        []uint8
	fixedConflict  []bool
	affinitySource []VReg
	affinityWeight []uint32
	gprActive      []activeInterval
	fprActive      []activeInterval
	spillActive    []spillInterval
	spillFree      []uint16
	verifySeen     []bool
	positionSeen   []bool
}

type LinearQConfig struct {
	GPRs uint8
	FPRs uint8
}

func DefaultLinearQConfig(target Target) LinearQConfig {
	switch target {
	case TargetAMD64:
		return LinearQConfig{GPRs: 10, FPRs: 12}
	case TargetARM64:
		return LinearQConfig{GPRs: 20, FPRs: 24}
	default:
		return LinearQConfig{}
	}
}

// AllocateLinearQ is the deterministic legal-allocation baseline. This first
// stage models separate banks, fixed uses, call-clobber regions, weighted edge
// affinities, dense reusable spill slots, and rematerializable constants.
// Fixed-use moves are retained explicitly for late SSA exit.
func AllocateLinearQ(f *Func, config LinearQConfig, reuse *Allocation) (*Allocation, error) {
	return allocateLinearQ(f, nil, config, reuse)
}

// AllocateLinearQForSchedule computes live ranges in the selected per-block
// instruction order. The schedule has already passed dependency verification;
// this boundary additionally verifies its dense block-local permutation.
func AllocateLinearQForSchedule(f *Func, schedule *Schedule, config LinearQConfig, reuse *Allocation) (*Allocation, error) {
	if schedule == nil {
		return nil, fmt.Errorf("railmach: scheduled RALinearQ requires a schedule")
	}
	return allocateLinearQ(f, schedule, config, reuse)
}

func allocateLinearQ(f *Func, schedule *Schedule, config LinearQConfig, reuse *Allocation) (*Allocation, error) {
	if err := Verify(f); err != nil {
		return nil, err
	}
	if config.GPRs == 0 || config.FPRs == 0 {
		return nil, fmt.Errorf("railmach: RALinearQ requires nonzero GPR and FPR sets")
	}
	if reuse == nil {
		reuse = new(Allocation)
	}
	locations := resize(reuse.Locations, len(f.VRegs))
	intervals := reuse.Intervals[:0]
	fixedMoves := reuse.FixedMoves[:0]
	instructionPositions := resize(reuse.InstructionPositions, len(f.Insts))
	scratch := reuse.scratch
	*reuse = Allocation{Locations: locations, Intervals: intervals, FixedMoves: fixedMoves, InstructionPositions: instructionPositions, scratch: scratch}
	positionSeen := resize(reuse.scratch.positionSeen, len(f.Insts))
	reuse.scratch.positionSeen = positionSeen
	if err := populateInstructionPositions(f, schedule, reuse.InstructionPositions, positionSeen); err != nil {
		return nil, err
	}

	starts := resize(reuse.scratch.starts, len(f.VRegs))
	ends := resize(reuse.scratch.ends, len(f.VRegs))
	weights := resize(reuse.scratch.weights, len(f.VRegs))
	used := resize(reuse.scratch.used, len(f.VRegs))
	callPositions := reuse.scratch.callPositions[:0]
	fixedAt := resize(reuse.scratch.fixedAt, len(f.VRegs))
	for index := range fixedAt {
		fixedAt[index] = NoFixedReg
	}
	fixedConflict := resize(reuse.scratch.fixedConflict, len(f.VRegs))
	affinitySource := resize(reuse.scratch.affinitySource, len(f.VRegs))
	affinityWeight := resize(reuse.scratch.affinityWeight, len(f.VRegs))
	reuse.scratch.starts, reuse.scratch.ends, reuse.scratch.weights = starts, ends, weights
	reuse.scratch.used, reuse.scratch.callPositions = used, callPositions
	reuse.scratch.fixedAt, reuse.scratch.fixedConflict = fixedAt, fixedConflict
	reuse.scratch.affinitySource, reuse.scratch.affinityWeight = affinitySource, affinityWeight
	for id, data := range f.VRegs {
		if id == 0 {
			continue
		}
		if data.Flags&(VRegInitial|VRegBlockParam) != 0 {
			starts[id] = data.Def
		} else {
			starts[id] = scheduledLogicalPosition(data.Def, reuse.InstructionPositions)
		}
		ends[id] = starts[id]
		weights[id] = 1
	}
	for instructionID, instruction := range f.Insts {
		position := reuse.InstructionPositions[instructionID]*6 + 2
		if instruction.Op == 0 {
			return nil, fmt.Errorf("railmach: RALinearQ saw invalid instruction %d", instructionID)
		}
		if IsCall(instruction.Op) {
			callPositions = append(callPositions, reuse.InstructionPositions[instructionID]*6+4)
		}
		if (instruction.Op == wasm.InstrBrOnCast || instruction.Op == wasm.InstrBrOnCastFail) && instruction.Result != 0 {
			// The helper result is consumed by the control edge after the call. It
			// is not a Wasm operand-stack value, so retain that implicit use here.
			used[instruction.Result] = true
			ends[instruction.Result] = reuse.InstructionPositions[instructionID]*6 + 5
			weights[instruction.Result]++
		}
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Flags&OperandColdRemat != 0 {
				continue
			}
			used[operand.Reg] = true
			if position > ends[operand.Reg] {
				ends[operand.Reg] = position
			}
			weights[operand.Reg]++
			if operand.Flags&OperandFixed != 0 {
				if fixedAt[operand.Reg] == NoFixedReg {
					fixedAt[operand.Reg] = operand.Fixed
				} else if fixedAt[operand.Reg] != operand.Fixed {
					fixedConflict[operand.Reg] = true
				}
			}
		}
	}
	for _, transfer := range f.Transfers {
		position := blockScheduleEnd(f, schedule, uint32(transfer.From)) * 6
		used[transfer.Src], used[transfer.Dst] = true, true
		if position > ends[transfer.Src] {
			ends[transfer.Src] = position
		}
		if transfer.Weight > weights[transfer.Src] {
			weights[transfer.Src] = transfer.Weight
		}
		if transfer.Weight >= affinityWeight[transfer.Dst] {
			affinitySource[transfer.Dst], affinityWeight[transfer.Dst] = transfer.Src, transfer.Weight
		}
	}
	resultPosition := uint32(len(f.Insts))*6 + 5
	for _, result := range f.Results {
		used[result] = true
		if resultPosition > ends[result] {
			ends[result] = resultPosition
		}
	}
	extendLoopLiveIntervals(f, starts, ends, used)
	reuse.scratch.callPositions = callPositions
	for id := 1; id < len(f.VRegs); id++ {
		data := f.VRegs[id]
		if !used[id] {
			continue
		}
		reuse.Intervals = append(reuse.Intervals, LiveInterval{Reg: VReg(id), Start: starts[id], End: ends[id], Weight: weights[id], Bank: data.Bank})
	}
	slices.SortFunc(reuse.Intervals, func(a, b LiveInterval) int {
		if a.Start != b.Start {
			if a.Start < b.Start {
				return -1
			}
			return 1
		}
		if a.End != b.End {
			if a.End < b.End {
				return -1
			}
			return 1
		}
		return int(a.Reg) - int(b.Reg)
	})

	var gprStorage, fprStorage [64]bool
	gprFree := gprStorage[:config.GPRs]
	fprFree := fprStorage[:config.FPRs]
	for i := range gprFree {
		gprFree[i] = true
	}
	for i := range fprFree {
		fprFree[i] = true
	}
	gprActive := reuse.scratch.gprActive[:0]
	fprActive := reuse.scratch.fprActive[:0]
	spillActive := reuse.scratch.spillActive[:0]
	spillFree := reuse.scratch.spillFree[:0]
	nextSpill := uint16(0)

	expireRegisters := func(position uint32, active *[]activeInterval, free []bool) {
		kept := (*active)[:0]
		for _, item := range *active {
			if item.interval.End < position {
				free[item.physical] = true
			} else {
				kept = append(kept, item)
			}
		}
		*active = kept
	}
	expireSpills := func(position uint32) {
		kept := spillActive[:0]
		for _, item := range spillActive {
			if item.end < position {
				spillFree = append(spillFree, item.slot)
			} else {
				kept = append(kept, item)
			}
		}
		spillActive = kept
	}
	spill := func(interval LiveInterval) Location {
		expireSpills(interval.Start)
		var slot uint16
		if len(spillFree) != 0 {
			slices.Sort(spillFree)
			slot = spillFree[0]
			spillFree = spillFree[1:]
		} else {
			slot = nextSpill
			nextSpill++
		}
		spillActive = append(spillActive, spillInterval{end: interval.End, slot: slot})
		return Location{Kind: LocationSpill, Bank: interval.Bank, Index: slot}
	}
	for _, interval := range reuse.Intervals {
		free, active := gprFree, &gprActive
		if interval.Bank == BankFPR {
			free, active = fprFree, &fprActive
		}
		expireRegisters(interval.Start, active, free)
		crossesCall := false
		for _, call := range callPositions {
			if interval.Start < call && call < interval.End {
				crossesCall = true
				break
			}
		}
		location := Location{}
		if !crossesCall {
			preferred := -1
			if !fixedConflict[interval.Reg] && fixedAt[interval.Reg] != NoFixedReg && int(fixedAt[interval.Reg]) < len(free) {
				preferred = int(fixedAt[interval.Reg])
			}
			if preferred < 0 && affinitySource[interval.Reg] != 0 {
				source := reuse.Locations[affinitySource[interval.Reg]]
				if source.Kind == LocationRegister && source.Bank == interval.Bank && int(source.Index) < len(free) {
					preferred = int(source.Index)
				}
			}
			if preferred >= 0 && free[preferred] {
				location = Location{Kind: LocationRegister, Bank: interval.Bank, Index: uint16(preferred)}
			} else {
				for physical, available := range free {
					if available {
						location = Location{Kind: LocationRegister, Bank: interval.Bank, Index: uint16(physical)}
						break
					}
				}
			}
		}
		if location.Kind == LocationInvalid {
			if f.VRegs[interval.Reg].Flags&VRegRematerializable != 0 {
				location = Location{Kind: LocationRematerialize, Bank: interval.Bank}
			} else {
				location = spill(interval)
			}
		} else {
			free[location.Index] = false
			*active = append(*active, activeInterval{interval: interval, physical: location.Index})
		}
		reuse.Locations[interval.Reg] = location
	}
	for instructionID := range f.Insts {
		position := reuse.InstructionPositions[instructionID]*6 + 2
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Flags&OperandFixed == 0 || operand.Flags&OperandColdRemat != 0 {
				continue
			}
			location := reuse.Locations[operand.Reg]
			if location.Kind != LocationRegister || location.Index != uint16(operand.Fixed) {
				reuse.FixedMoves = append(reuse.FixedMoves, FixedMove{Position: position, Reg: operand.Reg, Bank: operand.Bank, Physical: operand.Fixed})
			}
		}
	}
	reuse.scratch.gprActive, reuse.scratch.fprActive = gprActive, fprActive
	reuse.scratch.spillActive, reuse.scratch.spillFree = spillActive, spillFree
	reuse.SpillSlots = nextSpill
	reuse.FrameBytes = (uint32(nextSpill)*8 + 15) &^ 15
	if err := verifyAllocationReusingScratch(f, reuse, config); err != nil {
		return nil, err
	}
	return reuse, nil
}

// extendLoopLiveIntervals keeps values defined outside a loop live through
// each backedge when they are used in the loop. A source-linear interval that
// ended at an early loop use otherwise allowed a later branch result to reuse
// and overwrite the invariant's physical register before the next iteration.
// Loop-header block parameters are excluded because their incoming edge move
// deliberately replaces the previous iteration's value.
func extendLoopLiveIntervals(f *Func, starts, ends []uint32, used []bool) {
	for _, edge := range f.Edges {
		if int(edge.From) >= len(f.Blocks) || int(edge.To) >= len(f.Blocks) || edge.From < edge.To || f.Blocks[edge.To].Flags&railssa.BlockLoopHeader == 0 {
			continue
		}
		header := f.Blocks[edge.To].InstStart * 6
		backedge := (f.Blocks[edge.From].InstStart + f.Blocks[edge.From].InstCount) * 6
		for id := 1; id < len(f.VRegs); id++ {
			data := f.VRegs[id]
			if !used[id] || starts[id] > header || ends[id] < header || ends[id] >= backedge || data.Flags&VRegBlockParam != 0 && data.Def == header {
				continue
			}
			ends[id] = backedge
		}
	}
}

func populateInstructionPositions(f *Func, schedule *Schedule, positions []uint32, seen []bool) error {
	if len(positions) != len(f.Insts) {
		return fmt.Errorf("railmach: instruction-position storage mismatch")
	}
	if schedule == nil {
		for instruction := range positions {
			positions[instruction] = uint32(instruction)
		}
		return nil
	}
	if len(schedule.Order) != len(f.Insts) || len(schedule.BlockRanges) != len(f.Blocks) {
		return fmt.Errorf("railmach: malformed allocation schedule")
	}
	if len(seen) != len(f.Insts) {
		return fmt.Errorf("railmach: instruction-position verification storage mismatch")
	}
	for blockID := range f.Blocks {
		range_ := schedule.BlockRanges[blockID]
		if uint64(range_.Start)+uint64(range_.Count) > uint64(len(schedule.Order)) {
			return fmt.Errorf("railmach: schedule block %d range is invalid", blockID)
		}
		for ordinal := range_.Start; ordinal < range_.Start+range_.Count; ordinal++ {
			instruction := schedule.Order[ordinal]
			if scheduleInstructionBlock(f, schedule, instruction) != railssa.BlockID(blockID) || seen[instruction] {
				return fmt.Errorf("railmach: schedule block %d has invalid instruction %d", blockID, instruction)
			}
			seen[instruction] = true
			positions[instruction] = ordinal
		}
	}
	return nil
}

func scheduledLogicalPosition(position uint32, instructionPositions []uint32) uint32 {
	instruction, phase := position/6, position%6
	if int(instruction) >= len(instructionPositions) {
		return position
	}
	return instructionPositions[instruction]*6 + phase
}

func blockScheduleEnd(f *Func, schedule *Schedule, block uint32) uint32 {
	if schedule != nil {
		range_ := schedule.BlockRanges[block]
		return range_.Start + range_.Count
	}
	source := f.Blocks[block]
	return source.InstStart + source.InstCount
}

func VerifyAllocation(f *Func, allocation *Allocation, config LinearQConfig) error {
	if f == nil {
		return fmt.Errorf("railmach: malformed RALinearQ allocation")
	}
	seen := make([]bool, len(f.Insts))
	return verifyAllocation(f, allocation, config, seen)
}

func verifyAllocationReusingScratch(f *Func, allocation *Allocation, config LinearQConfig) error {
	if f == nil || allocation == nil {
		return fmt.Errorf("railmach: malformed RALinearQ allocation")
	}
	seen := resize(allocation.scratch.verifySeen, len(f.Insts))
	allocation.scratch.verifySeen = seen
	return verifyAllocation(f, allocation, config, seen)
}

func verifyAllocation(f *Func, allocation *Allocation, config LinearQConfig, seenPosition []bool) error {
	if f == nil || allocation == nil || len(allocation.Locations) != len(f.VRegs) || len(allocation.InstructionPositions) != len(f.Insts) {
		return fmt.Errorf("railmach: malformed RALinearQ allocation")
	}
	for instruction, position := range allocation.InstructionPositions {
		if int(position) >= len(f.Insts) || seenPosition[position] {
			return fmt.Errorf("railmach: instruction %d has invalid allocation position %d", instruction, position)
		}
		seenPosition[position] = true
	}
	startOrdered := true
	for index, interval := range allocation.Intervals {
		if interval.Reg == 0 || int(interval.Reg) >= len(f.VRegs) || interval.End < interval.Start {
			return fmt.Errorf("railmach: invalid live interval %#v", interval)
		}
		if index != 0 && allocation.Intervals[index-1].Start > interval.Start {
			startOrdered = false
		}
		location := allocation.Locations[interval.Reg]
		switch location.Kind {
		case LocationRegister:
			limit := config.GPRs
			if interval.Bank == BankFPR {
				limit = config.FPRs
			}
			if location.Bank != interval.Bank || location.Index >= uint16(limit) {
				return fmt.Errorf("railmach: vreg %d has invalid register location %#v", interval.Reg, location)
			}
		case LocationSpill:
			if location.Index >= allocation.SpillSlots {
				return fmt.Errorf("railmach: vreg %d has invalid spill location %#v", interval.Reg, location)
			}
		case LocationRematerialize:
			if f.VRegs[interval.Reg].Flags&VRegRematerializable == 0 {
				return fmt.Errorf("railmach: vreg %d cannot rematerialize", interval.Reg)
			}
		default:
			return fmt.Errorf("railmach: vreg %d is unallocated", interval.Reg)
		}
	}
	for _, edge := range f.Edges {
		if int(edge.From) >= len(f.Blocks) || int(edge.To) >= len(f.Blocks) || edge.From < edge.To || f.Blocks[edge.To].Flags&railssa.BlockLoopHeader == 0 {
			continue
		}
		header := f.Blocks[edge.To].InstStart * 6
		backedge := (f.Blocks[edge.From].InstStart + f.Blocks[edge.From].InstCount) * 6
		for _, interval := range allocation.Intervals {
			data := f.VRegs[interval.Reg]
			if interval.Start <= header && interval.End >= header && interval.End < backedge && !(data.Flags&VRegBlockParam != 0 && data.Def == header) {
				return fmt.Errorf("railmach: loop-live vreg %d ends at %d before backedge %d", interval.Reg, interval.End, backedge)
			}
		}
	}
	if startOrdered {
		var registerEnd [2][64]uint32
		var registerReg [2][64]VReg
		spillEnd := resize(allocation.scratch.callPositions, int(allocation.SpillSlots))
		spillReg := resize(allocation.scratch.affinitySource, int(allocation.SpillSlots))
		clear(spillReg)
		allocation.scratch.callPositions, allocation.scratch.affinitySource = spillEnd, spillReg
		for _, interval := range allocation.Intervals {
			location := allocation.Locations[interval.Reg]
			switch location.Kind {
			case LocationRegister:
				bank := 0
				if location.Bank == BankFPR {
					bank = 1
				}
				if previous := registerReg[bank][location.Index]; previous != 0 && interval.Start <= registerEnd[bank][location.Index] {
					return fmt.Errorf("railmach: overlapping vregs %d and %d share register %d", previous, interval.Reg, location.Index)
				}
				registerReg[bank][location.Index], registerEnd[bank][location.Index] = interval.Reg, interval.End
			case LocationSpill:
				if previous := spillReg[location.Index]; previous != 0 && interval.Start <= spillEnd[location.Index] {
					return fmt.Errorf("railmach: overlapping vregs %d and %d share spill %d", previous, interval.Reg, location.Index)
				}
				spillReg[location.Index], spillEnd[location.Index] = interval.Reg, interval.End
			}
		}
	} else {
		for i, a := range allocation.Intervals {
			la := allocation.Locations[a.Reg]
			for _, b := range allocation.Intervals[i+1:] {
				if a.End < b.Start || b.End < a.Start {
					continue
				}
				lb := allocation.Locations[b.Reg]
				if la.Kind == LocationRegister && lb.Kind == LocationRegister && la.Bank == lb.Bank && la.Index == lb.Index {
					return fmt.Errorf("railmach: overlapping vregs %d and %d share register %d", a.Reg, b.Reg, la.Index)
				}
				if la.Kind == LocationSpill && lb.Kind == LocationSpill && la.Index == lb.Index {
					return fmt.Errorf("railmach: overlapping vregs %d and %d share spill %d", a.Reg, b.Reg, la.Index)
				}
			}
		}
	}
	for _, move := range allocation.FixedMoves {
		limit := config.GPRs
		if move.Bank == BankFPR {
			limit = config.FPRs
		}
		interval, found := allocationInterval(allocation.Intervals, move.Reg)
		if !found || move.Position < interval.Start || move.Position > interval.End || move.Physical >= limit {
			return fmt.Errorf("railmach: invalid fixed move %#v", move)
		}
	}
	if allocation.FrameBytes != (uint32(allocation.SpillSlots)*8+15)&^15 {
		return fmt.Errorf("railmach: frame bytes %d disagree with %d spill slots", allocation.FrameBytes, allocation.SpillSlots)
	}
	return nil
}

func allocationInterval(intervals []LiveInterval, reg VReg) (LiveInterval, bool) {
	for _, interval := range intervals {
		if interval.Reg == reg {
			return interval, true
		}
	}
	return LiveInterval{}, false
}
