package railmach

import (
	"fmt"
	"slices"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type GreedyConfig struct {
	Linear          LinearQConfig
	CallerGPRs      uint8
	CallerFPRs      uint8
	MaxStage        uint8
	PreserveGPRCost uint16
	PreserveFPRCost uint16
	// CallClobbers overrides the conservative caller-register mask for direct
	// calls whose callee has already been allocated. Unlisted calls retain the
	// target's complete caller-clobbered mask.
	CallClobbers []CallClobber
}

type CallClobber struct {
	Instruction uint32
	GPR         uint64
	FPR         uint64
}

func DefaultGreedyConfig(target Target) GreedyConfig {
	linear := DefaultLinearQConfig(target)
	switch target {
	case TargetAMD64:
		// R10/R11 are finalizer temporaries, while the five low abstract
		// registers map to the volatile RAX/RCX/RDX/R8/R9 set. Higher
		// abstract registers map to registers preserved by the private ABI.
		return GreedyConfig{Linear: linear, CallerGPRs: 5, CallerFPRs: 8, MaxStage: 4, PreserveGPRCost: 2, PreserveFPRCost: 2}
	case TargetARM64:
		return GreedyConfig{Linear: linear, CallerGPRs: 12, CallerFPRs: 16, MaxStage: 4, PreserveGPRCost: 2, PreserveFPRCost: 2}
	default:
		return GreedyConfig{}
	}
}

type GreedyMetrics struct {
	Promotions        uint32
	Evictions         uint32
	CalleeSaved       uint32
	SpillSlots        uint32
	WeightedDebt      uint64
	PreservationCost  uint64
	RegionalFragments uint32
	RegionalReloads   uint32
	RegionalStores    uint32
	RegionalBenefit   uint64
}

type SpillSet struct {
	MemberStart uint32
	MemberCount uint16
	Slot        uint16
	Weight      uint32
	Bank        Bank
	_           [3]byte
}

// AllocationFragment overrides one spilled vreg's location for a bounded
// logical-position region. The spill slot remains authoritative at region
// boundaries, so entry requires exactly one reload and exit requires no store
// for immutable machine SSA values.
type AllocationFragment struct {
	Reg      VReg
	Start    uint32
	End      uint32
	Location Location
	Victim   VReg
	// VictimSlot preserves a register-resident victim while the fragment owns
	// its physical register. It is ignored when Victim is zero.
	VictimSlot uint16
	_          uint16
}

type callPosition struct {
	instruction uint32
	position    uint32
}

type regionalState struct {
	interval            uint32
	head, tail          uint32 // 1-based regionalSegments indexes
	start, end, benefit uint32
	epoch               uint32
	callLive            bool
	_                   [3]byte
}

type regionalSegment struct {
	start, end, benefit uint32
	next                uint32 // 1-based index
}

type GreedyAllocation struct {
	Allocation
	Stage        uint8
	Metrics      GreedyMetrics
	SpillSets    []SpillSet
	SpillMembers []VReg
	Fragments    []AllocationFragment

	priorityIntervals []LiveInterval
	callPositions     []callPosition
	candidateVictims  []VReg
	bestVictims       []VReg
	verifySpillSeen   []bool
	occupantHead      [2][64]uint32
	occupantNext      []uint32
	intervalByReg     []uint32
	regionalStates    []regionalState
	regionalSegments  []regionalSegment
}

// AllocateGreedyP progressively improves a complete RALinearQ allocation.
// Stage 1 promotes nonconflicting ranges, stage 2 permits weighted eviction,
// and stage 3 uses callee-saved regions for ranges crossing calls. Every stage
// leaves a complete verifiable allocation and can be the configured stop point.
func AllocateGreedyP(f *Func, config GreedyConfig, reuse *GreedyAllocation) (*GreedyAllocation, error) {
	return allocateGreedyP(f, nil, config, reuse)
}

// AllocateGreedyPForSchedule improves a complete schedule-aware linear
// allocation while retaining the same logical positions for calls and fixed
// operand repair.
func AllocateGreedyPForSchedule(f *Func, schedule *Schedule, config GreedyConfig, reuse *GreedyAllocation) (*GreedyAllocation, error) {
	if schedule == nil {
		return nil, fmt.Errorf("railmach: scheduled RAGreedyP requires a schedule")
	}
	return allocateGreedyP(f, schedule, config, reuse)
}

func allocateGreedyP(f *Func, schedule *Schedule, config GreedyConfig, reuse *GreedyAllocation) (*GreedyAllocation, error) {
	if config.MaxStage > 4 || config.MaxStage == 0 || config.CallerGPRs > config.Linear.GPRs || config.CallerFPRs > config.Linear.FPRs {
		return nil, fmt.Errorf("railmach: invalid RAGreedyP configuration %#v", config)
	}
	if reuse == nil {
		reuse = new(GreedyAllocation)
	}
	spillSets := reuse.SpillSets[:0]
	spillMembers := reuse.SpillMembers[:0]
	fragments := reuse.Fragments[:0]
	base, err := allocateLinearQ(f, schedule, config.Linear, &reuse.Allocation)
	if err != nil {
		return nil, err
	}
	reuse.Allocation = *base
	reuse.Stage = config.MaxStage
	reuse.Metrics = GreedyMetrics{}
	reuse.SpillSets = spillSets
	reuse.SpillMembers = spillMembers
	reuse.Fragments = fragments

	callPositions := reuse.callPositions[:0]
	for id, instruction := range f.Insts {
		if IsCall(instruction.Op) {
			// Operands are consumed at +2 and the result becomes live at +3.
			// Sampling later would incorrectly classify the call's own result as
			// crossing the call and promote it into a callee-saved region.
			callPositions = append(callPositions, callPosition{instruction: uint32(id), position: reuse.InstructionPositions[id]*6 + 2})
		}
	}
	slices.SortFunc(callPositions, func(a, b callPosition) int {
		if a.position != b.position {
			return int(a.position) - int(b.position)
		}
		return int(a.instruction) - int(b.instruction)
	})
	reuse.callPositions = callPositions
	crossesCall := func(interval LiveInterval) bool {
		first := firstCallAfter(callPositions, interval.Start)
		return first < len(callPositions) && callPositions[first].position < interval.End
	}
	callSurvivorMask := func(interval LiveInterval) uint64 {
		survivors := ^uint64(0)
		for index := firstCallAfter(callPositions, interval.Start); index < len(callPositions) && callPositions[index].position < interval.End; index++ {
			call := callPositions[index]
			mask := conservativeCallMask(config, interval.Bank)
			if overrideIndex, ok := slices.BinarySearchFunc(config.CallClobbers, call.instruction, func(override CallClobber, instruction uint32) int {
				return int(override.Instruction) - int(instruction)
			}); ok {
				override := config.CallClobbers[overrideIndex]
				mask = override.GPR
				if interval.Bank == BankFPR {
					mask = override.FPR
				}
			}
			survivors &^= mask
		}
		return survivors
	}
	intervals := append(reuse.priorityIntervals[:0], reuse.Intervals...)
	var calleeUsed [2]uint64
	clear(reuse.occupantHead[:])
	occupantNext := resize(reuse.occupantNext, len(reuse.Intervals))
	intervalByReg := resize(reuse.intervalByReg, len(reuse.Locations))
	clear(occupantNext)
	clear(intervalByReg)
	reuse.occupantNext, reuse.intervalByReg = occupantNext, intervalByReg
	for intervalIndex, interval := range reuse.Intervals {
		intervalByReg[interval.Reg] = uint32(intervalIndex) + 1
		location := reuse.Locations[interval.Reg]
		bank := 0
		if interval.Bank == BankFPR {
			bank = 1
		}
		if location.Kind == LocationRegister && location.Index < 64 {
			occupantNext[intervalIndex] = reuse.occupantHead[bank][location.Index]
			reuse.occupantHead[bank][location.Index] = uint32(intervalIndex) + 1
		}
		if !crossesCall(interval) {
			continue
		}
		first := config.CallerGPRs
		if interval.Bank == BankFPR {
			first = config.CallerFPRs
		}
		if location.Kind == LocationRegister && location.Index >= uint16(first) && location.Index < 64 {
			calleeUsed[bank] |= uint64(1) << location.Index
		}
	}
	slices.SortFunc(intervals, func(a, b LiveInterval) int {
		costA := uint64(a.Weight) * uint64(a.End-a.Start+1)
		costB := uint64(b.Weight) * uint64(b.End-b.Start+1)
		if costA != costB {
			if costA > costB {
				return -1
			}
			return 1
		}
		return int(a.Reg) - int(b.Reg)
	})
	reuse.priorityIntervals = intervals
	for _, interval := range intervals {
		current := reuse.Locations[interval.Reg]
		if current.Kind == LocationRegister {
			continue
		}
		callLive := crossesCall(interval)
		if callLive && config.MaxStage < 3 {
			continue
		}
		limit := int(config.Linear.GPRs)
		if interval.Bank == BankFPR {
			limit = int(config.Linear.FPRs)
		}
		best, bestCost := -1, ^uint64(0)
		bestVictims := reuse.bestVictims[:0]
		survivors := ^uint64(0)
		if callLive {
			survivors = callSurvivorMask(interval)
		}
		for physical := 0; physical < limit; physical++ {
			if callLive && survivors&(uint64(1)<<physical) == 0 {
				continue
			}
			victims := reuse.candidateVictims[:0]
			cost := uint64(0)
			bankIndex := 0
			preserveCost := config.PreserveGPRCost
			if interval.Bank == BankFPR {
				bankIndex, preserveCost = 1, config.PreserveFPRCost
			}
			callerLimit := config.CallerGPRs
			if interval.Bank == BankFPR {
				callerLimit = config.CallerFPRs
			}
			if callLive && physical >= int(callerLimit) && calleeUsed[bankIndex]&(uint64(1)<<physical) == 0 {
				cost += uint64(preserveCost)
			}
			for occupant := reuse.occupantHead[bankIndex][physical]; occupant != 0; occupant = occupantNext[occupant-1] {
				other := reuse.Intervals[occupant-1]
				location := reuse.Locations[other.Reg]
				if location.Kind != LocationRegister || location.Bank != interval.Bank || int(location.Index) != physical || !intervalsOverlap(interval, other) {
					continue
				}
				victims = append(victims, other.Reg)
				cost += uint64(other.Weight) * uint64(other.End-other.Start+1)
			}
			if len(victims) == 0 {
				best, bestCost = physical, cost
				bestVictims = bestVictims[:0]
				break
			}
			if config.MaxStage >= 2 && cost < bestCost {
				best, bestCost = physical, cost
				bestVictims = append(bestVictims[:0], victims...)
			}
			reuse.candidateVictims = victims
		}
		reuse.bestVictims = bestVictims
		valueCost := uint64(interval.Weight) * uint64(interval.End-interval.Start+1)
		if best < 0 || bestCost >= valueCost || len(bestVictims) != 0 && config.MaxStage < 2 {
			continue
		}
		for _, victim := range bestVictims {
			victimLocation := reuse.Locations[victim]
			victimBank := 0
			if victimLocation.Bank == BankFPR {
				victimBank = 1
			}
			if victimLocation.Kind == LocationRegister && victimLocation.Index < 64 {
				head := &reuse.occupantHead[victimBank][victimLocation.Index]
				previous := uint32(0)
				for occupant := *head; occupant != 0; occupant = occupantNext[occupant-1] {
					if reuse.Intervals[occupant-1].Reg != victim {
						previous = occupant
						continue
					}
					next := occupantNext[occupant-1]
					if previous == 0 {
						*head = next
					} else {
						occupantNext[previous-1] = next
					}
					occupantNext[occupant-1] = 0
					break
				}
			}
			if f.VRegs[victim].Flags&VRegRematerializable != 0 {
				reuse.Locations[victim] = Location{Kind: LocationRematerialize, Bank: f.VRegs[victim].Bank}
			} else {
				reuse.Locations[victim] = Location{Kind: LocationSpill, Bank: f.VRegs[victim].Bank}
			}
			reuse.Metrics.Evictions++
		}
		reuse.Locations[interval.Reg] = Location{Kind: LocationRegister, Bank: interval.Bank, Index: uint16(best)}
		bankIndex := 0
		if interval.Bank == BankFPR {
			bankIndex = 1
		}
		intervalIndex := intervalByReg[interval.Reg]
		if intervalIndex != 0 {
			occupantNext[intervalIndex-1] = reuse.occupantHead[bankIndex][best]
			reuse.occupantHead[bankIndex][best] = intervalIndex
		}
		reuse.Metrics.Promotions++
		callerLimit := config.CallerGPRs
		if interval.Bank == BankFPR {
			callerLimit = config.CallerFPRs
		}
		if callLive && best >= int(callerLimit) {
			reuse.Metrics.CalleeSaved++
			bankIndex, preserveCost := 0, config.PreserveGPRCost
			if interval.Bank == BankFPR {
				bankIndex, preserveCost = 1, config.PreserveFPRCost
			}
			if calleeUsed[bankIndex]&(uint64(1)<<best) == 0 {
				calleeUsed[bankIndex] |= uint64(1) << best
				reuse.Metrics.PreservationCost += uint64(preserveCost)
			}
		}
	}
	recolorGreedySpills(reuse)
	if config.MaxStage >= 4 {
		// Rebuild exact physical occupants in increasing live-range order. Greedy
		// promotion mutates locations and links in priority order; regional
		// queries need neither stale evictions nor a full physical-list scan.
		clear(reuse.occupantHead[:])
		clear(occupantNext)
		for intervalIndex := len(reuse.Intervals) - 1; intervalIndex >= 0; intervalIndex-- {
			interval := reuse.Intervals[intervalIndex]
			location := reuse.Locations[interval.Reg]
			if location.Kind != LocationRegister || location.Index >= 64 {
				continue
			}
			bank := 0
			if location.Bank == BankFPR {
				bank = 1
			}
			occupantNext[intervalIndex] = reuse.occupantHead[bank][location.Index]
			reuse.occupantHead[bank][location.Index] = uint32(intervalIndex) + 1
		}
		planRegionalFragments(f, schedule, config, reuse)
	}
	if err := buildSpillSets(reuse); err != nil {
		return nil, err
	}
	rebuildFixedMoves(f, &reuse.Allocation)
	for _, interval := range reuse.Intervals {
		location := reuse.Locations[interval.Reg]
		if location.Kind == LocationSpill {
			reuse.Metrics.WeightedDebt += uint64(interval.Weight) * uint64(interval.End-interval.Start+1)
		}
	}
	if reuse.Metrics.RegionalBenefit >= reuse.Metrics.WeightedDebt {
		reuse.Metrics.WeightedDebt = 0
	} else {
		reuse.Metrics.WeightedDebt -= reuse.Metrics.RegionalBenefit
	}
	reuse.Metrics.SpillSlots = uint32(reuse.SpillSlots)
	if err := verifyAllocationReusingScratch(f, &reuse.Allocation, config.Linear); err != nil {
		return nil, err
	}
	if err := verifySpillSetsReusingScratch(reuse); err != nil {
		return nil, err
	}
	if err := VerifyRegionalFragments(f, schedule, reuse, config); err != nil {
		return nil, err
	}
	for _, interval := range reuse.Intervals {
		location := reuse.Locations[interval.Reg]
		if location.Kind == LocationRegister && callSurvivorMask(interval)&(uint64(1)<<location.Index) == 0 {
			return nil, fmt.Errorf("railmach: vreg %d occupies call-clobbered register %d", interval.Reg, location.Index)
		}
	}
	return reuse, nil
}

func firstCallAfter(calls []callPosition, position uint32) int {
	return sort.Search(len(calls), func(index int) bool { return calls[index].position > position })
}

func conservativeCallMask(config GreedyConfig, bank Bank) uint64 {
	count := config.CallerGPRs
	if bank == BankFPR {
		count = config.CallerFPRs
	}
	if count >= 64 {
		return ^uint64(0)
	}
	return uint64(1)<<count - 1
}

// LocationAt returns the regional location in effect at position, falling back
// to the vreg's complete baseline allocation outside split regions.
func (allocation *GreedyAllocation) LocationAt(reg VReg, position uint32) Location {
	if allocation == nil || int(reg) >= len(allocation.Locations) {
		return Location{}
	}
	for _, fragment := range allocation.Fragments {
		if fragment.Reg == reg && fragment.Start <= position && position <= fragment.End {
			return fragment.Location
		}
	}
	return allocation.Locations[reg]
}

func planRegionalFragments(f *Func, schedule *Schedule, config GreedyConfig, allocation *GreedyAllocation) {
	states := allocation.regionalStates[:0]
	segments := allocation.regionalSegments[:0]
	byReg := allocation.intervalByReg
	clear(byReg)
	for intervalIndex, interval := range allocation.Intervals {
		if allocation.Locations[interval.Reg].Kind != LocationSpill {
			continue
		}
		byReg[interval.Reg] = uint32(len(states)) + 1
		states = append(states, regionalState{interval: uint32(intervalIndex), callLive: intervalCrossesPositions(allocation.callPositions, interval)})
	}
	flush := func(state *regionalState) {
		if state.benefit >= 2 && state.start <= state.end {
			index := uint32(len(segments)) + 1
			segments = append(segments, regionalSegment{start: state.start, end: state.end, benefit: state.benefit})
			if state.tail == 0 {
				state.head = index
			} else {
				segments[state.tail-1].next = index
			}
			state.tail = index
		}
		state.start, state.end, state.benefit = 0, 0, 0
	}
	epoch := uint32(1)
	activeBlock := ^railssa.BlockID(0)
	for ordinal := range f.Insts {
		instructionID := uint32(ordinal)
		if schedule != nil {
			instructionID = schedule.Order[ordinal]
		}
		instruction := f.Insts[instructionID]
		block := scheduleInstructionBlock(f, schedule, uint32(instructionID))
		if activeBlock == ^railssa.BlockID(0) {
			activeBlock = block
		} else if block != activeBlock {
			epoch++
			activeBlock = block
		}
		barrier := IsCall(instruction.Op) || nativeControlOp(instruction.Op)
		if barrier {
			epoch++
			activeBlock = ^railssa.BlockID(0)
			continue
		}
		position := allocation.InstructionPositions[instructionID]*6 + 2
		for _, operand := range f.InstructionOperands(instructionID) {
			if int(operand.Reg) >= len(byReg) || operand.Flags&(OperandFixed|OperandColdRemat) != 0 {
				continue
			}
			stateIndex := byReg[operand.Reg]
			if stateIndex == 0 {
				continue
			}
			state := &states[stateIndex-1]
			weight := f.Blocks[block].Weight
			if weight < 8 && !state.callLive {
				continue
			}
			if state.benefit != 0 && state.epoch != epoch {
				flush(state)
			}
			if state.benefit == 0 {
				state.start, state.epoch = position, epoch
			}
			state.end = position
			state.benefit = uint32(min(uint64(^uint32(0)), uint64(state.benefit)+uint64(max(weight, 1))))
		}
	}
	for index := range states {
		flush(&states[index])
	}
	allocation.regionalStates, allocation.regionalSegments = states, segments

	for stateIndex := range states {
		state := &states[stateIndex]
		interval := allocation.Intervals[state.interval]
		for segmentIndex := state.head; segmentIndex != 0; segmentIndex = segments[segmentIndex-1].next {
			segment := segments[segmentIndex-1]
			start, end, benefit := segment.start, segment.end, segment.benefit
			limit := config.Linear.GPRs
			if interval.Bank == BankFPR {
				limit = config.Linear.FPRs
			}
			committed := false
			for physical := uint16(0); physical < uint16(limit); physical++ {
				if regionalPhysicalFree(allocation, interval.Bank, physical, start, end) {
					allocation.Fragments = append(allocation.Fragments, AllocationFragment{
						Reg: interval.Reg, Start: start, End: end,
						Location: Location{Kind: LocationRegister, Bank: interval.Bank, Index: physical},
					})
					allocation.Metrics.RegionalFragments++
					allocation.Metrics.RegionalReloads++
					allocation.Metrics.RegionalBenefit += uint64(benefit - 1)
					committed = true
					break
				}
			}
			if !committed && !regionalFragmentEndsAtBlockBoundary(f, schedule, end) && !regionalFragmentFeedsDeferredComparison(f, schedule, interval.Reg, start, end) {
				for physical := uint16(0); physical < uint16(limit); physical++ {
					victim, ok := regionalInactiveVictim(f, allocation, interval.Bank, physical, start, end)
					if !ok {
						continue
					}
					slot := allocation.SpillSlots
					allocation.SpillSlots++
					allocation.FrameBytes = (uint32(allocation.SpillSlots)*8 + 15) &^ 15
					allocation.Fragments = append(allocation.Fragments, AllocationFragment{
						Reg: interval.Reg, Start: start, End: end, Victim: victim, VictimSlot: slot,
						Location: Location{Kind: LocationRegister, Bank: interval.Bank, Index: physical},
					})
					allocation.Metrics.RegionalFragments++
					allocation.Metrics.RegionalReloads += 2
					allocation.Metrics.RegionalStores++
					if benefit > 2 {
						allocation.Metrics.RegionalBenefit += uint64(benefit - 2)
					}
					break
				}
			}
		}
	}
}

func regionalFragmentFeedsDeferredComparison(f *Func, schedule *Schedule, reg VReg, start, end uint32) bool {
	for ordinal := start / 6; ordinal <= end/6; ordinal++ {
		instructionID := ordinal
		if schedule != nil {
			if int(ordinal) >= len(schedule.Order) {
				return true
			}
			instructionID = schedule.Order[ordinal]
		}
		instruction := f.Insts[instructionID]
		if instruction.Op != wasm.InstrI32Eqz && instruction.Op != wasm.InstrI64Eqz &&
			!(instruction.Op >= wasm.InstrI32Eq && instruction.Op <= wasm.InstrI32GeU) &&
			!(instruction.Op >= wasm.InstrI64Eq && instruction.Op <= wasm.InstrI64GeU) {
			continue
		}
		for _, operand := range f.InstructionOperands(instructionID) {
			if operand.Reg == reg {
				return true
			}
		}
	}
	return false
}

func regionalFragmentEndsAtBlockBoundary(f *Func, schedule *Schedule, end uint32) bool {
	ordinal := end / 6
	instructionID := ordinal
	if schedule != nil {
		if int(ordinal) >= len(schedule.Order) {
			return true
		}
		instructionID = schedule.Order[ordinal]
	}
	block := scheduleInstructionBlock(f, schedule, instructionID)
	return int(block) >= len(f.Blocks) || blockScheduleEnd(f, schedule, uint32(block)) == ordinal+1
}

func regionalInactiveVictim(f *Func, allocation *GreedyAllocation, bank Bank, physical uint16, start, end uint32) (VReg, bool) {
	if !regionalFragmentsPhysicalFree(allocation, bank, physical, start, end) {
		return 0, false
	}
	var victim VReg
	bankIndex := 0
	if bank == BankFPR {
		bankIndex = 1
	}
	for occupant := allocation.occupantHead[bankIndex][physical]; occupant != 0; occupant = allocation.occupantNext[occupant-1] {
		interval := allocation.Intervals[occupant-1]
		// A regional victim is restored before the next instruction. Reject a
		// result assigned to this physical register after the final operand use:
		// restoring the victim would otherwise overwrite that newly defined value.
		if interval.Start == end+1 {
			return 0, false
		}
		if interval.Start > end+1 {
			break
		}
		if !intervalsOverlapRange(interval, start, end) {
			continue
		}
		if victim != 0 && victim != interval.Reg || interval.Start >= start && interval.Start <= end {
			return 0, false
		}
		victim = interval.Reg
	}
	if victim == 0 {
		return 0, false
	}
	for instructionID := range f.Insts {
		position := allocation.InstructionPositions[instructionID]*6 + 2
		if position < start || position > end {
			continue
		}
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Reg == victim {
				return 0, false
			}
		}
	}
	return victim, true
}

