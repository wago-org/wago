package dragline

import (
	"fmt"
	"math/bits"
	"sync"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railspec"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
)

// nativeBackendPlan is the complete verifier-gated RailMach product consumed
// by target finalizers. Every pointer remains valid until the planner's next
// call, keeping all candidate storage function-local and reusable.
type nativeBackendPlan struct {
	Stack      *railssa.StackFunc
	CFG        *railssa.CFG
	Semantic   *railssa.SemanticFunc
	Machine    *railmach.Func
	Selection  *railmach.SelectionPlan
	DAG        *railmach.DependencyDAG
	Schedule   *railmach.Schedule
	Allocation *railmach.GreedyAllocation
	Exit       *railmach.SSAExit
	PostRA     *railmach.PostRAPlan
	Specialize *railssa.SpecializationPlan
	Roots      *railssa.RootPlan
	Emission   *railssa.EmissionPlan
	Pressure   *railssa.PressurePlan
	Simplified *railssa.SimplifyResult
	Remat      *railmach.RematPlan
	Layout     *railmach.BlockLayout
	ABI        railmach.ABIContract
	// LocalABI excludes transitive call clobbers. It is retained so a complete
	// recursive SCC can solve its simultaneous caller-mask contract before one
	// bounded refined recompile.
	LocalABI railmach.ABIContract
	Calls    []railmach.CallContract
	Frame    railmach.FrameLayout
	// CalleeSaves are verifier-gated save/restore pairs moved into one bounded
	// explicitly profile-cold, acyclic block chain per physical register.
	CalleeSaves []railmach.CalleeSaveRegion
	// ExternalCallFPRs is the union of allocated FPRs live across imported or
	// indirect calls. Platform callees may clobber these even though local
	// Dragline callees honor the private callee-save contract.
	ExternalCallFPRs uint64
	// ExternalCallVectorFPRs is the full-width subset of ExternalCallFPRs.
	ExternalCallVectorFPRs uint64
	// HelperSafepointBase is the module-global ID assigned to the first
	// allocating runtime-helper instruction in this function.
	HelperSafepointBase uint32
	// CallArgumentBytes is the fixed caller-owned canonical argument/result
	// vector prefix. External-call FPR saves follow it in Frame.CallAreaBytes.
	CallArgumentBytes          uint32
	Score                      railmach.ScheduleScore
	BackendAttempts            uint8
	ScheduleCandidates         uint8
	ScheduleForced             bool
	TargetSelectedInstructions uint32
	GenericMachineInstructions uint32
	// InitialScheduleScores retain the realized post-allocation debt for the
	// bounded first-pass candidates. Metrics-enabled compilation also attaches
	// their first-pass post-RA opportunities. Retry scores are deliberately
	// excluded so this remains a comparable view of scheduling rather than
	// allocator-policy changes.
	InitialScheduleScores     [3]railmach.ScheduleScore
	InitialScheduleScoreCount uint8
	InitialCandidateFrontier  uint8
	RetryScheduleScores       [3]railmach.ScheduleScore
	RetryScheduleScoreCount   uint8
	RetryCandidateFrontier    uint8
	// Segmented baseline/candidate fields retain exact spill and copy debt on
	// both sides of the one bounded segmented-liveness trial. The trial is
	// deliberately separate from schedule search so observability does not
	// multiply allocator work.
	SegmentedBaselineDebt    uint64
	SegmentedCandidateDebt   uint64
	SegmentedBaselineCopies  uint32
	SegmentedCandidateCopies uint32
	SegmentedCandidateRanges uint32
	SegmentedAttempted       bool
	SegmentedAdmitted        bool
	IPRARefinedCalls         uint32
	SignalsBounds            bool
	AMD64BMI2                bool
	// AMD64MemoryBoundEnd is the access end offset subtracted from the stable
	// memory-0 byte length cached in the final allocatable GPR. Zero disables
	// the cache.
	AMD64MemoryBoundEnd uint64
	// AMD64StackCachedGlobals keep call-crossing scalar globals in frame homes
	// when their weighted reads repay the call refreshes. The backing globals
	// remain write-through, so calls and traps always observe current state.
	AMD64StackCachedGlobals      [2]uint32
	AMD64StackCachedGlobalOffset uint32
	AMD64StackCachedGlobalCount  uint8
	// AMD64DivisionSaveOffset names three frame homes used to preserve unrelated
	// values allocated in RAX and RDX across x86's implicit integer-division
	// clobbers and to stage the divisor across fixed-register repair.
	// AMD64DivisionSave is false when the function has no division.
	AMD64DivisionSaveOffset uint32
	AMD64DivisionSave       bool
	// AMD64ImmediateRemainders admits the longer multiply/shift/multiply/sub
	// lowering only when repeated uses repay its extra instruction footprint.
	AMD64ImmediateRemainders       bool
	AMD64SignedImmediateRemainders bool
	// AMD64WideVectorScratch permits non-Windows finalizers to use XMM15 for
	// single-source shuffle masks and, when no low-XMM semantic scratch remains,
	// allocate the full XMM0-XMM11 register set.
	AMD64WideVectorScratch bool
	// AMD64ShuffledFPRs selects the SysV vector register order that keeps the
	// fixed XMM3-XMM5 SIMD scratch bank outside the allocatable prefix.
	AMD64ShuffledFPRs bool
	// AMD64AddressRematerialize marks spilled wrapping affine addresses whose
	// every memory use can reconstruct the value from an already-live register.
	AMD64AddressRematerialize nativeBitSet

	BlockOffsets        []int
	BranchPatches       []nativeBranchPatch
	ConditionalPatches  []nativeBranchPatch
	ColdTrapPatches     []nativeBranchPatch
	MemoryCheckSlots    nativeDenseRelation
	MemoryCheckEnds     []uint64
	MemoryCheckTouched  []uint32
	PostRAPairWith      nativeInstructionRelation
	PostRASkip          nativeBitSet
	PostRAForwardFrom   nativeInstructionRelation
	PostRAFusionWith    nativeInstructionRelation
	PostRAMemoryFrom    nativeInstructionRelation
	PostRARepeatFirst   nativeInstructionRelation
	PostRAPreIndex      nativeBitSet
	PostRAPostIndexWith nativeInstructionRelation
	AMD64DeadStoreSkip  nativeBitSet
	AMD64DeadStoreFrom  nativeInstructionRelation

	AMD64GlobalUpdateSkip  nativeBitSet
	AMD64GlobalUpdateAdd   nativeBitSet
	AMD64GlobalUpdateDelta []uint32

	// PostRADirect enables verifier-gated rewrites whose realization needs no
	// instruction-indexed side table. The PostRA plan remains the sparse source
	// of instruction identity.
	PostRADirect       bool
	ImmediateProducer  nativeInstructionRelation
	ImmediateSkip      nativeBitSet
	DeadGCReservations []bool
	NoBarrierGCStores  []bool
}

type nativeBranchPatch struct {
	At     int
	Target uint32
	// Base is nonzero for a raw relative jump-table entry. Ordinary near
	// branches retain zero and use the architectural end-of-displacement base.
	Base int
	Code uint8
}

// nativeDenseRelation stores one optional uint32 identity per dense key.
// Ordinary functions use 16-bit identities; functions too large for the
// zero-reserving encoding retain the full 32-bit representation.
type nativeDenseRelation struct {
	narrow []uint16
	wide   []uint32
}

type nativeBitSet struct {
	words []uint64
}

func (s *nativeBitSet) prepare(bits int, needed bool) {
	if !needed {
		s.words = s.words[:0]
		return
	}
	s.words = resizeNativeSlice(s.words, (bits+63)/64)
	clear(s.words)
}

func (s *nativeBitSet) set(bit uint32, value bool) {
	word, mask := bit>>6, uint64(1)<<(bit&63)
	if value {
		s.words[word] |= mask
	} else {
		s.words[word] &^= mask
	}
}

func (s nativeBitSet) has(bit uint32) bool {
	word := bit >> 6
	return int(word) < len(s.words) && s.words[word]&(uint64(1)<<(bit&63)) != 0
}

func (s nativeBitSet) prepared(bits int) bool {
	return len(s.words) == (bits+63)/64
}

func (s nativeBitSet) capacityBytes() uint64 {
	return sliceBytes(s.words)
}

// Most users relate machine instructions. Retain the specific alias so plan
// fields document that contract while bounds-cache slots can key by VReg.
type nativeInstructionRelation = nativeDenseRelation

func (r *nativeDenseRelation) prepare(keys int, needed bool) {
	if !needed {
		r.narrow = r.narrow[:0]
		r.wide = r.wide[:0]
		return
	}
	if keys <= int(^uint16(0)) {
		r.wide = nil
		r.narrow = resizeNativeSlice(r.narrow, keys)
		clear(r.narrow)
		return
	}
	r.narrow = nil
	r.wide = resizeNativeSlice(r.wide, keys)
	clear(r.wide)
}

func (r *nativeDenseRelation) set(key, related uint32) {
	if len(r.narrow) != 0 {
		r.narrow[key] = uint16(related + 1)
		return
	}
	r.wide[key] = related + 1
}

func (r nativeDenseRelation) get(key uint32) (uint32, bool) {
	var encoded uint32
	if int(key) < len(r.narrow) {
		encoded = uint32(r.narrow[key])
	} else if int(key) < len(r.wide) {
		encoded = r.wide[key]
	}
	return encoded - 1, encoded != 0
}

func (r nativeDenseRelation) has(key uint32) bool {
	_, ok := r.get(key)
	return ok
}

func (r nativeDenseRelation) capacityBytes() uint64 {
	return sliceBytes(r.narrow) + sliceBytes(r.wide)
}

func clearPostRAEmissionRewrites(plan *nativeBackendPlan) {
	plan.PostRAPairWith = nativeInstructionRelation{}
	plan.PostRASkip = nativeBitSet{}
	plan.PostRAForwardFrom = nativeInstructionRelation{}
	plan.PostRAFusionWith = nativeInstructionRelation{}
	plan.PostRAMemoryFrom = nativeInstructionRelation{}
	plan.PostRARepeatFirst = nativeInstructionRelation{}
	plan.PostRAPreIndex = nativeBitSet{}
	plan.PostRAPostIndexWith = nativeInstructionRelation{}
	plan.AMD64DeadStoreSkip = nativeBitSet{}
	plan.AMD64DeadStoreFrom = nativeInstructionRelation{}
	plan.AMD64GlobalUpdateSkip = nativeBitSet{}
	plan.AMD64GlobalUpdateAdd = nativeBitSet{}
	plan.AMD64GlobalUpdateDelta = nil
	plan.PostRADirect = false
}

// nativeBackendPlanner owns all temporary storage for one production
// RailSSA-to-RailMach compilation. Candidate schedules are allocated and
// scored sequentially; only the winning candidate is rebuilt.
type nativeBackendPlanner struct {
	cfg                 railssa.CFG
	locals              railssa.LocalSSA
	flow                railssa.ValueFlow
	semantic            railssa.SemanticFunc
	metadata            railssa.Metadata
	simplified          railssa.SimplifyResult
	machine             railmach.Func
	selection           railmach.SelectionPlan
	dag                 railmach.DependencyDAG
	schedule            railmach.Schedule
	allocation          railmach.GreedyAllocation
	exit                railmach.SSAExit
	postRA              railmach.PostRAPlan
	specialize          railssa.SpecializationPlan
	rootPlan            railssa.RootPlan
	gcValues            []railssa.GCValueFact
	emission            railssa.EmissionPlan
	pressure            railssa.PressurePlan
	remat               railmach.RematPlan
	layout              railmach.BlockLayout
	edgeWeights         []uint64
	edgeObserved        []bool
	blockBytes          []uint32
	coldBlocks          []bool
	calleeSaveRegions   []railmach.CalleeSaveRegion
	blockOffsets        []int
	branchPatches       []nativeBranchPatch
	conditionalPatches  []nativeBranchPatch
	coldTrapPatches     []nativeBranchPatch
	memoryCheckSlots    nativeDenseRelation
	memoryCheckEnds     []uint64
	memoryCheckTouched  []uint32
	postRAPairWith      nativeInstructionRelation
	postRASkip          nativeBitSet
	postRAForwardFrom   nativeInstructionRelation
	postRAFusionWith    nativeInstructionRelation
	postRAMemoryFrom    nativeInstructionRelation
	postRARepeatFirst   nativeInstructionRelation
	postRAPreIndex      nativeBitSet
	postRAPostIndexWith nativeInstructionRelation
	amd64DeadStoreSkip  nativeBitSet
	amd64DeadStoreFrom  nativeInstructionRelation

	amd64GlobalUpdateSkip  nativeBitSet
	amd64GlobalUpdateAdd   nativeBitSet
	amd64GlobalUpdateDelta []uint32

	immediateProducer   nativeInstructionRelation
	immediateSkip       nativeBitSet
	immediateUses       []uint32
	amd64AddressRemat   nativeBitSet
	amd64AddressState   []uint32
	deadGCReservations  []bool
	noBarrierGCStores   []bool
	amd64MemoryBounds   []nativeAMD64MemoryBoundUse
	parallelCandidates  bool
	candidatePostRA     bool
	forcedSchedule      railmach.ScheduleKind
	signalsBounds       bool
	candidateScratch    *[2]nativeCandidateWorkspace
	plan                nativeBackendPlan
	peakRailSSA         railssa.PipelineCapacityBreakdown
	peakSSABytes        uint64
	peakMachineBytes    uint64
	peakNativeBytes     uint64
	peakNativeBreakdown NativePlannerCapacityBreakdown
	peakCapacityBytes   uint64
	exceptionalFunction bool
}

type nativeCandidateWorkspace struct {
	schedule   railmach.Schedule
	allocation railmach.GreedyAllocation
	exit       railmach.SSAExit
	postRA     railmach.PostRAPlan
}

type nativeAMD64MemoryBoundUse struct {
	end    uint64
	weight uint64
}

// nativeBackendPlannerRetentionBytes bounds reusable compiler workspace after
// an exceptional function. Ordinary functions keep their planner storage and
// avoid rebuilding it; giant functions release their slabs once their plan has
// been fully consumed so later native-code growth does not overlap the
// exceptional high-water mark.
const nativeBackendPlannerRetentionBytes = 16 << 20

type nativeCandidateRef struct {
	schedule   *railmach.Schedule
	allocation *railmach.GreedyAllocation
	exit       *railmach.SSAExit
	postRA     *railmach.PostRAPlan
}

func (p *nativeBackendPlanner) evaluateScheduleCandidates(machine *railmach.Func, selection *railmach.SelectionPlan, dag *railmach.DependencyDAG, pressure *railssa.PressurePlan, greedy railmach.GreedyConfig, kinds [3]railmach.ScheduleKind, parallel bool) ([3]railmach.ScheduleScore, [3]error) {
	if p.candidateScratch == nil {
		p.candidateScratch = new([2]nativeCandidateWorkspace)
	}
	refs := [3]nativeCandidateRef{
		{schedule: &p.schedule, allocation: &p.allocation, exit: &p.exit, postRA: &p.postRA},
		{schedule: &p.candidateScratch[0].schedule, allocation: &p.candidateScratch[0].allocation, exit: &p.candidateScratch[0].exit, postRA: &p.candidateScratch[0].postRA},
		{schedule: &p.candidateScratch[1].schedule, allocation: &p.candidateScratch[1].allocation, exit: &p.candidateScratch[1].exit, postRA: &p.candidateScratch[1].postRA},
	}
	var scores [3]railmach.ScheduleScore
	var errs [3]error
	evaluate := func(index int) {
		ref := refs[index]
		candidateDAG := *dag
		candidateDAG.ResetVerifierScratch()
		candidate, err := railmach.BuildScheduleWithPressure(machine, selection, &candidateDAG, kinds[index], pressure, ref.schedule)
		if err != nil {
			errs[index] = err
			return
		}
		allocation, err := railmach.AllocateGreedyPForSchedule(machine, candidate, greedy, ref.allocation)
		if err != nil {
			errs[index] = err
			return
		}
		exit, err := railmach.LateSSAExitVerifiedAllocation(machine, &allocation.Allocation, ref.exit)
		if err != nil {
			errs[index] = err
			return
		}
		scores[index], errs[index] = railmach.ScoreVerifiedScheduleCandidate(machine, selection, dag, candidate, allocation, exit)
		if errs[index] == nil && p.candidatePostRA {
			postRA, err := railmach.PlanPostRAVerifiedAllocation(machine.Target, machine, selection, candidate, allocation, exit, ref.postRA)
			if err != nil {
				errs[index] = err
				return
			}
			scores[index] = railmach.ScorePostRAOpportunities(scores[index], candidate, postRA)
		}
	}
	if parallel {
		var wait sync.WaitGroup
		wait.Add(len(refs))
		for index := range refs {
			go func(index int) {
				defer wait.Done()
				evaluate(index)
			}(index)
		}
		wait.Wait()
	} else {
		for index := range refs {
			refs[index] = refs[0]
			evaluate(index)
		}
	}
	return scores, errs
}

func (p *nativeBackendPlanner) retainScheduleCandidate(index int) {
	if index == 0 {
		return
	}
	workspace := &p.candidateScratch[index-1]
	p.schedule, workspace.schedule = workspace.schedule, p.schedule
	p.allocation, workspace.allocation = workspace.allocation, p.allocation
	p.exit, workspace.exit = workspace.exit, p.exit
}

// CapacityBytes reports all retained native-planner backing storage. Returned
// plan slices alias these owners and are therefore not counted a second time;
// ABI call contracts are the one plan-owned slab.
func (p *nativeBackendPlanner) CapacityBytes() uint64 {
	ssa, machine, native := p.capacityBreakdown()
	return ssa + machine + native
}

func retainNativeBackendPlannerWithin(p *nativeBackendPlanner, limit uint64) *nativeBackendPlanner {
	if p != nil && p.CapacityBytes() > limit {
		return nil
	}
	return p
}

func (p *nativeBackendPlanner) resetCapacityPeak() {
	p.peakRailSSA = railssa.PipelineCapacityBreakdown{}
	p.peakSSABytes = 0
	p.peakMachineBytes = 0
	p.peakNativeBytes = 0
	p.peakNativeBreakdown = NativePlannerCapacityBreakdown{}
	p.peakCapacityBytes = 0
	p.exceptionalFunction = false
}

func (p *nativeBackendPlanner) observeCapacity() uint64 {
	ssa, machine, native := p.capacityBreakdown()
	total := ssa + machine + native
	if total > p.peakCapacityBytes {
		p.peakRailSSA = railssa.MeasurePipelineCapacity(&p.cfg, &p.locals, &p.flow, &p.semantic, &p.metadata, &p.simplified, &p.pressure, &p.specialize, &p.emission)
		p.peakSSABytes = ssa
		p.peakMachineBytes = machine
		p.peakNativeBytes = native
		p.peakNativeBreakdown = p.nativeCapacityBreakdown()
		p.peakCapacityBytes = total
	}
	return total
}

func (p *nativeBackendPlanner) releaseLocalSSAScratchAbove(limit uint64) bool {
	if p == nil || p.observeCapacity() <= limit {
		return false
	}
	p.locals = railssa.LocalSSA{}
	p.exceptionalFunction = true
	return true
}

func (p *nativeBackendPlanner) releaseValueFlowScratch() {
	if p == nil || !p.exceptionalFunction {
		return
	}
	p.observeCapacity()
	p.flow = railssa.ValueFlow{}
}

// releasePlanningScratchAbove drops exceptional-function storage whose products
// have already been copied into the semantic and machine plans consumed by the
// finalizers. Ordinary planners retain these slabs for the next function. The
// selected program, verifier state, CFG, simplification facts, roots, ABI, and
// emission metadata remain owned by p until finalization completes.
func (p *nativeBackendPlanner) releasePlanningScratchAbove(limit uint64) bool {
	if p == nil || !p.exceptionalFunction && p.CapacityBytes() <= limit {
		return false
	}
	p.observeCapacity()
	p.locals = railssa.LocalSSA{}
	p.flow = railssa.ValueFlow{}
	p.metadata = railssa.Metadata{}
	p.pressure = railssa.PressurePlan{}
	p.plan.Pressure = nil
	p.candidateScratch = nil
	p.edgeWeights = nil
	p.edgeObserved = nil
	p.blockBytes = nil
	p.coldBlocks = nil
	p.immediateUses = nil
	p.amd64AddressState = nil
	p.gcValues = nil
	p.amd64MemoryBounds = nil
	return true
}

func (p *nativeBackendPlanner) capacityBreakdown() (ssa, machine, native uint64) {
	if p == nil {
		return 0, 0, 0
	}
	ssa = railssa.PipelineCapacityBytes(&p.cfg, &p.locals, &p.flow, &p.semantic, &p.metadata, &p.simplified, &p.pressure, &p.specialize, &p.emission)
	machine = railmach.PipelineCapacityBytes(&p.machine, &p.selection, &p.dag, &p.schedule, &p.allocation, &p.exit, &p.postRA, &p.remat, &p.layout)
	if p.candidateScratch != nil {
		for index := range p.candidateScratch {
			candidate := &p.candidateScratch[index]
			machine += railmach.PipelineCapacityBytes(nil, nil, nil, &candidate.schedule, &candidate.allocation, &candidate.exit, &candidate.postRA, nil, nil)
		}
	}
	native = p.nativeCapacityBreakdown().Total()
	return ssa, machine, native
}

func (p *nativeBackendPlanner) nativeCapacityBreakdown() NativePlannerCapacityBreakdown {
	if p == nil {
		return NativePlannerCapacityBreakdown{}
	}
	return NativePlannerCapacityBreakdown{
		ControlFlow: sliceBytes(p.edgeWeights) + sliceBytes(p.edgeObserved) + sliceBytes(p.blockBytes) + sliceBytes(p.coldBlocks) + sliceBytes(p.calleeSaveRegions) + sliceBytes(p.blockOffsets) + sliceBytes(p.branchPatches) + sliceBytes(p.conditionalPatches) + sliceBytes(p.coldTrapPatches),
		Bounds:      p.memoryCheckSlots.capacityBytes() + sliceBytes(p.memoryCheckEnds) + sliceBytes(p.memoryCheckTouched) + sliceBytes(p.amd64MemoryBounds),
		PostRA:      p.postRAPairWith.capacityBytes() + p.postRASkip.capacityBytes() + p.postRAForwardFrom.capacityBytes() + p.postRAFusionWith.capacityBytes() + p.postRAMemoryFrom.capacityBytes() + p.postRARepeatFirst.capacityBytes() + p.postRAPreIndex.capacityBytes() + p.postRAPostIndexWith.capacityBytes() + p.amd64DeadStoreSkip.capacityBytes() + p.amd64DeadStoreFrom.capacityBytes() + p.amd64GlobalUpdateSkip.capacityBytes() + p.amd64GlobalUpdateAdd.capacityBytes() + sliceBytes(p.amd64GlobalUpdateDelta),
		Immediates:  p.immediateProducer.capacityBytes() + p.immediateSkip.capacityBytes() + p.amd64AddressRemat.capacityBytes() + sliceBytes(p.immediateUses) + sliceBytes(p.amd64AddressState),
		GC:          sliceBytes(p.deadGCReservations) + sliceBytes(p.noBarrierGCStores) + sliceBytes(p.gcValues),
		CallsRoots:  sliceBytes(p.plan.Calls) + sliceBytes(p.rootPlan.Sites) + sliceBytes(p.rootPlan.Roots),
	}
}

