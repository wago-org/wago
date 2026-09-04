package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

// CalleeSaveRegion moves one private-ABI save/restore pair from the function
// boundary into a bounded explicitly profile-cold, non-loop region. Block is
// the entry block and RestoreBlock contains RestoreBefore. Every block after
// Block has exactly one predecessor in the region, and every block before
// RestoreBlock has exactly one successor in the region. Physical is the
// bank-relative allocator register and SlotOffset is relative to SP.
type CalleeSaveRegion struct {
	RestoreBefore uint32
	SlotOffset    uint32
	Block         railssa.BlockID
	RestoreBlock  railssa.BlockID
	Physical      uint8
	Bank          Bank
	_             uint16
}

const maxCalleeSaveRegionBlocks = 8

type calleeSaveUse struct {
	blocks   [maxCalleeSaveRegionBlocks]railssa.BlockID
	last     [maxCalleeSaveRegionBlocks]uint32
	count    uint8
	present  bool
	eligible bool
}

// PlanCalleeSaveRegions admits only the bounded form whose every physical
// clobber is confined to one explicitly cold, acyclic single-entry/single-exit
// chain and whose last clobber precedes another instruction in the exit block.
// The following instruction is the verifier-stable restore point, including
// when it is the block terminator.
func PlanCalleeSaveRegions(f *Func, schedule *Schedule, allocation *GreedyAllocation, contract ABIContract, frame FrameLayout, coldBlocks []bool, regions []railssa.Region, reuse []CalleeSaveRegion) ([]CalleeSaveRegion, error) {
	if schedule == nil || len(schedule.Order) != len(f.Insts) || len(schedule.BlockRanges) != len(f.Blocks) || len(schedule.BlockOf) != len(f.Insts) {
		return nil, fmt.Errorf("railmach: malformed schedule for callee-save planning")
	}
	if err := verifyAllocationReusingScratch(f, &allocation.Allocation, DefaultGreedyConfig(f.Target).Linear); err != nil {
		return nil, err
	}
	if len(coldBlocks) != len(f.Blocks) {
		return nil, fmt.Errorf("railmach: %d cold-block flags for %d blocks", len(coldBlocks), len(f.Blocks))
	}

	var gpr, fpr [64]calleeSaveUse
	initializeCalleeSaveUses(&gpr, contract.CalleeGPRs)
	initializeCalleeSaveUses(&fpr, contract.CalleeFPRs)
	visit := func(bank Bank, physical uint16, start, end uint32) {
		if physical >= 64 {
			return
		}
		uses := &gpr
		if bank == BankFPR {
			uses = &fpr
		}
		use := &uses[physical]
		if !use.eligible {
			return
		}
		firstLogical, firstBlock, ok := calleeSavePositionBlock(schedule, start)
		lastLogical, lastBlock, okLast := calleeSavePositionBlock(schedule, end)
		if !ok || !okLast {
			use.eligible = false
			return
		}
		use.present = true
		record := func(block railssa.BlockID, logical uint32) bool {
			for index := uint8(0); index < use.count; index++ {
				if use.blocks[index] == block {
					use.last[index] = max(use.last[index], logical)
					return true
				}
			}
			if use.count == maxCalleeSaveRegionBlocks {
				return false
			}
			use.blocks[use.count] = block
			use.last[use.count] = logical
			use.count++
			return true
		}
		if !record(firstBlock, firstLogical) || !record(lastBlock, lastLogical) {
			use.eligible = false
		}
	}
	for _, interval := range allocation.Intervals {
		location := allocation.Locations[interval.Reg]
		if location.Kind == LocationRegister {
			visit(location.Bank, location.Index, interval.Start, interval.End)
		}
	}
	for value, data := range f.VRegs {
		if value == 0 || data.Flags&VRegInitial == 0 {
			continue
		}
		location := allocation.Locations[value]
		if location.Kind == LocationRegister && location.Index < 64 {
			uses := &gpr
			if location.Bank == BankFPR {
				uses = &fpr
			}
			uses[location.Index].eligible = false
		}
	}
	for _, fragment := range allocation.Fragments {
		if fragment.Location.Kind == LocationRegister {
			visit(fragment.Location.Bank, fragment.Location.Index, fragment.Start, fragment.End)
		}
	}
	for _, move := range allocation.FixedMoves {
		visit(move.Bank, uint16(move.Physical), move.Position, move.Position)
	}

	result := reuse[:0]
	appendBank := func(bank Bank, mask uint64, uses *[64]calleeSaveUse) {
		for physical := uint8(0); physical < 64; physical++ {
			if mask&(uint64(1)<<physical) == 0 {
				continue
			}
			use := uses[physical]
			if !use.eligible || !use.present {
				continue
			}
			entry, restoreBlock, last, ok := calleeSaveUseChain(f, use, coldBlocks, regions)
			if !ok {
				continue
			}
			blockRange := schedule.BlockRanges[restoreBlock]
			end := blockRange.Start + blockRange.Count
			if last+1 >= end || int(last+1) >= len(schedule.Order) {
				continue
			}
			result = append(result, CalleeSaveRegion{
				RestoreBefore: schedule.Order[last+1],
				SlotOffset:    calleeSaveSlotOffset(contract, frame, bank, physical),
				Block:         entry,
				RestoreBlock:  restoreBlock,
				Physical:      physical,
				Bank:          bank,
			})
		}
	}
	appendBank(BankGPR, contract.CalleeGPRs, &gpr)
	appendBank(BankFPR, contract.CalleeFPRs, &fpr)
	if err := VerifyCalleeSaveRegions(f, schedule, allocation, contract, frame, coldBlocks, regions, result); err != nil {
		return nil, err
	}
	return result, nil
}