func regionalFragmentsPhysicalFree(allocation *GreedyAllocation, bank Bank, physical uint16, start, end uint32) bool {
	for _, fragment := range allocation.Fragments {
		if fragment.Location.Bank == bank && fragment.Location.Index == physical && fragment.Start <= end && start <= fragment.End {
			return false
		}
	}
	return true
}

func intervalCrossesPositions(calls []callPosition, interval LiveInterval) bool {
	first := firstCallAfter(calls, interval.Start)
	return first < len(calls) && calls[first].position < interval.End
}

func regionalPhysicalFree(allocation *GreedyAllocation, bank Bank, physical uint16, start, end uint32) bool {
	bankIndex := 0
	if bank == BankFPR {
		bankIndex = 1
	}
	for occupant := allocation.occupantHead[bankIndex][physical]; occupant != 0; occupant = allocation.occupantNext[occupant-1] {
		interval := allocation.Intervals[occupant-1]
		if interval.Start > end {
			break
		}
		if intervalsOverlapRange(interval, start, end) {
			return false
		}
	}
	return regionalFragmentsPhysicalFree(allocation, bank, physical, start, end)
}

func intervalsOverlapRange(interval LiveInterval, start, end uint32) bool {
	return interval.Start <= end && start <= interval.End
}

func buildSpillSets(allocation *GreedyAllocation) error {
	allocation.SpillSets = allocation.SpillSets[:0]
	allocation.SpillMembers = allocation.SpillMembers[:0]
	for slot := uint16(0); slot < allocation.SpillSlots; slot++ {
		start := uint32(len(allocation.SpillMembers))
		var weight uint32
		bank := BankInvalid
		for _, interval := range allocation.Intervals {
			location := allocation.Locations[interval.Reg]
			if location.Kind != LocationSpill || location.Index != slot {
				continue
			}
			allocation.SpillMembers = append(allocation.SpillMembers, interval.Reg)
			weight = uint32(min(uint64(weight)+uint64(interval.Weight), uint64(^uint32(0))))
			if bank == BankInvalid {
				bank = interval.Bank
			}
		}
		if uint32(len(allocation.SpillMembers)) != start {
			count := uint32(len(allocation.SpillMembers)) - start
			if count > uint32(^uint16(0)) {
				return &BudgetError{Resource: fmt.Sprintf("spillset %d members", slot), Required: uint64(count), Limit: uint64(^uint16(0))}
			}
			allocation.SpillSets = append(allocation.SpillSets, SpillSet{MemberStart: start, MemberCount: uint16(count), Slot: slot, Weight: weight, Bank: bank})
		}
	}
	return nil
}