func railMachCandidate(stack *railssa.StackFunc, moduleHasV128 bool) bool {
	if stack == nil {
		return false
	}
	if stackHasV128Value(stack) || stackHasSIMDInstruction(stack) {
		return railMachV128FoundationCandidate(stack)
	}
	if nativeGiantTrappingConversion(stack) {
		return false
	}
	if stack.HasReferences {
		return true
	}
	for _, region := range stack.Regions {
		// The compact structured emitter models only the MVP empty/single-result
		// control signature. RailMach's block arguments carry full type-indexed
		// parameter and result vectors.
		if region.ParamArity != 0 || region.ResultArity > 1 {
			return true
		}
	}
	for _, instruction := range stack.Instrs {
		switch instruction.Kind {
		case wasm.InstrInvalid, wasm.InstrNop, wasm.InstrDrop, wasm.InstrReturn,
			wasm.InstrUnreachable, wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
			wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable,
			wasm.InstrLocalGet, wasm.InstrLocalSet, wasm.InstrLocalTee,
			wasm.InstrI32Const, wasm.InstrI64Const,
			wasm.InstrI32Eqz, wasm.InstrI64Eqz,
			wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt,
			wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt,
			wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32Mul, wasm.InstrI64Mul,
			wasm.InstrI32DivS, wasm.InstrI32DivU, wasm.InstrI32RemS, wasm.InstrI32RemU,
			wasm.InstrI64DivS, wasm.InstrI64DivU, wasm.InstrI64RemS, wasm.InstrI64RemU,
			wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or,
			wasm.InstrI32Xor, wasm.InstrI64Xor,
			wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS,
			wasm.InstrI32ShrU, wasm.InstrI64ShrU, wasm.InstrI32Rotl, wasm.InstrI64Rotl,
			wasm.InstrI32Rotr, wasm.InstrI64Rotr,
			wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
			wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
			wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
			wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
			wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU:
		case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U,
			wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
			wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		case wasm.InstrF32Const, wasm.InstrF64Const,
			wasm.InstrF32Eq, wasm.InstrF64Eq, wasm.InstrF32Ne, wasm.InstrF64Ne,
			wasm.InstrF32Lt, wasm.InstrF64Lt, wasm.InstrF32Gt, wasm.InstrF64Gt,
			wasm.InstrF32Le, wasm.InstrF64Le, wasm.InstrF32Ge, wasm.InstrF64Ge,
			wasm.InstrF32Abs, wasm.InstrF64Abs, wasm.InstrF32Neg, wasm.InstrF64Neg,
			wasm.InstrF32Ceil, wasm.InstrF64Ceil, wasm.InstrF32Floor, wasm.InstrF64Floor,
			wasm.InstrF32Trunc, wasm.InstrF64Trunc, wasm.InstrF32Nearest, wasm.InstrF64Nearest,
			wasm.InstrF32Sqrt, wasm.InstrF64Sqrt,
			wasm.InstrF32Add, wasm.InstrF64Add, wasm.InstrF32Sub, wasm.InstrF64Sub,
			wasm.InstrF32Mul, wasm.InstrF64Mul, wasm.InstrF32Div, wasm.InstrF64Div,
			wasm.InstrF32Min, wasm.InstrF64Min, wasm.InstrF32Max, wasm.InstrF64Max,
			wasm.InstrF32Copysign, wasm.InstrF64Copysign:
		case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
			wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U, wasm.InstrF32DemoteF64,
			wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U,
			wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U, wasm.InstrF64PromoteF32,
			wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64,
			wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64:
		case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
			wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
			wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
			wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U:
		case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
			wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
			wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U,
			wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		case wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
			wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
			wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U,
			wasm.InstrI64Load32S, wasm.InstrI64Load32U,
			wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
			wasm.InstrI32Store8, wasm.InstrI32Store16,
			wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32:
		case wasm.InstrMemorySize, wasm.InstrMemoryGrow, wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
		case wasm.InstrSelect:
		case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
		case wasm.InstrCall:
			if instruction.Inline() != wasm.InstrInvalid {
				return false
			}
		case wasm.InstrCallIndirect:
			if instruction.Inline() != wasm.InstrInvalid {
				return false
			}
		case wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
		default:
			return false
		}
	}
	return true
}

const nativeGiantStructuredInstructions = 4096

// nativeGiantTrappingConversion retains the compact emitter for exceptionally
// large scalar functions whose exact trapping conversions make the optimizing
// pipeline materially more expensive. The shared Machine-SSA path remains the
// default below this measured compile-cost boundary and for vector functions.
func nativeGiantTrappingConversion(stack *railssa.StackFunc) bool {
	if stack == nil || len(stack.Instrs) < nativeGiantStructuredInstructions {
		return false
	}
	for _, instruction := range stack.Instrs {
		if railMachTrappingTrunc(instruction.Kind) {
			return true
		}
	}
	return false
}

// railMachRejectionReason reports the first source-level capability boundary
// that keeps a function on structured emission. It deliberately derives
// scalar opcode support by probing railMachCandidate itself so diagnostics
// cannot drift from the production admission policy.
func railMachRejectionReason(stack *railssa.StackFunc, moduleHasV128 bool) string {
	if stack == nil {
		return "missing-stack"
	}
	if railMachCandidate(stack, moduleHasV128) {
		return ""
	}
	if stackHasV128Value(stack) || stackHasSIMDInstruction(stack) {
		for _, instruction := range stack.Instrs {
			if !wasm.IsSIMDValidationInstructionKind(instruction.Kind) {
				continue
			}
			probe := *stack
			probe.Instrs = []railssa.StackInstr{instruction}
			if !railMachV128FoundationCandidate(&probe) {
				return "unsupported-op:" + instruction.Kind.String()
			}
		}
		return "unsupported-vector-flow"
	}
	if nativeGiantTrappingConversion(stack) {
		return "fast-structured:giant-trapping-conversion"
	}
	for _, instruction := range stack.Instrs {
		probe := &railssa.StackFunc{Instrs: []railssa.StackInstr{instruction}}
		if !railMachCandidate(probe, false) {
			return "unsupported-op:" + instruction.Kind.String()
		}
	}
	return "shared-capability"
}

// railMachV128FoundationCandidate admits vector operations whose machine
// lowering is complete. Vector parameters, calls, results, globals, locals,
// and block values use the typed allocation and transfer machinery.
func railMachV128FoundationCandidate(stack *railssa.StackFunc) bool {
	if stack == nil {
		return false
	}
	hasVectorOperation := false
	for _, instruction := range stack.Instrs {
		if !wasm.IsSIMDValidationInstructionKind(instruction.Kind) {
			continue
		}
		hasVectorOperation = true
		switch instruction.Kind {
		case wasm.InstrV128Const, wasm.InstrV128Load, wasm.InstrV128Store,
			wasm.InstrV128Load8x8S, wasm.InstrV128Load8x8U, wasm.InstrV128Load16x4S, wasm.InstrV128Load16x4U,
			wasm.InstrV128Load32x2S, wasm.InstrV128Load32x2U,
			wasm.InstrV128Load8Splat, wasm.InstrV128Load16Splat, wasm.InstrV128Load32Splat, wasm.InstrV128Load64Splat,
			wasm.InstrV128Load32Zero, wasm.InstrV128Load64Zero,
			wasm.InstrV128Load8Lane, wasm.InstrV128Load16Lane, wasm.InstrV128Load32Lane, wasm.InstrV128Load64Lane,
			wasm.InstrV128Store8Lane, wasm.InstrV128Store16Lane, wasm.InstrV128Store32Lane, wasm.InstrV128Store64Lane,
			wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor, wasm.InstrV128Not, wasm.InstrV128Bitselect,
			wasm.InstrI8x16Add, wasm.InstrI8x16AddSatS, wasm.InstrI8x16AddSatU,
			wasm.InstrI8x16Sub, wasm.InstrI8x16SubSatS, wasm.InstrI8x16SubSatU,
			wasm.InstrI16x8Add, wasm.InstrI16x8AddSatS, wasm.InstrI16x8AddSatU,
			wasm.InstrI16x8Sub, wasm.InstrI16x8SubSatS, wasm.InstrI16x8SubSatU,
			wasm.InstrI32x4Add, wasm.InstrI32x4Sub,
			wasm.InstrI64x2Add, wasm.InstrI64x2Sub,
			wasm.InstrI8x16MinS, wasm.InstrI8x16MinU, wasm.InstrI8x16MaxS, wasm.InstrI8x16MaxU, wasm.InstrI8x16AvgrU,
			wasm.InstrI16x8Mul, wasm.InstrI16x8MinS, wasm.InstrI16x8MinU, wasm.InstrI16x8MaxS, wasm.InstrI16x8MaxU, wasm.InstrI16x8AvgrU,
			wasm.InstrI32x4Mul, wasm.InstrI32x4MinS, wasm.InstrI32x4MinU, wasm.InstrI32x4MaxS, wasm.InstrI32x4MaxU,
			wasm.InstrI64x2Mul,
			wasm.InstrI8x16Abs, wasm.InstrI8x16Neg, wasm.InstrI16x8Abs, wasm.InstrI16x8Neg,
			wasm.InstrI32x4Abs, wasm.InstrI32x4Neg, wasm.InstrI64x2Abs, wasm.InstrI64x2Neg,
			wasm.InstrI8x16Eq, wasm.InstrI8x16Ne, wasm.InstrI16x8Eq, wasm.InstrI16x8Ne,
			wasm.InstrI32x4Eq, wasm.InstrI32x4Ne, wasm.InstrI64x2Eq, wasm.InstrI64x2Ne,
			wasm.InstrI8x16LtS, wasm.InstrI8x16GtS, wasm.InstrI8x16LeS, wasm.InstrI8x16GeS,
			wasm.InstrI16x8LtS, wasm.InstrI16x8GtS, wasm.InstrI16x8LeS, wasm.InstrI16x8GeS,
			wasm.InstrI32x4LtS, wasm.InstrI32x4GtS, wasm.InstrI32x4LeS, wasm.InstrI32x4GeS,
			wasm.InstrI64x2LtS, wasm.InstrI64x2GtS, wasm.InstrI64x2LeS, wasm.InstrI64x2GeS,
			wasm.InstrI8x16LtU, wasm.InstrI8x16GtU, wasm.InstrI8x16LeU, wasm.InstrI8x16GeU,
			wasm.InstrI16x8LtU, wasm.InstrI16x8GtU, wasm.InstrI16x8LeU, wasm.InstrI16x8GeU,
			wasm.InstrI32x4LtU, wasm.InstrI32x4GtU, wasm.InstrI32x4LeU, wasm.InstrI32x4GeU,
			wasm.InstrI8x16Shl, wasm.InstrI8x16ShrS, wasm.InstrI8x16ShrU,
			wasm.InstrI16x8Shl, wasm.InstrI16x8ShrS, wasm.InstrI16x8ShrU,
			wasm.InstrI32x4Shl, wasm.InstrI32x4ShrS, wasm.InstrI32x4ShrU,
			wasm.InstrI64x2Shl, wasm.InstrI64x2ShrS, wasm.InstrI64x2ShrU,
			wasm.InstrI8x16Splat, wasm.InstrI16x8Splat, wasm.InstrI32x4Splat,
			wasm.InstrI64x2Splat, wasm.InstrF32x4Splat, wasm.InstrF64x2Splat,
			wasm.InstrI8x16ExtractLaneS, wasm.InstrI8x16ExtractLaneU, wasm.InstrI8x16ReplaceLane,
			wasm.InstrI16x8ExtractLaneS, wasm.InstrI16x8ExtractLaneU, wasm.InstrI16x8ReplaceLane,
			wasm.InstrI32x4ExtractLane, wasm.InstrI32x4ReplaceLane,
			wasm.InstrI64x2ExtractLane, wasm.InstrI64x2ReplaceLane,
			wasm.InstrF32x4ExtractLane, wasm.InstrF32x4ReplaceLane,
			wasm.InstrF64x2ExtractLane, wasm.InstrF64x2ReplaceLane,
			wasm.InstrI8x16NarrowI16x8S, wasm.InstrI8x16NarrowI16x8U,
			wasm.InstrI16x8NarrowI32x4S, wasm.InstrI16x8NarrowI32x4U,
			wasm.InstrI16x8ExtendLowI8x16S, wasm.InstrI16x8ExtendHighI8x16S,
			wasm.InstrI16x8ExtendLowI8x16U, wasm.InstrI16x8ExtendHighI8x16U,
			wasm.InstrI32x4ExtendLowI16x8S, wasm.InstrI32x4ExtendHighI16x8S,
			wasm.InstrI32x4ExtendLowI16x8U, wasm.InstrI32x4ExtendHighI16x8U,
			wasm.InstrI64x2ExtendLowI32x4S, wasm.InstrI64x2ExtendHighI32x4S,
			wasm.InstrI64x2ExtendLowI32x4U, wasm.InstrI64x2ExtendHighI32x4U,
			wasm.InstrI16x8ExtmulLowI8x16S, wasm.InstrI16x8ExtmulHighI8x16S,
			wasm.InstrI16x8ExtmulLowI8x16U, wasm.InstrI16x8ExtmulHighI8x16U,
			wasm.InstrI32x4ExtmulLowI16x8S, wasm.InstrI32x4ExtmulHighI16x8S,
			wasm.InstrI32x4ExtmulLowI16x8U, wasm.InstrI32x4ExtmulHighI16x8U,
			wasm.InstrI64x2ExtmulLowI32x4S, wasm.InstrI64x2ExtmulHighI32x4S,
			wasm.InstrI64x2ExtmulLowI32x4U, wasm.InstrI64x2ExtmulHighI32x4U,
			wasm.InstrI8x16Shuffle, wasm.InstrI8x16Swizzle,
			wasm.InstrV128AnyTrue,
			wasm.InstrI8x16AllTrue, wasm.InstrI16x8AllTrue, wasm.InstrI32x4AllTrue, wasm.InstrI64x2AllTrue,
			wasm.InstrI8x16Bitmask, wasm.InstrI16x8Bitmask, wasm.InstrI32x4Bitmask, wasm.InstrI64x2Bitmask,
			wasm.InstrI16x8ExtaddPairwiseI8x16S, wasm.InstrI16x8ExtaddPairwiseI8x16U,
			wasm.InstrI32x4ExtaddPairwiseI16x8S, wasm.InstrI32x4ExtaddPairwiseI16x8U,
			wasm.InstrI32x4DotI16x8S,
			wasm.InstrF32x4Eq, wasm.InstrF32x4Ne, wasm.InstrF32x4Lt, wasm.InstrF32x4Gt, wasm.InstrF32x4Le, wasm.InstrF32x4Ge,
			wasm.InstrF64x2Eq, wasm.InstrF64x2Ne, wasm.InstrF64x2Lt, wasm.InstrF64x2Gt, wasm.InstrF64x2Le, wasm.InstrF64x2Ge,
			wasm.InstrF32x4Abs, wasm.InstrF32x4Neg, wasm.InstrF32x4Sqrt,
			wasm.InstrF32x4Add, wasm.InstrF32x4Sub, wasm.InstrF32x4Mul, wasm.InstrF32x4Div,
			wasm.InstrF64x2Abs, wasm.InstrF64x2Neg, wasm.InstrF64x2Sqrt,
			wasm.InstrF64x2Add, wasm.InstrF64x2Sub, wasm.InstrF64x2Mul, wasm.InstrF64x2Div,
			wasm.InstrF32x4Min, wasm.InstrF32x4Max, wasm.InstrF32x4Pmin, wasm.InstrF32x4Pmax,
			wasm.InstrF64x2Min, wasm.InstrF64x2Max, wasm.InstrF64x2Pmin, wasm.InstrF64x2Pmax,
			wasm.InstrF32x4Ceil, wasm.InstrF32x4Floor, wasm.InstrF32x4Trunc, wasm.InstrF32x4Nearest,
			wasm.InstrF64x2Ceil, wasm.InstrF64x2Floor, wasm.InstrF64x2Trunc, wasm.InstrF64x2Nearest,
			wasm.InstrF32x4DemoteF64x2Zero, wasm.InstrF64x2PromoteLowF32x4,
			wasm.InstrF32x4ConvertI32x4S, wasm.InstrF32x4ConvertI32x4U,
			wasm.InstrF64x2ConvertLowI32x4S, wasm.InstrF64x2ConvertLowI32x4U,
			wasm.InstrI32x4TruncSatF32x4S, wasm.InstrI32x4TruncSatF32x4U,
			wasm.InstrI32x4TruncSatF64x2SZero, wasm.InstrI32x4TruncSatF64x2UZero,
			wasm.InstrI8x16Popcnt, wasm.InstrI16x8Q15mulrSatS,
			wasm.InstrI8x16RelaxedSwizzle,
			wasm.InstrI32x4RelaxedTruncF32x4S, wasm.InstrI32x4RelaxedTruncF32x4U,
			wasm.InstrI32x4RelaxedTruncZeroF64x2S, wasm.InstrI32x4RelaxedTruncZeroF64x2U,
			wasm.InstrF32x4RelaxedMadd, wasm.InstrF32x4RelaxedNmadd, wasm.InstrF64x2RelaxedMadd, wasm.InstrF64x2RelaxedNmadd,
			wasm.InstrI8x16RelaxedLaneselect, wasm.InstrI16x8RelaxedLaneselect,
			wasm.InstrI32x4RelaxedLaneselect, wasm.InstrI64x2RelaxedLaneselect,
			wasm.InstrF32x4RelaxedMin, wasm.InstrF32x4RelaxedMax, wasm.InstrF64x2RelaxedMin, wasm.InstrF64x2RelaxedMax,
			wasm.InstrI16x8RelaxedQ15mulrS, wasm.InstrI16x8RelaxedDotI8x16I7x16S,
			wasm.InstrI32x4RelaxedDotI8x16I7x16AddS:
		default:
			return false
		}
	}
	// Typed vector boundaries still require the vector-capable machine ABI even
	// when the function contains no SIMD opcode (for example, a v128 identity or
	// a pure global.get/global.set accessor).
	return hasVectorOperation || stackHasV128Value(stack)
}

func stackHasSIMDInstruction(stack *railssa.StackFunc) bool {
	for index, instruction := range stack.Instrs {
		if wasm.IsSIMDValidationInstructionKind(instruction.Kind) {
			// A vector constant immediately discarded is pure and has no observable
			// trap. Do not let feature-probing dead code move an otherwise scalar
			// function away from its better target path.
			if instruction.Kind == wasm.InstrV128Const && index+1 < len(stack.Instrs) && stack.Instrs[index+1].Kind == wasm.InstrDrop {
				continue
			}
			return true
		}
	}
	return false
}

func stackHasV128Value(stack *railssa.StackFunc) bool {
	for _, types := range [][]wasm.ValType{stack.Params, stack.Results, stack.Locals, stack.Globals, stack.ResultTypes} {
		for _, typ := range types {
			if typ == wasm.V128 {
				return true
			}
		}
	}
	return false
}

// structuredBranchCastCandidate retains edge-specific refined reference types
// in the structured emitter until the machine SSA models distinct branch and
// fallthrough identities. The slice is deliberately non-allocating and uses
// only scalar control signatures, so no collector root can cross a safepoint.
//
//lint:ignore U1000 retained for the staged structured-branch admission gate
func structuredBranchCastCandidate(stack *railssa.StackFunc) bool {
	if stack == nil || stack.HasV128 || len(stack.BranchCasts) == 0 {
		return false
	}
	hasCollectorType := false
	for _, group := range stack.Module.Types {
		for _, subtype := range group.SubTypes {
			hasCollectorType = hasCollectorType || subtype.Comp.Kind == wasm.CompStruct || subtype.Comp.Kind == wasm.CompArray
		}
	}
	if !hasCollectorType {
		return false
	}
	for _, region := range stack.Regions {
		if region.ParamArity != 0 || region.ResultArity > 1 {
			return false
		}
	}
	for _, instruction := range stack.Instrs {
		switch instruction.Kind {
		case wasm.InstrCall, wasm.InstrCallIndirect,
			wasm.InstrStructNew, wasm.InstrStructNewDefault,
			wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed,
			wasm.InstrArrayNewData, wasm.InstrArrayNewElem:
			return false
		}
	}
	return true
}

// structuredV128ManagedCandidate keeps two-slot managed values on the mature
// structured SIMD stack until RailMach grows a 128-bit machine value and spill
// contract. It admits one root-free default allocation before any managed
// value use, followed only by non-collecting helpers; the allocation publishes
// its deterministic empty-root safepoint through the ordinary artifact path.
func structuredV128ManagedCandidate(stack *railssa.StackFunc) bool {
	if stack == nil || stack.Module == nil {
		return false
	}
	for _, typ := range stack.Params {
		if typ.Kind() == wasm.ValRef {
			return false
		}
	}
	found := false
	allocated := false
	for _, instruction := range stack.Instrs {
		switch instruction.Kind {
		case wasm.InstrStructNewDefault, wasm.InstrArrayNewDefault:
			if allocated || found {
				return false
			}
			allocated = true
		case wasm.InstrArrayNew:
			field, ok := stack.Module.ArrayField(instruction.U32())
			if !ok || field.Storage().Val() != wasm.V128 || allocated || found {
				return false
			}
			allocated = true
			found = true
		case wasm.InstrArrayNewFixed:
			field, ok := stack.Module.ArrayField(instruction.U32())
			if !ok || field.Storage().Val() != wasm.V128 || allocated || found {
				return false
			}
			allocated = true
			found = true
		case wasm.InstrStructNew:
			if allocated || found {
				return false
			}
			fieldCount, ok := stack.Module.StructFieldCount(instruction.U32())
			if !ok {
				return false
			}
			hasV128 := false
			for fieldID := uint32(0); fieldID < fieldCount; fieldID++ {
				field, _ := stack.Module.StructField(instruction.U32(), fieldID)
				if field.Storage().Val().Kind() == wasm.ValRef {
					return false
				}
				hasV128 = hasV128 || field.Storage().Val() == wasm.V128
			}
			if !hasV128 {
				return false
			}
			allocated = true
			found = true
		case wasm.InstrStructGet:
			typeID, fieldID := uint32(instruction.U64()>>32), instruction.U32()
			field, ok := stack.Module.StructField(typeID, fieldID)
			if !ok || field.Storage().Val() != wasm.V128 {
				return false
			}
			found = true
		case wasm.InstrStructSet:
			typeID, fieldID := uint32(instruction.U64()>>32), instruction.U32()
			field, ok := stack.Module.StructField(typeID, fieldID)
			if !ok || field.Storage().Val() != wasm.V128 {
				return false
			}
			found = true
		case wasm.InstrArrayGet, wasm.InstrArraySet, wasm.InstrArrayFill:
			field, ok := stack.Module.ArrayField(instruction.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return false
			}
			found = true
		case wasm.InstrCall, wasm.InstrCallIndirect,
			wasm.InstrRefNull, wasm.InstrRefFunc, wasm.InstrRefIsNull, wasm.InstrRefEq, wasm.InstrRefAsNonNull,
			wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny,
			wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU,
			wasm.InstrStructGetS, wasm.InstrStructGetU,
			wasm.InstrArrayNewData, wasm.InstrArrayNewElem,
			wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArrayLen,
			wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem,
			wasm.InstrElemDrop:
			return false
		}
	}
	return found && allocated
}

func buildNativeImmediateCombinations(plan *nativeBackendPlan, producers *nativeInstructionRelation, skipped *nativeBitSet, uses []uint32) {
	producers.prepare(len(plan.Machine.Insts), true)
	skipped.prepare(len(plan.Machine.Insts), true)
	countNativeMachineUses(plan.Machine, uses)
	if plan.Allocation != nil {
		for instructionID, instruction := range plan.Machine.Insts {
			if instruction.Result == 0 || int(instruction.Result) >= len(plan.Machine.VRegs) || int(instruction.Result) >= len(plan.Allocation.Locations) {
				continue
			}
			data := plan.Machine.VRegs[instruction.Result]
			// A rematerialized durable location has no definition-time home. Every
			// ordinary, fixed, edge, and result use reconstructs the value at its
			// use site, so emitting the pure rematerializable definition only writes
			// a scratch register that is dead immediately afterward.
			if data.Flags&railmach.VRegRematerializable != 0 && plan.Allocation.Locations[instruction.Result].Kind == railmach.LocationRematerialize {
				skipped.set(uint32(instructionID), true)
			}
		}
	}
	for _, combination := range plan.Selection.Combinations {
		if combination.Kind != railmach.CombineImmediate || combination.Producer == ^uint32(0) || int(combination.Producer) >= len(plan.Machine.Insts) || int(combination.Consumer) >= len(plan.Machine.Insts) {
			continue
		}
		producer := plan.Machine.Insts[combination.Producer]
		if producer.Result == 0 || uses[producer.Result] == 0 || producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const {
			continue
		}
		consumerOperands := plan.Machine.InstructionOperands(combination.Consumer)
		if len(consumerOperands) != 2 || consumerOperands[1].Reg != producer.Result {
			// Earlier machine contractions may have changed the selected
			// consumer after RailSpec recorded this relation. Only a still-binary
			// operation with the literal in operand two owns an immediate form.
			continue
		}
		producers.set(combination.Consumer, combination.Producer)
		uses[producer.Result]--
		if uses[producer.Result] == 0 {
			skipped.set(combination.Producer, true)
		}
	}
	for consumerID, consumer := range plan.Machine.Insts {
		if producers.has(uint32(consumerID)) {
			continue
		}
		operands := plan.Machine.InstructionOperands(uint32(consumerID))
		constantDivision := false
		arithmeticImmediate := false
		if plan.Machine.Target == railmach.TargetAMD64 {
			constantDivision = nativeAMD64ConstantDivisionUse(plan, consumer, operands)
			arithmeticImmediate = nativeAMD64ArithmeticImmediateUse(plan, consumer, operands)
		}
		if len(operands) != 2 || !nativeImmediateShiftUse(consumer.Op) && !constantDivision && !arithmeticImmediate {
			continue
		}
		if plan.Machine.Target == railmach.TargetAMD64 && nativeAMD64VectorShiftNeedsRegister(consumer.Op) {
			// Packed byte shifts have no direct x86 immediate form. Keep their
			// scalar count live for the widening-and-mask lowering.
			continue
		}
		value := operands[1].Reg
		if value == 0 || int(value) >= len(plan.Machine.VRegs) {
			continue
		}
		definition := plan.Machine.VRegs[value].Def
		if definition < 3 || (definition-3)%6 != 0 {
			continue
		}
		producerID := (definition - 3) / 6
		if int(producerID) >= len(plan.Machine.Insts) {
			continue
		}
		producer := plan.Machine.Insts[producerID]
		if producer.Result != value || producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const {
			continue
		}
		rematerialized := plan.Allocation != nil && int(value) < len(plan.Allocation.Locations) && plan.Allocation.Locations[value].Kind == railmach.LocationRematerialize
		if skipped.has(producerID) && !rematerialized {
			continue
		}
		producers.set(uint32(consumerID), producerID)
		if uses[value] != 0 {
			uses[value]--
			if uses[value] == 0 {
				skipped.set(producerID, true)
			}
		}
	}
	// Later edge-rematerialization decisions consume the original use counts.
	countNativeMachineUses(plan.Machine, uses)
}