// calleeSaveUseChain finds a bounded single-entry/single-exit chain containing
// every block that clobbers one physical register. Intervening blocks may be
// empty of clobbers, but remain explicitly cold and acyclic. The unique-edge
// contract prevents entering after the save or leaving before the restore.
func calleeSaveUseChain(f *Func, use calleeSaveUse, coldBlocks []bool, regions []railssa.Region) (entry, exit railssa.BlockID, last uint32, ok bool) {
	for candidateIndex := uint8(0); candidateIndex < use.count; candidateIndex++ {
		candidate := use.blocks[candidateIndex]
		current := candidate
		matched := uint8(0)
		for depth := 0; depth < maxCalleeSaveRegionBlocks; depth++ {
			if !calleeSaveBlockEligible(f, current, coldBlocks, regions) {
				break
			}
			for index := uint8(0); index < use.count; index++ {
				if use.blocks[index] == current {
					matched++
					exit, last = current, use.last[index]
					break
				}
			}
			if matched == use.count {
				return candidate, exit, last, true
			}
			next, unique := calleeSaveUniqueSuccessor(f, current)
			predecessor, uniquePredecessor := calleeSaveUniquePredecessor(f, next)
			if !unique || next == current || !uniquePredecessor || predecessor != current {
				break
			}
			current = next
		}
	}
	return 0, 0, 0, false
}

func calleeSaveUniqueSuccessor(f *Func, block railssa.BlockID) (railssa.BlockID, bool) {
	var successor railssa.BlockID
	found := false
	for _, edge := range f.Edges {
		if edge.From != block {
			continue
		}
		if found && edge.To != successor {
			return 0, false
		}
		successor, found = edge.To, true
	}
	return successor, found
}

func calleeSaveUniquePredecessor(f *Func, block railssa.BlockID) (railssa.BlockID, bool) {
	var predecessor railssa.BlockID
	found := false
	for _, edge := range f.Edges {
		if edge.To != block {
			continue
		}
		if found && edge.From != predecessor {
			return 0, false
		}
		predecessor, found = edge.From, true
	}
	if !found {
		return 0, false
	}
	return predecessor, true
}

func initializeCalleeSaveUses(uses *[64]calleeSaveUse, mask uint64) {
	for physical := range uses {
		uses[physical] = calleeSaveUse{eligible: mask&(uint64(1)<<physical) != 0}
	}
}

func calleeSavePositionBlock(schedule *Schedule, position uint32) (uint32, railssa.BlockID, bool) {
	logical := position / 6
	if schedule == nil || int(logical) >= len(schedule.Order) {
		return 0, 0, false
	}
	instruction := schedule.Order[logical]
	if int(instruction) >= len(schedule.BlockOf) {
		return 0, 0, false
	}
	return logical, schedule.BlockOf[instruction], true
}

func calleeSaveBlockEligible(f *Func, block railssa.BlockID, coldBlocks []bool, regions []railssa.Region) bool {
	if int(block) >= len(f.Blocks) || block == 0 || !coldBlocks[block] {
		return false
	}
	machineBlock := f.Blocks[block]
	if machineBlock.Flags&(railssa.BlockLoopHeader|railssa.BlockExit) != 0 {
		return false
	}
	return machineBlock.Region == railssa.NoRegion || int(machineBlock.Region) < len(regions) && regions[machineBlock.Region].LoopDepth == 0
}

func calleeSaveSlotOffset(contract ABIContract, frame FrameLayout, bank Bank, physical uint8) uint32 {
	offset := frame.SpillBytes + frame.RootBytes
	for index := uint8(0); index < 64; index++ {
		if contract.CalleeGPRs&(uint64(1)<<index) != 0 {
			if bank == BankGPR && index == physical {
				return offset
			}
			offset += 8
		}
	}
	for index := uint8(0); index < 64; index++ {
		if contract.CalleeFPRs&(uint64(1)<<index) != 0 {
			if bank == BankFPR && index == physical {
				return offset
			}
			offset += 8
		}
	}
	return ^uint32(0)
}