func VerifySpillSets(allocation *GreedyAllocation) error {
	if allocation == nil {
		return fmt.Errorf("railmach: nil spillset allocation")
	}
	return verifySpillSets(allocation, make([]bool, len(allocation.Locations)))
}

func verifySpillSetsReusingScratch(allocation *GreedyAllocation) error {
	if allocation == nil {
		return fmt.Errorf("railmach: nil spillset allocation")
	}
	seen := resize(allocation.verifySpillSeen, len(allocation.Locations))
	clear(seen)
	allocation.verifySpillSeen = seen
	return verifySpillSets(allocation, seen)
}

func verifySpillSets(allocation *GreedyAllocation, seen []bool) error {
	for _, set := range allocation.SpillSets {
		end := uint64(set.MemberStart) + uint64(set.MemberCount)
		if set.MemberCount == 0 || end > uint64(len(allocation.SpillMembers)) || set.Slot >= allocation.SpillSlots || set.Bank == BankInvalid {
			return fmt.Errorf("railmach: malformed spillset %#v", set)
		}
		for _, member := range allocation.SpillMembers[set.MemberStart:uint32(end)] {
			if int(member) >= len(allocation.Locations) || seen[member] || allocation.Locations[member].Kind != LocationSpill || allocation.Locations[member].Index != set.Slot {
				return fmt.Errorf("railmach: invalid spillset member %d in %#v", member, set)
			}
			seen[member] = true
		}
	}
	for reg, location := range allocation.Locations {
		if location.Kind == LocationSpill && !seen[reg] {
			return fmt.Errorf("railmach: spilled register %d has no spillset", reg)
		}
	}
	clear(allocation.occupantHead[:])
	occupantNext := resize(allocation.occupantNext, len(allocation.Intervals))
	clear(occupantNext)
	allocation.occupantNext = occupantNext
	for intervalIndex, interval := range allocation.Intervals {
		location := allocation.Locations[interval.Reg]
		if location.Kind != LocationRegister || location.Index >= 64 {
			continue
		}
		bank := 0
		if location.Bank == BankFPR {
			bank = 1
		}
		occupantNext[intervalIndex] = allocation.occupantHead[bank][location.Index]
		allocation.occupantHead[bank][location.Index] = uint32(intervalIndex) + 1
	}
	for index, fragment := range allocation.Fragments {
		if fragment.Reg == 0 || int(fragment.Reg) >= len(allocation.Locations) || fragment.End < fragment.Start ||
			allocation.Locations[fragment.Reg].Kind != LocationSpill || fragment.Location.Kind != LocationRegister ||
			fragment.Location.Bank != allocation.Locations[fragment.Reg].Bank {
			return fmt.Errorf("railmach: invalid regional fragment %#v", fragment)
		}
		bank := 0
		if fragment.Location.Bank == BankFPR {
			bank = 1
		}
		for occupant := allocation.occupantHead[bank][fragment.Location.Index]; occupant != 0; occupant = occupantNext[occupant-1] {
			interval := allocation.Intervals[occupant-1]
			location := allocation.Locations[interval.Reg]
			if fragment.Victim != 0 && interval.Reg != fragment.Victim && interval.Start == fragment.End+1 {
				return fmt.Errorf("railmach: regional fragment %d overwrites newly defined vreg %d before victim restore", index, interval.Reg)
			}
			if location.Kind == LocationRegister && location.Bank == fragment.Location.Bank && location.Index == fragment.Location.Index && interval.Start <= fragment.End && fragment.Start <= interval.End && interval.Reg != fragment.Victim {
				return fmt.Errorf("railmach: regional fragment %d conflicts with vreg %d", index, interval.Reg)
			}
		}
		if fragment.Victim != 0 {
			if int(fragment.Victim) >= len(allocation.Locations) || fragment.VictimSlot >= allocation.SpillSlots || allocation.Locations[fragment.Victim] != fragment.Location {
				return fmt.Errorf("railmach: regional fragment %d has invalid victim", index)
			}
		}
		for otherIndex, other := range allocation.Fragments[:index] {
			if other.Location.Bank == fragment.Location.Bank && other.Location.Index == fragment.Location.Index && other.Start <= fragment.End && fragment.Start <= other.End {
				return fmt.Errorf("railmach: regional fragments %d and %d overlap", otherIndex, index)
			}
		}
	}
	return nil
}