func nativeAMD64ArithmeticImmediateUse(plan *nativeBackendPlan, instruction railmach.Inst, operands []railmach.Operand) bool {
	if len(operands) != 2 {
		return false
	}
	value, constant := nativeIntegerConstant(plan, operands[1].Reg)
	if !constant {
		return false
	}
	switch instruction.Op {
	case railmach.OpAMD64I32Add, railmach.OpAMD64I32Sub, railmach.OpAMD64I32Mul,
		railmach.OpAMD64I32And, railmach.OpAMD64I32Or, railmach.OpAMD64I32Xor:
		return true
	case railmach.OpAMD64I64Add, railmach.OpAMD64I64Sub, railmach.OpAMD64I64Mul,
		railmach.OpAMD64I64And, railmach.OpAMD64I64Or, railmach.OpAMD64I64Xor:
		return uint64(int64(int32(value))) == value
	default:
		return false
	}
}

func nativeAMD64ConstantDivisionUse(plan *nativeBackendPlan, instruction railmach.Inst, operands []railmach.Operand) bool {
	if len(operands) != 2 {
		return false
	}
	value, constant := nativeIntegerConstant(plan, operands[1].Reg)
	if !constant {
		return false
	}
	kind := railmach.SemanticOpcode(instruction.Op)
	switch kind {
	case wasm.InstrI32DivS, wasm.InstrI32RemS:
		if kind == wasm.InstrI32RemS && !plan.AMD64SignedImmediateRemainders {
			return false
		}
		_, _, ok := amd64SignedI32ImmediateMagic(int32(value))
		return ok
	case wasm.InstrI32DivU, wasm.InstrI32RemU:
		divisor := uint32(value)
		if divisor == 0 || railmach.SemanticOpcode(instruction.Op) == wasm.InstrI32RemU && !plan.AMD64ImmediateRemainders {
			return false
		}
		return true
	default:
		return false
	}
}

func countNativeMachineUses(machine *railmach.Func, uses []uint32) {
	clear(uses)
	for instructionID := range machine.Insts {
		for _, operand := range machine.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, transfer := range machine.Transfers {
		uses[transfer.Src]++
	}
	for _, result := range machine.Results {
		uses[result]++
	}
}

// planNativeAMD64SpilledAddressRematerialization replaces a spilled wrapping
// affine address with use-site LEAs only when its original base is already live
// in one durable register at every memory consumer. The allocation remains the
// independent proof source; no live range is extended by this rewrite.
func planNativeAMD64SpilledAddressRematerialization(machine *railmach.Func, allocation *railmach.GreedyAllocation, values, skipped *nativeBitSet, intervalByReg *[]uint32) uint32 {
	enabled := machine != nil && machine.Target == railmach.TargetAMD64 && allocation != nil && len(allocation.Locations) == len(machine.VRegs) && len(allocation.InstructionPositions) == len(machine.Insts)
	values.prepare(len(machine.VRegs), enabled)
	if !enabled {
		return 0
	}
	if uint64(len(allocation.Intervals)) > uint64(^uint32(0)>>2) {
		return 0
	}
	*intervalByReg = resizeNativeSlice(*intervalByReg, len(machine.VRegs))
	clear(*intervalByReg)
	intervalForReg := *intervalByReg
	for index, interval := range allocation.Intervals {
		intervalForReg[interval.Reg] = uint32(index) + 1
	}
	for instructionID, instruction := range machine.Insts {
		result := instruction.Result
		if result == 0 || int(result) >= len(machine.VRegs) || allocation.Locations[result].Kind != railmach.LocationSpill || machine.VRegs[result].Type != railmach.TypeI32 {
			continue
		}
		switch railmach.SemanticOpcode(instruction.Op) {
		case wasm.InstrI32Add, wasm.InstrI32Sub:
		default:
			continue
		}
		operands := machine.InstructionOperands(uint32(instructionID))
		if len(operands) != 2 || operands[0].Reg == 0 {
			continue
		}
		if _, constant := nativeMachineIntegerConstant(machine, operands[1].Reg); !constant {
			continue
		}
		baseLocation := allocation.Locations[operands[0].Reg]
		if baseLocation.Kind != railmach.LocationRegister || baseLocation.Bank != railmach.BankGPR {
			continue
		}
		values.set(uint32(result), true)
	}
	for instructionID := range machine.Insts {
		operands := machine.InstructionOperands(uint32(instructionID))
		for operandIndex, operand := range operands {
			if !values.has(uint32(operand.Reg)) {
				continue
			}
			access, memory := machine.MemoryAccessAt(uint32(instructionID))
			definition := machine.VRegs[operand.Reg].Def / 6
			definitionOperands := machine.InstructionOperands(definition)
			base := definitionOperands[0].Reg
			encodedInterval := intervalForReg[base]
			position := allocation.InstructionPositions[instructionID]*6 + 2
			baseLocation := allocation.Locations[base]
			if operandIndex != 0 || operand.Flags&(railmach.OperandFixed|railmach.OperandColdRemat) != 0 || !memory || access.AddressValue != operand.Reg || encodedInterval == 0 ||
				allocation.LocationAt(base, position) != baseLocation ||
				!nativeAllocationIntervalContains(allocation, allocation.Intervals[encodedInterval-1], position) {
				values.set(uint32(operand.Reg), false)
			}
		}
	}
	for _, transfer := range machine.Transfers {
		values.set(uint32(transfer.Src), false)
		values.set(uint32(transfer.Dst), false)
	}
	for _, result := range machine.Results {
		values.set(uint32(result), false)
	}
	for _, fragment := range allocation.Fragments {
		values.set(uint32(fragment.Reg), false)
		values.set(uint32(fragment.Victim), false)
	}
	var committed uint32
	for instructionID, instruction := range machine.Insts {
		if instruction.Result != 0 && values.has(uint32(instruction.Result)) {
			skipped.set(uint32(instructionID), true)
			committed++
		}
	}
	return committed
}

func nativeAllocationIntervalContains(allocation *railmach.GreedyAllocation, interval railmach.LiveInterval, position uint32) bool {
	if position < interval.Start || position > interval.End {
		return false
	}
	for _, range_ := range allocation.LiveSegmentRanges {
		if range_.Reg != interval.Reg {
			continue
		}
		end := uint32(range_.SegmentStart) + uint32(range_.SegmentCount)
		if end > uint32(len(allocation.LiveSegments)) {
			return false
		}
		for _, segment := range allocation.LiveSegments[range_.SegmentStart:end] {
			if segment.Start <= position && position <= segment.End {
				return true
			}
		}
		return false
	}
	return true
}

func nativeImmediateShiftUse(op railmach.MOpcode) bool {
	switch railmach.SemanticOpcode(op) {
	case wasm.InstrI32Shl, wasm.InstrI64Shl,
		wasm.InstrI32ShrS, wasm.InstrI64ShrS,
		wasm.InstrI32ShrU, wasm.InstrI64ShrU,
		wasm.InstrI32Rotl, wasm.InstrI64Rotl,
		wasm.InstrI32Rotr, wasm.InstrI64Rotr,
		wasm.InstrI8x16Shl, wasm.InstrI8x16ShrS, wasm.InstrI8x16ShrU,
		wasm.InstrI16x8Shl, wasm.InstrI16x8ShrS, wasm.InstrI16x8ShrU,
		wasm.InstrI32x4Shl, wasm.InstrI32x4ShrS, wasm.InstrI32x4ShrU,
		wasm.InstrI64x2Shl, wasm.InstrI64x2ShrS, wasm.InstrI64x2ShrU:
		return true
	}
	switch op {
	case railmach.OpAMD64I16x8Shl, railmach.OpAMD64I16x8ShrS, railmach.OpAMD64I16x8ShrU,
		railmach.OpAMD64I32x4Shl, railmach.OpAMD64I32x4ShrS, railmach.OpAMD64I32x4ShrU,
		railmach.OpAMD64I64x2Shl, railmach.OpAMD64I64x2ShrU,
		railmach.OpARM64I8x16Shl, railmach.OpARM64I8x16ShrS, railmach.OpARM64I8x16ShrU,
		railmach.OpARM64I16x8Shl, railmach.OpARM64I16x8ShrS, railmach.OpARM64I16x8ShrU,
		railmach.OpARM64I32x4Shl, railmach.OpARM64I32x4ShrS, railmach.OpARM64I32x4ShrU,
		railmach.OpARM64I64x2Shl, railmach.OpARM64I64x2ShrS, railmach.OpARM64I64x2ShrU:
		return true
	default:
		return false
	}
}

func nativeAMD64VectorShiftNeedsRegister(op railmach.MOpcode) bool {
	switch op {
	case railmach.OpAMD64I8x16Shl, railmach.OpAMD64I8x16ShrS, railmach.OpAMD64I8x16ShrU, railmach.OpAMD64I64x2ShrS:
		return true
	}
	switch railmach.SemanticOpcode(op) {
	case wasm.InstrI8x16Shl, wasm.InstrI8x16ShrS, wasm.InstrI8x16ShrU, wasm.InstrI64x2ShrS:
		return true
	default:
		return false
	}
}

func applyNativeARM64ShiftImmediateRematerialization(machine *railmach.Func, states []uint32) {
	if machine == nil || machine.Target != railmach.TargetARM64 || len(states) < len(machine.VRegs) {
		return
	}
	states = states[:len(machine.VRegs)]
	clear(states)
	for _, instruction := range machine.Insts {
		if instruction.Result != 0 && (instruction.Op == wasm.InstrI32Const || instruction.Op == wasm.InstrI64Const) {
			states[instruction.Result] = shiftImmediateCandidate
		}
	}
	for instructionID, consumer := range machine.Insts {
		operands := machine.InstructionOperands(uint32(instructionID))
		for operandIndex, operand := range operands {
			if states[operand.Reg]&shiftImmediateCandidate == 0 {
				continue
			}
			states[operand.Reg] |= shiftImmediateSeen
			if !nativeARM64ShiftImmediateUse(consumer.Op, operandIndex, len(operands)) {
				states[operand.Reg] |= shiftImmediateRejected
			}
		}
	}
	for _, transfer := range machine.Transfers {
		if states[transfer.Src]&shiftImmediateCandidate != 0 {
			states[transfer.Src] |= shiftImmediateRejected
		}
		if states[transfer.Dst]&shiftImmediateCandidate != 0 {
			states[transfer.Dst] |= shiftImmediateRejected
		}
	}
	for _, result := range machine.Results {
		if states[result]&shiftImmediateCandidate != 0 {
			states[result] |= shiftImmediateRejected
		}
	}
	for instructionID := range machine.Insts {
		operands := machine.InstructionOperands(uint32(instructionID))
		for index := range operands {
			if states[operands[index].Reg] == shiftImmediateCandidate|shiftImmediateSeen {
				operands[index].Flags |= railmach.OperandColdRemat
			}
		}
	}
}

const (
	shiftImmediateCandidate uint32 = 1 << iota
	shiftImmediateSeen
	shiftImmediateRejected
)

func nativeARM64ShiftImmediateUse(op railmach.MOpcode, operandIndex, operandCount int) bool {
	if operandIndex != 1 || operandCount != 2 {
		return false
	}
	switch op {
	case wasm.InstrI32Shl, wasm.InstrI64Shl,
		wasm.InstrI32ShrS, wasm.InstrI64ShrS,
		wasm.InstrI32ShrU, wasm.InstrI64ShrU,
		wasm.InstrI32Rotl, wasm.InstrI64Rotl,
		wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		return true
	default:
		return false
	}
}

// nativeIntegerConstant returns the exact defining constant for a machine SSA
// value. Finalizers use this only for semantics-preserving target rewrites; the
// definition/result checks keep the decision independently auditable instead
// of trusting rematerialization flags alone.
func nativeIntegerConstant(plan *nativeBackendPlan, value railmach.VReg) (uint64, bool) {
	if plan == nil {
		return 0, false
	}
	return nativeMachineIntegerConstant(plan.Machine, value)
}

func nativeMachineIntegerConstant(machine *railmach.Func, value railmach.VReg) (uint64, bool) {
	if machine == nil || value == 0 || int(value) >= len(machine.VRegs) {
		return 0, false
	}
	definition := machine.VRegs[value].Def
	if definition < 3 || (definition-3)%6 != 0 {
		return 0, false
	}
	instructionID := (definition - 3) / 6
	if int(instructionID) >= len(machine.Insts) {
		return 0, false
	}
	instruction := machine.Insts[instructionID]
	semanticOp := railmach.SemanticOpcode(instruction.Op)
	if instruction.Result != value || semanticOp != wasm.InstrI32Const && semanticOp != wasm.InstrI64Const {
		return 0, false
	}
	return instruction.Aux, true
}

func nativeHasPostRARewrite(plan *nativeBackendPlan, instructionID uint32, kind railmach.RewriteKind) bool {
	if plan == nil || plan.PostRA == nil {
		return false
	}
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.First == instructionID && rewrite.Kind == kind {
			return true
		}
	}
	return false
}

//lint:ignore U1000 retained for architecture finalizers consuming producer rewrites
func nativePostRAProducer(plan *nativeBackendPlan, consumer uint32, kind railmach.RewriteKind) (uint32, bool) {
	if plan == nil || plan.PostRA == nil {
		return 0, false
	}
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.Second == consumer && rewrite.Kind == kind {
			return rewrite.First, true
		}
	}
	return 0, false
}

func nativePostRAConsumer(plan *nativeBackendPlan, producer uint32, kind railmach.RewriteKind) (uint32, bool) {
	if plan == nil || plan.PostRA == nil {
		return 0, false
	}
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.First == producer && rewrite.Kind == kind {
			return rewrite.Second, true
		}
	}
	return 0, false
}

func planInstructionsAdjacent(schedule *railmach.Schedule, first, second uint32) bool {
	if schedule == nil || int(first) >= len(schedule.BlockOf) || int(second) >= len(schedule.BlockOf) || schedule.BlockOf[first] != schedule.BlockOf[second] {
		return false
	}
	for index, instruction := range schedule.Order {
		if instruction == first {
			return index+1 < len(schedule.Order) && schedule.Order[index+1] == second
		}
	}
	return false
}

func nativeScheduleScoreBetter(objective corecompiler.OptimizationObjective, target railmach.Target, instructions int, usesFPR bool, candidate, retained railmach.ScheduleScore) bool {
	if objective == corecompiler.ObjectiveSpeed {
		licmWithinBound := func(hoisted, other railmach.ScheduleScore) bool {
			spillWithinBound := hoisted.WeightedSpillDebt <= other.WeightedSpillDebt
			if hoisted.EstimatedCycles < other.EstimatedCycles {
				divisor := uint64(6)
				if target == railmach.TargetAMD64 && usesFPR {
					// Repeated SIMD literal loads are costlier than the range-area
					// spill model reflects. Permit a bounded debt increase when LICM
					// also lowers the scheduled cycle estimate.
					divisor = 2
				}
				spillWithinBound = other.WeightedSpillDebt > ^uint64(0)/(divisor+1) || hoisted.WeightedSpillDebt <= other.WeightedSpillDebt+other.WeightedSpillDebt/divisor
			}
			return hoisted.LoopInvariantOps > other.LoopInvariantOps &&
				spillWithinBound &&
				hoisted.CopyCycles <= other.CopyCycles &&
				hoisted.PhysicalCopies <= other.PhysicalCopies+hoisted.LoopInvariantOps-other.LoopInvariantOps &&
				hoisted.FixedRepairs <= other.FixedRepairs && hoisted.BrokenFusions <= other.BrokenFusions
		}
		if licmWithinBound(candidate, retained) {
			return true
		}
		if licmWithinBound(retained, candidate) {
			return false
		}
	}
	if objective == corecompiler.ObjectiveSpeed && target == railmach.TargetAMD64 && usesFPR && instructions >= 64 {
		latencyWithoutDebt := func(latency, other railmach.ScheduleScore) bool {
			return latency.Kind == railmach.ScheduleKindLatencyFusion &&
				latency.WeightedSpillDebt <= other.WeightedSpillDebt &&
				latency.CopyCycles <= other.CopyCycles && latency.PhysicalCopies <= other.PhysicalCopies &&
				latency.FixedRepairs <= other.FixedRepairs && latency.BrokenFusions <= other.BrokenFusions
		}
		if latencyWithoutDebt(candidate, retained) {
			return true
		}
		if latencyWithoutDebt(retained, candidate) {
			return false
		}
	}
	if objective == corecompiler.ObjectiveSpeed && target == railmach.TargetARM64 && usesFPR && instructions >= 256 && instructions < 1024 {
		latencyWithinBound := func(latency, other railmach.ScheduleScore) bool {
			spillWithinBound := other.WeightedSpillDebt > ^uint64(0)/3 || latency.WeightedSpillDebt <= other.WeightedSpillDebt*3
			return latency.Kind == railmach.ScheduleKindLatencyFusion &&
				spillWithinBound &&
				latency.CopyCycles <= other.CopyCycles && latency.PhysicalCopies <= other.PhysicalCopies &&
				latency.FixedRepairs <= other.FixedRepairs && latency.BrokenFusions <= other.BrokenFusions
		}
		if latencyWithinBound(candidate, retained) {
			return true
		}
		if latencyWithinBound(retained, candidate) {
			return false
		}
	}
	if objective == corecompiler.ObjectiveSpeed && instructions >= 1024 {
		latencyWithinBound := func(latency, other railmach.ScheduleScore) bool {
			return latency.Kind == railmach.ScheduleKindLatencyFusion &&
				latency.WeightedSpillDebt <= other.WeightedSpillDebt+other.WeightedSpillDebt/3 &&
				latency.CopyCycles <= other.CopyCycles && latency.PhysicalCopies <= other.PhysicalCopies &&
				latency.FixedRepairs <= other.FixedRepairs && latency.BrokenFusions <= other.BrokenFusions
		}
		if latencyWithinBound(candidate, retained) {
			return true
		}
		if latencyWithinBound(retained, candidate) {
			return false
		}
	}
	pressureCopyTradeoff := candidate.Kind == railmach.ScheduleKindPressure &&
		candidate.WeightedSpillDebt < retained.WeightedSpillDebt && candidate.PhysicalCopies > retained.PhysicalCopies ||
		retained.Kind == railmach.ScheduleKindPressure &&
			retained.WeightedSpillDebt < candidate.WeightedSpillDebt && retained.PhysicalCopies > candidate.PhysicalCopies
	if objective == corecompiler.ObjectiveSpeed && target == railmach.TargetAMD64 && pressureCopyTradeoff {
		// A realized register copy executes on every path that carries it. Charge
		// marginal pressure schedules enough to reject tiny spill-debt wins bought
		// with extra copies, without changing source/latency ordering.
		const physicalCopyCost = uint64(32)
		executionDebt := func(score railmach.ScheduleScore) uint64 {
			copies := uint64(score.PhysicalCopies)
			if copies > (^uint64(0)-score.WeightedSpillDebt)/physicalCopyCost {
				return ^uint64(0)
			}
			return score.WeightedSpillDebt + copies*physicalCopyCost
		}
		candidateDebt, retainedDebt := executionDebt(candidate), executionDebt(retained)
		if candidateDebt != retainedDebt {
			return candidateDebt < retainedDebt
		}
	}
	return candidate.BetterThan(retained)
}

func nativeSegmentedAllocationBetter(candidate, retained railmach.ScheduleScore, candidateAllocation *railmach.GreedyAllocation, retainedMetrics railmach.GreedyMetrics, retainedSpillSlots uint16, minimumDebtReduction uint64) bool {
	return candidateAllocation != nil && len(candidateAllocation.LiveSegmentRanges) != 0 &&
		candidate.WeightedSpillDebt < retained.WeightedSpillDebt &&
		retained.WeightedSpillDebt-candidate.WeightedSpillDebt >= max(minimumDebtReduction, 1) &&
		candidateAllocation.SpillSlots <= retainedSpillSlots &&
		candidateAllocation.Metrics.PreservationCost <= retainedMetrics.PreservationCost &&
		candidate.CopyCycles <= retained.CopyCycles &&
		candidate.PhysicalCopies <= retained.PhysicalCopies &&
		candidate.FixedRepairs <= retained.FixedRepairs &&
		candidate.BrokenFusions <= retained.BrokenFusions
}

func nativeSegmentedMinimumDebtReduction(machine *railmach.Func) uint64 {
	if machine != nil {
		for _, block := range machine.Blocks {
			if block.Flags&railssa.BlockLoopHeader != 0 {
				// Loop assignments perturb repeated hot code. Require a material
				// measured debt reduction before changing their physical mapping.
				return 8
			}
		}
	}
	return 1
}

func nativeShouldTrySegmentedLiveness(machine *railmach.Func, score railmach.ScheduleScore) bool {
	if score.WeightedSpillDebt == 0 || !railmach.CanUseSegmentedLiveness(machine) {
		return false
	}
	for _, block := range machine.Blocks {
		if block.Flags&railssa.BlockLoopHeader != 0 {
			return score.WeightedSpillDebt >= 64
		}
	}
	return true
}

// nativeARM64PrePostIndexProfitable keeps writeback addressing for integer
// memory traffic, where it reduces address materialization without crossing
// register banks. Scalar floating-point traffic already has a compact indexed
// form; forcing it through the integer scratch bank adds FMOVs and serializes
// otherwise independent accesses through the written-back address register.
func nativeARM64PrePostIndexProfitable(machine *railmach.Func, rewrite railmach.Rewrite) bool {
	if machine == nil || int(rewrite.First) >= len(machine.Insts) {
		return false
	}
	instruction := machine.Insts[rewrite.First]
	_, _, store, memory := nativeMemoryAccess(instruction.Op)
	if !memory {
		// The post-RA verifier already proves non-scalar memory rewrites. This
		// policy only overrides scalar floating-point forms.
		return true
	}
	value := instruction.Result
	if store {
		operands := machine.InstructionOperands(rewrite.First)
		if len(operands) != 2 {
			return false
		}
		value = operands[1].Reg
	}
	return value != 0 && int(value) < len(machine.VRegs) && machine.VRegs[value].Bank != railmach.BankFPR
}

func railMachPhysicalLiveAcross(plan *nativeBackendPlan, instructionID uint32, bank railmach.Bank, physical uint16) bool {
	position := plan.Allocation.InstructionPositions[instructionID]*6 + 2
	for _, interval := range plan.Allocation.Intervals {
		location := plan.Allocation.Locations[interval.Reg]
		if interval.Bank == bank && location.Kind == railmach.LocationRegister && location.Index == physical && interval.Start < position && interval.End > position && plan.Allocation.IntervalContains(interval, position) {
			return true
		}
	}
	return false
}

func nativeExternalCallFPRMasks(stack *railssa.StackFunc, machine *railmach.Func, allocation *railmach.GreedyAllocation) (all, vector uint64) {
	if stack == nil || machine == nil || allocation == nil || machine.Target != railmach.TargetAMD64 {
		return 0, 0
	}
	for instructionID, instruction := range machine.Insts {
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		external := semanticOp != wasm.InstrCall && railmach.IsCall(instruction.Op) || semanticOp == wasm.InstrCall && uint32(instruction.Aux) < stack.ImportedFuncs
		if !external {
			continue
		}
		position := allocation.InstructionPositions[instructionID]*6 + 2
		for _, interval := range allocation.Intervals {
			if interval.Bank != railmach.BankFPR || interval.Start >= position || interval.End <= position || !allocation.IntervalContains(interval, position) {
				continue
			}
			location := allocation.Locations[interval.Reg]
			if location.Kind == railmach.LocationRegister && location.Index < 64 {
				mask := uint64(1) << location.Index
				all |= mask
				if machine.VRegs[interval.Reg].Type == railmach.TypeV128 {
					vector |= mask
				}
			}
		}
	}
	return all, vector
}

func nativeMachineHasExternalCall(stack *railssa.StackFunc, machine *railmach.Func) bool {
	if stack == nil || machine == nil {
		return false
	}
	for _, instruction := range machine.Insts {
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		if semanticOp == wasm.InstrMemoryGrow ||
			semanticOp != wasm.InstrCall && railmach.IsCall(instruction.Op) ||
			semanticOp == wasm.InstrCall && uint32(instruction.Aux) < stack.ImportedFuncs {
			return true
		}
	}
	return false
}

