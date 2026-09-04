package railmach

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railspec"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const PostRAScanLimit = 8

type RewriteKind uint8

const (
	RewriteInvalid RewriteKind = iota
	RewriteAMD64LEA
	RewriteAMD64FusionRepair
	RewriteAMD64FixedRepair
	RewriteARM64Pair
	RewriteARM64PrePostIndex
	RewritePhysicalRename
	RewriteLoadStoreForward
	RewriteAMD64MemoryFold
	RewriteARM64CompareBranch
	RewriteARM64CondIncrement
	RewriteARM64RepeatedAdd
	RewriteARM64ByteWiden
	RewriteARM64ByteSwap
	RewriteARM64Narrow16To8
)

type Rewrite struct {
	First  uint32
	Second uint32
	Kind   RewriteKind
	_      [3]byte
}

type PostRAPlan struct {
	Rewrites        []Rewrite
	EliminatedMoves uint32
	ScanLimit       uint8

	position []uint32
	seen     []bool
	uses     []uint32
}

// PlanPostRA performs a bounded physical-quality scan. It records legal local
// rewrites; encoding commits them only after target-specific verification.
func PlanPostRA(target Target, f *Func, selection *SelectionPlan, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit, reuse *PostRAPlan) (*PostRAPlan, error) {
	if schedule == nil || len(schedule.Order) != len(f.Insts) || selection == nil || len(selection.Selections) != len(f.Insts) || exit == nil {
		return nil, fmt.Errorf("railmach: post-RA planning requires selection, schedule, allocation, and SSA exit")
	}
	if err := verifyAllocationReusingScratch(f, &allocation.Allocation, DefaultGreedyConfig(target).Linear); err != nil {
		return nil, err
	}
	return planPostRAVerifiedAllocation(target, f, selection, schedule, allocation, exit, reuse)
}

// PlanPostRAVerifiedAllocation consumes an allocation returned by a verified
// allocator without replaying that complete verifier at the adjacent post-RA
// boundary. Post-RA schedule and rewrite legality checks remain unchanged.
func PlanPostRAVerifiedAllocation(target Target, f *Func, selection *SelectionPlan, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit, reuse *PostRAPlan) (*PostRAPlan, error) {
	if f == nil || allocation == nil {
		return nil, fmt.Errorf("railmach: post-RA planning requires a verified allocation")
	}
	return planPostRAVerifiedAllocation(target, f, selection, schedule, allocation, exit, reuse)
}

