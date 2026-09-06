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
	Flags  uint8
	_      uint16
}

const liveIntervalSegmented uint8 = 1 << iota

// LiveSegment is one inclusive occupied range within a LiveInterval's
// conservative Start/End span. Intervals without holes keep SegmentCount zero
// and use the span directly, avoiding per-value segment storage.
type LiveSegment struct {
	Start uint32
	End   uint32
}

type LiveSegmentRange struct {
	Reg          VReg
	SegmentStart uint32
	SegmentCount uint16
	_            uint16
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
	LiveSegments         []LiveSegment
	LiveSegmentRanges    []LiveSegmentRange
	FrameBytes           uint32
	SpillSlots           uint16

	// schedule retains the verified position coordinate system through the
	// immediately following allocation, SSA-exit, ABI, and post-RA checks.
	schedule *Schedule
	scratch  linearQScratch
}

type activeInterval struct {
	interval LiveInterval
	physical uint16
}

type spillInterval struct {
	end   uint32
	slot  uint16
	units uint8
}

func takeFreeSpillUnits(free []uint16, units uint16) (uint16, []uint16, bool) {
	if len(free) == 0 {
		return 0, free, false
	}
	slices.Sort(free)
	if units == 1 {
		return free[0], free[1:], true
	}
	for i := 0; i+1 < len(free); i++ {
		if free[i]&1 == 0 && free[i+1] == free[i]+1 {
			slot := free[i]
			copy(free[i:], free[i+2:])
			return slot, free[:len(free)-2], true
		}
	}
	return 0, free, false
}

func reserveSpillUnits(next *uint16, units uint16) uint16 {
	if units == 2 && *next&1 != 0 {
		*next++
	}
	slot := *next
	*next += units
	return slot
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
	// registerOccupants indexes assigned intervals by bank and physical
	// register. Segmented allocation consults one physical register at a time;
	// retaining a per-register list avoids rescanning every live interval for
	// each otherwise occupied register.
	registerOccupants [2][64][]activeInterval
	spillActive       []spillInterval
	spillFree         []uint16
	verifySeen        []bool
	positionSeen      []bool
	verifyRegNext     []uint32
	verifyRegHead     [2][64]uint32
	segmentPredOff    []uint32
	segmentPreds      []uint32
	segmentUseHead    []uint32
	segmentUseNext    []uint32
	segmentUseAt      []uint32
	segmentSeen       []uint32
	segmentWork       []uint32
	segmentBlocks     []uint32
	segmentBlockAt    []uint32
	segmentEligible   []bool
	segmentRangeAt    []uint32
	segmentRepack     []LiveSegment
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
		return LinearQConfig{GPRs: 20, FPRs: 28}
	default:
		return LinearQConfig{}
	}
}

// AllocateLinearQ is the deterministic legal-allocation baseline. This first
// stage models separate banks, fixed uses, call-clobber regions, weighted edge
// affinities, dense reusable spill slots, and rematerializable constants.
// Fixed-use moves are retained explicitly for late SSA exit.
func AllocateLinearQ(f *Func, config LinearQConfig, reuse *Allocation) (*Allocation, error) {
	return allocateLinearQ(f, nil, config, reuse, true)
}

// AllocateLinearQForSchedule computes live ranges in the selected per-block
// instruction order. The schedule has already passed dependency verification;
// this boundary additionally verifies its dense block-local permutation.
func AllocateLinearQForSchedule(f *Func, schedule *Schedule, config LinearQConfig, reuse *Allocation) (*Allocation, error) {
	if schedule == nil {
		return nil, fmt.Errorf("railmach: scheduled RALinearQ requires a schedule")
	}
	return allocateLinearQ(f, schedule, config, reuse, true)
}