func nativeMemoryAccess(kind wasm.InstrKind) (size int, signed, store, ok bool) {
	kind = railmach.SemanticOpcode(kind)
	if kind < wasm.InstrI32Load || kind > wasm.InstrI64Store32 {
		return 0, false, false, false
	}
	size, store, ok = 4, kind >= wasm.InstrI32Store, true
	switch kind {
	case wasm.InstrI64Load, wasm.InstrF64Load, wasm.InstrI64Store, wasm.InstrF64Store:
		size = 8
	case wasm.InstrI32Load8S, wasm.InstrI64Load8S:
		size, signed = 1, true
	case wasm.InstrI32Load8U, wasm.InstrI64Load8U, wasm.InstrI32Store8, wasm.InstrI64Store8:
		size = 1
	case wasm.InstrI32Load16S, wasm.InstrI64Load16S:
		size, signed = 2, true
	case wasm.InstrI32Load16U, wasm.InstrI64Load16U, wasm.InstrI32Store16, wasm.InstrI64Store16:
		size = 2
	case wasm.InstrI64Load32S:
		signed = true
	}
	return size, signed, store, ok
}

// planAMD64DeadStores removes the first of adjacent emitted scalar stores when
// the second store overwrites the exact same bytes. Private linear memory makes
// the first write unobservable; retaining its source on the surviving store
// preserves the first trapping operation's Wasm attribution. Machine
// instructions already proven elided do not separate emitted stores.
func planAMD64DeadStores(stack *railssa.StackFunc, machine *railmach.Func, schedule *railmach.Schedule, forbidden nativeBitSet, skip *nativeBitSet, source *nativeInstructionRelation) bool {
	if skip == nil || source == nil {
		return false
	}
	skip.prepare(0, false)
	source.prepare(0, false)
	if stack == nil || stack.Module == nil || machine == nil || machine.Target != railmach.TargetAMD64 || schedule == nil || len(schedule.Order) < 2 || len(schedule.BlockOf) < len(machine.Insts) {
		return false
	}
	found := false
	previous := ^uint32(0)
	for _, second := range schedule.Order {
		if int(second) >= len(machine.Insts) {
			previous = ^uint32(0)
			continue
		}
		instruction := machine.Insts[second]
		if forbidden.has(second) {
			previous = ^uint32(0)
			continue
		}
		if instruction.Result != 0 && int(instruction.Result) < len(machine.VRegs) && machine.VRegs[instruction.Result].Flags&railmach.VRegElided != 0 {
			continue
		}
		first := previous
		previous = second
		if first == ^uint32(0) || schedule.BlockOf[first] != schedule.BlockOf[second] {
			continue
		}
		_, _, firstStore, firstMemory := nativeMemoryAccess(machine.Insts[first].Op)
		_, _, secondStore, secondMemory := nativeMemoryAccess(instruction.Op)
		if !firstMemory || !secondMemory || !firstStore || !secondStore {
			continue
		}
		left, leftOK := machine.MemoryAccessAt(first)
		right, rightOK := machine.MemoryAccessAt(second)
		if !leftOK || !rightOK || left.MemoryIndex != right.MemoryIndex || left.AddressValue != right.AddressValue || left.Offset != right.Offset || left.SemanticWidth == 0 || left.SemanticWidth != right.SemanticWidth || left.EncodedWidth != right.EncodedWidth {
			continue
		}
		memoryType, ok := stack.Module.MemoryType(left.MemoryIndex)
		if !ok || memoryType.Shared {
			continue
		}
		if !found {
			skip.prepare(len(machine.Insts), true)
			source.prepare(len(machine.Insts), true)
			found = true
		}
		firstSource := first
		if prior, ok := source.get(first); ok {
			firstSource = prior
		}
		skip.set(first, true)
		source.set(second, firstSource)
	}
	return found
}

func amd64DeadStoreSource(plan *nativeBackendPlan, instruction uint32) uint32 {
	if plan == nil || plan.Machine == nil || int(instruction) >= len(plan.Machine.Insts) {
		return 0
	}
	if first, ok := plan.AMD64DeadStoreFrom.get(instruction); ok && int(first) < len(plan.Machine.Insts) {
		return plan.Machine.Insts[first].Source
	}
	return plan.Machine.Insts[instruction].Source
}

type nativeAMD64GlobalUpdate struct {
	global                  uint32
	delta                   uint32
	get, constant, add, set uint32
}

func nativeAMD64I32GlobalUpdate(machine *railmach.Func, setID uint32) (nativeAMD64GlobalUpdate, bool) {
	if machine == nil || int(setID) >= len(machine.Insts) {
		return nativeAMD64GlobalUpdate{}, false
	}
	set := machine.Insts[setID]
	if railmach.SemanticOpcode(set.Op) != wasm.InstrGlobalSet {
		return nativeAMD64GlobalUpdate{}, false
	}
	setOperands := machine.InstructionOperands(setID)
	if len(setOperands) != 1 {
		return nativeAMD64GlobalUpdate{}, false
	}
	definition := func(value railmach.VReg) (uint32, bool) {
		if value == 0 || int(value) >= len(machine.VRegs) {
			return 0, false
		}
		encoded := machine.VRegs[value].Def
		instruction := encoded / 6
		return instruction, encoded%6 == 3 && int(instruction) < len(machine.Insts) && machine.Insts[instruction].Result == value
	}
	addID, ok := definition(setOperands[0].Reg)
	if !ok || railmach.SemanticOpcode(machine.Insts[addID].Op) != wasm.InstrI32Add || machine.VRegs[setOperands[0].Reg].Type != railmach.TypeI32 {
		return nativeAMD64GlobalUpdate{}, false
	}
	addOperands := machine.InstructionOperands(addID)
	if len(addOperands) != 2 {
		return nativeAMD64GlobalUpdate{}, false
	}
	leftID, leftOK := definition(addOperands[0].Reg)
	rightID, rightOK := definition(addOperands[1].Reg)
	if !leftOK || !rightOK {
		return nativeAMD64GlobalUpdate{}, false
	}
	getID, constantID := leftID, rightID
	get, constant := machine.Insts[getID], machine.Insts[constantID]
	if railmach.SemanticOpcode(get.Op) != wasm.InstrGlobalGet || railmach.SemanticOpcode(constant.Op) != wasm.InstrI32Const || uint32(get.Aux) != uint32(set.Aux) {
		return nativeAMD64GlobalUpdate{}, false
	}
	return nativeAMD64GlobalUpdate{global: uint32(set.Aux), delta: uint32(constant.Aux), get: getID, constant: constantID, add: addID, set: setID}, true
}

// planAMD64AdjacentGlobalUpdates folds consecutive wrapping i32 additions to
// one mutable global. With no emitted operation between the sets, intermediate
// global values are unobservable and the deltas compose modulo 2^32.
func planAMD64AdjacentGlobalUpdates(machine *railmach.Func, schedule *railmach.Schedule, forbidden nativeBitSet, skip, combined *nativeBitSet, deltas *[]uint32) bool {
	if skip == nil || combined == nil || deltas == nil {
		return false
	}
	skip.prepare(0, false)
	combined.prepare(0, false)
	*deltas = (*deltas)[:0]
	if machine == nil || machine.Target != railmach.TargetAMD64 || schedule == nil || len(schedule.Order) < 8 || len(schedule.BlockOf) < len(machine.Insts) {
		return false
	}
	invalid := ^uint32(0)
	previous := [4]uint32{invalid, invalid, invalid, invalid}
	active := nativeAMD64GlobalUpdate{set: invalid}
	activeDelta := uint32(0)
	activeBlock := railssa.BlockID(^uint32(0))
	found := false
	reset := func(block railssa.BlockID) {
		previous = [4]uint32{invalid, invalid, invalid, invalid}
		active = nativeAMD64GlobalUpdate{set: invalid}
		activeDelta = 0
		activeBlock = block
	}
	for _, instructionID := range schedule.Order {
		if int(instructionID) >= len(machine.Insts) {
			reset(activeBlock)
			continue
		}
		block := schedule.BlockOf[instructionID]
		if block != activeBlock {
			reset(block)
		}
		instruction := machine.Insts[instructionID]
		if forbidden.has(instructionID) {
			reset(block)
			continue
		}
		if instruction.Result != 0 && int(instruction.Result) < len(machine.VRegs) && machine.VRegs[instruction.Result].Flags&railmach.VRegElided != 0 {
			continue
		}
		if update, ok := nativeAMD64I32GlobalUpdate(machine, instructionID); ok && previous[0] == update.add {
			beforeStart := invalid
			pattern := false
			switch {
			case previous[1] == update.get:
				beforeStart = previous[2]
				pattern = true
			case previous[1] == update.constant && previous[2] == update.get:
				beforeStart = previous[3]
				pattern = true
			}
			if !pattern {
				active, activeDelta = nativeAMD64GlobalUpdate{set: invalid}, 0
			} else if active.set != invalid && active.set == beforeStart && active.global == update.global {
				if !found {
					skip.prepare(len(machine.Insts), true)
					combined.prepare(len(machine.Insts), true)
					*deltas = resizeNativeSlice(*deltas, len(machine.Insts))
					clear(*deltas)
					found = true
				}
				skip.set(active.get, true)
				skip.set(active.add, true)
				skip.set(active.set, true)
				combined.set(active.add, false)
				activeDelta += update.delta
				combined.set(update.add, true)
				(*deltas)[update.add] = activeDelta
				active = update
			} else {
				active, activeDelta = update, update.delta
			}
		}
		previous[3], previous[2], previous[1], previous[0] = previous[2], previous[1], previous[0], instructionID
	}
	return found
}

func (p *nativeBackendPlanner) Plan(stack *railssa.StackFunc, target corecompiler.Target) (*nativeBackendPlan, error) {
	return p.PlanProfile(stack, target, 0, nil)
}

func (p *nativeBackendPlanner) PlanProfile(stack *railssa.StackFunc, target corecompiler.Target, functionIndex uint32, observations *profile.Module) (*nativeBackendPlan, error) {
	return p.PlanProfileIPRA(stack, target, corecompiler.ObjectiveSpeed, functionIndex, observations, nil, nil, nil, nil, -1)
}