func planPostRAVerifiedAllocation(target Target, f *Func, selection *SelectionPlan, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit, reuse *PostRAPlan) (*PostRAPlan, error) {
	if schedule == nil || len(schedule.Order) != len(f.Insts) || selection == nil || len(selection.Selections) != len(f.Insts) || exit == nil {
		return nil, fmt.Errorf("railmach: post-RA planning requires selection, schedule, allocation, and SSA exit")
	}
	if reuse == nil {
		reuse = new(PostRAPlan)
	}
	rewrites := reuse.Rewrites[:0]
	position := resize(reuse.position, len(f.Insts))
	seen := resize(reuse.seen, len(f.Insts))
	uses := resize(reuse.uses, len(f.VRegs))
	*reuse = PostRAPlan{Rewrites: rewrites, EliminatedMoves: exit.Debt.Coalesced, ScanLimit: PostRAScanLimit, position: position, seen: seen, uses: uses}
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	for index, instruction := range schedule.Order {
		if int(instruction) >= len(f.Insts) || seen[instruction] {
			return nil, fmt.Errorf("railmach: post-RA schedule has invalid instruction %d", instruction)
		}
		seen[instruction] = true
		position[instruction] = uint32(index)
	}
	for instructionID, instruction := range f.Insts {
		switch target {
		case TargetAMD64:
			if instruction.Result != 0 && allocation.Locations[instruction.Result].Kind == LocationRegister && amd64LEARepairable(f, selection, uint32(instructionID)) {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: ^uint32(0), Kind: RewriteAMD64LEA})
			}
			if selection.Selections[instructionID].Rule == railspec.RuleAMD64DivFixed && hasFixedRepairAt(allocation.FixedMoves, uint32(instructionID)*6+2) {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: ^uint32(0), Kind: RewriteAMD64FixedRepair})
			}
		case TargetARM64:
			if arm64PreIndexable(instruction) {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: ^uint32(0), Kind: RewriteARM64PrePostIndex})
			}
		}
		if isMemoryOp(instruction.Op) {
			for distance := 1; distance <= PostRAScanLimit && instructionID+distance < len(f.Insts); distance++ {
				next := f.Insts[instructionID+distance]
				if isMemoryBarrier(next.Op) {
					break
				}
				adjacent := position[instructionID+distance] == position[instructionID]+1
				sameBlock := schedule.BlockOf[instructionID+distance] == schedule.BlockOf[instructionID]
				if target == TargetARM64 && adjacent && sameBlock && pairableMemory(instruction, next) && sameMemoryBase(f, uint32(instructionID), uint32(instructionID+distance)) {
					reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: uint32(instructionID + distance), Kind: RewriteARM64Pair})
					break
				}
				if target == TargetARM64 && adjacent && sameBlock && arm64PostIndexChainable(instruction, next) && sameMemoryBase(f, uint32(instructionID), uint32(instructionID+distance)) {
					reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: uint32(instructionID + distance), Kind: RewriteARM64PrePostIndex})
					break
				}
				if adjacent && forwardableStoreLoad(f, uint32(instructionID), uint32(instructionID+distance)) {
					reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: uint32(instructionID + distance), Kind: RewriteLoadStoreForward})
					break
				}
				if target == TargetAMD64 && adjacent && sameBlock && amd64FoldableLoadConsumer(f, uint32(instructionID), uint32(instructionID+distance), uses) {
					reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: uint32(instructionID), Second: uint32(instructionID + distance), Kind: RewriteAMD64MemoryFold})
					break
				}
				if isMemoryOp(next.Op) {
					break
				}
			}
		}
	}
	for _, combination := range selection.Combinations {
		if combination.Kind != CombineCompareBranch || combination.Producer == ^uint32(0) {
			continue
		}
		adjacent := position[combination.Consumer] == position[combination.Producer]+1 && schedule.BlockOf[combination.Consumer] == schedule.BlockOf[combination.Producer]
		if target == TargetAMD64 && adjacent && compareBranchFusionRepairable(target, f, combination.Producer, combination.Consumer, uses) {
			reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: combination.Producer, Second: combination.Consumer, Kind: RewriteAMD64FusionRepair})
		} else if target == TargetARM64 && adjacent && compareBranchFusionRepairable(target, f, combination.Producer, combination.Consumer, uses) {
			reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: combination.Producer, Second: combination.Consumer, Kind: RewriteARM64CompareBranch})
		} else if !adjacent && physicalFlagsRenameable(target, f, schedule, combination.Producer, combination.Consumer, position, uses) {
			reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: combination.Producer, Second: combination.Consumer, Kind: RewritePhysicalRename})
		}
	}
	if target == TargetARM64 {
		for index := 0; index+1 < len(schedule.Order); index++ {
			producer, consumer := schedule.Order[index], schedule.Order[index+1]
			if schedule.BlockOf[producer] == schedule.BlockOf[consumer] && arm64CondIncrementable(f, producer, consumer, uses) {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: producer, Second: consumer, Kind: RewriteARM64CondIncrement})
			}
		}
		for index := 0; index < len(schedule.Order); index++ {
			first := schedule.Order[index]
			last, _, _, _, ok := arm64RepeatedAddChainFrom(f, schedule, index, uses)
			if !ok {
				continue
			}
			reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: first, Second: last, Kind: RewriteARM64RepeatedAdd})
			for index+1 < len(schedule.Order) && schedule.Order[index] != last {
				index++
			}
		}
		for _, final := range schedule.Order {
			first, _, ok := arm64ByteWidenChain(f, schedule, final, position, uses)
			if ok {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: first, Second: final, Kind: RewriteARM64ByteWiden})
			}
			if _, members, ok := verifyARM64ByteSwapChain(f, schedule, final, position, uses); ok {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: members[0], Second: final, Kind: RewriteARM64ByteSwap})
			}
			if _, members, ok := verifyARM64Narrow16To8Chain(f, schedule, final, position, uses); ok {
				reuse.Rewrites = append(reuse.Rewrites, Rewrite{First: members[0], Second: final, Kind: RewriteARM64Narrow16To8})
			}
		}
	}
	if err := verifyPostRAPlanReusingScratch(target, f, selection, schedule, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func arm64PostIndexScheduled(f *Func, schedule *Schedule, position []uint32, first, second uint32) bool {
	return second != ^uint32(0) && position[second] == position[first]+1 &&
		schedule.BlockOf[first] == schedule.BlockOf[second] && sameMemoryBase(f, first, second) &&
		arm64PostIndexChainable(f.Insts[first], f.Insts[second])
}

func arm64PreIndexable(instruction Inst) bool {
	offset := uint32(instruction.Aux)
	return isMemoryOp(instruction.Op) && offset > 0 && offset <= 255 && postRAMemoryWidth(instruction.Op) != 0
}

// arm64PostIndexChainable admits two adjacent scalar accesses whose effective
// addresses differ by a signed imm9. The first access can update the reserved
// native-address scratch after touching memory; the second consumes that
// scratch without changing either Wasm-visible address value.
func arm64PostIndexChainable(first, second Inst) bool {
	if !isMemoryOp(first.Op) || !isMemoryOp(second.Op) || postRAMemoryWidth(first.Op) == 0 || postRAMemoryWidth(second.Op) == 0 {
		return false
	}
	delta := int64(uint32(second.Aux)) - int64(uint32(first.Aux))
	return delta >= -256 && delta <= 255 && delta != 0
}

// physicalFlagsRenameable proves that a one-use comparison boolean can be kept
// in AMD64 EFLAGS or ARM64 NZCV until its nonadjacent branch consumer. The
// initial policy deliberately admits only integer constants between the pair:
// their MOV materialization cannot alter either flags register, while every
// more complex opcode remains a near miss until its exact emitted sequence is
// independently listed.
func physicalFlagsRenameable(target Target, f *Func, schedule *Schedule, producer, consumer uint32, position, uses []uint32) bool {
	if target != TargetAMD64 && target != TargetARM64 || schedule == nil || int(producer) >= len(position) || int(consumer) >= len(position) ||
		schedule.BlockOf[producer] != schedule.BlockOf[consumer] || position[producer]+1 >= position[consumer] ||
		!compareBranchFusionRepairable(target, f, producer, consumer, uses) {
		return false
	}
	for scheduled := position[producer] + 1; scheduled < position[consumer]; scheduled++ {
		instruction := f.Insts[schedule.Order[scheduled]]
		if instruction.Result != 0 && f.VRegs[instruction.Result].Flags&VRegElided != 0 {
			continue
		}
		if instruction.Op != wasm.InstrI32Const && instruction.Op != wasm.InstrI64Const {
			return false
		}
	}
	return true
}

func arm64RepeatedAddChainFrom(f *Func, schedule *Schedule, start int, uses []uint32) (last uint32, count uint8, initial, invariant VReg, ok bool) {
	if start < 0 || start >= len(schedule.Order) {
		return 0, 0, 0, 0, false
	}
	firstID := schedule.Order[start]
	first := f.Insts[firstID]
	operands := f.InstructionOperands(firstID)
	if first.Op != wasm.InstrI32Add || first.Result == 0 || len(operands) != 2 {
		return 0, 0, 0, 0, false
	}
	try := func(initialCandidate, invariantCandidate VReg) (uint32, uint8, bool) {
		previous := first.Result
		chainCount := uint8(1)
		lastID := firstID
		for index := start + 1; index < len(schedule.Order) && chainCount < 32; index++ {
			instructionID := schedule.Order[index]
			instruction := f.Insts[instructionID]
			if instruction.Result != 0 && f.VRegs[instruction.Result].Flags&VRegElided != 0 {
				continue
			}
			args := f.InstructionOperands(instructionID)
			if schedule.BlockOf[instructionID] != schedule.BlockOf[firstID] || instruction.Op != wasm.InstrI32Add || instruction.Result == 0 || len(args) != 2 || int(previous) >= len(uses) || uses[previous] != 1 {
				break
			}
			if !((args[0].Reg == previous && args[1].Reg == invariantCandidate) || (args[1].Reg == previous && args[0].Reg == invariantCandidate)) {
				break
			}
			previous, lastID = instruction.Result, instructionID
			chainCount++
		}
		powerOfTwo := chainCount >= 4 && chainCount&(chainCount-1) == 0
		return lastID, chainCount, powerOfTwo
	}
	if lastID, chainCount, matched := try(operands[0].Reg, operands[1].Reg); matched {
		return lastID, chainCount, operands[0].Reg, operands[1].Reg, true
	}
	if lastID, chainCount, matched := try(operands[1].Reg, operands[0].Reg); matched {
		return lastID, chainCount, operands[1].Reg, operands[0].Reg, true
	}
	return 0, 0, 0, 0, false
}

// VerifyARM64RepeatedAddChain independently reconstructs a planned power-of-two
// add chain from the scheduled machine SSA.
func VerifyARM64RepeatedAddChain(f *Func, schedule *Schedule, first, last uint32) (initial, invariant VReg, count uint8, ok bool) {
	uses := make([]uint32, len(f.VRegs))
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	for position, instructionID := range schedule.Order {
		if instructionID != first {
			continue
		}
		gotLast, gotCount, gotInitial, gotInvariant, matched := arm64RepeatedAddChainFrom(f, schedule, position, uses)
		return gotInitial, gotInvariant, gotCount, matched && gotLast == last
	}
	return 0, 0, 0, false
}

// VerifyARM64ByteWidenChain independently reconstructs a scalar expansion of
// four low bytes into four zero-extended 16-bit lanes. When final is ^uint32(0),
// first is treated as the planned first instruction and the matching final is
// discovered from the schedule.
func VerifyARM64ByteWidenChain(f *Func, schedule *Schedule, first, final uint32) (uint32, VReg, bool) {
	if f == nil || schedule == nil {
		return 0, 0, false
	}
	position := make([]uint32, len(f.Insts))
	uses := make([]uint32, len(f.VRegs))
	for index, instruction := range schedule.Order {
		if int(instruction) >= len(position) {
			return 0, 0, false
		}
		position[instruction] = uint32(index)
	}
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	if final != ^uint32(0) {
		gotFirst, source, ok := arm64ByteWidenChain(f, schedule, final, position, uses)
		return gotFirst, source, ok && gotFirst == first
	}
	for _, candidate := range schedule.Order {
		gotFirst, source, ok := arm64ByteWidenChain(f, schedule, candidate, position, uses)
		if ok && gotFirst == first {
			return candidate, source, true
		}
	}
	return 0, 0, false
}

func arm64ByteWidenChain(f *Func, schedule *Schedule, final uint32, position, uses []uint32) (uint32, VReg, bool) {
	if int(final) >= len(f.Insts) || len(position) != len(f.Insts) || len(uses) != len(f.VRegs) {
		return 0, 0, false
	}
	definition := func(value VReg, op wasm.InstrKind) (uint32, []Operand, bool) {
		if value == 0 || int(value) >= len(f.VRegs) {
			return 0, nil, false
		}
		id := f.VRegs[value].Def / 6
		if int(id) >= len(f.Insts) || f.Insts[id].Result != value || f.Insts[id].Op != op {
			return 0, nil, false
		}
		return id, f.InstructionOperands(id), true
	}
	constant := func(value VReg, want uint64) bool {
		id, _, ok := definition(value, wasm.InstrI64Const)
		return ok && f.Insts[id].Aux == want
	}
	andConstant := func(id uint32, want uint64) (VReg, bool) {
		if int(id) >= len(f.Insts) || f.Insts[id].Op != wasm.InstrI64And {
			return 0, false
		}
		operands := f.InstructionOperands(id)
		if len(operands) != 2 {
			return 0, false
		}
		if constant(operands[0].Reg, want) {
			return operands[1].Reg, true
		}
		if constant(operands[1].Reg, want) {
			return operands[0].Reg, true
		}
		return 0, false
	}
	orShift := func(value VReg, shift uint64) (base VReg, orID, shiftID uint32, ok bool) {
		orID, operands, ok := definition(value, wasm.InstrI64Or)
		if !ok || len(operands) != 2 || uses[value] != 1 {
			return 0, 0, 0, false
		}
		for side := range 2 {
			base, shifted := operands[side].Reg, operands[1-side].Reg
			shiftID, shiftOperands, matched := definition(shifted, wasm.InstrI64Shl)
			if !matched || len(shiftOperands) != 2 || uses[shifted] != 1 {
				continue
			}
			if shiftOperands[0].Reg == base && constant(shiftOperands[1].Reg, shift) || shiftOperands[1].Reg == base && constant(shiftOperands[0].Reg, shift) {
				return base, orID, shiftID, true
			}
		}
		return 0, 0, 0, false
	}
	outer, ok := andConstant(final, 0x00ff00ff00ff00ff)
	if !ok {
		return 0, 0, false
	}
	base2, or2, shift8, ok := orShift(outer, 8)
	if !ok || uses[base2] != 2 {
		return 0, 0, false
	}
	and1 := f.VRegs[base2].Def / 6
	outer, ok = andConstant(and1, 0x0000ffff0000ffff)
	if !ok {
		return 0, 0, false
	}
	base1, or1, shift16, ok := orShift(outer, 16)
	if !ok || uses[base1] != 2 {
		return 0, 0, false
	}
	first := f.VRegs[base1].Def / 6
	source, ok := andConstant(first, 0xffffffff)
	if !ok {
		return 0, 0, false
	}
	ids := [...]uint32{first, shift16, or1, and1, shift8, or2, final}
	block := schedule.BlockOf[first]
	for index, id := range ids {
		if int(id) >= len(schedule.BlockOf) || schedule.BlockOf[id] != block || index != 0 && position[id] <= position[ids[index-1]] {
			return 0, 0, false
		}
	}
	if position[final]-position[first] > 12 {
		return 0, 0, false
	}
	for scheduled := position[first]; scheduled <= position[final]; scheduled++ {
		id := schedule.Order[scheduled]
		matched := false
		for _, candidate := range ids {
			matched = matched || id == candidate
		}
		if matched {
			continue
		}
		if f.Insts[id].Op != wasm.InstrI64Const {
			return 0, 0, false
		}
	}
	return first, source, true
}

// VerifyARM64ByteSwapChain recognizes the canonical scalar i32 byte-swap tree
// emitted by Rust for from_be: ror(x & 0x00ff00ff, 8) |
// (ror(x, 24) & 0x00ff00ff). The returned members are in required schedule
// order and include the final or.
func VerifyARM64ByteSwapChain(f *Func, schedule *Schedule, final uint32) (VReg, [5]uint32, bool) {
	if f == nil || schedule == nil {
		return 0, [5]uint32{}, false
	}
	position := make([]uint32, len(f.Insts))
	uses := make([]uint32, len(f.VRegs))
	for index, instruction := range schedule.Order {
		if int(instruction) >= len(position) {
			return 0, [5]uint32{}, false
		}
		position[instruction] = uint32(index)
	}
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	return verifyARM64ByteSwapChain(f, schedule, final, position, uses)
}

func verifyARM64ByteSwapChain(f *Func, schedule *Schedule, final uint32, position, uses []uint32) (VReg, [5]uint32, bool) {
	var none [5]uint32
	if int(final) >= len(f.Insts) || len(position) != len(f.Insts) || len(uses) != len(f.VRegs) || f.Insts[final].Op != wasm.InstrI32Or {
		return 0, none, false
	}
	definition := func(value VReg, op wasm.InstrKind) (uint32, []Operand, bool) {
		if value == 0 || int(value) >= len(f.VRegs) || f.VRegs[value].Def%6 != 3 {
			return 0, nil, false
		}
		id := f.VRegs[value].Def / 6
		if int(id) >= len(f.Insts) || f.Insts[id].Result != value || f.Insts[id].Op != op {
			return 0, nil, false
		}
		return id, f.InstructionOperands(id), true
	}
	constant := func(value VReg, want uint64) bool {
		id, _, ok := definition(value, wasm.InstrI32Const)
		return ok && uint64(uint32(f.Insts[id].Aux)) == want
	}
	matchMaskedRotate := func(value VReg) (source VReg, andID, rotateID uint32, ok bool) {
		rotateID, rotateOperands, ok := definition(value, wasm.InstrI32Rotr)
		if !ok || len(rotateOperands) != 2 || uses[value] != 1 || !constant(rotateOperands[1].Reg, 8) {
			return 0, 0, 0, false
		}
		masked := rotateOperands[0].Reg
		andID, andOperands, ok := definition(masked, wasm.InstrI32And)
		if !ok || len(andOperands) != 2 || uses[masked] != 1 {
			return 0, 0, 0, false
		}
		if constant(andOperands[0].Reg, 0x00ff00ff) {
			return andOperands[1].Reg, andID, rotateID, true
		}
		if constant(andOperands[1].Reg, 0x00ff00ff) {
			return andOperands[0].Reg, andID, rotateID, true
		}
		return 0, 0, 0, false
	}
	matchRotateMasked := func(value VReg) (source VReg, rotateID, andID uint32, ok bool) {
		andID, andOperands, ok := definition(value, wasm.InstrI32And)
		if !ok || len(andOperands) != 2 || uses[value] != 1 {
			return 0, 0, 0, false
		}
		rotated := andOperands[0].Reg
		if constant(rotated, 0x00ff00ff) {
			rotated = andOperands[1].Reg
		} else if !constant(andOperands[1].Reg, 0x00ff00ff) {
			return 0, 0, 0, false
		}
		rotateID, rotateOperands, ok := definition(rotated, wasm.InstrI32Rotr)
		if !ok || len(rotateOperands) != 2 || uses[rotated] != 1 || !constant(rotateOperands[1].Reg, 24) {
			return 0, 0, 0, false
		}
		return rotateOperands[0].Reg, rotateID, andID, true
	}
	operands := f.InstructionOperands(final)
	if len(operands) != 2 {
		return 0, none, false
	}
	for side := range 2 {
		source, andFirst, rotate8, leftOK := matchMaskedRotate(operands[side].Reg)
		otherSource, rotate24, andSecond, rightOK := matchRotateMasked(operands[1-side].Reg)
		members := [5]uint32{andFirst, rotate8, rotate24, andSecond, final}
		if !leftOK || !rightOK || source != otherSource {
			continue
		}
		block := schedule.BlockOf[members[0]]
		previous := position[members[0]]
		for index, id := range members {
			if int(id) >= len(schedule.BlockOf) || schedule.BlockOf[id] != block || index != 0 && position[id] <= previous {
				leftOK = false
				break
			}
			previous = position[id]
		}
		if !leftOK {
			continue
		}
		return source, members, true
	}
	return 0, none, false
}

// VerifyARM64Narrow16To8Chain recognizes an i64 shift/mask/or tree that packs
// the low bytes of four adjacent 16-bit lanes into the low 32 bits. ARM64 XTN
// performs the same truncating lane pack without scalar shifts and masks.
func VerifyARM64Narrow16To8Chain(f *Func, schedule *Schedule, final uint32) (VReg, [10]uint32, bool) {
	if f == nil || schedule == nil {
		return 0, [10]uint32{}, false
	}
	position := make([]uint32, len(f.Insts))
	uses := make([]uint32, len(f.VRegs))
	for index, instruction := range schedule.Order {
		if int(instruction) >= len(position) {
			return 0, [10]uint32{}, false
		}
		position[instruction] = uint32(index)
	}
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	return verifyARM64Narrow16To8Chain(f, schedule, final, position, uses)
}

func verifyARM64Narrow16To8Chain(f *Func, schedule *Schedule, final uint32, position, uses []uint32) (VReg, [10]uint32, bool) {
	var members [10]uint32
	if int(final) >= len(f.Insts) || len(position) != len(f.Insts) || len(uses) != len(f.VRegs) || f.Insts[final].Op != wasm.InstrI64Or {
		return 0, members, false
	}
	definition := func(value VReg, op wasm.InstrKind) (uint32, []Operand, bool) {
		if value == 0 || int(value) >= len(f.VRegs) || f.VRegs[value].Def%6 != 3 {
			return 0, nil, false
		}
		id := f.VRegs[value].Def / 6
		if int(id) >= len(f.Insts) || f.Insts[id].Result != value || f.Insts[id].Op != op {
			return 0, nil, false
		}
		return id, f.InstructionOperands(id), true
	}
	constant := func(value VReg, want uint64) bool {
		id, _, ok := definition(value, wasm.InstrI64Const)
		return ok && uint64(f.Insts[id].Aux) == want
	}
	memberCount := 0
	addMember := func(id uint32) bool {
		if memberCount >= len(members) {
			return false
		}
		members[memberCount] = id
		memberCount++
		return true
	}
	var source VReg
	seen := uint8(0)
	var collect func(VReg, bool) bool
	collect = func(value VReg, root bool) bool {
		if orID, operands, ok := definition(value, wasm.InstrI64Or); ok && len(operands) == 2 && (root || uses[value] == 1) {
			return addMember(orID) && collect(operands[0].Reg, false) && collect(operands[1].Reg, false)
		}
		andID, operands, ok := definition(value, wasm.InstrI64And)
		if !ok || len(operands) != 2 || uses[value] != 1 || !addMember(andID) {
			return false
		}
		masked, maskValue := operands[0].Reg, operands[1].Reg
		maskID, _, maskOK := definition(maskValue, wasm.InstrI64Const)
		if !maskOK {
			masked, maskValue = maskValue, masked
			maskID, _, maskOK = definition(maskValue, wasm.InstrI64Const)
		}
		if !maskOK {
			return false
		}
		mask := uint64(f.Insts[maskID].Aux)
		shift := uint64(0)
		switch mask {
		case 0xff:
		case 0xff00:
			shift = 8
		case 0xff0000:
			shift = 16
		case 0xff000000:
			shift = 24
		default:
			return false
		}
		candidate := masked
		if shift != 0 {
			shiftID, shiftOperands, ok := definition(masked, wasm.InstrI64ShrU)
			if !ok || len(shiftOperands) != 2 || uses[masked] != 1 || !constant(shiftOperands[1].Reg, shift) || !addMember(shiftID) {
				return false
			}
			candidate = shiftOperands[0].Reg
		}
		bit := uint8(1 << (shift / 8))
		if seen&bit != 0 || source != 0 && source != candidate {
			return false
		}
		seen |= bit
		source = candidate
		return true
	}
	if !collect(f.Insts[final].Result, true) || memberCount != len(members) || seen != 0x0f || source == 0 {
		return 0, [10]uint32{}, false
	}
	for i := 1; i < len(members); i++ {
		for j := i; j > 0 && position[members[j]] < position[members[j-1]]; j-- {
			members[j], members[j-1] = members[j-1], members[j]
		}
	}
	block := schedule.BlockOf[members[0]]
	for index, id := range members {
		if int(id) >= len(schedule.BlockOf) || schedule.BlockOf[id] != block || index != 0 && position[id] <= position[members[index-1]] {
			return 0, [10]uint32{}, false
		}
	}
	if members[len(members)-1] != final {
		return 0, [10]uint32{}, false
	}
	return source, members, true
}

func arm64CondIncrementable(f *Func, producerID, consumerID uint32, uses []uint32) bool {
	if int(producerID) >= len(f.Insts) || int(consumerID) >= len(f.Insts) {
		return false
	}
	producer, consumer := f.Insts[producerID], f.Insts[consumerID]
	if producer.Result == 0 || int(producer.Result) >= len(uses) || uses[producer.Result] != 1 || consumer.Op != wasm.InstrI32Add {
		return false
	}
	comparable := producer.Op == wasm.InstrI32Eqz || producer.Op == wasm.InstrI64Eqz ||
		producer.Op >= wasm.InstrI32Eq && producer.Op <= wasm.InstrI64GeU ||
		producer.Op >= wasm.InstrF32Eq && producer.Op <= wasm.InstrF64Ge
	if !comparable {
		return false
	}
	operands := f.InstructionOperands(consumerID)
	return len(operands) == 2 && (operands[0].Reg == producer.Result || operands[1].Reg == producer.Result)
}

func amd64FoldableLoadConsumer(f *Func, loadID, consumerID uint32, uses []uint32) bool {
	load, consumer := f.Insts[loadID], f.Insts[consumerID]
	if (load.Op != wasm.InstrI32Load && load.Op != wasm.InstrI64Load && load.Op != wasm.InstrF32Load && load.Op != wasm.InstrF64Load) ||
		load.Result == 0 || int(load.Result) >= len(uses) || uses[load.Result] != 1 {
		return false
	}
	switch consumer.Op {
	case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor:
		if load.Op != wasm.InstrI32Load {
			return false
		}
	case wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor:
		if load.Op != wasm.InstrI64Load {
			return false
		}
	case wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div:
		if load.Op != wasm.InstrF32Load {
			return false
		}
	case wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div:
		if load.Op != wasm.InstrF64Load {
			return false
		}
	default:
		return false
	}
	operands := f.InstructionOperands(consumerID)
	return len(operands) == 2 && operands[1].Reg == load.Result
}

func compareBranchFusionRepairable(target Target, f *Func, producerID, consumerID uint32, uses []uint32) bool {
	if int(producerID) >= len(f.Insts) || int(consumerID) >= len(f.Insts) {
		return false
	}
	producer, consumer := f.Insts[producerID], f.Insts[consumerID]
	if producer.Result == 0 || int(producer.Result) >= len(uses) || uses[producer.Result] != 1 || (consumer.Op != wasm.InstrIf && consumer.Op != wasm.InstrBrIf) {
		return false
	}
	operands := f.InstructionOperands(consumerID)
	if len(operands) != 1 || operands[0].Reg != producer.Result {
		return false
	}
	if producer.Op == wasm.InstrI32Eqz || producer.Op == wasm.InstrI64Eqz {
		return true
	}
	return producer.Op >= wasm.InstrI32Eq && producer.Op <= wasm.InstrI64GeU ||
		target == TargetARM64 && producer.Op >= wasm.InstrF32Eq && producer.Op <= wasm.InstrF64Ge
}

// amd64LEARepairable admits register addition and immediate subtraction. A
// general register subtraction has no LEA form. MinInt32 is excluded because
// negating it cannot be represented by LEA's signed displacement for i64.
func amd64LEARepairable(f *Func, selection *SelectionPlan, instructionID uint32) bool {
	instruction := f.Insts[instructionID]
	switch instruction.Op {
	case wasm.InstrI32Add, wasm.InstrI64Add:
		return true
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
	default:
		return false
	}
	for _, combination := range selection.Combinations {
		if combination.Consumer != instructionID || combination.Kind != CombineImmediate || combination.Producer == ^uint32(0) || int(combination.Producer) >= len(f.Insts) {
			continue
		}
		producer := f.Insts[combination.Producer]
		if (producer.Op == wasm.InstrI32Const || producer.Op == wasm.InstrI64Const) && int32(producer.Aux) != math.MinInt32 {
			return true
		}
	}
	return false
}

func sameMemoryBase(f *Func, first, second uint32) bool {
	left, right := f.InstructionOperands(first), f.InstructionOperands(second)
	return len(left) != 0 && len(right) != 0 && left[0].Reg == right[0].Reg
}

func hasFixedRepairAt(moves []FixedMove, position uint32) bool {
	for _, move := range moves {
		if move.Position == position {
			return true
		}
	}
	return false
}

func isMemoryOp(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Store32
}

func isMemoryBarrier(kind wasm.InstrKind) bool {
	return IsCall(kind) || kind == wasm.InstrMemoryGrow
}

func pairableMemory(a, b Inst) bool {
	if !isMemoryOp(a.Op) || a.Op != b.Op {
		return false
	}
	switch a.Op {
	case wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load:
	default:
		// AArch64 has no byte/halfword LDP, and an STP cannot preserve the
		// first Wasm store when only the second access is out of bounds.
		// Narrow loads and all stores remain eligible for the independently
		// checked post-index chain.
		return false
	}
	left, right := uint64(uint32(a.Aux)), uint64(uint32(b.Aux))
	width := postRAMemoryWidth(a.Op)
	return width != 0 && left+width == right
}

func forwardableStoreLoad(f *Func, storeID, loadID uint32) bool {
	store, load := f.Insts[storeID], f.Insts[loadID]
	matching := store.Op == wasm.InstrI32Store && load.Op == wasm.InstrI32Load || store.Op == wasm.InstrI64Store && load.Op == wasm.InstrI64Load || store.Op == wasm.InstrF32Store && load.Op == wasm.InstrF32Load || store.Op == wasm.InstrF64Store && load.Op == wasm.InstrF64Load
	if !matching || uint32(store.Aux) != uint32(load.Aux) {
		return false
	}
	storeOperands, loadOperands := f.InstructionOperands(storeID), f.InstructionOperands(loadID)
	return len(storeOperands) == 2 && len(loadOperands) == 1 && storeOperands[0].Reg == loadOperands[0].Reg
}

func postRAMemoryWidth(kind wasm.InstrKind) uint64 {
	switch kind {
	case wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI32Store8, wasm.InstrI64Store8:
		return 1
	case wasm.InstrI32Load16S, wasm.InstrI32Load16U, wasm.InstrI64Load16S, wasm.InstrI64Load16U, wasm.InstrI32Store16, wasm.InstrI64Store16:
		return 2
	case wasm.InstrI32Load, wasm.InstrF32Load, wasm.InstrI64Load32S, wasm.InstrI64Load32U, wasm.InstrI32Store, wasm.InstrF32Store, wasm.InstrI64Store32:
		return 4
	case wasm.InstrI64Load, wasm.InstrF64Load, wasm.InstrI64Store, wasm.InstrF64Store:
		return 8
	default:
		return 0
	}
}

func VerifyPostRAPlan(target Target, f *Func, selection *SelectionPlan, schedule *Schedule, plan *PostRAPlan) error {
	return verifyPostRAPlan(target, f, selection, schedule, plan, make([]uint32, len(f.Insts)), make([]uint32, len(f.VRegs)))
}

func verifyPostRAPlanReusingScratch(target Target, f *Func, selection *SelectionPlan, schedule *Schedule, plan *PostRAPlan) error {
	if plan == nil {
		return fmt.Errorf("railmach: malformed post-RA plan")
	}
	position := resize(plan.position, len(f.Insts))
	uses := resize(plan.uses, len(f.VRegs))
	plan.position, plan.uses = position, uses
	return verifyPostRAPlan(target, f, selection, schedule, plan, position, uses)
}

func verifyPostRAPlan(target Target, f *Func, selection *SelectionPlan, schedule *Schedule, plan *PostRAPlan, position, uses []uint32) error {
	if plan == nil || plan.ScanLimit == 0 || plan.ScanLimit > PostRAScanLimit {
		return fmt.Errorf("railmach: malformed post-RA plan")
	}
	for index, instruction := range schedule.Order {
		position[instruction] = uint32(index)
	}
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	for id, rewrite := range plan.Rewrites {
		if rewrite.Kind == RewriteInvalid || int(rewrite.First) >= len(f.Insts) || rewrite.Second != ^uint32(0) && (int(rewrite.Second) >= len(f.Insts) || rewrite.Second <= rewrite.First || rewrite.Second-rewrite.First > PostRAScanLimit && rewrite.Kind != RewriteAMD64FusionRepair && rewrite.Kind != RewritePhysicalRename && rewrite.Kind != RewriteARM64CompareBranch && rewrite.Kind != RewriteARM64RepeatedAdd && rewrite.Kind != RewriteARM64ByteWiden && rewrite.Kind != RewriteARM64ByteSwap && rewrite.Kind != RewriteARM64Narrow16To8) {
			return fmt.Errorf("railmach: invalid post-RA rewrite %d: %#v", id, rewrite)
		}
		if target == TargetAMD64 && (rewrite.Kind == RewriteARM64Pair || rewrite.Kind == RewriteARM64PrePostIndex || rewrite.Kind == RewriteARM64CompareBranch || rewrite.Kind == RewriteARM64CondIncrement || rewrite.Kind == RewriteARM64RepeatedAdd || rewrite.Kind == RewriteARM64ByteWiden || rewrite.Kind == RewriteARM64ByteSwap || rewrite.Kind == RewriteARM64Narrow16To8) || target == TargetARM64 && (rewrite.Kind == RewriteAMD64LEA || rewrite.Kind == RewriteAMD64FusionRepair || rewrite.Kind == RewriteAMD64FixedRepair || rewrite.Kind == RewriteAMD64MemoryFold) {
			return fmt.Errorf("railmach: cross-target post-RA rewrite %d: %#v", id, rewrite)
		}
		if rewrite.Kind == RewriteAMD64FusionRepair && position[rewrite.Second] == position[rewrite.First]+1 {
			if schedule.BlockOf[rewrite.First] != schedule.BlockOf[rewrite.Second] || !compareBranchFusionRepairable(target, f, rewrite.First, rewrite.Second, uses) {
				return fmt.Errorf("railmach: illegal AMD64 flag fusion %d: %#v", id, rewrite)
			}
			matched := false
			for _, combination := range selection.Combinations {
				matched = matched || combination.Kind == CombineCompareBranch && combination.Producer == rewrite.First && combination.Consumer == rewrite.Second
			}
			if !matched {
				return fmt.Errorf("railmach: AMD64 flag fusion %d has no selected pair", id)
			}
		}
		if rewrite.Kind == RewriteARM64CompareBranch {
			if position[rewrite.Second] != position[rewrite.First]+1 || schedule.BlockOf[rewrite.First] != schedule.BlockOf[rewrite.Second] || !compareBranchFusionRepairable(target, f, rewrite.First, rewrite.Second, uses) {
				return fmt.Errorf("railmach: illegal ARM64 compare/branch fusion %d: %#v", id, rewrite)
			}
			matched := false
			for _, combination := range selection.Combinations {
				matched = matched || combination.Kind == CombineCompareBranch && combination.Producer == rewrite.First && combination.Consumer == rewrite.Second
			}
			if !matched {
				return fmt.Errorf("railmach: ARM64 compare/branch fusion %d has no selected pair", id)
			}
		}
		if rewrite.Kind == RewritePhysicalRename {
			if !physicalFlagsRenameable(target, f, schedule, rewrite.First, rewrite.Second, position, uses) {
				return fmt.Errorf("railmach: illegal %s flags physical rename %d: %#v", target, id, rewrite)
			}
			matched := false
			for _, combination := range selection.Combinations {
				matched = matched || combination.Kind == CombineCompareBranch && combination.Producer == rewrite.First && combination.Consumer == rewrite.Second
			}
			if !matched {
				return fmt.Errorf("railmach: %s flags physical rename %d has no selected pair", target, id)
			}
		}
		if rewrite.Kind == RewriteARM64Pair {
			if position[rewrite.Second] != position[rewrite.First]+1 || schedule.BlockOf[rewrite.First] != schedule.BlockOf[rewrite.Second] ||
				!pairableMemory(f.Insts[rewrite.First], f.Insts[rewrite.Second]) || !sameMemoryBase(f, rewrite.First, rewrite.Second) {
				return fmt.Errorf("railmach: illegal ARM64 pair rewrite %d: %#v", id, rewrite)
			}
		}
		if rewrite.Kind == RewriteARM64PrePostIndex {
			preIndex := rewrite.Second == ^uint32(0) && arm64PreIndexable(f.Insts[rewrite.First])
			postIndex := arm64PostIndexScheduled(f, schedule, position, rewrite.First, rewrite.Second)
			if !preIndex && !postIndex {
				if rewrite.Second != ^uint32(0) {
					return fmt.Errorf("railmach: illegal ARM64 pre/post-index rewrite %d: rewrite=%#v first=%#v second=%#v positions=%d/%d blocks=%d/%d same-base=%t chainable=%t", id, rewrite, f.Insts[rewrite.First], f.Insts[rewrite.Second], position[rewrite.First], position[rewrite.Second], schedule.BlockOf[rewrite.First], schedule.BlockOf[rewrite.Second], sameMemoryBase(f, rewrite.First, rewrite.Second), arm64PostIndexChainable(f.Insts[rewrite.First], f.Insts[rewrite.Second]))
				}
				return fmt.Errorf("railmach: illegal ARM64 pre/post-index rewrite %d: rewrite=%#v first=%#v", id, rewrite, f.Insts[rewrite.First])
			}
		}
		if rewrite.Kind == RewriteARM64CondIncrement {
			if position[rewrite.Second] != position[rewrite.First]+1 || schedule.BlockOf[rewrite.First] != schedule.BlockOf[rewrite.Second] || !arm64CondIncrementable(f, rewrite.First, rewrite.Second, uses) {
				return fmt.Errorf("railmach: illegal ARM64 conditional increment %d: %#v", id, rewrite)
			}
		}
		if rewrite.Kind == RewriteARM64RepeatedAdd {
			if _, _, _, ok := VerifyARM64RepeatedAddChain(f, schedule, rewrite.First, rewrite.Second); !ok {
				return fmt.Errorf("railmach: illegal ARM64 repeated add %d: %#v", id, rewrite)
			}
		}
		if rewrite.Kind == RewriteARM64ByteWiden {
			if _, _, ok := VerifyARM64ByteWidenChain(f, schedule, rewrite.First, rewrite.Second); !ok {
				return fmt.Errorf("railmach: illegal ARM64 byte widen %d: %#v", id, rewrite)
			}
		}
		if rewrite.Kind == RewriteARM64ByteSwap {
			if _, members, ok := VerifyARM64ByteSwapChain(f, schedule, rewrite.Second); !ok || members[0] != rewrite.First {
				return fmt.Errorf("railmach: illegal ARM64 byte swap %d: %#v", id, rewrite)
			}
		}
		if rewrite.Kind == RewriteARM64Narrow16To8 {
			if _, members, ok := VerifyARM64Narrow16To8Chain(f, schedule, rewrite.Second); !ok || members[0] != rewrite.First {
				return fmt.Errorf("railmach: illegal ARM64 16-to-8 lane narrowing %d: %#v", id, rewrite)
			}
		}
		if rewrite.Kind == RewriteAMD64MemoryFold {
			if position[rewrite.Second] != position[rewrite.First]+1 || schedule.BlockOf[rewrite.First] != schedule.BlockOf[rewrite.Second] || !amd64FoldableLoadConsumer(f, rewrite.First, rewrite.Second, uses) {
				return fmt.Errorf("railmach: illegal AMD64 memory fold %d: %#v", id, rewrite)
			}
		}
	}
	return nil
}
