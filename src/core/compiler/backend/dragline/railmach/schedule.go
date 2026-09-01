package railmach

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railspec"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type DependencyKind uint8

const (
	DependencyData DependencyKind = 1 << iota
	DependencyEffect
	DependencyTrap
	DependencyFixed
	DependencyFusion
)

type Dependency struct {
	Instruction uint32
	Kind        DependencyKind
	_           [3]byte
}

type DependencyDAG struct {
	Offsets      []uint32
	Dependencies []Dependency

	scratch    []Dependency
	verifySeen []uint32
	definition []uint32
	defined    []bool
}

// ResetVerifierScratch detaches reusable verifier state from a shallow DAG
// copy. Immutable offsets and dependencies remain shared, allowing independent
// candidate verification without duplicating the graph.
func (dag *DependencyDAG) ResetVerifierScratch() {
	if dag != nil {
		dag.verifySeen = nil
	}
}

func BuildDependencyDAG(f *Func, selection *SelectionPlan, metadata *railssa.Metadata, reuse *DependencyDAG) (*DependencyDAG, error) {
	if err := Verify(f); err != nil {
		return nil, err
	}
	if selection == nil || len(selection.Selections) != len(f.Insts) || metadata == nil {
		return nil, fmt.Errorf("railmach: dependency DAG requires selection and metadata")
	}
	if reuse == nil {
		reuse = new(DependencyDAG)
	}
	offsets := resize(reuse.Offsets, len(f.Insts)+1)
	dependencies := reuse.Dependencies[:0]
	scratch := reuse.scratch[:0]
	if cap(dependencies) == 0 {
		// Data dependencies are bounded by machine operands; instruction and
		// combination counts cover the usual effect, trap, fixed, and fusion
		// edges. Both slabs are reusable and remain function-bounded if an
		// unusually dense barrier graph needs to grow beyond this first hint.
		capacity := len(f.Operands) + len(f.Insts) + len(selection.Combinations)
		dependencies = make([]Dependency, 0, capacity)
		scratch = make([]Dependency, 0, capacity)
	}
	verifySeen := reuse.verifySeen
	definition := resize(reuse.definition, len(f.VRegs))
	defined := resize(reuse.defined, len(f.VRegs))
	*reuse = DependencyDAG{Offsets: offsets, Dependencies: dependencies, scratch: scratch, verifySeen: verifySeen, definition: definition, defined: defined}
	for instructionID, instruction := range f.Insts {
		for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
			result := instruction.Result + VReg(ordinal)
			reuse.definition[result], reuse.defined[result] = uint32(instructionID), true
		}
	}
	for _, block := range f.Blocks {
		lastTrap, lastCall, lastBarrier := ^uint32(0), ^uint32(0), ^uint32(0)
		var lastHeap [9]uint32
		var hasHeap [9]bool
		var lastFixed [2][256]uint32
		var hasFixed [2][256]bool
		for instructionID := block.InstStart; instructionID < block.InstStart+block.InstCount; instructionID++ {
			reuse.Offsets[instructionID] = uint32(len(reuse.Dependencies))
			dependencyStart := len(reuse.Dependencies)
			instruction := f.Insts[instructionID]
			for _, operand := range f.InstructionOperands(instructionID) {
				if reuse.defined[operand.Reg] && reuse.definition[operand.Reg] < instructionID {
					appendDependency(&reuse.Dependencies, reuse.definition[operand.Reg], DependencyData)
				}
				if operand.Flags&OperandFixed != 0 {
					bank := 0
					if operand.Bank == BankFPR {
						bank = 1
					}
					if hasFixed[bank][operand.Fixed] {
						appendDependency(&reuse.Dependencies, lastFixed[bank][operand.Fixed], DependencyFixed)
					}
					lastFixed[bank][operand.Fixed], hasFixed[bank][operand.Fixed] = instructionID, true
				}
			}
			meta := metadata.Instructions[instruction.Source]
			barrier := meta.Flags&(railssa.EffectMayGrow|railssa.EffectMayAllocate|railssa.EffectMayCollect|railssa.EffectMayReenter|railssa.EffectMayThrow|railssa.EffectMayTrap) != 0
			if (meta.Reads != 0 || meta.Writes != 0 || meta.Flags != 0) && lastBarrier != ^uint32(0) {
				appendUniqueDependency(&reuse.Dependencies, dependencyStart, lastBarrier, DependencyEffect)
			}
			if barrier {
				for heap := range lastHeap {
					if hasHeap[heap] {
						appendUniqueDependency(&reuse.Dependencies, dependencyStart, lastHeap[heap], DependencyEffect)
					}
				}
				if lastCall != ^uint32(0) {
					appendUniqueDependency(&reuse.Dependencies, dependencyStart, lastCall, DependencyEffect)
				}
				for heap := range lastHeap {
					lastHeap[heap], hasHeap[heap] = instructionID, true
				}
				lastBarrier = instructionID
			} else {
				heaps := meta.Reads | meta.Writes
				for heap := range lastHeap {
					bit := railssa.HeapMask(1) << heap
					if heaps&bit == 0 {
						continue
					}
					if hasHeap[heap] {
						appendUniqueDependency(&reuse.Dependencies, dependencyStart, lastHeap[heap], DependencyEffect)
					}
					lastHeap[heap], hasHeap[heap] = instructionID, true
				}
			}
			if meta.Flags&railssa.EffectCall != 0 {
				if lastCall != ^uint32(0) {
					appendUniqueDependency(&reuse.Dependencies, dependencyStart, lastCall, DependencyEffect)
				}
				lastCall = instructionID
			}
			if meta.Traps != 0 {
				if lastTrap != ^uint32(0) {
					appendDependency(&reuse.Dependencies, lastTrap, DependencyTrap)
				}
				lastTrap = instructionID
			}
			for _, combination := range selection.Combinations {
				if combination.Consumer == instructionID && combination.Producer != ^uint32(0) && combination.Producer < instructionID {
					appendDependency(&reuse.Dependencies, combination.Producer, DependencyFusion)
				}
			}
		}
	}
	reuse.Offsets[len(f.Insts)] = uint32(len(reuse.Dependencies))
	// Deduplication compacts each tail in place but cannot resize the aggregate;
	// rebuild once into the reusable slab with exact per-node ranges.
	compacted := reuse.scratch[:0]
	for instructionID := range f.Insts {
		start, end := reuse.Offsets[instructionID], reuse.Offsets[instructionID+1]
		reuse.Offsets[instructionID] = uint32(len(compacted))
		for _, dependency := range reuse.Dependencies[start:end] {
			duplicate := false
			for index := range compacted[reuse.Offsets[instructionID]:] {
				item := &compacted[int(reuse.Offsets[instructionID])+index]
				if item.Instruction == dependency.Instruction {
					item.Kind |= dependency.Kind
					duplicate = true
					break
				}
			}
			if !duplicate {
				compacted = append(compacted, dependency)
			}
		}
	}
	reuse.Offsets[len(f.Insts)] = uint32(len(compacted))
	reuse.Dependencies = append(reuse.Dependencies[:0], compacted...)
	reuse.scratch = compacted[:0]
	if err := verifyDependencyDAGReusingScratch(f, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func appendDependency(out *[]Dependency, instruction uint32, kind DependencyKind) {
	*out = append(*out, Dependency{Instruction: instruction, Kind: kind})
}

func appendUniqueDependency(out *[]Dependency, start int, instruction uint32, kind DependencyKind) {
	for index := start; index < len(*out); index++ {
		if (*out)[index].Instruction == instruction {
			(*out)[index].Kind |= kind
			return
		}
	}
	appendDependency(out, instruction, kind)
}

func VerifyDependencyDAG(f *Func, dag *DependencyDAG) error {
	return verifyDependencyDAG(f, dag, make([]uint32, len(f.Insts)))
}

func verifyDependencyDAGReusingScratch(f *Func, dag *DependencyDAG) error {
	if dag == nil {
		return fmt.Errorf("railmach: malformed dependency DAG")
	}
	seen := resize(dag.verifySeen, len(f.Insts))
	dag.verifySeen = seen
	return verifyDependencyDAG(f, dag, seen)
}

func verifyDependencyDAG(f *Func, dag *DependencyDAG, seen []uint32) error {
	if dag == nil || len(dag.Offsets) != len(f.Insts)+1 || dag.Offsets[0] != 0 || int(dag.Offsets[len(f.Insts)]) != len(dag.Dependencies) {
		return fmt.Errorf("railmach: malformed dependency DAG")
	}
	generation := uint32(0)
	for instruction := range f.Insts {
		if dag.Offsets[instruction] > dag.Offsets[instruction+1] {
			return fmt.Errorf("railmach: dependency offsets regress at %d", instruction)
		}
		generation++
		for _, dependency := range dag.Dependencies[dag.Offsets[instruction]:dag.Offsets[instruction+1]] {
			if dependency.Instruction >= uint32(instruction) || dependency.Kind == 0 || seen[dependency.Instruction] == generation {
				return fmt.Errorf("railmach: instruction %d has invalid dependency %#v", instruction, dependency)
			}
			seen[dependency.Instruction] = generation
		}
	}
	return nil
}

type ScheduleKind uint8

const (
	ScheduleKindSourceStable ScheduleKind = iota + 1
	ScheduleKindLatencyFusion
	ScheduleKindPressure
)

type Schedule struct {
	Kind                ScheduleKind
	Order               []uint32
	BlockRanges         []MoveRange
	Score               uint64
	CommittedSinks      uint32
	CommittedInductions uint32
	CommittedLICM       uint32
	CommittedFusions    uint32
	BlockOf             []railssa.BlockID

	remaining       []bool
	sinkBefore      []uint32
	sinkProducer    []uint32
	lateBefore      []uint32
	lateProducer    []uint32
	fusionBefore    []uint32
	fusionSource    []uint32
	verifyPosition  []uint32
	verifySeen      []bool
	uses            []uint32
	blockCandidates []uint32
	pressureSpecial []uint32
}

func BuildSchedule(f *Func, selection *SelectionPlan, dag *DependencyDAG, kind ScheduleKind, reuse *Schedule) (*Schedule, error) {
	return BuildScheduleWithPressure(f, selection, dag, kind, nil, reuse)
}

// BuildScheduleWithPressure commits verifier-produced cheap-operation sinks in
// the pressure candidate. A sink is delayed until every other dependency of
// its sole consumer is ready, then the pair is emitted adjacently.
func BuildScheduleWithPressure(f *Func, selection *SelectionPlan, dag *DependencyDAG, kind ScheduleKind, pressure *railssa.PressurePlan, reuse *Schedule) (*Schedule, error) {
	if err := verifyDependencyDAGReusingScratch(f, dag); err != nil {
		return nil, err
	}
	if reuse == nil {
		reuse = new(Schedule)
	}
	order := reuse.Order[:0]
	ranges := resize(reuse.BlockRanges, len(f.Blocks))
	remainingScratch := reuse.remaining[:0]
	blockOf := resize(reuse.BlockOf, len(f.Insts))
	for blockID, block := range f.Blocks {
		for instruction := block.InstStart; instruction < block.InstStart+block.InstCount; instruction++ {
			blockOf[instruction] = railssa.BlockID(blockID)
		}
	}
	sinkBefore := resize(reuse.sinkBefore, len(f.Insts))
	sinkProducer := resize(reuse.sinkProducer, len(f.Insts))
	lateBefore := resize(reuse.lateBefore, len(f.Insts))
	lateProducer := resize(reuse.lateProducer, len(f.Insts))
	fusionBefore := resize(reuse.fusionBefore, len(f.Insts))
	fusionSource := resize(reuse.fusionSource, len(f.Insts))
	for index := range sinkBefore {
		sinkBefore[index] = ^uint32(0)
		sinkProducer[index] = ^uint32(0)
		lateBefore[index] = ^uint32(0)
		lateProducer[index] = ^uint32(0)
		fusionBefore[index] = ^uint32(0)
		fusionSource[index] = ^uint32(0)
	}
	uses := resize(reuse.uses, len(f.VRegs))
	reuse.uses = uses
	for instructionID := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	if kind == ScheduleKindPressure && pressure != nil {
		for _, sink := range pressure.Sinks {
			if err := validatePressureSink(f, sink, uses); err != nil {
				if pressureSinkInvalidatedByElision(f, sink, uses) {
					continue
				}
				return nil, err
			}
			previous := sinkProducer[sink.Before]
			if previous != ^uint32(0) && previous > sink.Instruction {
				continue
			}
			if previous != ^uint32(0) {
				sinkBefore[previous] = ^uint32(0)
			}
			sinkBefore[sink.Instruction] = sink.Before
			sinkProducer[sink.Before] = sink.Instruction
		}
		// An instruction cannot be adjacent to both its own producer and its
		// consumer. Prefer the later pair, which shortens the outer live range.
		for consumer, producer := range sinkProducer {
			if producer != ^uint32(0) && sinkBefore[consumer] != ^uint32(0) {
				sinkBefore[producer] = ^uint32(0)
				sinkProducer[consumer] = ^uint32(0)
			}
		}
		for _, induction := range pressure.Inductions {
			instruction, terminator, ok := validateInductionPlacement(f, induction)
			if !ok || lateBefore[instruction] != ^uint32(0) {
				continue
			}
			if previous := lateProducer[terminator]; previous != ^uint32(0) {
				if previous > instruction {
					continue
				}
				lateBefore[previous] = ^uint32(0)
			}
			if producer := sinkProducer[terminator]; producer != ^uint32(0) {
				sinkBefore[producer], sinkProducer[terminator] = ^uint32(0), ^uint32(0)
			}
			sinkBefore[instruction] = ^uint32(0)
			lateBefore[instruction] = terminator
			lateProducer[terminator] = instruction
		}
		for _, move := range pressure.LICM {
			if move.Instruction >= uint32(len(f.Insts)) || int(move.Preheader) >= len(f.Blocks) || int(move.Loop) >= len(f.Blocks) || blockOf[move.Instruction] != move.Loop {
				return nil, fmt.Errorf("railmach: invalid LICM placement %#v", move)
			}
			if target := sinkBefore[move.Instruction]; target != ^uint32(0) {
				sinkProducer[target] = ^uint32(0)
				sinkBefore[move.Instruction] = ^uint32(0)
			}
			if producer := sinkProducer[move.Instruction]; producer != ^uint32(0) {
				sinkBefore[producer] = ^uint32(0)
				sinkProducer[move.Instruction] = ^uint32(0)
			}
			blockOf[move.Instruction] = move.Preheader
		}
	}
	for _, transfer := range f.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range f.Results {
		uses[result]++
	}
	if f.Target == TargetAMD64 || f.Target == TargetARM64 {
		for _, combination := range selection.Combinations {
			producer, consumer := combination.Producer, combination.Consumer
			if combination.Kind != CombineCompareBranch || producer == ^uint32(0) || int(producer) >= len(f.Insts) || int(consumer) >= len(f.Insts) || blockOf[producer] != blockOf[consumer] || !compareBranchFusionRepairable(f.Target, f, producer, consumer, uses) {
				continue
			}
			if lateProducer[consumer] != ^uint32(0) || sinkProducer[consumer] != ^uint32(0) && sinkProducer[consumer] != producer || sinkBefore[producer] != ^uint32(0) || lateBefore[producer] != ^uint32(0) {
				continue
			}
			fusionBefore[producer] = consumer
			fusionSource[consumer] = producer
			if sinkProducer[consumer] == producer {
				sinkBefore[producer] = ^uint32(0)
				sinkProducer[consumer] = ^uint32(0)
			}
		}
	}
	if f.Target == TargetARM64 {
		for firstID, first := range f.Insts {
			if !isMemoryOp(first.Op) {
				continue
			}
			for distance := 1; distance <= PostRAScanLimit && firstID+distance < len(f.Insts); distance++ {
				secondID := firstID + distance
				second := f.Insts[secondID]
				if isMemoryBarrier(second.Op) {
					break
				}
				if !isMemoryOp(second.Op) {
					continue
				}
				firstIndex, secondIndex := uint32(firstID), uint32(secondID)
				if blockOf[firstIndex] == blockOf[secondIndex] && pairableMemory(first, second) && sameMemoryBase(f, firstIndex, secondIndex) &&
					schedulePairDependenciesReady(firstIndex, secondIndex, dag, blockOf) &&
					fusionBefore[firstIndex] == ^uint32(0) && fusionSource[firstIndex] == ^uint32(0) &&
					fusionBefore[secondIndex] == ^uint32(0) && fusionSource[secondIndex] == ^uint32(0) &&
					sinkBefore[firstIndex] == ^uint32(0) && sinkProducer[firstIndex] == ^uint32(0) &&
					lateBefore[firstIndex] == ^uint32(0) && lateProducer[firstIndex] == ^uint32(0) &&
					sinkBefore[secondIndex] == ^uint32(0) && sinkProducer[secondIndex] == ^uint32(0) &&
					lateBefore[secondIndex] == ^uint32(0) && lateProducer[secondIndex] == ^uint32(0) {
					fusionBefore[firstIndex] = secondIndex
					fusionSource[secondIndex] = firstIndex
				}
				break
			}
		}
	}
	committed := uint32(0)
	committedInductions := uint32(0)
	committedLICM := uint32(0)
	committedFusions := uint32(0)
	for _, target := range sinkBefore {
		if target != ^uint32(0) {
			committed++
		}
	}
	for _, target := range lateBefore {
		if target != ^uint32(0) {
			committedInductions++
		}
	}
	for _, target := range fusionBefore {
		if target != ^uint32(0) {
			committedFusions++
		}
	}
	if kind == ScheduleKindPressure && pressure != nil {
		committedLICM = uint32(len(pressure.LICM))
	}
	pressureSpecial := reuse.pressureSpecial[:0]
	if kind == ScheduleKindPressure {
		for instruction := range f.Insts {
			if sinkBefore[instruction] != ^uint32(0) || lateBefore[instruction] != ^uint32(0) {
				pressureSpecial = append(pressureSpecial, uint32(instruction))
			}
		}
	}
	verifyPosition, verifySeen, uses, blockCandidates := reuse.verifyPosition, reuse.verifySeen, reuse.uses, reuse.blockCandidates[:0]
	*reuse = Schedule{Kind: kind, Order: order, BlockRanges: ranges, CommittedSinks: committed, CommittedInductions: committedInductions, CommittedLICM: committedLICM, CommittedFusions: committedFusions, BlockOf: blockOf, remaining: remainingScratch, sinkBefore: sinkBefore, sinkProducer: sinkProducer, lateBefore: lateBefore, lateProducer: lateProducer, fusionBefore: fusionBefore, fusionSource: fusionSource, verifyPosition: verifyPosition, verifySeen: verifySeen, uses: uses, blockCandidates: blockCandidates, pressureSpecial: pressureSpecial}
	for blockID := range f.Blocks {
		start := uint32(len(reuse.Order))
		remaining := resize(reuse.remaining, len(f.Insts))
		candidates := reuse.blockCandidates[:0]
		block := f.Blocks[blockID]
		for candidate := block.InstStart; candidate < block.InstStart+block.InstCount; candidate++ {
			if reuse.BlockOf[candidate] == railssa.BlockID(blockID) {
				candidates = append(candidates, candidate)
				remaining[candidate] = true
			}
		}
		if kind == ScheduleKindPressure && pressure != nil {
			for _, move := range pressure.LICM {
				if move.Preheader == railssa.BlockID(blockID) && (move.Instruction < block.InstStart || move.Instruction >= block.InstStart+block.InstCount) {
					candidates = append(candidates, move.Instruction)
					remaining[move.Instruction] = true
				}
			}
		}
		staticPriority := kind == ScheduleKindLatencyFusion || kind == ScheduleKindPressure
		if staticPriority {
			slices.SortFunc(candidates, func(a, b uint32) int {
				aScore, bScore := schedulePriority(f, selection, a, kind), schedulePriority(f, selection, b, kind)
				if aScore > bScore {
					return -1
				}
				if aScore < bScore {
					return 1
				}
				return int(a) - int(b)
			})
		}
		reuse.blockCandidates = candidates
		blockCount := uint32(len(candidates))
		for emitted := uint32(0); emitted < blockCount; emitted++ {
			pendingCount := blockCount - emitted
			best := ^uint32(0)
			bestScore := int64(-1 << 60)
			if len(reuse.Order) > int(start) {
				previous := reuse.Order[len(reuse.Order)-1]
				target := reuse.fusionBefore[previous]
				if kind == ScheduleKindPressure && target == ^uint32(0) {
					target = reuse.sinkBefore[previous]
					if target == ^uint32(0) {
						target = reuse.lateBefore[previous]
					}
				}
				if target < uint32(len(remaining)) && reuse.BlockOf[target] == railssa.BlockID(blockID) && remaining[target] && scheduleReadyPlaced(railssa.BlockID(blockID), target, dag, remaining, reuse.BlockOf, ^uint32(0)) {
					best = target
				}
			}
			if best == ^uint32(0) {
				// Dynamic pressure bonuses exist only on the bounded sink/late
				// candidate list. Select a positive special candidate first; it
				// outranks every ordinary static-priority candidate.
				if kind == ScheduleKindPressure {
					for _, candidate := range reuse.pressureSpecial {
						if !remaining[candidate] || reuse.BlockOf[candidate] != railssa.BlockID(blockID) || scheduleControlOp(f.Insts[candidate].Op) && pendingCount != 1 {
							continue
						}
						if target := reuse.fusionBefore[candidate]; target != ^uint32(0) && remaining[target] && pendingCount != 2 && scheduleControlOp(f.Insts[target].Op) {
							continue
						}
						if target := reuse.fusionBefore[candidate]; target != ^uint32(0) && remaining[target] && !scheduleControlOp(f.Insts[target].Op) &&
							!scheduleReadyPlaced(railssa.BlockID(blockID), target, dag, remaining, reuse.BlockOf, candidate) {
							continue
						}
						if reuse.lateProducer[candidate] != ^uint32(0) && remaining[reuse.lateProducer[candidate]] || !scheduleReadyPlaced(railssa.BlockID(blockID), candidate, dag, remaining, reuse.BlockOf, ^uint32(0)) {
							continue
						}
						score := schedulePriority(f, selection, candidate, kind)
						if target := reuse.sinkBefore[candidate]; target != ^uint32(0) {
							if scheduleReadyPlaced(railssa.BlockID(blockID), target, dag, remaining, reuse.BlockOf, candidate) {
								score += 1 << 40
							} else {
								score -= 1 << 40
							}
						}
						if target := reuse.lateBefore[candidate]; target != ^uint32(0) {
							if pendingCount == 2 && remaining[target] {
								score += 1 << 39
							} else {
								score -= 1 << 39
							}
						}
						if score > 1<<38 && (best == ^uint32(0) || score > bestScore || score == bestScore && candidate < best) {
							best, bestScore = candidate, score
						}
					}
				}
			}
			if best == ^uint32(0) {
				for _, candidate := range candidates {
					if !remaining[candidate] {
						continue
					}
					if scheduleControlOp(f.Insts[candidate].Op) && pendingCount != 1 {
						continue
					}
					if target := reuse.fusionBefore[candidate]; target != ^uint32(0) && remaining[target] && pendingCount != 2 && scheduleControlOp(f.Insts[target].Op) {
						continue
					}
					if target := reuse.fusionBefore[candidate]; target != ^uint32(0) && remaining[target] && !scheduleControlOp(f.Insts[target].Op) &&
						!scheduleReadyPlaced(railssa.BlockID(blockID), target, dag, remaining, reuse.BlockOf, candidate) {
						continue
					}
					if kind == ScheduleKindPressure && reuse.lateProducer[candidate] != ^uint32(0) && remaining[reuse.lateProducer[candidate]] {
						continue
					}
					if !scheduleReadyPlaced(railssa.BlockID(blockID), candidate, dag, remaining, reuse.BlockOf, ^uint32(0)) {
						continue
					}
					score := schedulePriority(f, selection, candidate, kind)
					if kind == ScheduleKindPressure && reuse.sinkBefore[candidate] != ^uint32(0) {
						if scheduleReadyPlaced(railssa.BlockID(blockID), reuse.sinkBefore[candidate], dag, remaining, reuse.BlockOf, candidate) {
							score += 1 << 40
						} else {
							score -= 1 << 40
						}
					}
					if kind == ScheduleKindPressure && reuse.lateBefore[candidate] != ^uint32(0) {
						if pendingCount == 2 && remaining[reuse.lateBefore[candidate]] {
							score += 1 << 39
						} else {
							score -= 1 << 39
						}
					}
					if best == ^uint32(0) || score > bestScore || score == bestScore && candidate < best {
						best, bestScore = candidate, score
						// Source-stable candidates are source ordered and latency
						// candidates are priority ordered above. The first legal ready
						// instruction is therefore the exact winner. Pressure needs a
						// complete scan only when sink/late placement adds dynamic bonuses.
						ordinaryPressure := kind == ScheduleKindPressure && reuse.sinkBefore[candidate] == ^uint32(0) && reuse.lateBefore[candidate] == ^uint32(0)
						if staticPriority && kind != ScheduleKindPressure || kind == ScheduleKindSourceStable || ordinaryPressure {
							break
						}
					}
				}
			}
			if best == ^uint32(0) {
				return nil, fmt.Errorf("railmach: scheduler found a cycle in block %d", blockID)
			}
			remaining[best] = false
			reuse.Order = append(reuse.Order, best)
			reuse.Score += uint64(max(bestScore, 0))
		}
		reuse.BlockRanges[blockID] = MoveRange{Start: start, Count: uint32(len(reuse.Order)) - start}
		reuse.remaining = remaining[:0]
	}
	dropUncommittedMemoryPairs(f, reuse)
	if err := verifyScheduleReusingScratch(f, dag, reuse); err != nil {
		return nil, err
	}
	if kind == ScheduleKindPressure && pressure != nil {
		if err := verifyCommittedSinks(reuse); err != nil {
			return nil, err
		}
	}
	if err := verifyCommittedFusions(f, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func dropUncommittedMemoryPairs(f *Func, schedule *Schedule) {
	position := resize(schedule.verifyPosition, len(f.Insts))
	schedule.verifyPosition = position
	for index, instruction := range schedule.Order {
		position[instruction] = uint32(index)
	}
	committed := uint32(0)
	for producer, consumer := range schedule.fusionBefore {
		if consumer == ^uint32(0) {
			continue
		}
		if int(consumer) < len(f.Insts) && isMemoryOp(f.Insts[consumer].Op) && position[consumer] != position[producer]+1 {
			schedule.fusionBefore[producer] = ^uint32(0)
			if int(consumer) < len(schedule.fusionSource) && schedule.fusionSource[consumer] == uint32(producer) {
				schedule.fusionSource[consumer] = ^uint32(0)
			}
			continue
		}
		committed++
	}
	schedule.CommittedFusions = committed
}

func verifyCommittedFusions(f *Func, schedule *Schedule) error {
	if schedule.CommittedFusions == 0 {
		return nil
	}
	position := resize(schedule.verifyPosition, len(f.Insts))
	schedule.verifyPosition = position
	for index, instruction := range schedule.Order {
		position[instruction] = uint32(index)
	}
	var committed uint32
	for producer, consumer := range schedule.fusionBefore {
		if consumer == ^uint32(0) {
			continue
		}
		committed++
		if int(consumer) >= len(f.Insts) || schedule.fusionSource[consumer] != uint32(producer) || schedule.BlockOf[producer] != schedule.BlockOf[consumer] || position[consumer] != position[producer]+1 {
			return fmt.Errorf("railmach: committed fusion %d -> %d is not adjacent", producer, consumer)
		}
	}
	if committed != schedule.CommittedFusions {
		return fmt.Errorf("railmach: committed fusion count %d does not match %d", committed, schedule.CommittedFusions)
	}
	return nil
}

func pressureSinkInvalidatedByElision(f *Func, sink railssa.SinkMove, uses []uint32) bool {
	if sink.Instruction >= uint32(len(f.Insts)) {
		return false
	}
	result := f.Insts[sink.Instruction].Result
	if result == 0 || int(result) >= len(uses) {
		return false
	}
	if uses[result] == 0 {
		return true
	}
	if sink.Before < uint32(len(f.Insts)) {
		consumerResult := f.Insts[sink.Before].Result
		if consumerResult != 0 && f.VRegs[consumerResult].Flags&VRegElided != 0 {
			return true
		}
	}
	// Simplification may remove or alias the sole consumer after pressure
	// planning. With no machine use left, the optional sink has no placement
	// obligation and must not turn a valid function into a scheduler error.
	return false
}

func scheduleReadyPlaced(block railssa.BlockID, instruction uint32, dag *DependencyDAG, remaining []bool, blockOf []railssa.BlockID, ignore uint32) bool {
	if int(instruction) >= len(blockOf) || blockOf[instruction] != block {
		return false
	}
	for _, dependency := range dag.Dependencies[dag.Offsets[instruction]:dag.Offsets[instruction+1]] {
		if dependency.Instruction != ignore && blockOf[dependency.Instruction] == block && remaining[dependency.Instruction] {
			return false
		}
	}
	return true
}

func validateInductionPlacement(f *Func, induction railssa.Induction) (instruction, terminator uint32, ok bool) {
	if induction.Value == 0 || int(induction.Value) >= len(f.VRegs) || int(induction.Block) >= len(f.Blocks) {
		return 0, 0, false
	}
	instruction = f.VRegs[induction.Value].Def / 6
	block := f.Blocks[induction.Block]
	if instruction < block.InstStart || instruction >= block.InstStart+block.InstCount || block.InstCount == 0 {
		return 0, 0, false
	}
	terminator = block.InstStart + block.InstCount - 1
	if !scheduleControlOp(f.Insts[terminator].Op) {
		return 0, 0, false
	}
	for candidate := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(candidate)) {
			if operand.Reg == VReg(induction.Value) {
				return 0, 0, false
			}
		}
	}
	for _, transfer := range f.Transfers {
		if transfer.From == induction.Block && transfer.Src == VReg(induction.Value) {
			return instruction, terminator, true
		}
	}
	return 0, 0, false
}

func scheduleControlOp(kind wasm.InstrKind) bool {
	return kind == wasm.InstrIf || kind == wasm.InstrBr || kind == wasm.InstrBrIf || kind == wasm.InstrBrTable || kind == wasm.InstrReturn || kind == wasm.InstrUnreachable
}

func validatePressureSink(f *Func, sink railssa.SinkMove, useCounts []uint32) error {
	if sink.Instruction >= uint32(len(f.Insts)) || sink.Before >= uint32(len(f.Insts)) || sink.Instruction >= sink.Before || int(sink.Block) >= len(f.Blocks) {
		return fmt.Errorf("railmach: invalid pressure sink %#v", sink)
	}
	block := f.Blocks[sink.Block]
	if sink.Instruction < block.InstStart || sink.Before >= block.InstStart+block.InstCount {
		return fmt.Errorf("railmach: pressure sink crosses block %#v", sink)
	}
	result := f.Insts[sink.Instruction].Result
	if result == 0 {
		return fmt.Errorf("railmach: pressure sink has no result %#v", sink)
	}
	uses := uint32(0)
	if int(result) < len(useCounts) {
		uses = useCounts[result]
	}
	if uses != 1 {
		return fmt.Errorf("railmach: pressure sink result has %d uses %#v", uses, sink)
	}
	for _, operand := range f.InstructionOperands(sink.Before) {
		if operand.Reg == result {
			return nil
		}
	}
	return fmt.Errorf("railmach: pressure sink result has a different consumer %#v", sink)
}

func verifyCommittedSinks(schedule *Schedule) error {
	position := resize(schedule.verifyPosition, len(schedule.Order))
	schedule.verifyPosition = position
	for index, instruction := range schedule.Order {
		position[instruction] = uint32(index)
	}
	verified := uint32(0)
	for instruction, target := range schedule.sinkBefore {
		if target == ^uint32(0) {
			continue
		}
		if position[instruction]+1 != position[target] {
			return fmt.Errorf("railmach: pressure sink %d was not committed before %d in %v", instruction, target, schedule.Order)
		}
		verified++
	}
	if verified != schedule.CommittedSinks {
		return fmt.Errorf("railmach: verified %d of %d committed pressure sinks", verified, schedule.CommittedSinks)
	}
	verifiedInductions := uint32(0)
	for instruction, target := range schedule.lateBefore {
		if target == ^uint32(0) {
			continue
		}
		if position[instruction]+1 != position[target] {
			return fmt.Errorf("railmach: induction %d was not placed before terminator %d in %v", instruction, target, schedule.Order)
		}
		verifiedInductions++
	}
	if verifiedInductions != schedule.CommittedInductions {
		return fmt.Errorf("railmach: verified %d of %d committed inductions", verifiedInductions, schedule.CommittedInductions)
	}
	return nil
}

func schedulePriority(f *Func, selection *SelectionPlan, instruction uint32, kind ScheduleKind) int64 {
	switch kind {
	case ScheduleKindSourceStable:
		return -int64(instruction)
	case ScheduleKindLatencyFusion:
		selection := selection.Selections[instruction]
		priority := int64(selection.Cost.Latency)*1024 + int64(selection.Cost.ResourceCost)*32
		if selection.ResultForm == FormFlags {
			priority += 1 << 20
		}
		return priority - int64(instruction)
	case ScheduleKindPressure:
		uses := len(f.InstructionOperands(instruction))
		defines := int(f.Insts[instruction].ResultCount())
		return int64(uses-defines)*1024 - int64(instruction)
	default:
		return -int64(instruction)
	}
}

func schedulePairDependenciesReady(first, second uint32, dag *DependencyDAG, blockOf []railssa.BlockID) bool {
	if dag == nil || int(second)+1 >= len(dag.Offsets) || int(second) >= len(blockOf) {
		return false
	}
	block := blockOf[second]
	for _, dependency := range dag.Dependencies[dag.Offsets[second]:dag.Offsets[second+1]] {
		if dependency.Instruction != first && int(dependency.Instruction) < len(blockOf) && blockOf[dependency.Instruction] == block && dependency.Instruction > first {
			return false
		}
	}
	return true
}

func VerifySchedule(f *Func, dag *DependencyDAG, schedule *Schedule) error {
	return verifySchedule(f, dag, schedule, make([]uint32, len(f.Insts)), make([]bool, len(f.Insts)))
}

func verifyScheduleReusingScratch(f *Func, dag *DependencyDAG, schedule *Schedule) error {
	if schedule == nil {
		return fmt.Errorf("railmach: malformed schedule")
	}
	position := resize(schedule.verifyPosition, len(f.Insts))
	seen := resize(schedule.verifySeen, len(f.Insts))
	schedule.verifyPosition, schedule.verifySeen = position, seen
	return verifySchedule(f, dag, schedule, position, seen)
}

func verifySchedule(f *Func, dag *DependencyDAG, schedule *Schedule, position []uint32, seen []bool) error {
	if schedule == nil || len(schedule.Order) != len(f.Insts) || len(schedule.BlockRanges) != len(f.Blocks) || len(schedule.BlockOf) != 0 && len(schedule.BlockOf) != len(f.Insts) {
		return fmt.Errorf("railmach: malformed schedule")
	}
	expectedStart := uint32(0)
	for blockID, range_ := range schedule.BlockRanges {
		if range_.Start != expectedStart || uint64(range_.Start)+uint64(range_.Count) > uint64(len(schedule.Order)) {
			return fmt.Errorf("railmach: schedule block %d has malformed range %#v", blockID, range_)
		}
		for _, instruction := range schedule.Order[range_.Start : range_.Start+range_.Count] {
			planned := scheduleInstructionBlock(f, schedule, instruction)
			if planned != railssa.BlockID(blockID) {
				return fmt.Errorf("railmach: instruction %d is emitted in block %d, planned %d", instruction, blockID, planned)
			}
		}
		expectedStart += range_.Count
	}
	if expectedStart != uint32(len(schedule.Order)) {
		return fmt.Errorf("railmach: schedule ranges cover %d of %d instructions", expectedStart, len(schedule.Order))
	}
	for index, instruction := range schedule.Order {
		if int(instruction) >= len(f.Insts) || seen[instruction] {
			return fmt.Errorf("railmach: schedule position %d has invalid instruction %d", index, instruction)
		}
		seen[instruction], position[instruction] = true, uint32(index)
	}
	for instruction := range f.Insts {
		for _, dependency := range dag.Dependencies[dag.Offsets[instruction]:dag.Offsets[instruction+1]] {
			if position[dependency.Instruction] >= position[instruction] {
				return fmt.Errorf("railmach: schedule violates %d -> %d", dependency.Instruction, instruction)
			}
		}
	}
	return nil
}

func scheduleInstructionBlock(f *Func, schedule *Schedule, instruction uint32) railssa.BlockID {
	if schedule != nil && len(schedule.BlockOf) == len(f.Insts) {
		return schedule.BlockOf[instruction]
	}
	for blockID, block := range f.Blocks {
		if instruction >= block.InstStart && instruction < block.InstStart+block.InstCount {
			return railssa.BlockID(blockID)
		}
	}
	return ^railssa.BlockID(0)
}

type RetryDecision struct {
	Retry  bool
	Reason uint8
}

const MaxBackendAttempts = 2

func DecideRetry(attempt uint8, allocation *GreedyAllocation, debt CopyDebt) RetryDecision {
	if attempt+1 >= MaxBackendAttempts || allocation == nil {
		return RetryDecision{}
	}
	// The debt is already profile-weighted. More than four retained weighted
	// spill units is enough to justify one bounded alternative; lower values
	// are cheaper than rebuilding all candidates.
	if allocation.Metrics.WeightedDebt > 4 {
		return RetryDecision{Retry: true, Reason: 1}
	}
	if debt.Physical > debt.Coalesced+32 || debt.Cycles > 2 {
		return RetryDecision{Retry: true, Reason: 2}
	}
	return RetryDecision{}
}

func RuleCost(plan *SelectionPlan, instruction uint32) railspec.Rule {
	return railspec.Rules[plan.Selections[instruction].Rule]
}
