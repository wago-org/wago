package railmach

// ScoreAllocatedEmission estimates weighted native resource use after register
// allocation and verified memory folding. Unlike interval-area spill debt, a
// reload is charged at its actual use and a folded load is not charged again as
// a standalone instruction or a spilled intermediate. This is a ranking model,
// not an instruction count: finalizer peepholes and pending-spill forwarding can
// still remove some of the conservatively counted traffic.
func ScoreAllocatedEmission(score ScheduleScore, f *Func, selection *SelectionPlan, schedule *Schedule, allocation *GreedyAllocation, exit *SSAExit, postRA *PostRAPlan) ScheduleScore {
	score = ScorePostRAOpportunities(score, schedule, postRA)
	if f.Target != TargetAMD64 {
		return score
	}
	flags := resize(postRA.scoreFlags, len(f.Insts))
	folded := resize(postRA.scoreFolded, len(f.Insts))
	postRA.scoreFlags, postRA.scoreFolded = flags, folded
	const forwarded = 1
	const skipped = 2
	for _, r := range postRA.Rewrites {
		if r.Kind == RewriteLoadStoreForward {
			flags[r.Second] |= forwarded
		}
	}
	for _, r := range postRA.Rewrites {
		if r.Kind == RewriteAMD64MemoryFold && flags[r.First]&(forwarded|skipped) == 0 && flags[r.Second]&skipped == 0 {
			flags[r.First] |= skipped
			folded[r.Second] = f.Insts[r.First].Result
		}
	}
	weightAt := func(position uint32) uint64 {
		ordinal := position / 6
		if int(ordinal) >= len(schedule.Order) {
			return 1
		}
		return uint64(max(f.Blocks[schedule.BlockOf[schedule.Order[ordinal]]].Weight, 1))
	}
	cost := uint64(0)
	charge := func(units, weight uint64) { cost = saturatingAdd(cost, saturatingMultiply(units, weight)) }
	for id, inst := range f.Insts {
		if flags[id]&skipped != 0 || inst.Result != 0 && f.VRegs[inst.Result].Flags&VRegElided != 0 {
			continue
		}
		position := allocation.InstructionPositions[id]*6 + 2
		weight := weightAt(position)
		charge(uint64(max(selection.Selections[id].Cost.ResourceCost, 1)), weight)
		operands := f.InstructionOperands(uint32(id))
		for index, operand := range operands {
			if operand.Reg == folded[id] {
				continue
			}
			duplicate := false
			for _, previous := range operands[:index] {
				duplicate = duplicate || previous.Reg == operand.Reg
			}
			if duplicate {
				continue
			}
			if operand.Flags&OperandColdRemat != 0 || allocation.LocationAt(operand.Reg, position).Kind != LocationRegister {
				charge(1, weight)
			}
		}
		if inst.Result != 0 && allocation.LocationAt(inst.Result, position).Kind == LocationSpill {
			charge(1, weight)
		}
	}
	for _, fragment := range allocation.Fragments {
		charge(1, weightAt(fragment.Start))
		if fragment.Victim != 0 {
			charge(2, weightAt(fragment.Start))
		}
	}
	for edge, range_ := range exit.EdgeMoves {
		charge(uint64(range_.Count), uint64(max(f.Blocks[f.Edges[edge].From].Weight, 1)))
	}
	for _, range_ := range exit.FixedMoves {
		for _, move := range exit.Moves[range_.Start : range_.Start+range_.Count] {
			charge(1, weightAt(move.Position))
		}
	}
	score.NativeResourceCost = cost
	return score
}