// VerifyCalleeSaveRegions independently checks the bounded shrink-wrapping
// contract, including exact frame slots, the single-entry/single-exit chain,
// and that every physical clobber remains inside it before its restore point.
func VerifyCalleeSaveRegions(f *Func, schedule *Schedule, allocation *GreedyAllocation, contract ABIContract, frame FrameLayout, coldBlocks []bool, regions []railssa.Region, planned []CalleeSaveRegion) error {
	var seenGPR, seenFPR uint64
	for _, region := range planned {
		if region.Physical >= 64 || region.Bank != BankGPR && region.Bank != BankFPR ||
			!calleeSaveBlockEligible(f, region.Block, coldBlocks, regions) || !calleeSaveBlockEligible(f, region.RestoreBlock, coldBlocks, regions) ||
			!verifyCalleeSaveChain(f, region.Block, region.RestoreBlock, coldBlocks, regions) {
			return fmt.Errorf("railmach: invalid callee-save region %#v", region)
		}
		mask, seen := contract.CalleeGPRs, &seenGPR
		if region.Bank == BankFPR {
			mask, seen = contract.CalleeFPRs, &seenFPR
		}
		bit := uint64(1) << region.Physical
		if mask&bit == 0 || *seen&bit != 0 || region.SlotOffset != calleeSaveSlotOffset(contract, frame, region.Bank, region.Physical) {
			return fmt.Errorf("railmach: inconsistent callee-save region %#v", region)
		}
		*seen |= bit
		if int(region.RestoreBefore) >= len(schedule.BlockOf) || schedule.BlockOf[region.RestoreBefore] != region.RestoreBlock {
			return fmt.Errorf("railmach: restore instruction %d leaves block %d", region.RestoreBefore, region.RestoreBlock)
		}
		restoreLogical := allocation.InstructionPositions[region.RestoreBefore]
		sawEntry, sawRestore := false, false
		check := func(bank Bank, physical uint16, start, end uint32) error {
			if bank != region.Bank || physical != uint16(region.Physical) {
				return nil
			}
			_, firstBlock, ok := calleeSavePositionBlock(schedule, start)
			lastLogical, lastBlock, okLast := calleeSavePositionBlock(schedule, end)
			if !ok || !okLast || !calleeSaveBlockOnChain(f, region.Block, region.RestoreBlock, firstBlock) ||
				!calleeSaveBlockOnChain(f, region.Block, region.RestoreBlock, lastBlock) || lastBlock == region.RestoreBlock && lastLogical >= restoreLogical {
				return fmt.Errorf("railmach: physical %d/%d escapes shrink-wrapped blocks %d..%d", bank, physical, region.Block, region.RestoreBlock)
			}
			sawEntry = sawEntry || firstBlock == region.Block
			sawRestore = sawRestore || lastBlock == region.RestoreBlock
			return nil
		}
		for _, interval := range allocation.Intervals {
			location := allocation.Locations[interval.Reg]
			if location.Kind == LocationRegister {
				if err := check(location.Bank, location.Index, interval.Start, interval.End); err != nil {
					return err
				}
			}
		}
		for value, data := range f.VRegs {
			location := allocation.Locations[value]
			if value != 0 && data.Flags&VRegInitial != 0 && location.Kind == LocationRegister && location.Bank == region.Bank && location.Index == uint16(region.Physical) {
				return fmt.Errorf("railmach: initial vreg %d clobbers shrink-wrapped physical register", value)
			}
		}
		for _, fragment := range allocation.Fragments {
			if fragment.Location.Kind == LocationRegister {
				if err := check(fragment.Location.Bank, fragment.Location.Index, fragment.Start, fragment.End); err != nil {
					return err
				}
			}
		}
		for _, move := range allocation.FixedMoves {
			if err := check(move.Bank, uint16(move.Physical), move.Position, move.Position); err != nil {
				return err
			}
		}
		if !sawEntry || !sawRestore {
			return fmt.Errorf("railmach: callee-save region %d..%d does not have endpoint clobbers", region.Block, region.RestoreBlock)
		}
	}
	return nil
}

func verifyCalleeSaveChain(f *Func, entry, exit railssa.BlockID, coldBlocks []bool, regions []railssa.Region) bool {
	current := entry
	for depth := 0; depth < maxCalleeSaveRegionBlocks; depth++ {
		if !calleeSaveBlockEligible(f, current, coldBlocks, regions) {
			return false
		}
		if current == exit {
			return true
		}
		next, unique := calleeSaveUniqueSuccessor(f, current)
		predecessor, uniquePredecessor := calleeSaveUniquePredecessor(f, next)
		if !unique || next == current || !uniquePredecessor || predecessor != current {
			return false
		}
		current = next
	}
	return false
}

func calleeSaveBlockOnChain(f *Func, entry, exit, want railssa.BlockID) bool {
	current := entry
	for depth := 0; depth < maxCalleeSaveRegionBlocks; depth++ {
		if current == want {
			return true
		}
		if current == exit {
			return false
		}
		next, unique := calleeSaveUniqueSuccessor(f, current)
		if !unique || next == current {
			return false
		}
		current = next
	}
	return false
}