// VerifyRegionalFragments independently replays the safety conditions needed
// by entry reloads and temporary victim displacement.
func VerifyRegionalFragments(f *Func, schedule *Schedule, allocation *GreedyAllocation, config GreedyConfig) error {
	if f == nil || allocation == nil {
		return fmt.Errorf("railmach: nil regional allocation")
	}
	for index, fragment := range allocation.Fragments {
		limit := config.Linear.GPRs
		if fragment.Location.Bank == BankFPR {
			limit = config.Linear.FPRs
		}
		if fragment.Location.Index >= uint16(limit) {
			return fmt.Errorf("railmach: regional fragment %d exceeds physical bank", index)
		}
		if fragment.Victim != 0 && regionalFragmentEndsAtBlockBoundary(f, schedule, fragment.End) {
			return fmt.Errorf("railmach: regional fragment %d restores its victim before block control", index)
		}
		if fragment.Victim != 0 && regionalFragmentFeedsDeferredComparison(f, schedule, fragment.Reg, fragment.Start, fragment.End) {
			return fmt.Errorf("railmach: regional fragment %d restores its victim before deferred comparison use", index)
		}
		foundStart, foundEnd := false, false
		startBlock := ^railssa.BlockID(0)
		for instructionID, instruction := range f.Insts {
			position := allocation.InstructionPositions[instructionID]*6 + 2
			if position < fragment.Start || position > fragment.End {
				continue
			}
			if IsCall(instruction.Op) || nativeControlOp(instruction.Op) {
				return fmt.Errorf("railmach: regional fragment %d crosses a control boundary", index)
			}
			block := scheduleInstructionBlock(f, schedule, uint32(instructionID))
			if startBlock == ^railssa.BlockID(0) {
				startBlock = block
			} else if block != startBlock {
				return fmt.Errorf("railmach: regional fragment %d crosses blocks", index)
			}
			for _, operand := range f.InstructionOperands(uint32(instructionID)) {
				if operand.Reg == fragment.Reg && operand.Flags&(OperandFixed|OperandColdRemat) == 0 {
					foundStart = foundStart || position == fragment.Start
					foundEnd = foundEnd || position == fragment.End
				}
				if fragment.Victim != 0 && operand.Reg == fragment.Victim {
					return fmt.Errorf("railmach: regional fragment %d displaces a used victim", index)
				}
			}
		}
		if !foundStart || !foundEnd {
			return fmt.Errorf("railmach: regional fragment %d lacks use boundaries", index)
		}
		if fragment.Victim != 0 {
			data := f.VRegs[fragment.Victim]
			start := data.Def
			if data.Flags&(VRegInitial|VRegBlockParam) == 0 {
				start = scheduledLogicalPosition(start, allocation.InstructionPositions)
			}
			if fragment.Start <= start && start <= fragment.End {
				return fmt.Errorf("railmach: regional fragment %d displaces a victim definition", index)
			}
		}
	}
	return nil
}