func (p *nativeBackendPlanner) PlanProfileIPRA(stack *railssa.StackFunc, target corecompiler.Target, objective corecompiler.OptimizationObjective, functionIndex uint32, observations *profile.Module, host []railssa.HostEffectContract, moduleContracts []railmach.ABIContract, components []int, refinedRecursive []bool, localIndex int) (*nativeBackendPlan, error) {
	machineTarget := railmach.TargetInvalid
	switch target.GOARCH {
	case "amd64":
		machineTarget = railmach.TargetAMD64
	case "arm64":
		machineTarget = railmach.TargetARM64
	default:
		return nil, fmt.Errorf("dragline: RailMach target %s is unavailable", target.GOARCH)
	}
	if stack == nil {
		return nil, fmt.Errorf("dragline: RailMach planning requires structured Wasm")
	}
	p.resetCapacityPeak()
	cfg, err := railssa.BuildCFG(stack, &p.cfg)
	if err != nil {
		return nil, err
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, &p.locals)
	if err != nil {
		return nil, err
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, &p.flow)
	if err != nil {
		return nil, err
	}
	p.releaseLocalSSAScratchAbove(nativeBackendPlannerRetentionBytes)
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, &p.semantic)
	if err != nil {
		return nil, err
	}
	metadata, err := railssa.BuildMetadata(stack, &p.metadata)
	if err != nil {
		return nil, err
	}
	if err := railssa.RefineHostEffects(stack, metadata, host); err != nil {
		return nil, err
	}
	rootPlan, err := railssa.BuildRootPlan(stack.Module, stack, cfg, flow, semantic, metadata)
	if err != nil {
		return nil, err
	}
	p.rootPlan = *rootPlan
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), &p.simplified)
	if err != nil {
		return nil, err
	}
	pressure, err := railssa.PressureShape(stack, cfg, flow, semantic, metadata, simplified, &p.pressure)
	if err != nil {
		return nil, err
	}
	var emission *railssa.EmissionPlan
	if railssa.NeedsEmissionPlan(stack) {
		emission, err = railssa.BuildEmissionPlan(stack, flow, semantic, metadata, simplified, &p.emission)
		if err != nil {
			return nil, err
		}
	}
	gcValues, err := railssa.ProduceGCValueFacts(stack, semantic, p.gcValues)
	if err != nil {
		return nil, err
	}
	p.gcValues = gcValues
	specialize, err := railssa.PlanSpecialization(stack, semantic, metadata, simplified, railssa.SpecializationInputs{FunctionIndex: functionIndex, Host: host, Observations: observations, GCValues: gcValues}, &p.specialize)
	if err != nil {
		return nil, err
	}
	machine, err := railmach.BuildWithSimplify(machineTarget, cfg, flow, semantic, simplified, &p.machine)
	if err != nil {
		return nil, err
	}
	amd64ImmediateRemainders, amd64SignedImmediateRemainders := false, false
	if machineTarget == railmach.TargetAMD64 {
		amd64ImmediateRemainders = nativeAMD64ImmediateRemainders(machine)
		amd64SignedImmediateRemainders = nativeAMD64SignedImmediateRemainders(machine)
		refineAMD64ConstantDivisionConstraints(machine, amd64ImmediateRemainders, amd64SignedImmediateRemainders)
	}
	if err := railmach.BindBoundsProofs(machine, emission); err != nil {
		return nil, err
	}
	costModel, err := railspec.TargetCostModelForObjective(target, objective)
	if err != nil {
		return nil, err
	}
	selection, err := railmach.SelectOrderWithCostModel(machineTarget, flow, semantic, simplified, costModel, &p.selection)
	if err != nil {
		return nil, err
	}
	remat, err := railmach.PriceAffineRematerialization(machine, selection, pressure, &p.remat)
	if err != nil {
		return nil, err
	}
	if _, err := railmach.ApplyColdRematerialization(machine, pressure, remat); err != nil {
		return nil, err
	}
	if _, err := railmach.ApplyAddressFolding(machine, flow, semantic, simplified, selection); err != nil {
		return nil, err
	}
	p.releaseValueFlowScratch()
	p.immediateUses = resizeNativeSlice(p.immediateUses, len(machine.VRegs))
	if _, err := railmach.SelectARM64VectorRotatesVerified(machine, selection, p.immediateUses); err != nil {
		return nil, err
	}
	if _, err := railmach.SelectARM64MulHighIdioms(machine); err != nil {
		return nil, err
	}
	if _, err := railmach.SelectARM64MultiplyAdds(machine, p.immediateUses); err != nil {
		return nil, err
	}
	applyNativeARM64ShiftImmediateRematerialization(machine, p.immediateUses)
	dag, err := railmach.BuildDependencyDAG(machine, selection, metadata, &p.dag)
	if err != nil {
		return nil, err
	}
	defaultGreedy := railmach.DefaultGreedyConfig(machineTarget)
	amd64WideVectorScratch := machineTarget == railmach.TargetAMD64 && target.GOOS != "windows"
	if machineHasV128(machine) {
		if machineTarget == railmach.TargetAMD64 {
			// Windows keeps XMM6-XMM15 nonvolatile, while SysV makes every XMM
			// register volatile. XMM13-XMM15 remain finalizer/spill scratch on
			// SysV; scratch-free functions may also allocate XMM12.
			fprs := nativeAMD64VectorAllocatableFPRs(machine, amd64WideVectorScratch)
			defaultGreedy.Linear.FPRs = fprs
			defaultGreedy.CallerFPRs = fprs
			defaultGreedy.CallerFPRMask = callerRegisterMask(fprs)
		} else {
			// V8-V15 require preserving their full Q contents for vector values;
			// the frame and finalizer already distinguish those saves. Platform
			// callees preserve only their low halves, so functions with an external
			// call retain the all-volatile allocation until ARM64 grows the same
			// around-call Q saves as AMD64. V24-V27 remain finalizer scratch.
			if nativeMachineHasExternalCall(stack, machine) {
				defaultGreedy.Linear.FPRs = 16
				defaultGreedy.CallerFPRs = 16
				defaultGreedy.CallerFPRMask = callerRegisterMask(16)
			} else {
				fprs := nativeARM64VectorAllocatableFPRs(machine)
				defaultGreedy.Linear.FPRs = fprs
				defaultGreedy.CallerFPRs = 16
				defaultGreedy.CallerFPRMask = callerRegisterMask(16)
				if fprs == 28 {
					defaultGreedy.CallerFPRs = 20
					defaultGreedy.CallerFPRMask |= uint64(0xf) << 24
				}
			}
		}
	}
	amd64MemoryBoundEnd, cachesAMD64MemoryBound := p.nativeAMD64CachedMemoryBound(stack, machine, pressure)
	if nativeAMD64CachesGlobals(machine) {
		// The final two allocatable GPRs map to RBP/R12. Reserve them for one
		// hot global's write-through value and immutable descriptor.
		defaultGreedy.Linear.GPRs = nativeAMD64CachedGlobalValueRegister
	} else if nativeAMD64CachesGlobalDescriptors(machine) {
		// R12 retains the immutable global-descriptor array. Functions containing
		// calls cannot safely cache a mutable value, but still avoid reloading the
		// array from the instance context at every global access.
		defaultGreedy.Linear.GPRs = nativeAMD64GlobalsRegister
	} else if cachesAMD64MemoryBound {
		// R12 retains the stable memory-0 upper bound for the hottest access
		// width/offset, leaving the first nine GPRs available to allocation.
		defaultGreedy.Linear.GPRs = nativeAMD64MemoryBoundRegister
	}
	if machineTarget == railmach.TargetARM64 && !machineHasV128(machine) {
		defaultGreedy.Linear.FPRs = nativeARM64AllocatableFPRs(machine)
	}
	if nativeARM64CachesGlobals(machine) {
		// X27 retains the immutable global-descriptor array and is reloaded after
		// calls into structured code; keep it outside the allocator here.
		defaultGreedy.Linear.GPRs = nativeARM64GlobalsRegister
	}
	_, cachedGlobalCount := nativeARM64CachedGlobals(stack, machine)
	if cachedGlobalCount >= 2 {
		// X22/X23 retain a second selected descriptor and write-through value.
		defaultGreedy.Linear.GPRs = nativeARM64SecondCachedGlobalDescriptorRegister
	} else if cachedGlobalCount == 1 {
		// X24/X25 retain the selected descriptor and write-through value.
		defaultGreedy.Linear.GPRs = nativeARM64CachedGlobalDescriptorRegister
	}
	defaultGreedy.CallClobbers = nativeCallClobberOverrides(machine, stack.ImportedFuncs, moduleContracts, components, refinedRecursive, localIndex, defaultGreedy)
	defaultGreedy.RecursiveCalls = nativeFunctionHasRecursiveCall(machine, stack.ImportedFuncs, components, localIndex)
	usesFPR := false
	for _, data := range machine.VRegs {
		usesFPR = usesFPR || data.Bank == railmach.BankFPR
	}
	bestGreedy := defaultGreedy
	var best railmach.ScheduleScore
	haveBest := false
	bestIndex := 0
	fastMachine := railmach.FastMachinePolicy(len(machine.Insts))
	scheduleAlternatives := railmach.HasScheduleAlternatives(machine, dag, pressure)
	forcedSchedule := p.forcedSchedule
	if fastMachine || !scheduleAlternatives {
		forcedSchedule = 0
	}
	candidateCount := 3
	if fastMachine || !scheduleAlternatives || forcedSchedule != 0 {
		candidateCount = 1
	}
	parallelCandidates := p.parallelCandidates && len(machine.Insts) >= 1024 && !fastMachine && scheduleAlternatives
	// Evaluate the commonly retained source-stable candidate last. Candidate
	// scoring is order-independent (Kind is the deterministic final tie-break),
	// so its verified products can be consumed directly when it wins instead of
	// rebuilding a fourth identical schedule/allocation/exit chain.
	kinds := [3]railmach.ScheduleKind{railmach.ScheduleKindLatencyFusion, railmach.ScheduleKindPressure, railmach.ScheduleKindSourceStable}
	if fastMachine || !scheduleAlternatives {
		kinds[0] = railmach.ScheduleKindSourceStable
	} else if forcedSchedule != 0 {
		kinds[0] = forcedSchedule
	}
	var initialScheduleScores [3]railmach.ScheduleScore
	if parallelCandidates {
		scores, candidateErrs := p.evaluateScheduleCandidates(machine, selection, dag, pressure, defaultGreedy, kinds, true)
		for index, score := range scores {
			if candidateErrs[index] != nil {
				return nil, candidateErrs[index]
			}
			initialScheduleScores[index] = score
			if !haveBest || nativeScheduleScoreBetter(objective, machine.Target, len(machine.Insts), usesFPR, score, best) {
				best, bestIndex, haveBest = score, index, true
			}
		}
	} else {
		for index, kind := range kinds[:candidateCount] {
			candidate, candidateErr := railmach.BuildScheduleWithPressure(machine, selection, dag, kind, pressure, &p.schedule)
			if candidateErr != nil {
				return nil, candidateErr
			}
			var candidateAllocation *railmach.GreedyAllocation
			if fastMachine {
				candidateAllocation, candidateErr = railmach.AllocateFastMachineForSchedule(machine, candidate, defaultGreedy, &p.allocation)
			} else {
				candidateAllocation, candidateErr = railmach.AllocateGreedyPForSchedule(machine, candidate, defaultGreedy, &p.allocation)
			}
			if candidateErr != nil {
				return nil, candidateErr
			}
			candidateExit, candidateErr := railmach.LateSSAExitVerifiedAllocation(machine, &candidateAllocation.Allocation, &p.exit)
			if candidateErr != nil {
				return nil, candidateErr
			}
			score, candidateErr := railmach.ScoreVerifiedScheduleCandidate(machine, selection, dag, candidate, candidateAllocation, candidateExit)
			if candidateErr != nil {
				return nil, candidateErr
			}
			if p.candidatePostRA {
				candidatePostRA, candidateErr := railmach.PlanPostRAVerifiedAllocation(machineTarget, machine, selection, candidate, candidateAllocation, candidateExit, &p.postRA)
				if candidateErr != nil {
					return nil, candidateErr
				}
				score = railmach.ScorePostRAOpportunities(score, candidate, candidatePostRA)
			}
			initialScheduleScores[index] = score
			if !haveBest || nativeScheduleScoreBetter(objective, machine.Target, len(machine.Insts), usesFPR, score, best) {
				best, haveBest = score, true
			}
		}
	}
	initialCandidateFrontier := uint8(railmach.ScheduleFrontier(initialScheduleScores[:candidateCount]))
	var schedule *railmach.Schedule
	var allocation *railmach.GreedyAllocation
	var exit *railmach.SSAExit
	if parallelCandidates {
		p.retainScheduleCandidate(bestIndex)
		schedule, allocation, exit = &p.schedule, &p.allocation, &p.exit
	} else if p.schedule.Kind == best.Kind {
		schedule, allocation, exit = &p.schedule, &p.allocation, &p.exit
	} else {
		schedule, err = railmach.BuildScheduleWithPressure(machine, selection, dag, best.Kind, pressure, &p.schedule)
		if err != nil {
			return nil, err
		}
		allocation, err = railmach.AllocateGreedyPForSchedule(machine, schedule, bestGreedy, &p.allocation)
		if err != nil {
			return nil, err
		}
		exit, err = railmach.LateSSAExitVerifiedAllocation(machine, &allocation.Allocation, &p.exit)
		if err != nil {
			return nil, err
		}
	}
	backendAttempts := uint8(1)
	scheduleCandidates := uint8(candidateCount)
	var retryScheduleScores [3]railmach.ScheduleScore
	var retryScheduleScoreCount, retryCandidateFrontier uint8
	segmentedBaselineDebt, segmentedCandidateDebt := uint64(0), uint64(0)
	segmentedBaselineCopies, segmentedCandidateCopies := uint32(0), uint32(0)
	segmentedCandidateRanges := uint32(0)
	segmentedAttempted, segmentedAdmitted := false, false
	if decision := railmach.DecideRetry(0, allocation, exit.Debt); !fastMachine && decision.Retry {
		backendAttempts = railmach.MaxBackendAttempts
		retryCandidateCount := 3
		if !scheduleAlternatives {
			retryCandidateCount = 1
		}
		retryScheduleScoreCount = uint8(retryCandidateCount)
		scheduleCandidates += uint8(retryCandidateCount)
		retryGreedy := defaultGreedy
		retryGreedy.PreserveGPRCost = 0
		retryGreedy.PreserveFPRCost = 0
		retryBest := best
		retryKind := best.Kind
		retryIndex := 0
		improved := false
		retryKinds := [3]railmach.ScheduleKind{railmach.ScheduleKindSourceStable, railmach.ScheduleKindLatencyFusion, railmach.ScheduleKindPressure}
		if forcedSchedule != 0 {
			retryKinds[0] = forcedSchedule
			retryCandidateCount = 1
			retryScheduleScoreCount = 1
		}
		if parallelCandidates {
			retryScores, retryErrs := p.evaluateScheduleCandidates(machine, selection, dag, pressure, retryGreedy, retryKinds, true)
			for index, candidateScore := range retryScores {
				if retryErrs[index] != nil {
					return nil, retryErrs[index]
				}
				retryScheduleScores[index] = candidateScore
				if nativeScheduleScoreBetter(objective, machine.Target, len(machine.Insts), usesFPR, candidateScore, retryBest) {
					retryBest, retryKind, retryIndex, improved = candidateScore, retryKinds[index], index, true
				}
			}
		} else {
			for index, kind := range retryKinds[:retryCandidateCount] {
				candidate, retryErr := railmach.BuildScheduleWithPressure(machine, selection, dag, kind, pressure, &p.schedule)
				if retryErr != nil {
					return nil, retryErr
				}
				candidateAllocation, retryErr := railmach.AllocateGreedyPForSchedule(machine, candidate, retryGreedy, &p.allocation)
				if retryErr != nil {
					return nil, retryErr
				}
				candidateExit, retryErr := railmach.LateSSAExitVerifiedAllocation(machine, &candidateAllocation.Allocation, &p.exit)
				if retryErr != nil {
					return nil, retryErr
				}
				candidateScore, retryErr := railmach.ScoreVerifiedScheduleCandidate(machine, selection, dag, candidate, candidateAllocation, candidateExit)
				if retryErr != nil {
					return nil, retryErr
				}
				if p.candidatePostRA {
					candidatePostRA, retryErr := railmach.PlanPostRAVerifiedAllocation(machineTarget, machine, selection, candidate, candidateAllocation, candidateExit, &p.postRA)
					if retryErr != nil {
						return nil, retryErr
					}
					candidateScore = railmach.ScorePostRAOpportunities(candidateScore, candidate, candidatePostRA)
				}
				retryScheduleScores[index] = candidateScore
				if nativeScheduleScoreBetter(objective, machine.Target, len(machine.Insts), usesFPR, candidateScore, retryBest) {
					retryBest, retryKind, improved = candidateScore, kind, true
				}
			}
		}
		retryCandidateFrontier = uint8(railmach.ScheduleFrontier(retryScheduleScores[:retryCandidateCount]))
		if improved {
			best, bestGreedy = retryBest, retryGreedy
			if parallelCandidates {
				p.retainScheduleCandidate(retryIndex)
				schedule, allocation, exit = &p.schedule, &p.allocation, &p.exit
			} else if p.schedule.Kind == retryKind {
				schedule, allocation, exit = &p.schedule, &p.allocation, &p.exit
			} else {
				schedule, err = railmach.BuildScheduleWithPressure(machine, selection, dag, retryKind, pressure, &p.schedule)
				if err != nil {
					return nil, err
				}
				allocation, err = railmach.AllocateGreedyPForSchedule(machine, schedule, bestGreedy, &p.allocation)
				if err != nil {
					return nil, err
				}
				exit, err = railmach.LateSSAExitVerifiedAllocation(machine, &allocation.Allocation, &p.exit)
				if err != nil {
					return nil, err
				}
			}
		} else {
			schedule, err = railmach.BuildScheduleWithPressure(machine, selection, dag, best.Kind, pressure, &p.schedule)
			if err != nil {
				return nil, err
			}
			allocation, err = railmach.AllocateGreedyPForSchedule(machine, schedule, bestGreedy, &p.allocation)
			if err != nil {
				return nil, err
			}
			exit, err = railmach.LateSSAExitVerifiedAllocation(machine, &allocation.Allocation, &p.exit)
			if err != nil {
				return nil, err
			}
		}
	}
	if !fastMachine && nativeShouldTrySegmentedLiveness(machine, best) {
		segmentedAttempted = true
		segmentedBaselineDebt = best.WeightedSpillDebt
		segmentedBaselineCopies = best.PhysicalCopies
		retainedMetrics, retainedSpillSlots := allocation.Metrics, allocation.SpillSlots
		segmentedAllocation, segmentedErr := railmach.AllocateGreedyPSegmentedForSchedule(machine, schedule, bestGreedy, &p.allocation)
		if segmentedErr != nil {
			return nil, segmentedErr
		}
		segmentedExit, segmentedErr := railmach.LateSSAExitVerifiedAllocation(machine, &segmentedAllocation.Allocation, &p.exit)
		if segmentedErr != nil {
			return nil, segmentedErr
		}
		segmentedScore, segmentedErr := railmach.ScoreVerifiedScheduleCandidate(machine, selection, dag, schedule, segmentedAllocation, segmentedExit)
		if segmentedErr != nil {
			return nil, segmentedErr
		}
		segmentedCandidateDebt = segmentedScore.WeightedSpillDebt
		segmentedCandidateCopies = segmentedScore.PhysicalCopies
		segmentedCandidateRanges = uint32(len(segmentedAllocation.LiveSegmentRanges))
		if nativeSegmentedAllocationBetter(segmentedScore, best, segmentedAllocation, retainedMetrics, retainedSpillSlots, nativeSegmentedMinimumDebtReduction(machine)) {
			segmentedAdmitted = true
			allocation, exit, best = segmentedAllocation, segmentedExit, segmentedScore
		} else {
			allocation, err = railmach.AllocateGreedyPForSchedule(machine, schedule, bestGreedy, &p.allocation)
			if err != nil {
				return nil, err
			}
			exit, err = railmach.LateSSAExitVerifiedAllocation(machine, &allocation.Allocation, &p.exit)
			if err != nil {
				return nil, err
			}
		}
	}
	postRA, err := railmach.PlanPostRAVerifiedAllocation(machineTarget, machine, selection, schedule, allocation, exit, &p.postRA)
	if err != nil {
		return nil, err
	}
	postRADirect := machineTarget == railmach.TargetARM64 && len(postRA.WrapSpills) != 0
	hasPostRARealization := p.preparePostRAScratch(machineTarget, len(machine.Insts), postRA.Rewrites) || postRADirect
	if hasPostRARealization {
		for _, rewrite := range postRA.Rewrites {
			switch rewrite.Kind {
			case railmach.RewriteARM64Pair:
				if machineTarget != railmach.TargetARM64 {
					continue
				}
				if p.postRASkip.has(rewrite.First) || p.postRAPairWith.has(rewrite.Second) || !nativeARM64PairRealizable(machine, allocation, rewrite.First, rewrite.Second) {
					continue
				}
				p.postRAPairWith.set(rewrite.First, rewrite.Second)
				p.postRASkip.set(rewrite.Second, true)
			case railmach.RewriteLoadStoreForward:
				if !p.postRASkip.has(rewrite.First) && !p.postRASkip.has(rewrite.Second) {
					p.postRAForwardFrom.set(rewrite.Second, rewrite.First)
				}
			case railmach.RewriteAMD64FusionRepair:
				if machineTarget == railmach.TargetAMD64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) {
					p.postRAFusionWith.set(rewrite.First, rewrite.Second)
					p.postRAFusionWith.set(rewrite.Second, rewrite.First)
				}
			case railmach.RewriteARM64CompareBranch:
				if machineTarget == railmach.TargetARM64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) {
					p.postRAFusionWith.set(rewrite.First, rewrite.Second)
					p.postRAFusionWith.set(rewrite.Second, rewrite.First)
				}
			case railmach.RewriteARM64CompareSelect:
				if machineTarget == railmach.TargetARM64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) {
					p.postRAFusionWith.set(rewrite.First, rewrite.Second)
					p.postRAFusionWith.set(rewrite.Second, rewrite.First)
				}
			case railmach.RewritePhysicalRename:
				if machineTarget == railmach.TargetAMD64 || machineTarget == railmach.TargetARM64 {
					p.postRAFusionWith.set(rewrite.First, rewrite.Second)
					p.postRAFusionWith.set(rewrite.Second, rewrite.First)
				}
			case railmach.RewriteARM64PrePostIndex:
				if !nativeARM64PrePostIndexProfitable(machine, rewrite) {
					continue
				}
				if machineTarget != railmach.TargetARM64 || p.postRASkip.has(rewrite.First) || rewrite.Second != ^uint32(0) && p.postRASkip.has(rewrite.Second) {
					continue
				}
				if rewrite.Second == ^uint32(0) && !p.postRAPostIndexWith.has(rewrite.First) {
					p.postRAPreIndex.set(rewrite.First, true)
				} else if planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) && !p.postRAPostIndexWith.has(rewrite.First) && !p.postRAPostIndexWith.has(rewrite.Second) {
					p.postRAPostIndexWith.set(rewrite.First, rewrite.Second)
					p.postRAPostIndexWith.set(rewrite.Second, rewrite.First)
					p.postRAPreIndex.set(rewrite.First, false)
					p.postRAPreIndex.set(rewrite.Second, false)
				}
			case railmach.RewriteAMD64MemoryFold:
				if machineTarget == railmach.TargetAMD64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) && !p.postRAForwardFrom.has(rewrite.First) && !p.postRASkip.has(rewrite.First) && !p.postRASkip.has(rewrite.Second) {
					p.postRAMemoryFrom.set(rewrite.Second, rewrite.First)
					p.postRASkip.set(rewrite.First, true)
				}
			case railmach.RewriteARM64RepeatedAdd:
				if machineTarget != railmach.TargetARM64 || !nativeARM64RepeatedAddRealizable(machine, schedule, allocation, rewrite.First, rewrite.Second) {
					continue
				}
				p.postRARepeatFirst.set(rewrite.Second, rewrite.First)
				firstPosition := allocation.InstructionPositions[rewrite.First]
				lastPosition := allocation.InstructionPositions[rewrite.Second]
				for instructionID, position := range allocation.InstructionPositions {
					if position >= firstPosition && position < lastPosition {
						p.postRASkip.set(uint32(instructionID), true)
					}
				}
			case railmach.RewriteARM64ByteWiden:
				if machineTarget != railmach.TargetARM64 {
					continue
				}
				_, source, ok := railmach.VerifyARM64ByteWidenChain(machine, schedule, rewrite.First, rewrite.Second)
				firstPosition := allocation.InstructionPositions[rewrite.First]
				finalPosition := allocation.InstructionPositions[rewrite.Second]
				if !ok || allocation.LocationAt(source, firstPosition*6+2).Kind != railmach.LocationRegister || allocation.LocationAt(machine.Insts[rewrite.Second].Result, finalPosition*6+2).Kind != railmach.LocationRegister {
					continue
				}
				conflict := false
				for instructionID, position := range allocation.InstructionPositions {
					conflict = conflict || position > firstPosition && position < finalPosition && p.postRASkip.has(uint32(instructionID))
				}
				if conflict {
					continue
				}
				for instructionID, position := range allocation.InstructionPositions {
					if position > firstPosition && position < finalPosition {
						p.postRASkip.set(uint32(instructionID), true)
					}
				}
			case railmach.RewriteARM64LogicalShift:
				if machineTarget == railmach.TargetARM64 && nativeARM64LogicalShiftRealizable(machine, schedule, allocation, rewrite.First, rewrite.Second) && !p.postRASkip.has(rewrite.First) && !p.postRASkip.has(rewrite.Second) {
					p.postRASkip.set(rewrite.First, true)
				}
			case railmach.RewriteARM64BitmaskPopcnt:
				if machineTarget == railmach.TargetARM64 && nativeARM64BitmaskPopcntRealizable(machine, schedule, allocation, rewrite.First, rewrite.Second) && !p.postRASkip.has(rewrite.First) && !p.postRASkip.has(rewrite.Second) {
					p.postRASkip.set(rewrite.Second, true)
				}
			}
		}
	}
	planAMD64DeadStores(stack, machine, schedule, p.postRASkip, &p.amd64DeadStoreSkip, &p.amd64DeadStoreFrom)
	planAMD64AdjacentGlobalUpdates(machine, schedule, p.postRASkip, &p.amd64GlobalUpdateSkip, &p.amd64GlobalUpdateAdd, &p.amd64GlobalUpdateDelta)
	p.immediateUses = resizeNativeSlice(p.immediateUses, len(machine.VRegs))
	immediatePlan := nativeBackendPlan{Machine: machine, Selection: selection, Allocation: allocation, AMD64ImmediateRemainders: amd64ImmediateRemainders, AMD64SignedImmediateRemainders: amd64SignedImmediateRemainders}
	buildNativeImmediateCombinations(&immediatePlan, &p.immediateProducer, &p.immediateSkip, p.immediateUses)
	planNativeAMD64SpilledAddressRematerialization(machine, allocation, &p.amd64AddressRemat, &p.immediateSkip, &p.amd64AddressState)
	if machine.Target == railmach.TargetARM64 {
		buildNativeARM64LogicalImmediateCombinations(&immediatePlan, &p.immediateProducer, &p.immediateSkip, p.immediateUses)
		preserveNativeARM64RepeatedAddInputs(machine, schedule, p.postRARepeatFirst, &p.immediateSkip)
	}
	contract, calls, err := railmach.AnalyzeVerifiedABI(machine, allocation, metadata, stack.ImportedFuncs)
	if err != nil {
		return nil, err
	}
	if machine.Target == railmach.TargetARM64 {
		contract = railmach.PruneSkippedDefinitionClobbers(machine, allocation, contract, p.immediateSkip.words)
	}
	if nativeARM64PreparedIndirect(stack, machine, allocation) {
		contract.Class = railmach.ABIPreparedIndirect
	}
	if nativeAMD64CachesGlobalDescriptors(machine) {
		contract.GPRClobbers |= uint64(1) << nativeAMD64GlobalsRegister
		contract.CalleeGPRs |= uint64(1) << nativeAMD64GlobalsRegister
	}
	if nativeAMD64CachesGlobals(machine) {
		contract.GPRClobbers |= uint64(1) << nativeAMD64CachedGlobalValueRegister
		contract.CalleeGPRs |= uint64(1) << nativeAMD64CachedGlobalValueRegister
	}
	if cachesAMD64MemoryBound {
		contract.GPRClobbers |= uint64(1) << nativeAMD64MemoryBoundRegister
		contract.CalleeGPRs |= uint64(1) << nativeAMD64MemoryBoundRegister
	}
	if nativeARM64CachesGlobals(machine) {
		contract.GPRClobbers |= uint64(1) << nativeARM64GlobalsRegister
		contract.CalleeGPRs |= uint64(1) << nativeARM64GlobalsRegister
	}
	if cachedGlobalCount != 0 {
		contract.GPRClobbers |= uint64(1)<<nativeARM64CachedGlobalDescriptorRegister | uint64(1)<<nativeARM64CachedGlobalValueRegister
		contract.CalleeGPRs |= uint64(1)<<nativeARM64CachedGlobalDescriptorRegister | uint64(1)<<nativeARM64CachedGlobalValueRegister
		if cachedGlobalCount >= 2 {
			contract.GPRClobbers |= uint64(1)<<nativeARM64SecondCachedGlobalDescriptorRegister | uint64(1)<<nativeARM64SecondCachedGlobalValueRegister
			contract.CalleeGPRs |= uint64(1)<<nativeARM64SecondCachedGlobalDescriptorRegister | uint64(1)<<nativeARM64SecondCachedGlobalValueRegister
		}
	}
	localContract := contract
	refinedCalls := refineNativeCallContracts(calls, stack.ImportedFuncs, moduleContracts, components, refinedRecursive, localIndex)
	railmach.PropagateCallClobbers(&contract, calls, defaultGreedy)
	railmach.PropagateCallEffects(&contract, calls)
	callArgumentBytes := nativeCallArgumentBytes(machine)
	requirements, frame, err := railmach.FrameForAllocation(contract, allocation, callArgumentBytes/8)
	if err != nil {
		return nil, err
	}
	amd64DivisionSave := nativeAMD64HasDivision(machine)
	amd64DivisionSaveRuntimeOffset := requirements.RuntimeBytes
	if amd64DivisionSave {
		requirements.RuntimeBytes += 24
	}
	stackCachedGlobals, stackCachedGlobalCount := nativeAMD64StackCachedGlobals(stack, machine)
	stackCachedGlobalOffset := uint32(0)
	stackCachedGlobalRuntimeOffset := requirements.RuntimeBytes
	if stackCachedGlobalCount != 0 {
		requirements.RuntimeBytes += uint32(stackCachedGlobalCount) * 8
	}
	if amd64DivisionSave || stackCachedGlobalCount != 0 {
		frame, err = railmach.ComposeFrame(requirements)
		if err != nil {
			return nil, err
		}
	}
	if stackCachedGlobalCount != 0 {
		stackCachedGlobalOffset = frame.RuntimeOffset + stackCachedGlobalRuntimeOffset
	}
	amd64DivisionSaveOffset := uint32(0)
	if amd64DivisionSave {
		amd64DivisionSaveOffset = frame.RuntimeOffset + amd64DivisionSaveRuntimeOffset
	}
	externalCallFPRs, externalCallVectorFPRs := nativeExternalCallFPRMasks(stack, machine, allocation)
	if p.rootPlan.SlotCount != 0 || externalCallFPRs != 0 {
		requirements.RootSlots = p.rootPlan.SlotCount
		requirements.CallAreaBytes += uint32(bits.OnesCount64(externalCallFPRs)+bits.OnesCount64(externalCallVectorFPRs)) * 8
		frame, err = railmach.ComposeFrame(requirements)
		if err != nil {
			return nil, err
		}
	}
	var layout *railmach.BlockLayout
	if observations != nil && len(observations.EdgeCounts) != 0 {
		p.edgeWeights = resizeNativeSlice(p.edgeWeights, len(machine.Edges))
		p.edgeObserved = resizeNativeSlice(p.edgeObserved, len(machine.Edges))
		clear(p.edgeWeights)
		clear(p.edgeObserved)
		profileExecuted := false
		for edgeID, edge := range machine.Edges {
			from := cfg.Blocks[edge.From]
			if from.InstCount == 0 {
				continue
			}
			site := stack.Instrs[from.InstStart+from.InstCount-1].Offset
			targetOffset := uint32(0)
			to := cfg.Blocks[edge.To]
			if int(to.InstStart) < len(stack.Instrs) {
				targetOffset = stack.Instrs[to.InstStart].Offset
			}
			for _, count := range observations.EdgeCounts {
				if count.Site.Function == functionIndex && count.Site.Offset == site && count.Target == targetOffset {
					p.edgeWeights[edgeID] = count.Count
					p.edgeObserved[edgeID] = true
					profileExecuted = profileExecuted || count.Count != 0
					break
				}
			}
		}
		p.blockBytes = resizeNativeSlice(p.blockBytes, len(machine.Blocks))
		for blockID, blockRange := range schedule.BlockRanges {
			p.blockBytes[blockID] = max(uint32(blockRange.Count)*4, 4)
		}
		layout, err = railmach.BuildBlockLayout(machine, p.edgeWeights, p.blockBytes, &p.layout)
		if err != nil {
			return nil, err
		}
		p.coldBlocks = resizeNativeSlice(p.coldBlocks, len(machine.Blocks))
		clear(p.coldBlocks)
		if profileExecuted {
			for blockID := 1; blockID < len(machine.Blocks); blockID++ {
				hasIncoming, allObserved, incoming := false, true, uint64(0)
				for edgeID, edge := range machine.Edges {
					if int(edge.To) != blockID {
						continue
					}
					hasIncoming = true
					allObserved = allObserved && p.edgeObserved[edgeID]
					incoming |= p.edgeWeights[edgeID]
				}
				p.coldBlocks[blockID] = hasIncoming && allObserved && incoming == 0
			}
		}
		p.calleeSaveRegions, err = railmach.PlanCalleeSaveRegions(machine, schedule, allocation, contract, frame, p.coldBlocks, stack.Regions, p.calleeSaveRegions)
		if err != nil {
			return nil, err
		}
	} else {
		p.edgeObserved = p.edgeObserved[:0]
		p.edgeWeights = resizeNativeSlice(p.edgeWeights, len(machine.Edges))
		clear(p.edgeWeights)
		p.coldBlocks = resizeNativeSlice(p.coldBlocks, len(machine.Blocks))
		clear(p.coldBlocks)
		work := p.layout.Order[:0]
		if len(machine.Blocks) != 0 {
			exit := railssa.BlockID(len(machine.Blocks) - 1)
			p.coldBlocks[exit] = true
			work = append(work, exit)
		}
		for len(work) != 0 {
			block := work[len(work)-1]
			work = work[:len(work)-1]
			cfgBlock := cfg.Blocks[block]
			for _, predecessor := range cfg.Preds[cfgBlock.PredStart : cfgBlock.PredStart+uint32(cfgBlock.PredCount)] {
				if !p.coldBlocks[predecessor] {
					p.coldBlocks[predecessor] = true
					work = append(work, predecessor)
				}
			}
		}
		hasNoReturnEdge := false
		for edgeID, edge := range machine.Edges {
			if p.coldBlocks[edge.To] {
				p.edgeWeights[edgeID] = uint64(max(machine.Blocks[edge.From].Weight, 1))
			} else {
				hasNoReturnEdge = true
			}
		}
		if hasNoReturnEdge {
			p.blockBytes = resizeNativeSlice(p.blockBytes, len(machine.Blocks))
			for blockID, blockRange := range schedule.BlockRanges {
				p.blockBytes[blockID] = max(uint32(blockRange.Count)*4, 4)
			}
			layout, err = railmach.BuildBlockLayout(machine, p.edgeWeights, p.blockBytes, &p.layout)
			if err != nil {
				return nil, err
			}
		}
		p.coldBlocks = p.coldBlocks[:0]
		p.calleeSaveRegions = p.calleeSaveRegions[:0]
	}
	p.plan = nativeBackendPlan{
		Stack: stack, CFG: cfg, Semantic: semantic,
		Machine: machine, Selection: selection, DAG: dag, Schedule: schedule, Allocation: allocation, Exit: exit, PostRA: postRA,
		Specialize: specialize, Roots: &p.rootPlan, Emission: emission, Pressure: pressure, Remat: remat, Layout: layout, ABI: contract, LocalABI: localContract, Calls: calls, Frame: frame, CalleeSaves: p.calleeSaveRegions, ExternalCallFPRs: externalCallFPRs, ExternalCallVectorFPRs: externalCallVectorFPRs, CallArgumentBytes: callArgumentBytes, Score: best, BackendAttempts: backendAttempts, ScheduleCandidates: scheduleCandidates, ScheduleForced: forcedSchedule != 0,
		InitialScheduleScores: initialScheduleScores, InitialScheduleScoreCount: uint8(candidateCount), InitialCandidateFrontier: initialCandidateFrontier,
		RetryScheduleScores: retryScheduleScores, RetryScheduleScoreCount: retryScheduleScoreCount, RetryCandidateFrontier: retryCandidateFrontier,
		SegmentedBaselineDebt: segmentedBaselineDebt, SegmentedCandidateDebt: segmentedCandidateDebt, SegmentedBaselineCopies: segmentedBaselineCopies, SegmentedCandidateCopies: segmentedCandidateCopies, SegmentedCandidateRanges: segmentedCandidateRanges, SegmentedAttempted: segmentedAttempted, SegmentedAdmitted: segmentedAdmitted,
		Simplified: simplified, IPRARefinedCalls: refinedCalls, AMD64MemoryBoundEnd: amd64MemoryBoundEnd,
		AMD64StackCachedGlobals: stackCachedGlobals, AMD64StackCachedGlobalOffset: stackCachedGlobalOffset, AMD64StackCachedGlobalCount: uint8(stackCachedGlobalCount),
		AMD64DivisionSaveOffset: amd64DivisionSaveOffset, AMD64DivisionSave: amd64DivisionSave, AMD64ImmediateRemainders: amd64ImmediateRemainders, AMD64SignedImmediateRemainders: amd64SignedImmediateRemainders,
		AMD64WideVectorScratch:    amd64WideVectorScratch,
		AMD64ShuffledFPRs:         amd64WideVectorScratch && machineHasV128(machine),
		AMD64AddressRematerialize: p.amd64AddressRemat,
		AMD64BMI2:                 target.HasFeature(corecompiler.TargetFeatureAMD64BMI2),
		PostRAPairWith:            p.postRAPairWith,
		PostRASkip:                p.postRASkip,
		PostRAForwardFrom:         p.postRAForwardFrom,
		PostRAFusionWith:          p.postRAFusionWith,
		PostRAMemoryFrom:          p.postRAMemoryFrom,
		PostRARepeatFirst:         p.postRARepeatFirst,
		PostRAPreIndex:            p.postRAPreIndex,
		PostRAPostIndexWith:       p.postRAPostIndexWith,
		AMD64DeadStoreSkip:        p.amd64DeadStoreSkip,
		AMD64DeadStoreFrom:        p.amd64DeadStoreFrom,
		AMD64GlobalUpdateSkip:     p.amd64GlobalUpdateSkip,
		AMD64GlobalUpdateAdd:      p.amd64GlobalUpdateAdd,
		AMD64GlobalUpdateDelta:    p.amd64GlobalUpdateDelta,
		PostRADirect:              postRADirect,
	}
	p.plan.ImmediateProducer = p.immediateProducer
	p.plan.ImmediateSkip = p.immediateSkip
	if (machine.Target == railmach.TargetARM64 || machine.Target == railmach.TargetAMD64) && p.postRASkip.prepared(len(machine.Insts)) {
		for _, rewrite := range postRA.Rewrites {
			var source railmach.VReg
			var members [10]uint32
			memberCount := 0
			switch rewrite.Kind {
			case railmach.RewriteAMD64ByteSwap:
				if machine.Target != railmach.TargetAMD64 {
					continue
				}
				verifiedSource, verifiedMembers, ok := railmach.VerifyARM64ByteSwapChain(machine, schedule, rewrite.Second)
				if !ok {
					continue
				}
				source, memberCount = verifiedSource, len(verifiedMembers)
				copy(members[:], verifiedMembers[:])
			case railmach.RewriteARM64ByteSwap:
				if machine.Target != railmach.TargetARM64 {
					continue
				}
				verifiedSource, verifiedMembers, ok := railmach.VerifyARM64ByteSwapChain(machine, schedule, rewrite.Second)
				if !ok {
					continue
				}
				source, memberCount = verifiedSource, len(verifiedMembers)
				copy(members[:], verifiedMembers[:])
			case railmach.RewriteARM64Narrow16To8:
				verifiedSource, verifiedMembers, ok := railmach.VerifyARM64Narrow16To8Chain(machine, schedule, rewrite.Second)
				if !ok {
					continue
				}
				source, members, memberCount = verifiedSource, verifiedMembers, len(verifiedMembers)
			default:
				continue
			}
			activeMembers := members[:memberCount]
			if activeMembers[0] != rewrite.First {
				continue
			}
			firstPosition := allocation.InstructionPositions[activeMembers[0]]
			finalPosition := allocation.InstructionPositions[activeMembers[memberCount-1]]
			sourceLocation := allocation.LocationAt(source, firstPosition*6+2)
			firstLocation := allocation.LocationAt(machine.Insts[activeMembers[0]].Result, firstPosition*6+2)
			finalLocation := allocation.LocationAt(machine.Insts[activeMembers[memberCount-1]].Result, finalPosition*6+2)
			if sourceLocation.Kind != railmach.LocationRegister || sourceLocation.Bank != railmach.BankGPR ||
				finalLocation.Kind != railmach.LocationRegister || finalLocation.Bank != railmach.BankGPR ||
				(rewrite.Kind == railmach.RewriteARM64ByteSwap || rewrite.Kind == railmach.RewriteAMD64ByteSwap) && (firstLocation.Kind != railmach.LocationRegister || firstLocation.Bank != railmach.BankGPR || finalLocation != firstLocation) ||
				rewrite.Kind == railmach.RewriteARM64Narrow16To8 && finalLocation != sourceLocation {
				continue
			}
			conflict := false
			for scheduled := firstPosition; scheduled <= finalPosition; scheduled++ {
				instructionID := schedule.Order[scheduled]
				member := false
				for _, candidate := range activeMembers {
					member = member || instructionID == candidate
				}
				if member {
					conflict = conflict || p.postRASkip.has(uint32(instructionID))
				} else {
					instruction := machine.Insts[instructionID]
					elided := p.immediateSkip.has(uint32(instructionID)) || instruction.Result != 0 && machine.VRegs[instruction.Result].Flags&railmach.VRegElided != 0
					conflict = conflict || !elided
				}
			}
			if conflict {
				continue
			}
			for _, instructionID := range activeMembers[1:] {
				p.postRASkip.set(uint32(instructionID), true)
			}
		}
	}
	buildNativeEdgeConstantRematerialization(&p.plan, &p.immediateSkip, p.immediateUses)
	hasGCConstructor, hasGCReferenceStore := false, false
	for _, instruction := range machine.Insts {
		switch railmach.SemanticOpcode(instruction.Op) {
		case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrArrayNew,
			wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData:
			hasGCConstructor = true
		case wasm.InstrStructSet:
			typeID, fieldID := uint32(instruction.Aux>>32), uint32(instruction.Aux)
			field, ok := stack.Module.StructField(typeID, fieldID)
			hasGCReferenceStore = hasGCReferenceStore || ok && field.Storage().Val().Kind() == wasm.ValRef
		case wasm.InstrArraySet:
			field, ok := stack.Module.ArrayField(uint32(instruction.Aux))
			hasGCReferenceStore = hasGCReferenceStore || ok && field.Storage().Val().Kind() == wasm.ValRef
		}
	}
	p.deadGCReservations = p.deadGCReservations[:0]
	if hasGCConstructor {
		p.deadGCReservations = resizeNativeSlice(p.deadGCReservations, len(machine.Insts))
		clear(p.deadGCReservations)
		for instructionID, instruction := range machine.Insts {
			if instruction.Result == 0 || p.immediateUses[instruction.Result] != 0 {
				continue
			}
			switch railmach.SemanticOpcode(instruction.Op) {
			case wasm.InstrStructNew, wasm.InstrStructNewDefault,
				wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData:
				p.deadGCReservations[instructionID] = true
			case wasm.InstrArrayNew:
				// The checked uniform helper validates the initializer but deliberately
				// rejects reference elements: omitting those payload writes would skip
				// their publication semantics. Keep such constructors conservative.
				field, ok := stack.Module.ArrayField(uint32(instruction.Aux))
				p.deadGCReservations[instructionID] = ok && field.Storage().Val().Kind() != wasm.ValRef
			}
		}
	}
	p.plan.DeadGCReservations = p.deadGCReservations
	p.noBarrierGCStores = p.noBarrierGCStores[:0]
	if hasGCReferenceStore {
		p.noBarrierGCStores = resizeNativeSlice(p.noBarrierGCStores, len(machine.Insts))
		clear(p.noBarrierGCStores)
		for instructionID, instruction := range machine.Insts {
			operands := machine.InstructionOperands(uint32(instructionID))
			var child railmach.VReg
			switch railmach.SemanticOpcode(instruction.Op) {
			case wasm.InstrStructSet:
				if len(operands) != 2 {
					return nil, fmt.Errorf("RailMach struct.set operand count is %d", len(operands))
				}
				typeID, fieldID := uint32(instruction.Aux>>32), uint32(instruction.Aux)
				field, ok := stack.Module.StructField(typeID, fieldID)
				if !ok || field.Storage().Val().Kind() != wasm.ValRef {
					continue
				}
				child = operands[1].Reg
			case wasm.InstrArraySet:
				if len(operands) != 3 {
					return nil, fmt.Errorf("RailMach array.set operand count is %d", len(operands))
				}
				field, ok := stack.Module.ArrayField(uint32(instruction.Aux))
				if !ok || field.Storage().Val().Kind() != wasm.ValRef {
					continue
				}
				child = operands[2].Reg
			default:
				continue
			}
			p.noBarrierGCStores[instructionID] = nativeValueCannotCreateCollectorEdge(machine, child)
		}
	}
	p.plan.NoBarrierGCStores = p.noBarrierGCStores
	if cap(p.blockOffsets) < len(machine.Blocks) {
		p.blockOffsets = make([]int, len(machine.Blocks))
	} else {
		p.blockOffsets = p.blockOffsets[:len(machine.Blocks)]
		clear(p.blockOffsets)
	}
	p.branchPatches = p.branchPatches[:0]
	p.conditionalPatches = p.conditionalPatches[:0]
	p.coldTrapPatches = p.coldTrapPatches[:0]
	p.memoryCheckEnds = p.memoryCheckEnds[:0]
	p.memoryCheckTouched = p.memoryCheckTouched[:0]
	p.memoryCheckSlots.prepare(len(machine.VRegs), len(machine.Memory) != 0)
	if len(machine.Memory) != 0 {
		// Immediate-use scratch is dead after edge rematerialization and GC
		// analysis. Reuse it to assign one dense bounds-cache slot to each
		// distinct memory address without retaining another VReg-sized slab.
		clear(p.immediateUses)
		uniqueAddresses := 0
		for index := range machine.Memory {
			address := machine.Memory[index].AddressValue
			if p.immediateUses[address] == 0 {
				uniqueAddresses++
				p.immediateUses[address] = uint32(uniqueAddresses)
				p.memoryCheckSlots.set(uint32(address), uint32(uniqueAddresses-1))
			}
		}
		p.memoryCheckEnds = resizeNativeSlice(p.memoryCheckEnds, uniqueAddresses)
		clear(p.memoryCheckEnds)
		p.memoryCheckTouched = resizeNativeSlice(p.memoryCheckTouched, uniqueAddresses)[:0]
	}
	p.plan.BlockOffsets = p.blockOffsets
	p.plan.BranchPatches = p.branchPatches
	p.plan.ConditionalPatches = p.conditionalPatches
	p.plan.ColdTrapPatches = p.coldTrapPatches
	p.plan.MemoryCheckSlots = p.memoryCheckSlots
	p.plan.MemoryCheckEnds = p.memoryCheckEnds
	p.plan.MemoryCheckTouched = p.memoryCheckTouched
	if _, err := railmach.SelectTargetOpcodes(machine); err != nil {
		return nil, err
	}
	if machine.Target == railmach.TargetARM64 {
		if _, err := railmach.SelectARM64ImmediateOpcodes(machine, p.immediateProducer.narrow, p.immediateProducer.wide); err != nil {
			return nil, err
		}
		p.plan.ImmediateProducer = nativeInstructionRelation{}
	}
	p.plan.TargetSelectedInstructions, p.plan.GenericMachineInstructions = 0, 0
	if p.candidatePostRA {
		for _, instruction := range machine.Insts {
			if railmach.IsSelectedOpcode(instruction.Op) {
				p.plan.TargetSelectedInstructions++
			}
		}
		p.plan.GenericMachineInstructions = uint32(len(machine.Insts)) - p.plan.TargetSelectedInstructions
	}
	p.observeCapacity()
	return &p.plan, nil
}