func allocateLinearQ(f *Func, schedule *Schedule, config LinearQConfig, reuse *Allocation, allowSegments bool) (*Allocation, error) {
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
	liveSegments := reuse.LiveSegments[:0]
	liveSegmentRanges := reuse.LiveSegmentRanges[:0]
	scratch := reuse.scratch
	*reuse = Allocation{Locations: locations, Intervals: intervals, FixedMoves: fixedMoves, InstructionPositions: instructionPositions, LiveSegments: liveSegments, LiveSegmentRanges: liveSegmentRanges, schedule: schedule, scratch: scratch}
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
		block := scheduleInstructionBlock(f, schedule, uint32(instructionID))
		useWeight := uint32(1)
		if int(block) < len(f.Blocks) {
			useWeight = max(f.Blocks[block].Weight, 1)
		}
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
			weights[instruction.Result] = uint32(min(uint64(^uint32(0)), uint64(weights[instruction.Result])+uint64(useWeight)))
		}
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Flags&OperandColdRemat != 0 {
				value := operand.Reg
				for {
					base, ok := coldRematerializationBase(f, value)
					if !ok {
						return nil, fmt.Errorf("railmach: cold rematerialization vreg %d has no encodable recipe", value)
					}
					if base == 0 {
						break
					}
					used[base] = true
					if position > ends[base] {
						ends[base] = position
					}
					weights[base] = uint32(min(uint64(^uint32(0)), uint64(weights[base])+uint64(useWeight)))
					if f.VRegs[base].Flags&VRegRematerializable == 0 {
						break
					}
					value = base
				}
				continue
			}
			used[operand.Reg] = true
			if position > ends[operand.Reg] {
				ends[operand.Reg] = position
			}
			weights[operand.Reg] = uint32(min(uint64(^uint32(0)), uint64(weights[operand.Reg])+uint64(useWeight)))
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
	extendLoopLiveIntervals(f, schedule, starts, ends, used)
	slices.Sort(callPositions)
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
	if allowSegments {
		if err := buildLiveSegments(f, schedule, reuse); err != nil {
			return nil, err
		}
	}

	var gprStorage, fprStorage [64]bool
	var gprCount, fprCount [64]uint16
	gprFree := gprStorage[:config.GPRs]
	fprFree := fprStorage[:config.FPRs]
	for physical := range gprFree {
		gprFree[physical] = true
	}
	for physical := range fprFree {
		fprFree[physical] = true
	}
	gprActive := reuse.scratch.gprActive[:0]
	fprActive := reuse.scratch.fprActive[:0]
	segmented := len(reuse.LiveSegmentRanges) != 0
	for bank := range reuse.scratch.registerOccupants {
		for physical := range reuse.scratch.registerOccupants[bank] {
			reuse.scratch.registerOccupants[bank][physical] = reuse.scratch.registerOccupants[bank][physical][:0]
		}
	}
	spillActive := reuse.scratch.spillActive[:0]
	spillFree := reuse.scratch.spillFree[:0]
	nextSpill := uint16(0)
	expireRegisters := func(position uint32, active *[]activeInterval, free []bool, counts *[64]uint16) {
		kept := (*active)[:0]
		for _, item := range *active {
			if item.interval.End < position {
				counts[item.physical]--
				free[item.physical] = counts[item.physical] == 0
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
				for unit := uint8(0); unit < item.units; unit++ {
					spillFree = append(spillFree, item.slot+uint16(unit))
				}
			} else {
				kept = append(kept, item)
			}
		}
		spillActive = kept
	}
	spill := func(interval LiveInterval) Location {
		expireSpills(interval.Start)
		units := f.VRegs[interval.Reg].Type.SpillSlotUnits()
		slot, remaining, ok := takeFreeSpillUnits(spillFree, units)
		spillFree = remaining
		if !ok {
			slot = reserveSpillUnits(&nextSpill, units)
		}
		spillActive = append(spillActive, spillInterval{end: interval.End, slot: slot, units: uint8(units)})
		return Location{Kind: LocationSpill, Bank: interval.Bank, Index: slot}
	}
	for _, interval := range reuse.Intervals {
		free, counts, active := gprFree, &gprCount, &gprActive
		bankIndex := 0
		if interval.Bank == BankFPR {
			free, counts, active = fprFree, &fprCount, &fprActive
			bankIndex = 1
		}
		expireRegisters(interval.Start, active, free, counts)
		registerAvailable := func(physical int) bool {
			if !segmented {
				for _, occupant := range *active {
					if int(occupant.physical) == physical {
						return false
					}
				}
				return true
			}
			occupants := &reuse.scratch.registerOccupants[bankIndex][physical]
			kept := (*occupants)[:0]
			available := true
			for _, occupant := range *occupants {
				if occupant.interval.End < interval.Start {
					continue
				}
				kept = append(kept, occupant)
				available = available && !allocationLiveRangesOverlap(reuse, interval, occupant.interval)
			}
			*occupants = kept
			return available
		}
		crossesCall := allocationLiveRangeCrossesPositions(reuse, interval, callPositions)
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
			if location.Kind == LocationInvalid && len(reuse.LiveSegmentRanges) != 0 {
				if preferred >= 0 && registerAvailable(preferred) {
					location = Location{Kind: LocationRegister, Bank: interval.Bank, Index: uint16(preferred)}
				} else {
					for physical := range free {
						if registerAvailable(physical) {
							location = Location{Kind: LocationRegister, Bank: interval.Bank, Index: uint16(physical)}
							break
						}
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
			counts[location.Index]++
			assigned := activeInterval{interval: interval, physical: location.Index}
			*active = append(*active, assigned)
			if segmented {
				reuse.scratch.registerOccupants[bankIndex][location.Index] = append(reuse.scratch.registerOccupants[bankIndex][location.Index], assigned)
			}
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
func extendLoopLiveIntervals(f *Func, schedule *Schedule, starts, ends []uint32, used []bool) {
	for _, edge := range f.Edges {
		if int(edge.From) >= len(f.Blocks) || int(edge.To) >= len(f.Blocks) || edge.From < edge.To || f.Blocks[edge.To].Flags&railssa.BlockLoopHeader == 0 {
			continue
		}
		header := blockScheduleStart(f, schedule, uint32(edge.To)) * 6
		backedge := blockScheduleEnd(f, schedule, uint32(edge.From)) * 6
		for id := 1; id < len(f.VRegs); id++ {
			data := f.VRegs[id]
			if !used[id] || starts[id] > header || ends[id] < header || ends[id] >= backedge || data.Flags&VRegBlockParam != 0 && starts[id] == header {
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

func blockScheduleStart(f *Func, schedule *Schedule, block uint32) uint32 {
	if schedule != nil {
		return schedule.BlockRanges[block].Start
	}
	return f.Blocks[block].InstStart
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
	nextSegment := uint32(0)
	var segmentSeen []bool
	var intervalByReg []uint32
	if len(allocation.LiveSegmentRanges) != 0 {
		intervalByReg = resize(allocation.scratch.starts, len(f.VRegs))
		allocation.scratch.starts = intervalByReg
		for intervalIndex, interval := range allocation.Intervals {
			intervalByReg[interval.Reg] = uint32(intervalIndex) + 1
		}
		segmentSeen = resize(allocation.scratch.segmentEligible, len(f.VRegs))
		allocation.scratch.segmentEligible = segmentSeen
		previousRangeReg := VReg(0)
		for rangeIndex, range_ := range allocation.LiveSegmentRanges {
			end := uint64(range_.SegmentStart) + uint64(range_.SegmentCount)
			if range_.Reg == 0 || int(range_.Reg) >= len(intervalByReg) || range_.SegmentCount < 2 || range_.SegmentStart != nextSegment || end > uint64(len(allocation.LiveSegments)) || rangeIndex != 0 && range_.Reg <= previousRangeReg {
				return fmt.Errorf("railmach: malformed live segment range %#v", range_)
			}
			intervalIndex := intervalByReg[range_.Reg]
			if intervalIndex == 0 || allocation.Intervals[intervalIndex-1].Flags&liveIntervalSegmented == 0 {
				return fmt.Errorf("railmach: live segment range for unsegmented vreg %d", range_.Reg)
			}
			interval := allocation.Intervals[intervalIndex-1]
			segments := allocation.LiveSegments[range_.SegmentStart:uint32(end)]
			if segments[0].Start != interval.Start || segments[len(segments)-1].End != interval.End {
				return fmt.Errorf("railmach: vreg %d segments do not cover interval endpoints", interval.Reg)
			}
			for segmentIndex, segment := range segments {
				if segment.End < segment.Start || segment.Start < interval.Start || segment.End > interval.End || segmentIndex != 0 && segment.Start <= segments[segmentIndex-1].End+1 {
					return fmt.Errorf("railmach: vreg %d has invalid live segment %#v", interval.Reg, segment)
				}
			}
			segmentSeen[range_.Reg] = true
			nextSegment, previousRangeReg = uint32(end), range_.Reg
		}
	} else if len(allocation.LiveSegments) != 0 {
		return fmt.Errorf("railmach: %d live segments have no ranges", len(allocation.LiveSegments))
	}
	for index, interval := range allocation.Intervals {
		if interval.Reg == 0 || int(interval.Reg) >= len(f.VRegs) || interval.End < interval.Start {
			return fmt.Errorf("railmach: invalid live interval %#v", interval)
		}
		if interval.Flags&^liveIntervalSegmented != 0 || interval.Flags&liveIntervalSegmented != 0 && (segmentSeen == nil || !segmentSeen[interval.Reg]) {
			return fmt.Errorf("railmach: vreg %d has inconsistent live segment flags", interval.Reg)
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
			units := f.VRegs[interval.Reg].Type.SpillSlotUnits()
			if uint32(location.Index)+uint32(units) > uint32(allocation.SpillSlots) || units == 2 && location.Index&1 != 0 {
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
	if nextSegment != uint32(len(allocation.LiveSegments)) {
		return fmt.Errorf("railmach: %d trailing live segments are unowned", uint32(len(allocation.LiveSegments))-nextSegment)
	}
	if len(allocation.LiveSegmentRanges) != 0 {
		for instructionID := range f.Insts {
			position := allocation.InstructionPositions[instructionID]*6 + 2
			for _, operand := range f.InstructionOperands(uint32(instructionID)) {
				if operand.Flags&OperandColdRemat != 0 {
					continue
				}
				if operand.Reg == 0 || int(operand.Reg) >= len(intervalByReg) {
					return fmt.Errorf("railmach: instruction %d has invalid operand vreg %d", instructionID, operand.Reg)
				}
				encoded := intervalByReg[operand.Reg]
				if encoded == 0 || !allocationLiveRangeContains(allocation, allocation.Intervals[encoded-1], position) {
					return fmt.Errorf("railmach: operand vreg %d is not live at instruction %d", operand.Reg, instructionID)
				}
			}
		}
		for _, transfer := range f.Transfers {
			position := blockScheduleEnd(f, allocation.schedule, uint32(transfer.From)) * 6
			if transfer.Src == 0 || int(transfer.Src) >= len(intervalByReg) {
				return fmt.Errorf("railmach: block %d has invalid edge source vreg %d", transfer.From, transfer.Src)
			}
			encoded := intervalByReg[transfer.Src]
			if encoded == 0 || !allocationLiveRangeContains(allocation, allocation.Intervals[encoded-1], position) {
				return fmt.Errorf("railmach: edge source vreg %d is not live at block %d exit", transfer.Src, transfer.From)
			}
		}
	}
	for _, edge := range f.Edges {
		if int(edge.From) >= len(f.Blocks) || int(edge.To) >= len(f.Blocks) || edge.From < edge.To || f.Blocks[edge.To].Flags&railssa.BlockLoopHeader == 0 {
			continue
		}
		header := blockScheduleStart(f, allocation.schedule, uint32(edge.To)) * 6
		backedge := blockScheduleEnd(f, allocation.schedule, uint32(edge.From)) * 6
		for _, interval := range allocation.Intervals {
			data := f.VRegs[interval.Reg]
			if interval.Start <= header && interval.End >= header && interval.End < backedge && !(data.Flags&VRegBlockParam != 0 && data.Def == header) {
				return fmt.Errorf("railmach: loop-live vreg %d ends at %d before backedge %d", interval.Reg, interval.End, backedge)
			}
		}
	}
	if startOrdered && len(allocation.LiveSegmentRanges) == 0 {
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
				units := f.VRegs[interval.Reg].Type.SpillSlotUnits()
				for unit := uint16(0); unit < units; unit++ {
					slot := location.Index + unit
					if previous := spillReg[slot]; previous != 0 && interval.Start <= spillEnd[slot] {
						return fmt.Errorf("railmach: overlapping vregs %d and %d share spill %d", previous, interval.Reg, slot)
					}
					spillReg[slot], spillEnd[slot] = interval.Reg, interval.End
				}
			}
		}
	} else if startOrdered {
		clear(allocation.scratch.verifyRegHead[:])
		registerNext := resize(allocation.scratch.verifyRegNext, len(allocation.Intervals))
		allocation.scratch.verifyRegNext = registerNext
		var registerMaxEnd [2][64]uint32
		spillEnd := resize(allocation.scratch.callPositions, int(allocation.SpillSlots))
		spillReg := resize(allocation.scratch.affinitySource, int(allocation.SpillSlots))
		clear(spillReg)
		allocation.scratch.callPositions, allocation.scratch.affinitySource = spillEnd, spillReg
		for intervalIndex, interval := range allocation.Intervals {
			location := allocation.Locations[interval.Reg]
			switch location.Kind {
			case LocationRegister:
				bank := 0
				if location.Bank == BankFPR {
					bank = 1
				}
				head := &allocation.scratch.verifyRegHead[bank][location.Index]
				if *head != 0 && interval.Start > registerMaxEnd[bank][location.Index] {
					*head = 0
				}
				previous := uint32(0)
				for occupant := *head; occupant != 0; {
					next := registerNext[occupant-1]
					other := allocation.Intervals[occupant-1]
					if other.End < interval.Start {
						if previous == 0 {
							*head = next
						} else {
							registerNext[previous-1] = next
						}
						registerNext[occupant-1] = 0
						occupant = next
						continue
					}
					if allocationLiveRangesOverlap(allocation, interval, other) {
						return fmt.Errorf("railmach: overlapping vregs %d and %d share register %d", other.Reg, interval.Reg, location.Index)
					}
					previous, occupant = occupant, next
				}
				registerNext[intervalIndex] = allocation.scratch.verifyRegHead[bank][location.Index]
				allocation.scratch.verifyRegHead[bank][location.Index] = uint32(intervalIndex) + 1
				registerMaxEnd[bank][location.Index] = max(registerMaxEnd[bank][location.Index], interval.End)
			case LocationSpill:
				units := f.VRegs[interval.Reg].Type.SpillSlotUnits()
				for unit := uint16(0); unit < units; unit++ {
					slot := location.Index + unit
					if previous := spillReg[slot]; previous != 0 && interval.Start <= spillEnd[slot] {
						return fmt.Errorf("railmach: overlapping vregs %d and %d share spill %d", previous, interval.Reg, slot)
					}
					spillReg[slot], spillEnd[slot] = interval.Reg, interval.End
				}
			}
		}
	} else {
		for i, a := range allocation.Intervals {
			la := allocation.Locations[a.Reg]
			for _, b := range allocation.Intervals[i+1:] {
				if !allocationLiveRangesOverlap(allocation, a, b) {
					continue
				}
				lb := allocation.Locations[b.Reg]
				if la.Kind == LocationRegister && lb.Kind == LocationRegister && la.Bank == lb.Bank && la.Index == lb.Index {
					return fmt.Errorf("railmach: overlapping vregs %d and %d share register %d", a.Reg, b.Reg, la.Index)
				}
				if la.Kind == LocationSpill && lb.Kind == LocationSpill && spillLocationsOverlap(la.Index, f.VRegs[a.Reg].Type, lb.Index, f.VRegs[b.Reg].Type) {
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
		if !found || !allocationLiveRangeContains(allocation, interval, move.Position) || move.Physical >= limit {
			return fmt.Errorf("railmach: invalid fixed move %#v", move)
		}
	}
	if allocation.FrameBytes != (uint32(allocation.SpillSlots)*8+15)&^15 {
		return fmt.Errorf("railmach: frame bytes %d disagree with %d spill slots", allocation.FrameBytes, allocation.SpillSlots)
	}
	return nil
}

func allocationIntervalSegments(allocation *Allocation, interval LiveInterval) []LiveSegment {
	if allocation == nil || interval.Flags&liveIntervalSegmented == 0 {
		return nil
	}
	index := -1
	if int(interval.Reg) < len(allocation.scratch.segmentRangeAt) {
		encoded := allocation.scratch.segmentRangeAt[interval.Reg]
		if encoded != 0 && int(encoded-1) < len(allocation.LiveSegmentRanges) && allocation.LiveSegmentRanges[encoded-1].Reg == interval.Reg {
			index = int(encoded - 1)
		}
	}
	if index < 0 {
		foundIndex, ok := slices.BinarySearchFunc(allocation.LiveSegmentRanges, interval.Reg, func(range_ LiveSegmentRange, reg VReg) int {
			return int(range_.Reg) - int(reg)
		})
		if !ok {
			return nil
		}
		index = foundIndex
	}
	range_ := allocation.LiveSegmentRanges[index]
	start := uint64(range_.SegmentStart)
	end := start + uint64(range_.SegmentCount)
	if end > uint64(len(allocation.LiveSegments)) {
		return nil
	}
	return allocation.LiveSegments[start:end]
}

func allocationLiveRangeContains(allocation *Allocation, interval LiveInterval, position uint32) bool {
	if interval.Flags&liveIntervalSegmented == 0 {
		return interval.Start <= position && position <= interval.End
	}
	segments := allocationIntervalSegments(allocation, interval)
	index, _ := slices.BinarySearchFunc(segments, position, func(segment LiveSegment, position uint32) int {
		if segment.End < position {
			return -1
		}
		if segment.Start > position {
			return 1
		}
		return 0
	})
	return index < len(segments) && segments[index].Start <= position && position <= segments[index].End
}

// IntervalContains reports whether interval is live at position after CFG
// holes are applied. Runtime-ABI planning uses this instead of reinterpreting
// the conservative Start/End span around calls and scratch-register sites.
func (allocation *Allocation) IntervalContains(interval LiveInterval, position uint32) bool {
	return allocationLiveRangeContains(allocation, interval, position)
}

func allocationLiveRangesOverlap(allocation *Allocation, a, b LiveInterval) bool {
	if a.End < b.Start || b.End < a.Start {
		return false
	}
	if a.Flags&liveIntervalSegmented == 0 && b.Flags&liveIntervalSegmented == 0 {
		return true
	}
	aSegments, bSegments := allocationIntervalSegments(allocation, a), allocationIntervalSegments(allocation, b)
	if len(aSegments) == 0 && len(bSegments) == 0 {
		return true
	}
	if len(aSegments) == 0 {
		return liveSegmentsOverlapRange(bSegments, a.Start, a.End)
	}
	if len(bSegments) == 0 {
		return liveSegmentsOverlapRange(aSegments, b.Start, b.End)
	}
	left, right := 0, 0
	for left < len(aSegments) && right < len(bSegments) {
		aSegment, bSegment := aSegments[left], bSegments[right]
		if aSegment.End < bSegment.Start {
			left++
		} else if bSegment.End < aSegment.Start {
			right++
		} else {
			return true
		}
	}
	return false
}

func liveSegmentsOverlapRange(segments []LiveSegment, start, end uint32) bool {
	for _, segment := range segments {
		if segment.Start > end {
			return false
		}
		if segment.End >= start {
			return true
		}
	}
	return false
}

func spillLocationsOverlap(a uint16, aType MachineType, b uint16, bType MachineType) bool {
	aEnd := uint32(a) + uint32(aType.SpillSlotUnits())
	bEnd := uint32(b) + uint32(bType.SpillSlotUnits())
	return uint32(a) < bEnd && uint32(b) < aEnd
}

func allocationInterval(intervals []LiveInterval, reg VReg) (LiveInterval, bool) {
	for _, interval := range intervals {
		if interval.Reg == reg {
			return interval, true
		}
	}
	return LiveInterval{}, false
}