func nativeControlOp(kind wasm.InstrKind) bool {
	return kind == wasm.InstrIf || kind == wasm.InstrBr || kind == wasm.InstrBrIf || kind == wasm.InstrBrTable || kind == wasm.InstrReturn || kind == wasm.InstrUnreachable
}

func intervalsOverlap(a, b LiveInterval) bool {
	return !(a.End < b.Start || b.End < a.Start)
}

func recolorGreedySpills(allocation *GreedyAllocation) {
	type activeSpill struct {
		end  uint32
		slot uint16
	}
	active := make([]activeSpill, 0, 8)
	free := make([]uint16, 0, 8)
	next := uint16(0)
	for _, interval := range allocation.Intervals {
		if allocation.Locations[interval.Reg].Kind != LocationSpill {
			continue
		}
		kept := active[:0]
		for _, item := range active {
			if item.end < interval.Start {
				free = append(free, item.slot)
			} else {
				kept = append(kept, item)
			}
		}
		active = kept
		var slot uint16
		if len(free) != 0 {
			slices.Sort(free)
			slot, free = free[0], free[1:]
		} else {
			slot, next = next, next+1
		}
		location := allocation.Locations[interval.Reg]
		location.Index = slot
		allocation.Locations[interval.Reg] = location
		active = append(active, activeSpill{end: interval.End, slot: slot})
	}
	allocation.SpillSlots = next
	allocation.FrameBytes = (uint32(next)*8 + 15) &^ 15
}

func rebuildFixedMoves(f *Func, allocation *Allocation) {
	allocation.FixedMoves = allocation.FixedMoves[:0]
	for instructionID := range f.Insts {
		position := allocation.InstructionPositions[instructionID]*6 + 2
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			if operand.Flags&OperandFixed == 0 {
				continue
			}
			location := allocation.Locations[operand.Reg]
			if location.Kind != LocationRegister || location.Index != uint16(operand.Fixed) {
				allocation.FixedMoves = append(allocation.FixedMoves, FixedMove{Position: position, Reg: operand.Reg, Bank: operand.Bank, Physical: operand.Fixed})
			}
		}
	}
}