// refineAMD64ConstantDivisionConstraints releases the fixed RAX dividend
// constraint when finalization can replace i32 division or remainder by exact
// immediate arithmetic. This lets ordinary allocation
// preserve the dividend in its natural register instead of paying repairs
// inherited from x86 DIV.
func refineAMD64ConstantDivisionConstraints(machine *railmach.Func, immediateRemainders, signedImmediateRemainders bool) {
	for instructionID, instruction := range machine.Insts {
		kind := railmach.SemanticOpcode(instruction.Op)
		signed := kind == wasm.InstrI32DivS || kind == wasm.InstrI32RemS
		unsigned := kind == wasm.InstrI32DivU || kind == wasm.InstrI32RemU
		if !signed && !unsigned {
			continue
		}
		operands := machine.InstructionOperands(uint32(instructionID))
		if len(operands) != 2 {
			continue
		}
		value, constant := nativeMachineIntegerConstant(machine, operands[1].Reg)
		if !constant {
			continue
		}
		immediate := false
		if signed {
			if kind == wasm.InstrI32RemS && !signedImmediateRemainders {
				continue
			}
			_, _, immediate = amd64SignedI32ImmediateMagic(int32(value))
		} else {
			divisor := uint32(value)
			if divisor == 0 || kind == wasm.InstrI32RemU && !immediateRemainders {
				continue
			}
			immediate = true
		}
		if !immediate {
			continue
		}
		operand := &machine.Operands[instruction.OperandStart]
		operand.Fixed = railmach.NoFixedReg
		operand.Flags &^= railmach.OperandFixed
	}
}

// nativeAMD64SignedImmediateRemainders admits a signed remainder when the
// function contains another signed constant-division replacement that shares
// the fixed-register relief and code-size cost.
func nativeAMD64SignedImmediateRemainders(machine *railmach.Func) bool {
	const minimumUses = 2
	uses := 0
	for instructionID, instruction := range machine.Insts {
		kind := railmach.SemanticOpcode(instruction.Op)
		if kind != wasm.InstrI32DivS && kind != wasm.InstrI32RemS {
			continue
		}
		operands := machine.InstructionOperands(uint32(instructionID))
		if len(operands) != 2 {
			continue
		}
		value, constant := nativeMachineIntegerConstant(machine, operands[1].Reg)
		if !constant {
			continue
		}
		if _, _, immediate := amd64SignedI32ImmediateMagic(int32(value)); immediate {
			uses++
			if uses == minimumUses {
				return true
			}
		}
	}
	return false
}

// nativeAMD64ImmediateRemainders admits exact constant unsigned remainders.
// Even one multiply-high sequence is materially cheaper than x86 DIV; keeping
// the decision function-scoped also lets allocation release RAX/RDX before
// finalization.
func nativeAMD64ImmediateRemainders(machine *railmach.Func) bool {
	for instructionID, instruction := range machine.Insts {
		if railmach.SemanticOpcode(instruction.Op) != wasm.InstrI32RemU {
			continue
		}
		operands := machine.InstructionOperands(uint32(instructionID))
		if len(operands) != 2 {
			continue
		}
		value, constant := nativeMachineIntegerConstant(machine, operands[1].Reg)
		divisor := uint32(value)
		if !constant || divisor == 0 {
			continue
		}
		return true
	}
	return false
}

// preserveNativeARM64RepeatedAddInputs keeps the invariant input of a
// repeated-add rewrite materialized. Ordinary immediate folding can otherwise
// suppress a shared constant after the rewrite has replaced all of its scalar
// consumers with one shifted-register add.
func preserveNativeARM64RepeatedAddInputs(machine *railmach.Func, schedule *railmach.Schedule, repeats nativeInstructionRelation, skipped *nativeBitSet) {
	if machine == nil || schedule == nil || skipped == nil {
		return
	}
	for last := range machine.Insts {
		first, ok := repeats.get(uint32(last))
		if !ok {
			continue
		}
		_, invariant, _, ok := railmach.VerifyARM64RepeatedAddChain(machine, schedule, first, uint32(last))
		if !ok || invariant == 0 || int(invariant) >= len(machine.VRegs) {
			continue
		}
		definition := machine.VRegs[invariant].Def
		if definition < 3 || (definition-3)%6 != 0 {
			continue
		}
		instruction := (definition - 3) / 6
		if int(instruction) < len(machine.Insts) && machine.Insts[instruction].Result == invariant {
			skipped.set(instruction, false)
		}
	}
}

func machineHasV128(machine *railmach.Func) bool {
	if machine == nil {
		return false
	}
	for _, value := range machine.VRegs {
		if value.Type == railmach.TypeV128 {
			return true
		}
	}
	return false
}

func machineAMD64VectorScratchCount(machine *railmach.Func, wideScratch bool) uint8 {
	if machine == nil || machine.Target != railmach.TargetAMD64 {
		return 0
	}
	count := uint8(0)
	for instructionID, instruction := range machine.Insts {
		switch instruction.Op {
		case wasm.InstrI16x8Shl, wasm.InstrI16x8ShrS, wasm.InstrI16x8ShrU,
			wasm.InstrI32x4Shl, wasm.InstrI32x4ShrS, wasm.InstrI32x4ShrU,
			wasm.InstrI64x2Shl, wasm.InstrI64x2ShrU,
			railmach.OpAMD64I16x8Shl, railmach.OpAMD64I16x8ShrS, railmach.OpAMD64I16x8ShrU,
			railmach.OpAMD64I32x4Shl, railmach.OpAMD64I32x4ShrS, railmach.OpAMD64I32x4ShrU,
			railmach.OpAMD64I64x2Shl, railmach.OpAMD64I64x2ShrU:
			operands := machine.InstructionOperands(uint32(instructionID))
			if len(operands) == 2 {
				if _, ok := nativeMachineIntegerConstant(machine, operands[1].Reg); ok {
					continue
				}
			}
			count = 1 // XMM5 carries a dynamic packed-lane shift count.
		case wasm.InstrI8x16Shuffle, railmach.OpAMD64I8x16Shuffle:
			if wideScratch || nativeAMD64ShuffleScratchCount(machine, uint32(instructionID)) == 0 {
				if !wideScratch {
					count = max(count, 1)
				}
				continue
			}
			return 2 // XMM4 holds one shuffled half while XMM5 holds each mask.
		case wasm.InstrI32x4TruncSatF32x4S, wasm.InstrI32x4TruncSatF32x4U,
			wasm.InstrI32x4TruncSatF64x2SZero, wasm.InstrI32x4TruncSatF64x2UZero,
			wasm.InstrI32x4RelaxedTruncF32x4S, wasm.InstrI32x4RelaxedTruncF32x4U,
			wasm.InstrI32x4RelaxedTruncZeroF64x2S, wasm.InstrI32x4RelaxedTruncZeroF64x2U,
			railmach.OpAMD64I32x4TruncSatF32x4S, railmach.OpAMD64I32x4TruncSatF32x4U,
			railmach.OpAMD64I32x4TruncSatF64x2SZero, railmach.OpAMD64I32x4TruncSatF64x2UZero,
			wasm.InstrI8x16Popcnt, wasm.InstrI16x8Q15mulrSatS,
			railmach.OpAMD64I8x16Popcnt, railmach.OpAMD64I16x8Q15mulrSatS:
			return 3 // XMM3-XMM5 cover clamp, mask, and conversion temporaries.
		case wasm.InstrI32x4RelaxedDotI8x16I7x16AddS, railmach.OpAMD64I32x4RelaxedDotI8x16I7x16AddS:
			return 3 // XMM3-XMM5 preserve the addend and form the signed byte dot product.
		case wasm.InstrI16x8RelaxedDotI8x16I7x16S, railmach.OpAMD64I16x8RelaxedDotI8x16I7x16S:
			return 3 // XMM3-XMM5 widen both byte halves before exact saturated packing.
		case wasm.InstrI8x16Shl, wasm.InstrI8x16ShrS, wasm.InstrI8x16ShrU, wasm.InstrI64x2ShrS,
			railmach.OpAMD64I8x16Shl, railmach.OpAMD64I8x16ShrS, railmach.OpAMD64I8x16ShrU, railmach.OpAMD64I64x2ShrS:
			return 3 // XMM3-XMM5 hold shift counts, widened halves, and masks.
		case wasm.InstrV128Load8x8U, wasm.InstrV128Load16x4U, wasm.InstrV128Load32x2S, wasm.InstrV128Load32x2U,
			railmach.OpAMD64V128Load8x8U, railmach.OpAMD64V128Load16x4U, railmach.OpAMD64V128Load32x2S, railmach.OpAMD64V128Load32x2U:
			count = 1 // XMM5 supplies zero or sign-extension lanes.
		case wasm.InstrI8x16RelaxedSwizzle,
			wasm.InstrMemoryCopy, railmach.OpAMD64MemoryCopy,
			wasm.InstrF32x4RelaxedMadd, wasm.InstrF32x4RelaxedNmadd, wasm.InstrF64x2RelaxedMadd, wasm.InstrF64x2RelaxedNmadd,
			wasm.InstrI8x16RelaxedLaneselect, wasm.InstrI16x8RelaxedLaneselect,
			wasm.InstrI32x4RelaxedLaneselect, wasm.InstrI64x2RelaxedLaneselect,
			wasm.InstrI16x8RelaxedQ15mulrS,
			railmach.OpAMD64F32x4RelaxedMadd, railmach.OpAMD64F32x4RelaxedNmadd,
			railmach.OpAMD64F64x2RelaxedMadd, railmach.OpAMD64F64x2RelaxedNmadd:
			count = 1 // XMM5 preserves a ternary input or supplies a vector scratch.
		case wasm.InstrI16x8ExtmulLowI8x16S, wasm.InstrI16x8ExtmulHighI8x16S,
			wasm.InstrI16x8ExtmulLowI8x16U, wasm.InstrI16x8ExtmulHighI8x16U,
			wasm.InstrI32x4ExtmulLowI16x8S, wasm.InstrI32x4ExtmulHighI16x8S,
			wasm.InstrI32x4ExtmulLowI16x8U, wasm.InstrI32x4ExtmulHighI16x8U,
			wasm.InstrI64x2ExtmulLowI32x4S, wasm.InstrI64x2ExtmulHighI32x4S,
			wasm.InstrI64x2ExtmulLowI32x4U, wasm.InstrI64x2ExtmulHighI32x4U,
			wasm.InstrI32x4ExtaddPairwiseI16x8U,
			wasm.InstrF32x4Min, wasm.InstrF32x4Max, wasm.InstrF64x2Min, wasm.InstrF64x2Max,
			wasm.InstrI64x2Mul, wasm.InstrF32x4ConvertI32x4U, wasm.InstrF64x2ConvertLowI32x4U,
			railmach.OpAMD64I64x2Mul, railmach.OpAMD64F32x4ConvertI32x4U, railmach.OpAMD64F64x2ConvertLowI32x4U:
			return 2 // XMM4-XMM5 preserve both widened inputs across destructive sequences.
		case wasm.InstrI8x16Ne, wasm.InstrI16x8Ne, wasm.InstrI32x4Ne, wasm.InstrI64x2Ne,
			wasm.InstrI8x16LeS, wasm.InstrI8x16GeS, wasm.InstrI16x8LeS, wasm.InstrI16x8GeS,
			wasm.InstrI32x4LeS, wasm.InstrI32x4GeS, wasm.InstrI64x2LeS, wasm.InstrI64x2GeS,
			wasm.InstrI8x16LtU, wasm.InstrI8x16GtU, wasm.InstrI8x16LeU, wasm.InstrI8x16GeU,
			wasm.InstrI16x8LtU, wasm.InstrI16x8GtU, wasm.InstrI16x8LeU, wasm.InstrI16x8GeU,
			wasm.InstrI32x4LtU, wasm.InstrI32x4GtU, wasm.InstrI32x4LeU, wasm.InstrI32x4GeU,
			wasm.InstrI16x8ExtendLowI8x16S, wasm.InstrI16x8ExtendHighI8x16S,
			wasm.InstrI16x8ExtendLowI8x16U, wasm.InstrI16x8ExtendHighI8x16U,
			wasm.InstrI32x4ExtendLowI16x8S, wasm.InstrI32x4ExtendHighI16x8S,
			wasm.InstrI32x4ExtendLowI16x8U, wasm.InstrI32x4ExtendHighI16x8U,
			wasm.InstrI64x2ExtendLowI32x4S, wasm.InstrI64x2ExtendHighI32x4S,
			wasm.InstrI64x2ExtendLowI32x4U, wasm.InstrI64x2ExtendHighI32x4U,
			wasm.InstrI8x16Swizzle, railmach.OpAMD64I8x16Swizzle,
			wasm.InstrI8x16AllTrue, wasm.InstrI16x8AllTrue, wasm.InstrI32x4AllTrue, wasm.InstrI64x2AllTrue,
			wasm.InstrI16x8Bitmask,
			wasm.InstrI16x8ExtaddPairwiseI8x16S, wasm.InstrI16x8ExtaddPairwiseI8x16U,
			wasm.InstrI32x4ExtaddPairwiseI16x8S,
			wasm.InstrF32x4Abs, wasm.InstrF32x4Neg, wasm.InstrF64x2Abs, wasm.InstrF64x2Neg,
			wasm.InstrI8x16Neg, wasm.InstrI16x8Neg, wasm.InstrI32x4Neg, wasm.InstrI64x2Abs, wasm.InstrI64x2Neg,
			wasm.InstrV128Not, wasm.InstrV128Bitselect:
			count = 1 // XMM5 is the ordinary vector lowering scratch.
		}
	}
	return count
}

func nativeAMD64VectorAllocatableFPRs(machine *railmach.Func, wideScratch bool) uint8 {
	scratch := machineAMD64VectorScratchCount(machine, wideScratch)
	if !wideScratch {
		return 6 - scratch
	}
	// The SysV physical register order places XMM3-XMM5 last, so trimming this
	// prefix reserves exactly the fixed scratch registers while retaining
	// XMM6-XMM12 for ordinary values.
	return 13 - scratch
}

func nativeAMD64ShuffleScratchCount(machine *railmach.Func, instructionID uint32) uint8 {
	if machine == nil || int(instructionID) >= len(machine.Insts) {
		return 2
	}
	operands := machine.InstructionOperands(instructionID)
	immediate, ok := machine.SIMDImmediateAt(instructionID)
	if len(operands) != 2 || !ok {
		return 2
	}
	if operands[0].Reg == operands[1].Reg {
		return 0
	}
	allLHS, allRHS := true, true
	for _, lane := range immediate.Bytes {
		allLHS = allLHS && lane < 16
		allRHS = allRHS && lane >= 16
	}
	if allLHS || allRHS {
		return 0
	}
	return 2
}

func nativeARM64AllocatableFPRs(machine *railmach.Func) uint8 {
	hasCall, hasF32Copysign := false, false
	for _, instruction := range machine.Insts {
		hasCall = hasCall || railmach.IsCall(instruction.Op)
		hasF32Copysign = hasF32Copysign || instruction.Op == wasm.InstrF32Copysign
	}
	switch {
	case !hasCall && hasF32Copysign:
		// V24-V26 may cache repeated floating constants and V27 holds the
		// f32.copysign mask.
		return 24
	case !hasCall:
		// Without the f32.copysign mask, V24 joins the allocation while
		// V25-V27 retain the three floating-constant cache slots.
		return 25
	case hasF32Copysign:
		// Calls disable constant caching, but f32.copysign still reserves V27.
		return 27
	default:
		return 28
	}
}

// nativeARM64VectorAllocatableFPRs admits V24-V27 only when every vector
// operation in the function has a lowering that leaves those registers
// untouched. V28-V30 remain spill/result scratch and V31 remains reserved for
// architectural masks. Unknown forms retain the conservative 24-register set.
func nativeARM64VectorAllocatableFPRs(machine *railmach.Func) uint8 {
	if machine == nil {
		return 24
	}
	for instructionID, instruction := range machine.Insts {
		vector := instruction.Result != 0 && machine.VRegs[instruction.Result].Type == railmach.TypeV128
		operands := machine.InstructionOperands(uint32(instructionID))
		for _, operand := range operands {
			vector = vector || machine.VRegs[operand.Reg].Type == railmach.TypeV128
		}
		if !vector {
			continue
		}
		switch instruction.Op {
		case wasm.InstrV128Const, wasm.InstrV128Load, wasm.InstrV128Store,
			wasm.InstrV128And, wasm.InstrV128Or, wasm.InstrV128Xor, wasm.InstrV128Not,
			wasm.InstrI32x4Add, wasm.InstrI32x4Sub, wasm.InstrI32x4Splat, wasm.InstrI32x4ReplaceLane:
			continue
		case wasm.InstrI8x16Shuffle:
			immediate, ok := machine.SIMDImmediateAt(uint32(instructionID))
			if ok && arm64ShuffleSpecialized(immediate.Bytes) {
				continue
			}
		case wasm.InstrI32x4Shl, wasm.InstrI32x4ShrS, wasm.InstrI32x4ShrU:
			if len(operands) == 2 {
				definition := machine.VRegs[operands[1].Reg].Def / 6
				if int(definition) < len(machine.Insts) {
					producer := machine.Insts[definition]
					if producer.Result == operands[1].Reg && (producer.Op == wasm.InstrI32Const || producer.Op == wasm.InstrI64Const) {
						continue
					}
				}
			}
		}
		return 24
	}
	return 28
}

const (
	nativeAMD64CachedGlobalValueRegister            = 8
	nativeAMD64GlobalsRegister                      = 9
	nativeAMD64MemoryBoundRegister                  = 9
	nativeARM64SecondCachedGlobalDescriptorRegister = 15
	nativeARM64SecondCachedGlobalValueRegister      = 16
	nativeARM64CachedGlobalDescriptorRegister       = 17
	nativeARM64CachedGlobalValueRegister            = 18
	nativeARM64GlobalsRegister                      = 19
)

func (p *nativeBackendPlanner) nativeAMD64CachedMemoryBound(stack *railssa.StackFunc, machine *railmach.Func, pressure *railssa.PressurePlan) (uint64, bool) {
	if stack == nil || machine == nil || pressure == nil || p.signalsBounds || machine.Target != railmach.TargetAMD64 || nativeAMD64CachesGlobalDescriptors(machine) || stack.MemoryMinBytes == 0 {
		return 0, false
	}
	p.amd64MemoryBounds = p.amd64MemoryBounds[:0]
	for blockID, block := range machine.Blocks {
		for instructionID := block.InstStart; instructionID < block.InstStart+block.InstCount; instructionID++ {
			instruction := machine.Insts[instructionID]
			size, _, _, memory := nativeMemoryAccess(instruction.Op)
			access, described := machine.MemoryAccessAt(instructionID)
			if !memory || described && access.BoundsProof != 0 {
				continue
			}
			end := uint64(uint32(instruction.Aux)) + uint64(size)
			if end == 0 || end > uint64(^uint32(0)>>1) || end > stack.MemoryMinBytes {
				continue
			}
			found := false
			for index := range p.amd64MemoryBounds {
				if p.amd64MemoryBounds[index].end == end {
					p.amd64MemoryBounds[index].weight += uint64(machine.Blocks[blockID].Weight)
					found = true
					break
				}
			}
			if !found {
				p.amd64MemoryBounds = append(p.amd64MemoryBounds, nativeAMD64MemoryBoundUse{end: end, weight: uint64(machine.Blocks[blockID].Weight)})
			}
		}
	}
	best := nativeAMD64MemoryBoundUse{}
	for _, candidate := range p.amd64MemoryBounds {
		if candidate.weight > best.weight || candidate.weight == best.weight && candidate.end < best.end {
			best = candidate
		}
	}
	// The weight threshold amortizes the extra saved register and prologue load.
	// Tiny functions have bounded allocation competition and need the lower
	// threshold to cover a single hot loop access. Non-matching access ends derive their
	// adjusted limit from the cached bound with one LEA, so mixed displacements
	// no longer require an instance-memory reload.
	minimumWeight := uint64(16)
	if len(machine.Insts) <= 32 {
		minimumWeight = 8
	}
	if best.weight < minimumWeight {
		return 0, false
	}
	return best.end, true
}

func nativeAMD64CachesGlobals(machine *railmach.Func) bool {
	_, ok := nativeAMD64CachedGlobal(machine)
	return ok
}

func nativeAMD64CachesGlobalDescriptors(machine *railmach.Func) bool {
	if machine == nil || machine.Target != railmach.TargetAMD64 {
		return false
	}
	// Exceptionally large functions can cross backend ABI seams
	// compiled without a complete local contract. Keep the descriptor address
	// as an ordinary reloadable value instead of extending R12 across the entire
	// function in that mode.
	if len(machine.Insts) >= 16<<10 {
		return false
	}
	uses, hasCall := 0, false
	for _, instruction := range machine.Insts {
		hasCall = hasCall || railmach.IsCall(instruction.Op)
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		if semanticOp == wasm.InstrGlobalGet || semanticOp == wasm.InstrGlobalSet {
			uses++
		}
	}
	return nativeAMD64CachesGlobals(machine) || hasCall && uses >= 8
}

func nativeAMD64CachedGlobal(machine *railmach.Func) (uint32, bool) {
	if machine == nil || machine.Target != railmach.TargetAMD64 {
		return 0, false
	}
	for _, instruction := range machine.Insts {
		if railmach.IsCall(instruction.Op) {
			return 0, false
		}
	}
	bestIndex, bestUses, bestWeight := uint32(0), uint32(0), uint32(0)
	for instructionID, candidate := range machine.Insts {
		candidateOp := railmach.SemanticOpcode(candidate.Op)
		if candidateOp != wasm.InstrGlobalGet && candidateOp != wasm.InstrGlobalSet {
			continue
		}
		if candidateOp == wasm.InstrGlobalGet && candidate.Result != 0 && machine.VRegs[candidate.Result].Type == railmach.TypeV128 {
			continue
		}
		if candidateOp == wasm.InstrGlobalSet {
			operands := machine.InstructionOperands(uint32(instructionID))
			if len(operands) != 0 && machine.VRegs[operands[0].Reg].Type == railmach.TypeV128 {
				continue
			}
		}
		index, uses, weightedUses := uint32(candidate.Aux), uint32(0), uint32(0)
		for _, block := range machine.Blocks {
			for instructionID := block.InstStart; instructionID < block.InstStart+block.InstCount; instructionID++ {
				instruction := machine.Insts[instructionID]
				semanticOp := railmach.SemanticOpcode(instruction.Op)
				if (semanticOp != wasm.InstrGlobalGet && semanticOp != wasm.InstrGlobalSet) || uint32(instruction.Aux) != index {
					continue
				}
				uses++
				if weightedUses > ^uint32(0)-block.Weight {
					weightedUses = ^uint32(0)
					break
				}
				weightedUses += block.Weight
			}
		}
		if weightedUses > bestWeight || weightedUses == bestWeight && index < bestIndex {
			bestIndex, bestUses, bestWeight = index, uses, weightedUses
		}
	}
	// Profile weight scales the entry save/load/restore cost along with the
	// accesses, so require enough structural reuse within one invocation too.
	return bestIndex, bestUses >= 4 && bestWeight >= 16
}

func nativeAMD64StackCachedGlobals(stack *railssa.StackFunc, machine *railmach.Func) ([2]uint32, int) {
	if stack == nil || machine == nil || machine.Target != railmach.TargetAMD64 || !nativeAMD64CachesGlobalDescriptors(machine) {
		return [2]uint32{}, 0
	}
	type cost struct {
		reads uint64
		sets  uint64
	}
	costs := make([]cost, len(stack.Globals))
	var calls uint64
	for _, block := range machine.Blocks {
		weight := uint64(max(block.Weight, 1))
		for instructionID := block.InstStart; instructionID < block.InstStart+block.InstCount; instructionID++ {
			instruction := machine.Insts[instructionID]
			if railmach.IsCall(instruction.Op) {
				calls += weight
				continue
			}
			semanticOp := railmach.SemanticOpcode(instruction.Op)
			if semanticOp != wasm.InstrGlobalGet && semanticOp != wasm.InstrGlobalSet {
				continue
			}
			index := uint32(instruction.Aux)
			if int(index) >= len(stack.Globals) || stack.Globals[index] != wasm.I32 && stack.Globals[index] != wasm.I64 {
				continue
			}
			if semanticOp == wasm.InstrGlobalGet {
				costs[index].reads += weight
			} else {
				costs[index].sets += weight
			}
		}
	}
	if calls == 0 {
		return [2]uint32{}, 0
	}
	var selected [2]uint32
	selectedCount := 0
	for slot := range selected {
		bestIndex, bestBenefit := uint32(0), uint64(0)
		for index, candidate := range costs {
			alreadySelected := false
			for previous := 0; previous < slot; previous++ {
				alreadySelected = alreadySelected || uint32(index) == selected[previous]
			}
			// A cached read removes one descriptor load. Entry initialization and
			// each call refresh cost a descriptor load, value load, and frame store;
			// write-through sets add one frame store.
			cost := (calls+1)*3 + candidate.sets
			if !alreadySelected && candidate.reads > cost && candidate.reads-cost > bestBenefit {
				bestIndex, bestBenefit = uint32(index), candidate.reads-cost
			}
		}
		if bestBenefit < 8 {
			break
		}
		selected[slot] = bestIndex
		selectedCount++
	}
	return selected, selectedCount
}

func nativeAMD64HasDivision(machine *railmach.Func) bool {
	if machine == nil || machine.Target != railmach.TargetAMD64 {
		return false
	}
	for _, instruction := range machine.Insts {
		if kind := railmach.SemanticOpcode(instruction.Op); kind == wasm.InstrI32DivS || kind == wasm.InstrI32DivU || kind == wasm.InstrI32RemS || kind == wasm.InstrI32RemU ||
			kind == wasm.InstrI64DivS || kind == wasm.InstrI64DivU || kind == wasm.InstrI64RemS || kind == wasm.InstrI64RemU {
			return true
		}
	}
	return false
}

func nativeARM64CachesGlobals(machine *railmach.Func) bool {
	if machine == nil || machine.Target != railmach.TargetARM64 {
		return false
	}
	uses := 0
	for _, instruction := range machine.Insts {
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		if semanticOp == wasm.InstrGlobalGet || semanticOp == wasm.InstrGlobalSet {
			uses++
		}
	}
	return uses >= 8
}

func nativeARM64CachedGlobal(stack *railssa.StackFunc, machine *railmach.Func) (uint32, bool) {
	globals, count := nativeARM64CachedGlobals(stack, machine)
	return globals[0], count != 0
}

func nativeARM64CachedGlobals(stack *railssa.StackFunc, machine *railmach.Func) ([2]uint32, int) {
	if stack == nil || !nativeARM64CachesGlobals(machine) {
		return [2]uint32{}, 0
	}
	type uses struct {
		all  int
		sets int
	}
	hasCall := false
	counts := make([]uses, len(stack.Globals))
	for _, instruction := range machine.Insts {
		hasCall = hasCall || railmach.IsCall(instruction.Op)
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		if semanticOp != wasm.InstrGlobalGet && semanticOp != wasm.InstrGlobalSet {
			continue
		}
		index := uint32(instruction.Aux)
		if int(index) >= len(stack.Globals) || stack.Globals[index] != wasm.I32 && stack.Globals[index] != wasm.I64 {
			continue
		}
		counts[index].all++
		if semanticOp == wasm.InstrGlobalSet {
			counts[index].sets++
		}
	}
	var selected [2]uint32
	selectedCount := 0
	for slot := range selected {
		bestIndex, bestUses := uint32(0), 0
		for index, count := range counts {
			alreadySelected := false
			for previous := 0; previous < slot; previous++ {
				alreadySelected = alreadySelected || uint32(index) == selected[previous]
			}
			if !alreadySelected && count.sets != 0 && count.all > bestUses {
				bestIndex, bestUses = uint32(index), count.all
			}
		}
		if !hasCall || bestUses < 8 {
			break
		}
		selected[slot] = bestIndex
		selectedCount++
	}
	return selected, selectedCount
}

func buildNativeEdgeConstantRematerialization(plan *nativeBackendPlan, skipped *nativeBitSet, uses []uint32) {
	if plan == nil || plan.Machine == nil || plan.Allocation == nil || plan.Exit == nil {
		return
	}
	const edgeMovesFlag = uint32(1 << 31)
	// Subtract edge uses from the complete use counts. A value whose count
	// reaches zero is used exclusively by edge transfers.
	for _, transfer := range plan.Machine.Transfers {
		if int(transfer.Src) < len(uses) && uses[transfer.Src] != 0 && uses[transfer.Src] < edgeMovesFlag {
			uses[transfer.Src]--
		}
	}
	// Encode the number of physical copies an edge-only value must have in the
	// high-bit-marked scratch count. Values with non-edge uses remain unmarked.
	for _, transfer := range plan.Machine.Transfers {
		if int(transfer.Src) >= len(uses) {
			continue
		}
		state := uses[transfer.Src]
		if state == 0 {
			uses[transfer.Src] = edgeMovesFlag | 1
		} else if state&edgeMovesFlag != 0 && state != ^uint32(0) {
			uses[transfer.Src]++
		}
	}
	// Consume one expected edge copy for each valid physical move. Any invalid
	// or excess move clears the candidate marker just as the former per-value
	// scan rejected the producer.
	for _, move := range plan.Exit.Moves {
		if int(move.Reg) >= len(uses) || uses[move.Reg]&edgeMovesFlag == 0 {
			continue
		}
		if int(move.Reg) >= len(plan.Allocation.Locations) || move.Kind != railmach.MoveCopy || move.Src != plan.Allocation.Locations[move.Reg] || uses[move.Reg] == edgeMovesFlag {
			uses[move.Reg] = 1
			continue
		}
		uses[move.Reg]--
	}
	for producerID, producer := range plan.Machine.Insts {
		if producer.Result != 0 && (producer.Op == wasm.InstrI32Const || producer.Op == wasm.InstrI64Const) && !skipped.has(uint32(producerID)) && int(producer.Result) < len(uses) && uses[producer.Result] == edgeMovesFlag {
			skipped.set(uint32(producerID), true)
		}
	}
	countNativeMachineUses(plan.Machine, uses)
}

func buildNativeARM64LogicalImmediateCombinations(plan *nativeBackendPlan, producers *nativeInstructionRelation, skipped *nativeBitSet, uses []uint32) {
	if plan == nil || plan.Machine == nil {
		return
	}
	countNativeMachineUses(plan.Machine, uses)
	for instructionID := range plan.Machine.Insts {
		producerID, ok := producers.get(uint32(instructionID))
		if !ok || int(producerID) >= len(plan.Machine.Insts) {
			continue
		}
		producer := plan.Machine.Insts[producerID]
		if producer.Result != 0 && uses[producer.Result] != 0 {
			uses[producer.Result]--
			if uses[producer.Result] == 0 {
				skipped.set(producerID, true)
			}
		}
	}
	for instructionID, instruction := range plan.Machine.Insts {
		if producers.has(uint32(instructionID)) {
			continue
		}
		operands := plan.Machine.InstructionOperands(uint32(instructionID))
		if len(operands) != 2 {
			continue
		}
		constant := plan.Machine.VRegs[operands[1].Reg]
		if constant.Flags&railmach.VRegRematerializable == 0 || int(constant.Def/6) >= len(plan.Machine.Insts) {
			continue
		}
		producerID := constant.Def / 6
		producer := plan.Machine.Insts[producerID]
		if (producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const) || !arm64RepeatedImmediateEncodable(instruction.Op, producer.Aux) {
			continue
		}
		// Shifted or sign-inverted add/sub immediates save code, but changing a
		// small/cold block's byte phase can perturb a later compact loop more than
		// the fold is worth. Restrict this extended class to repeatedly hot blocks;
		// ordinary 12-bit immediates and logical immediates retain their old scope.
		if producer.Aux > 0xfff && arm64AddSubKind(instruction.Op) && nativeMachineInstructionWeight(plan.Machine, uint32(instructionID)) < 64 {
			continue
		}
		producers.set(uint32(instructionID), producerID)
		if uses[producer.Result] != 0 {
			uses[producer.Result]--
			if uses[producer.Result] == 0 {
				skipped.set(producerID, true)
			}
		}
	}
	countNativeMachineUses(plan.Machine, uses)
}

func arm64AddSubKind(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Add || kind == wasm.InstrI64Add || kind == wasm.InstrI32Sub || kind == wasm.InstrI64Sub
}

func nativeMachineInstructionWeight(machine *railmach.Func, instruction uint32) uint32 {
	if machine == nil {
		return 0
	}
	for _, block := range machine.Blocks {
		if instruction >= block.InstStart && instruction < block.InstStart+block.InstCount {
			return block.Weight
		}
	}
	return 0
}

func arm64RepeatedImmediateEncodable(kind wasm.InstrKind, value uint64) bool {
	var probe arm64.Asm
	switch kind {
	case wasm.InstrI32Add:
		return arm64I32AddSubImmediateEncodable(uint32(value), false)
	case wasm.InstrI32Sub:
		return arm64I32AddSubImmediateEncodable(uint32(value), true)
	case wasm.InstrI64Add:
		return arm64I64AddSubImmediateEncodable(value, false)
	case wasm.InstrI64Sub:
		return arm64I64AddSubImmediateEncodable(value, true)
	case wasm.InstrI32Eq, wasm.InstrI32Ne, wasm.InstrI32LtS, wasm.InstrI32LtU,
		wasm.InstrI32GtS, wasm.InstrI32GtU, wasm.InstrI32LeS, wasm.InstrI32LeU,
		wasm.InstrI32GeS, wasm.InstrI32GeU,
		wasm.InstrI64Eq, wasm.InstrI64Ne, wasm.InstrI64LtS, wasm.InstrI64LtU,
		wasm.InstrI64GtS, wasm.InstrI64GtU, wasm.InstrI64LeS, wasm.InstrI64LeU,
		wasm.InstrI64GeS, wasm.InstrI64GeU:
		return value <= 4095
	case wasm.InstrI32And:
		return probe.AndImm32(arm64.X0, arm64.X1, uint32(value))
	case wasm.InstrI64And:
		return probe.AndImm64(arm64.X0, arm64.X1, value)
	case wasm.InstrI32Or:
		return probe.OrrImm32(arm64.X0, arm64.X1, uint32(value))
	case wasm.InstrI64Or:
		return probe.OrrImm64(arm64.X0, arm64.X1, value)
	case wasm.InstrI32Xor:
		return probe.EorImm32(arm64.X0, arm64.X1, uint32(value))
	case wasm.InstrI64Xor:
		return probe.EorImm64(arm64.X0, arm64.X1, value)
	default:
		return false
	}
}

func arm64AddSubImmediateMagnitude(value uint64) (immediate uint32, shifted bool, ok bool) {
	if value <= 0xfff {
		return uint32(value), false, true
	}
	if value&0xfff == 0 && value>>12 <= 0xfff {
		return uint32(value), true, true
	}
	return 0, false, false
}

func arm64I32AddSubImmediateEncodable(value uint32, subtract bool) bool {
	effective := value
	if subtract {
		effective = -effective
	}
	_, _, direct := arm64AddSubImmediateMagnitude(uint64(effective))
	_, _, inverse := arm64AddSubImmediateMagnitude(uint64(-effective))
	return direct || inverse
}

func arm64I64AddSubImmediateEncodable(value uint64, subtract bool) bool {
	effective := value
	if subtract {
		effective = -effective
	}
	_, _, direct := arm64AddSubImmediateMagnitude(effective)
	_, _, inverse := arm64AddSubImmediateMagnitude(-effective)
	return direct || inverse
}

func nativeValueCannotCreateCollectorEdge(machine *railmach.Func, value railmach.VReg) bool {
	if machine == nil || value == 0 || int(value) >= len(machine.VRegs) {
		return false
	}
	definition := machine.VRegs[value].Def
	if definition%6 != 3 || int(definition/6) >= len(machine.Insts) {
		return false
	}
	producer := machine.Insts[definition/6]
	semanticOp := railmach.SemanticOpcode(producer.Op)
	return producer.Result == value && (semanticOp == wasm.InstrRefNull || semanticOp == wasm.InstrRefI31)
}

// preparePostRAScratch retains only the instruction-indexed tables consumed by
// rewrites present for this target. Most functions realize one rewrite family;
// allocating every table made that bounded plan needlessly footprint-heavy.
func (p *nativeBackendPlanner) preparePostRAScratch(target railmach.Target, instructions int, rewrites []railmach.Rewrite) bool {
	needsPair, needsSkip, needsForward := false, false, false
	needsFusion, needsMemory, needsRepeat, needsPreIndex, needsPostIndex := false, false, false, false, false
	for _, rewrite := range rewrites {
		switch rewrite.Kind {
		case railmach.RewriteARM64Pair:
			if target == railmach.TargetARM64 {
				needsPair, needsSkip = true, true
			}
		case railmach.RewriteLoadStoreForward:
			needsSkip, needsForward = true, true
		case railmach.RewriteAMD64FusionRepair:
			needsFusion = target == railmach.TargetAMD64 || needsFusion
		case railmach.RewriteARM64CompareBranch:
			needsFusion = target == railmach.TargetARM64 || needsFusion
		case railmach.RewriteARM64CompareSelect:
			needsFusion = target == railmach.TargetARM64 || needsFusion
		case railmach.RewritePhysicalRename:
			needsFusion = target == railmach.TargetAMD64 || target == railmach.TargetARM64 || needsFusion
		case railmach.RewriteAMD64MemoryFold:
			if target == railmach.TargetAMD64 {
				needsSkip, needsMemory = true, true
			}
		case railmach.RewriteAMD64ByteSwap:
			if target == railmach.TargetAMD64 {
				needsSkip = true
			}
		case railmach.RewriteARM64RepeatedAdd:
			if target == railmach.TargetARM64 {
				needsSkip, needsRepeat = true, true
			}
		case railmach.RewriteARM64ByteWiden:
			if target == railmach.TargetARM64 {
				needsSkip = true
			}
		case railmach.RewriteARM64ByteSwap:
			if target == railmach.TargetARM64 {
				needsSkip = true
			}
		case railmach.RewriteARM64Narrow16To8:
			if target == railmach.TargetARM64 {
				needsSkip = true
			}
		case railmach.RewriteARM64LogicalShift:
			if target == railmach.TargetARM64 {
				needsSkip = true
			}
		case railmach.RewriteARM64BitmaskPopcnt:
			if target == railmach.TargetARM64 {
				needsSkip = true
			}
		case railmach.RewriteARM64PrePostIndex:
			if target == railmach.TargetARM64 {
				needsPreIndex = true
				needsPostIndex = rewrite.Second != ^uint32(0) || needsPostIndex
			}
		}
	}
	p.postRAPairWith.prepare(instructions, needsPair)
	p.postRASkip.prepare(instructions, needsSkip)
	p.postRAForwardFrom.prepare(instructions, needsForward)
	p.postRAFusionWith.prepare(instructions, needsFusion)
	p.postRAMemoryFrom.prepare(instructions, needsMemory)
	p.postRARepeatFirst.prepare(instructions, needsRepeat)
	p.postRAPreIndex.prepare(instructions, needsPreIndex)
	p.postRAPostIndexWith.prepare(instructions, needsPostIndex)
	return needsPair || needsSkip || needsForward || needsFusion || needsMemory || needsRepeat || needsPreIndex || needsPostIndex
}

func nativeCallArgumentBytes(machine *railmach.Func) uint32 {
	maxSlots := uint32(0)
	for instructionID, instruction := range machine.Insts {
		if !railmach.IsCall(instruction.Op) {
			continue
		}
		argumentSlots := uint32(0)
		for _, operand := range machine.InstructionOperands(uint32(instructionID)) {
			argumentSlots += uint32(machine.VRegs[operand.Reg].Type.SpillSlotUnits())
		}
		resultSlots := uint32(0)
		for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
			resultSlots += uint32(machine.VRegs[instruction.Result+railmach.VReg(ordinal)].Type.SpillSlotUnits())
		}
		slots := max(argumentSlots, resultSlots)
		maxSlots = max(maxSlots, slots)
	}
	return (maxSlots*8 + 15) &^ 15
}

func nativeCallClobberOverrides(machine *railmach.Func, imported uint32, contracts []railmach.ABIContract, components []int, refinedRecursive []bool, caller int, config railmach.GreedyConfig) []railmach.CallClobber {
	if caller < 0 || len(contracts) == 0 {
		return nil
	}
	var overrides []railmach.CallClobber
	for instructionID, instruction := range machine.Insts {
		if railmach.SemanticOpcode(instruction.Op) != wasm.InstrCall || uint32(instruction.Aux) < imported {
			continue
		}
		callee := int(uint32(instruction.Aux) - imported)
		if callee < 0 || callee >= len(contracts) || sameUnrefinedRecursiveComponent(components, refinedRecursive, caller, callee) {
			continue
		}
		if contracts[callee].Class == 0 {
			// A local function without a RailMach contract uses the structured
			// private emitter, whose working register set is not described by
			// RailMach's caller/callee partition. Keep call-live values out of
			// every allocatable register until an exact contract is available.
			overrides = append(overrides, railmach.CallClobber{
				Instruction: uint32(instructionID),
				GPR:         callerRegisterMask(config.Linear.GPRs),
				FPR:         callerRegisterMask(config.Linear.FPRs),
			})
			continue
		}
		contract := contracts[callee]
		overrides = append(overrides, railmach.CallClobber{
			Instruction: uint32(instructionID),
			GPR:         contract.GPRClobbers & config.CallerMask(railmach.BankGPR),
			FPR:         contract.FPRClobbers & config.CallerMask(railmach.BankFPR),
		})
	}
	return overrides
}

func nativeFunctionHasRecursiveCall(machine *railmach.Func, imported uint32, components []int, caller int) bool {
	if machine == nil || caller < 0 || caller >= len(components) {
		return false
	}
	for _, instruction := range machine.Insts {
		if railmach.SemanticOpcode(instruction.Op) != wasm.InstrCall || uint32(instruction.Aux) < imported {
			continue
		}
		callee := int(uint32(instruction.Aux) - imported)
		if callee >= 0 && callee < len(components) && components[callee] == components[caller] {
			return true
		}
	}
	return false
}

func refineNativeCallContracts(calls []railmach.CallContract, imported uint32, contracts []railmach.ABIContract, components []int, refinedRecursive []bool, caller int) uint32 {
	var refined uint32
	for index := range calls {
		call := &calls[index]
		if call.Callee < imported {
			continue
		}
		callee := int(call.Callee - imported)
		if callee < 0 || callee >= len(contracts) || contracts[callee].Class == 0 || sameUnrefinedRecursiveComponent(components, refinedRecursive, caller, callee) {
			continue
		}
		contract := contracts[callee]
		call.GPRClobbers, call.FPRClobbers, call.Class, call.Conservative = contract.GPRClobbers, contract.FPRClobbers, contract.Class, false
		call.WritesGlobal = contract.WritesGlobal
		call.MayGrow = contract.MayGrow
		refined++
	}
	return refined
}

func sameUnrefinedRecursiveComponent(components []int, refined []bool, caller, callee int) bool {
	same := caller >= 0 && caller < len(components) && callee >= 0 && callee < len(components) && components[caller] == components[callee]
	return same && !(caller < len(refined) && callee < len(refined) && refined[caller] && refined[callee])
}

func callerRegisterMask(count uint8) uint64 {
	if count >= 64 {
		return ^uint64(0)
	}
	return uint64(1)<<count - 1
}

func nativeObligationRequired(plan *nativeBackendPlan, instruction uint32, obligation railssa.ObligationMask) bool {
	return plan == nil || plan.Simplified == nil || int(instruction) >= len(plan.Simplified.Remaining) || plan.Simplified.Remaining[instruction]&obligation != 0
}

// nativeIndirectTarget returns a verifier-proven local target attached to the
// exact machine instruction. The source/op checks keep finalization independent
// of RailMach's current one-to-one semantic instruction lowering.
func nativeIndirectTarget(plan *nativeBackendPlan, instructionID uint32) (uint32, bool) {
	if plan == nil || plan.Stack == nil || plan.Semantic == nil || plan.Machine == nil || plan.Specialize == nil || int(instructionID) >= len(plan.Machine.Insts) {
		return 0, false
	}
	machine := plan.Machine.Insts[instructionID]
	if railmach.SemanticOpcode(machine.Op) != wasm.InstrCallIndirect {
		return 0, false
	}
	for _, entry := range plan.Specialize.Entries {
		if entry.Kind != railssa.SpecializeIndirectTarget || int(entry.Instruction) >= len(plan.Semantic.Insts) {
			continue
		}
		semantic := plan.Semantic.Insts[entry.Instruction]
		if semantic.Op == wasm.InstrCallIndirect && semantic.Source == machine.Source && entry.Target >= plan.Stack.ImportedFuncs && entry.Target < plan.Stack.FuncCount {
			return entry.Target, true
		}
	}
	return 0, false
}

// nativeDenseLocalTableTargets proves a small table is a fixed, dense vector of
// local functions. It deliberately accepts only the simple active-element form:
// the bounded proof is then sufficient for a dynamic selector to branch to the
// private Dragline ABI without publishing that ABI through a funcref descriptor.
func nativeDenseLocalTableTargets(m *wasm.Module) ([]uint32, bool) {
	if m == nil || m.ImportedTableCount() != 0 || len(m.Tables) != 1 || m.Tables[0].Init != nil ||
		m.Tables[0].Type.Limits.Min == 0 || m.Tables[0].Type.Limits.Min > 32 {
		return nil, false
	}
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable {
			return nil, false
		}
	}
	for local := range m.Code {
		stack, err := railssa.BuildStackFunc(m, local)
		if err != nil {
			return nil, false
		}
		for _, instruction := range stack.Instrs {
			switch instruction.Kind {
			case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableFill:
				return nil, false
			}
		}
	}
	if len(m.Elements) != 1 {
		return nil, false
	}
	element := m.Elements[0]
	if element.Mode.Kind != wasm.ElemActive || element.Mode.Table != 0 || !nativeZeroI32ConstExpr(element.Mode.Offset) ||
		element.Kind.Kind != wasm.ElemFuncs || uint64(len(element.Kind.Funcs)) != m.Tables[0].Type.Limits.Min {
		return nil, false
	}
	targets := make([]uint32, len(element.Kind.Funcs))
	imports := uint32(m.ImportedFuncCount())
	for index, target := range element.Kind.Funcs {
		global := uint32(target)
		if global < imports || global-imports >= uint32(len(m.Code)) {
			return nil, false
		}
		targets[index] = global
	}
	return targets, true
}

func nativeARM64PreparedIndirect(stack *railssa.StackFunc, machine *railmach.Func, allocation *railmach.GreedyAllocation) bool {
	if stack == nil || machine == nil || allocation == nil || machine.Target != railmach.TargetARM64 || len(stack.Params) != 3 || len(stack.Results) != 1 ||
		stack.Params[0] != wasm.I32 || stack.Params[1] != wasm.I32 || stack.Params[2] != wasm.I32 || stack.Results[0] != wasm.I32 || len(machine.Insts) != 1 || railmach.SemanticOpcode(machine.Insts[0].Op) != wasm.InstrCallIndirect {
		return false
	}
	for local := uint16(0); local < machine.ParamCount; local++ {
		found := false
		for value := railmach.VReg(1); int(value) < len(machine.VRegs); value++ {
			data := machine.VRegs[value]
			if data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != local {
				continue
			}
			location := allocation.Locations[value]
			if data.Bank != railmach.BankGPR || location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR || location.Index != local {
				return false
			}
			found = true
			break
		}
		if !found {
			return false
		}
	}
	targets, ok := nativeDenseLocalTableTargets(stack.Module)
	const maxPreparedIndirectTargets = 16
	if !ok || len(targets) == 0 || len(targets) > maxPreparedIndirectTargets {
		return false
	}
	expected := uint32(machine.Insts[0].Aux)
	for _, target := range targets {
		typeIndex, ok := stack.Module.FuncTypeIndex(target)
		if !ok || typeIndex.Rec || typeIndex.Index != expected {
			return false
		}
		if _, ok := nativeInlineI32BinaryTarget(stack.Module, target); !ok {
			return false
		}
	}
	return true
}

func nativeZeroI32ConstExpr(expr wasm.Expr) bool {
	if len(expr.Instrs) != 0 {
		return len(expr.Instrs) == 1 && expr.Instrs[0].Kind == wasm.InstrI32Const && expr.Instrs[0].I32 == 0
	}
	return len(expr.BodyBytes) == 3 && expr.BodyBytes[0] == 0x41 && expr.BodyBytes[1] == 0 && expr.BodyBytes[2] == 0x0b
}

func nativeInlineI32BinaryTarget(m *wasm.Module, target uint32) (wasm.InstrKind, bool) {
	if m == nil {
		return wasm.InstrInvalid, false
	}
	local := int(target) - m.ImportedFuncCount()
	if local < 0 || local >= len(m.Code) {
		return wasm.InstrInvalid, false
	}
	typ, ok := m.LocalFuncType(local)
	if !ok || len(typ.Params) != 2 || typ.Params[0] != wasm.I32 || typ.Params[1] != wasm.I32 || len(typ.Results) != 1 || typ.Results[0] != wasm.I32 {
		return wasm.InstrInvalid, false
	}
	stack, err := railssa.BuildStackFunc(m, local)
	if err != nil {
		return wasm.InstrInvalid, false
	}
	var semantic [3]railssa.StackInstr
	count := 0
	for _, instruction := range stack.Instrs {
		if instruction.Kind == wasm.InstrInvalid || instruction.Kind == wasm.InstrNop {
			continue
		}
		if count == len(semantic) {
			return wasm.InstrInvalid, false
		}
		semantic[count] = instruction
		count++
	}
	if count != len(semantic) || semantic[0].Kind != wasm.InstrLocalGet || semantic[0].U32() != 0 ||
		semantic[1].Kind != wasm.InstrLocalGet || semantic[1].U32() != 1 {
		return wasm.InstrInvalid, false
	}
	switch semantic[2].Kind {
	case wasm.InstrI32Add:
		return wasm.InstrI32Add, true
	case wasm.InstrI32Sub:
		return wasm.InstrI32Sub, true
	case wasm.InstrI32Mul:
		return wasm.InstrI32Mul, true
	case wasm.InstrI32And:
		return wasm.InstrI32And, true
	case wasm.InstrI32Or:
		return wasm.InstrI32Or, true
	case wasm.InstrI32Xor:
		return wasm.InstrI32Xor, true
	default:
		return wasm.InstrInvalid, false
	}
}

func nativeDivisorMayBeMinusOne(plan *nativeBackendPlan, operand railmach.VReg) bool {
	if plan == nil || plan.Simplified == nil || int(operand) >= len(plan.Machine.VRegs) {
		return true
	}
	fact := plan.Simplified.IntegerFactAt(railssa.FlowValueID(operand))
	if !fact.Known {
		return true
	}
	if plan.Machine.VRegs[operand].Type == railmach.TypeI32 {
		return uint32(fact.Min) == ^uint32(0)
	}
	return fact.Min == ^uint64(0)
}

func nativeARM64PairRealizable(machine *railmach.Func, allocation *railmach.GreedyAllocation, first, second uint32) bool {
	if machine == nil || allocation == nil || int(first) >= len(machine.Insts) || int(second) >= len(machine.Insts) {
		return false
	}
	a, b := machine.Insts[first], machine.Insts[second]
	size := uint64(0)
	switch a.Op {
	case wasm.InstrI32Load, wasm.InstrI32Store, wasm.InstrF32Load, wasm.InstrF32Store:
		size = 4
	case wasm.InstrI64Load, wasm.InstrI64Store, wasm.InstrF64Load, wasm.InstrF64Store:
		size = 8
	default:
		return false
	}
	if b.Op != a.Op || uint64(uint32(a.Aux))+size != uint64(uint32(b.Aux)) || uint64(uint32(a.Aux))%size != 0 || uint64(uint32(a.Aux))/size > 63 {
		return false
	}
	aOperands, bOperands := machine.InstructionOperands(first), machine.InstructionOperands(second)
	if len(aOperands) == 0 || len(bOperands) == 0 || aOperands[0].Reg != bOperands[0].Reg || allocation.Locations[aOperands[0].Reg].Kind != railmach.LocationRegister {
		return false
	}
	load := a.Op == wasm.InstrI32Load || a.Op == wasm.InstrI64Load || a.Op == wasm.InstrF32Load || a.Op == wasm.InstrF64Load
	wantBank := railmach.BankGPR
	if a.Op == wasm.InstrF32Load || a.Op == wasm.InstrF64Load || a.Op == wasm.InstrF32Store || a.Op == wasm.InstrF64Store {
		wantBank = railmach.BankFPR
	}
	if !load {
		return false
	}
	return a.Result != 0 && b.Result != 0 &&
		allocation.Locations[a.Result].Kind == railmach.LocationRegister && allocation.Locations[a.Result].Bank == wantBank &&
		allocation.Locations[b.Result].Kind == railmach.LocationRegister && allocation.Locations[b.Result].Bank == wantBank
}

func nativeARM64RepeatedAddRealizable(machine *railmach.Func, schedule *railmach.Schedule, allocation *railmach.GreedyAllocation, first, last uint32) bool {
	if machine == nil || schedule == nil || allocation == nil || len(allocation.Fragments) != 0 {
		return false
	}
	initial, invariant, _, ok := railmach.VerifyARM64RepeatedAddChain(machine, schedule, first, last)
	if !ok {
		return false
	}
	firstPosition := allocation.InstructionPositions[first]
	lastPosition := allocation.InstructionPositions[last]
	position := firstPosition*6 + 2
	initialLocation := allocation.LocationAt(initial, position)
	invariantLocation := allocation.LocationAt(invariant, position)
	if initialLocation.Kind != railmach.LocationRegister || initialLocation.Bank != railmach.BankGPR || invariantLocation.Kind != railmach.LocationRegister || invariantLocation.Bank != railmach.BankGPR || initialLocation.Index == invariantLocation.Index {
		return false
	}
	lastInstruction := machine.Insts[last]
	lastAt := lastPosition*6 + 2
	lastLocation := allocation.LocationAt(lastInstruction.Result, lastAt)
	return lastInstruction.Result != 0 && lastLocation.Kind == railmach.LocationRegister && lastLocation.Bank == railmach.BankGPR && allocation.LocationAt(invariant, lastAt) == invariantLocation
}

func nativeARM64LogicalShiftRealizable(machine *railmach.Func, schedule *railmach.Schedule, allocation *railmach.GreedyAllocation, producer, consumer uint32) bool {
	if machine == nil || schedule == nil || allocation == nil || int(producer) >= len(machine.Insts) || int(consumer) >= len(machine.Insts) || int(consumer) >= len(allocation.InstructionPositions) || !planInstructionsAdjacent(schedule, producer, consumer) {
		return false
	}
	shiftOperands := machine.InstructionOperands(producer)
	if len(shiftOperands) != 2 {
		return false
	}
	position := allocation.InstructionPositions[consumer]*6 + 2
	baseLocation := allocation.LocationAt(shiftOperands[0].Reg, position)
	resultLocation := allocation.LocationAt(machine.Insts[consumer].Result, position)
	return baseLocation.Kind == railmach.LocationRegister && baseLocation.Bank == railmach.BankGPR &&
		resultLocation.Kind == railmach.LocationRegister && resultLocation.Bank == railmach.BankGPR
}

func nativeARM64BitmaskPopcntRealizable(machine *railmach.Func, schedule *railmach.Schedule, allocation *railmach.GreedyAllocation, producer, consumer uint32) bool {
	if machine == nil || schedule == nil || allocation == nil || int(producer) >= len(machine.Insts) || int(consumer) >= len(machine.Insts) || !planInstructionsAdjacent(schedule, producer, consumer) {
		return false
	}
	source, result, ok := railmach.VerifyARM64BitmaskPopcnt(machine, schedule, producer, consumer)
	if !ok {
		return false
	}
	producerPosition := allocation.InstructionPositions[producer]*6 + 2
	consumerPosition := allocation.InstructionPositions[consumer]*6 + 2
	sourceLocation := allocation.LocationAt(source, producerPosition)
	resultLocation := allocation.LocationAt(result, consumerPosition)
	return sourceLocation.Kind == railmach.LocationRegister && sourceLocation.Bank == railmach.BankFPR &&
		resultLocation.Kind == railmach.LocationRegister && resultLocation.Bank == railmach.BankGPR
}

func resizeNativeSlice[T any](values []T, length int) []T {
	if cap(values) < length {
		return make([]T, length)
	}
	return values[:length]
}

func nativeControlInstruction(kind wasm.InstrKind) bool {
	kind = railmach.SemanticOpcode(kind)
	return kind == wasm.InstrIf || kind == wasm.InstrBr || kind == wasm.InstrBrIf ||
		kind == wasm.InstrBrTable || kind == wasm.InstrReturn || kind == wasm.InstrUnreachable
}

func nativeBlockEdgePair(plan *nativeBackendPlan, block uint32) (first, second uint32, count int) {
	for edgeID, edge := range plan.Machine.Edges {
		if uint32(edge.From) == block {
			if count == 0 {
				first = uint32(edgeID)
			} else if count == 1 {
				second = uint32(edgeID)
			}
			count++
		}
	}
	return
}

// nativeBranchTableEdge resolves one source-order br_table label back to the
// deduplicated machine edge. CFG edge order cannot encode table case order.
func nativeBranchTableEdge(plan *nativeBackendPlan, block uint32, label uint32) (uint32, bool) {
	if plan == nil || int(block) >= len(plan.CFG.Blocks) {
		return 0, false
	}
	region := plan.CFG.Blocks[block].Region
	for label != 0 && region != railssa.NoRegion {
		region = plan.Stack.Regions[region].Parent
		label--
	}
	targetInstruction := uint32(len(plan.Stack.Instrs))
	if region != railssa.NoRegion {
		target := plan.Stack.Regions[region]
		if target.Kind == wasm.InstrLoop {
			targetInstruction = target.StartInstr + 1
		} else {
			targetInstruction = target.EndInstr + 1
		}
	} else if label != 0 {
		return 0, false
	}
	targetBlock := uint32(len(plan.CFG.Blocks))
	for blockID, candidate := range plan.CFG.Blocks {
		if candidate.InstStart == targetInstruction {
			targetBlock = uint32(blockID)
			break
		}
	}
	if int(targetBlock) >= len(plan.CFG.Blocks) {
		return 0, false
	}
	for edgeID, edge := range plan.Machine.Edges {
		if uint32(edge.From) == block && uint32(edge.To) == targetBlock {
			return uint32(edgeID), true
		}
	}
	return 0, false
}

// nativeSuccessorEntryEdge returns the sole incoming edge when its physical
// copy bundle has been moved to the successor entry by LateSSAExit.
func nativeSuccessorEntryEdge(plan *nativeBackendPlan, block uint32) (uint32, bool) {
	if plan == nil || plan.Machine == nil || plan.Exit == nil {
		return 0, false
	}
	found, have := uint32(0), false
	for edgeID, edge := range plan.Machine.Edges {
		if uint32(edge.To) != block {
			continue
		}
		if have {
			return 0, false
		}
		found, have = uint32(edgeID), true
	}
	if !have || int(found) >= len(plan.Exit.EdgeMoves) {
		return 0, false
	}
	moves := plan.Exit.EdgeMoves[found]
	for _, move := range plan.Exit.Moves[moves.Start : moves.Start+moves.Count] {
		if move.Placement == railmach.PlaceSuccessorStart {
			return found, true
		}
	}
	return 0, false
}

// nativeRailMachExitRegisterSafe reports whether the current production
// finalizers can realize every late-SSA move. Spills, rematerializations, and
// fixed-register repairs remain verifier-valid shadow products, but must fall
// back before native emission until their physical realization is implemented.
//
//lint:ignore U1000 retained for staged native exit admission
func nativeRailMachExitRegisterSafe(plan *nativeBackendPlan, gprs, fprs int) bool {
	if plan == nil || plan.Exit == nil {
		return false
	}
	for index, moveRange := range plan.Exit.FixedMoves {
		if moveRange.Count != 0 && !nativeFixedPointHandledInline(plan, plan.Exit.FixedPoints[index]) &&
			!nativePhysicalMoveRangeRegisterSafe(plan, moveRange, gprs, fprs) {
			return false
		}
	}
	for _, moveRange := range plan.Exit.EdgeMoves {
		if !nativePhysicalMoveRangeRegisterSafe(plan, moveRange, gprs, fprs) {
			return false
		}
	}
	return true
}

//lint:ignore U1000 retained for staged native exit admission
func nativePhysicalMoveRangeRegisterSafe(plan *nativeBackendPlan, moveRange railmach.MoveRange, gprs, fprs int) bool {
	if uint64(moveRange.Start)+uint64(moveRange.Count) > uint64(len(plan.Exit.Moves)) {
		return false
	}
	for _, move := range plan.Exit.Moves[moveRange.Start : moveRange.Start+moveRange.Count] {
		if move.Kind == railmach.MoveRematerialize || move.Src.Kind == railmach.LocationSpill || move.Dst.Kind == railmach.LocationSpill {
			return false
		}
		if move.Kind != railmach.MoveCopy && move.Kind != railmach.MoveSaveTemporary && move.Kind != railmach.MoveRestoreTemporary {
			return false
		}
		for _, location := range [...]railmach.Location{move.Src, move.Dst} {
			if location.Kind != railmach.LocationRegister {
				continue
			}
			if location.Bank == railmach.BankGPR && int(location.Index) >= gprs ||
				location.Bank == railmach.BankFPR && int(location.Index) >= fprs ||
				location.Bank != railmach.BankGPR && location.Bank != railmach.BankFPR {
				return false
			}
		}
	}
	return true
}

//lint:ignore U1000 retained for staged native exit admission
func nativeFixedPointHandledInline(plan *nativeBackendPlan, position uint32) bool {
	for instructionID, logical := range plan.Allocation.InstructionPositions {
		if logical*6+2 == position {
			return railmach.IsCall(plan.Machine.Insts[instructionID].Op)
		}
	}
	return false
}

func nativeFixedMoveRange(plan *nativeBackendPlan, instructionID uint32) (railmach.MoveRange, bool) {
	position := plan.Allocation.InstructionPositions[instructionID]*6 + 2
	for index, point := range plan.Exit.FixedPoints {
		if point == position {
			return plan.Exit.FixedMoves[index], true
		}
	}
	return railmach.MoveRange{}, false
}

func nativeCallTargetSafe(plan *nativeBackendPlan, instructionID uint32) bool {
	position := plan.Allocation.InstructionPositions[instructionID]*6 + 2
	config := railmach.DefaultGreedyConfig(plan.Machine.Target)
	instruction := plan.Machine.Insts[instructionID]
	gprClobbers, fprClobbers := config.CallerMask(railmach.BankGPR), config.CallerMask(railmach.BankFPR)
	for _, call := range plan.Calls {
		if call.Instruction == instructionID && !call.Conservative {
			gprClobbers = call.GPRClobbers & config.CallerMask(railmach.BankGPR)
			fprClobbers = call.FPRClobbers & config.CallerMask(railmach.BankFPR)
			break
		}
	}
	for _, interval := range plan.Allocation.Intervals {
		if interval.Start >= position || interval.End <= position || !plan.Allocation.IntervalContains(interval, position) {
			continue
		}
		location := plan.Allocation.Locations[interval.Reg]
		if location.Kind == railmach.LocationSpill || location.Kind == railmach.LocationRematerialize {
			continue
		}
		if location.Kind != railmach.LocationRegister {
			return false
		}
		if interval.Bank == railmach.BankGPR {
			if location.Index < 64 && gprClobbers&(uint64(1)<<location.Index) != 0 {
				return false
			}
			continue
		}
		if interval.Bank != railmach.BankFPR || location.Index < 64 && fprClobbers&(uint64(1)<<location.Index) != 0 {
			return false
		}
		// AMD64 platform callees may clobber every XMM. The production frame
		// carries a bounded save area for the private callee-region registers
		// represented by ExternalCallFPRs.
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		external := semanticOp != wasm.InstrCall && railmach.IsCall(instruction.Op) || semanticOp == wasm.InstrCall && uint32(instruction.Aux) < plan.Stack.ImportedFuncs
		if plan.Machine.Target == railmach.TargetAMD64 && external && plan.ExternalCallFPRs&(uint64(1)<<location.Index) == 0 {
			return false
		}
	}
	return true
}
