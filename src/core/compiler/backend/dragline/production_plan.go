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
	// HelperSafepointBase is the module-global ID assigned to the first
	// allocating runtime-helper instruction in this function.
	HelperSafepointBase uint32
	// CallArgumentBytes is the fixed caller-owned canonical argument/result
	// vector prefix. External-call FPR saves follow it in Frame.CallAreaBytes.
	CallArgumentBytes uint32
	Score             railmach.ScheduleScore
	BackendAttempts   uint8
	IPRARefinedCalls  uint32
	SignalsBounds     bool

	BlockOffsets        []int
	BranchPatches       []nativeBranchPatch
	ConditionalPatches  []nativeBranchPatch
	ColdTrapPatches     []nativeBranchPatch
	MemoryCheckEnds     []uint64
	MemoryCheckTouched  []railmach.VReg
	PostRAPairWith      []uint32
	PostRASkip          []bool
	PostRAForwardFrom   []uint32
	PostRAFusionWith    []uint32
	PostRAMemoryFrom    []uint32
	PostRARepeatFirst   []uint32
	PostRAPreIndex      []bool
	PostRAPostIndexWith []uint32
	ImmediateProducer   []uint32
	ImmediateSkip       []bool
	DeadGCReservations  []bool
	NoBarrierGCStores   []bool
}

type nativeBranchPatch struct {
	At     int
	Target uint32
}

func clearPostRAEmissionRewrites(plan *nativeBackendPlan) {
	plan.PostRAPairWith = nil
	plan.PostRASkip = nil
	plan.PostRAForwardFrom = nil
	plan.PostRAFusionWith = nil
	plan.PostRAMemoryFrom = nil
	plan.PostRARepeatFirst = nil
	plan.PostRAPreIndex = nil
	plan.PostRAPostIndexWith = nil
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
	memoryCheckEnds     []uint64
	memoryCheckTouched  []railmach.VReg
	postRAPairWith      []uint32
	postRASkip          []bool
	postRAForwardFrom   []uint32
	postRAFusionWith    []uint32
	postRAMemoryFrom    []uint32
	postRARepeatFirst   []uint32
	postRAPreIndex      []bool
	postRAPostIndexWith []uint32
	immediateProducer   []uint32
	immediateSkip       []bool
	immediateUses       []uint32
	deadGCReservations  []bool
	noBarrierGCStores   []bool
	parallelCandidates  bool
	candidateScratch    *[2]nativeCandidateWorkspace
	plan                nativeBackendPlan
}

type nativeCandidateWorkspace struct {
	schedule   railmach.Schedule
	allocation railmach.GreedyAllocation
	exit       railmach.SSAExit
}

type nativeCandidateRef struct {
	schedule   *railmach.Schedule
	allocation *railmach.GreedyAllocation
	exit       *railmach.SSAExit
}

func (p *nativeBackendPlanner) evaluateScheduleCandidates(machine *railmach.Func, selection *railmach.SelectionPlan, dag *railmach.DependencyDAG, pressure *railssa.PressurePlan, greedy railmach.GreedyConfig, kinds [3]railmach.ScheduleKind, parallel bool) ([3]railmach.ScheduleScore, [3]error) {
	if p.candidateScratch == nil {
		p.candidateScratch = new([2]nativeCandidateWorkspace)
	}
	refs := [3]nativeCandidateRef{
		{schedule: &p.schedule, allocation: &p.allocation, exit: &p.exit},
		{schedule: &p.candidateScratch[0].schedule, allocation: &p.candidateScratch[0].allocation, exit: &p.candidateScratch[0].exit},
		{schedule: &p.candidateScratch[1].schedule, allocation: &p.candidateScratch[1].allocation, exit: &p.candidateScratch[1].exit},
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
		scores[index], errs[index] = railmach.ScoreVerifiedScheduleCandidate(machine, selection, candidate, allocation, exit)
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

func (p *nativeBackendPlanner) capacityBreakdown() (ssa, machine, native uint64) {
	if p == nil {
		return 0, 0, 0
	}
	ssa = railssa.PipelineCapacityBytes(&p.cfg, &p.locals, &p.flow, &p.semantic, &p.metadata, &p.simplified, &p.pressure, &p.specialize, &p.emission)
	machine = railmach.PipelineCapacityBytes(&p.machine, &p.selection, &p.dag, &p.schedule, &p.allocation, &p.exit, &p.postRA, &p.remat, &p.layout)
	if p.candidateScratch != nil {
		for index := range p.candidateScratch {
			candidate := &p.candidateScratch[index]
			machine += railmach.PipelineCapacityBytes(nil, nil, nil, &candidate.schedule, &candidate.allocation, &candidate.exit, nil, nil, nil)
		}
	}
	native = sliceBytes(p.edgeWeights) + sliceBytes(p.edgeObserved) + sliceBytes(p.blockBytes) + sliceBytes(p.coldBlocks) + sliceBytes(p.calleeSaveRegions) + sliceBytes(p.blockOffsets) + sliceBytes(p.branchPatches) + sliceBytes(p.conditionalPatches) + sliceBytes(p.coldTrapPatches) + sliceBytes(p.memoryCheckEnds) + sliceBytes(p.memoryCheckTouched) + sliceBytes(p.postRAPairWith) + sliceBytes(p.postRASkip) + sliceBytes(p.postRAForwardFrom) + sliceBytes(p.postRAFusionWith) + sliceBytes(p.postRAMemoryFrom) + sliceBytes(p.postRARepeatFirst) + sliceBytes(p.postRAPreIndex) + sliceBytes(p.postRAPostIndexWith) + sliceBytes(p.immediateProducer) + sliceBytes(p.immediateSkip) + sliceBytes(p.immediateUses) + sliceBytes(p.deadGCReservations) + sliceBytes(p.noBarrierGCStores) + sliceBytes(p.plan.Calls) + sliceBytes(p.rootPlan.Sites) + sliceBytes(p.rootPlan.Roots) + sliceBytes(p.gcValues)
	return ssa, machine, native
}

func railMachCandidate(stack *railssa.StackFunc, moduleHasV128 bool) bool {
	if stack == nil {
		return false
	}
	if stack.MaxLoopDepth != 0 {
		calls := 0
		for _, instruction := range stack.Instrs {
			if instruction.Kind == wasm.InstrCall || instruction.Kind == wasm.InstrCallIndirect {
				calls++
			}
		}
		largeModule := stack.Module != nil && len(stack.Module.Code) > 256
		if (moduleHasV128 || largeModule) && calls > 1 && len(stack.Instrs) > 256 {
			// In SIMD modules, large loop values that cross several local calls
			// still expose an incomplete RailMach edge/live-range contract even
			// when the caller itself is scalar. Retain the same bounded policy
			// for large modules so admission cannot multiply compile latency.
			// Small scalar-only modules use complete scalar edge refinement.
			return false
		}
	}
	// RailMach's scalar edge-refinement identity is complete, but its machine
	// value contract intentionally has no 128-bit register class yet. Keep mixed
	// SIMD/branch-cast functions on the structured SIMD emitter.
	if stack.HasV128 && len(stack.BranchCasts) != 0 {
		return false
	}
	if stack.HasV128 && structuredV128ManagedCandidate(stack) {
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
	// The established structured emitter has stronger lowering for the common
	// arithmetic/control loop shapes. RailMach is already faster for the target
	// operations below, whose structured lowering otherwise falls behind the
	// baseline. Keep this opcode policy explicit and source-derived: it applies to
	// arbitrary verified functions and is not keyed to a benchmark identity.
	loopNeedsRailMach := false
	loopI32Eqz := 0
	loopHasWrap, loopHasI32And, loopHasReinterpret := false, false, false
	loopHasSaturatingConversion, loopHasBulkMemory := false, false
	for _, instruction := range stack.Instrs {
		if stack.MaxLoopDepth != 0 {
			loopHasWrap = loopHasWrap || instruction.Kind == wasm.InstrI32WrapI64
			loopHasI32And = loopHasI32And || instruction.Kind == wasm.InstrI32And
			loopHasReinterpret = loopHasReinterpret || instruction.Kind == wasm.InstrI32ReinterpretF32 || instruction.Kind == wasm.InstrI64ReinterpretF64 || instruction.Kind == wasm.InstrF32ReinterpretI32 || instruction.Kind == wasm.InstrF64ReinterpretI64
			loopHasSaturatingConversion = loopHasSaturatingConversion || instruction.Kind >= wasm.InstrI32TruncSatF32S && instruction.Kind <= wasm.InstrI64TruncSatF64U
			loopHasBulkMemory = loopHasBulkMemory || instruction.Kind == wasm.InstrMemoryCopy || instruction.Kind == wasm.InstrMemoryFill
			if instruction.Kind == wasm.InstrI32Eqz {
				loopI32Eqz++
			}
			switch instruction.Kind {
			case wasm.InstrI64Eqz,
				wasm.InstrI64Add, wasm.InstrI64Mul,
				wasm.InstrI64Load, wasm.InstrI64Store,
				wasm.InstrGlobalGet, wasm.InstrGlobalSet,
				wasm.InstrF32Sqrt, wasm.InstrF64Sqrt,
				wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
				wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
				wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U,
				wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U,
				wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
				wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S,
				wasm.InstrF32Eq, wasm.InstrF64Eq, wasm.InstrF32Ne, wasm.InstrF64Ne,
				wasm.InstrF32Lt, wasm.InstrF64Lt, wasm.InstrF32Gt, wasm.InstrF64Gt,
				wasm.InstrF32Le, wasm.InstrF64Le, wasm.InstrF32Ge, wasm.InstrF64Ge,
				wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
				wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
				wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
				wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
				wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU,
				wasm.InstrF32Copysign, wasm.InstrF64Copysign,
				wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
				wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
				wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
				wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
				wasm.InstrF32Load, wasm.InstrF64Load,
				wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
				wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U,
				wasm.InstrI64Load32S, wasm.InstrI64Load32U,
				wasm.InstrMemorySize, wasm.InstrMemoryGrow,
				wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
				loopNeedsRailMach = true
			}
		}
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
	loopNeedsRailMach = loopNeedsRailMach || loopI32Eqz > 1 || loopHasWrap && loopHasI32And && !loopHasReinterpret
	if stack.MaxLoopDepth != 0 && !loopNeedsRailMach {
		return false
	}
	return true
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

func buildNativeImmediateCombinations(plan *nativeBackendPlan, producers []uint32, skipped []bool, uses []uint32) {
	for index := range producers {
		producers[index] = ^uint32(0)
	}
	clear(skipped)
	clear(uses)
	for instructionID := range plan.Machine.Insts {
		for _, operand := range plan.Machine.InstructionOperands(uint32(instructionID)) {
			uses[operand.Reg]++
		}
	}
	for _, combination := range plan.Selection.Combinations {
		if combination.Kind != railmach.CombineImmediate || combination.Producer == ^uint32(0) || int(combination.Producer) >= len(plan.Machine.Insts) || int(combination.Consumer) >= len(plan.Machine.Insts) {
			continue
		}
		producer := plan.Machine.Insts[combination.Producer]
		if producer.Result == 0 || uses[producer.Result] != 1 || producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const {
			continue
		}
		producers[combination.Consumer] = combination.Producer
		skipped[combination.Producer] = true
	}
	for producerID, producer := range plan.Machine.Insts {
		if producer.Result == 0 || producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const || skipped[producerID] {
			continue
		}
		foldedUses := uint32(0)
		for consumerID, consumer := range plan.Machine.Insts {
			operands := plan.Machine.InstructionOperands(uint32(consumerID))
			if len(operands) != 2 || operands[1].Reg != producer.Result {
				continue
			}
			switch consumer.Op {
			case wasm.InstrI32Shl, wasm.InstrI64Shl,
				wasm.InstrI32ShrS, wasm.InstrI64ShrS,
				wasm.InstrI32ShrU, wasm.InstrI64ShrU,
				wasm.InstrI32Rotl, wasm.InstrI64Rotl,
				wasm.InstrI32Rotr, wasm.InstrI64Rotr:
				producers[consumerID] = uint32(producerID)
				foldedUses++
			}
		}
		if uses[producer.Result] != 0 && foldedUses == uses[producer.Result] && !nativeMachineValueEscapes(plan.Machine, producer.Result) {
			skipped[producerID] = true
		}
	}
}

func nativeMachineValueEscapes(machine *railmach.Func, value railmach.VReg) bool {
	for _, transfer := range machine.Transfers {
		if transfer.Src == value || transfer.Dst == value {
			return true
		}
	}
	for _, result := range machine.Results {
		if result == value {
			return true
		}
	}
	return false
}

func applyNativeARM64ShiftImmediateRematerialization(machine *railmach.Func) {
	if machine == nil || machine.Target != railmach.TargetARM64 {
		return
	}
	for _, producer := range machine.Insts {
		if producer.Result == 0 || producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const || nativeMachineValueEscapes(machine, producer.Result) {
			continue
		}
		uses, eligible := uint32(0), uint32(0)
		for instructionID, consumer := range machine.Insts {
			operands := machine.InstructionOperands(uint32(instructionID))
			for operandIndex, operand := range operands {
				if operand.Reg != producer.Result {
					continue
				}
				uses++
				if operandIndex != 1 || len(operands) != 2 {
					continue
				}
				switch consumer.Op {
				case wasm.InstrI32Shl, wasm.InstrI64Shl,
					wasm.InstrI32ShrS, wasm.InstrI64ShrS,
					wasm.InstrI32ShrU, wasm.InstrI64ShrU,
					wasm.InstrI32Rotl, wasm.InstrI64Rotl,
					wasm.InstrI32Rotr, wasm.InstrI64Rotr:
					eligible++
				}
			}
		}
		if uses == 0 || eligible != uses {
			continue
		}
		for instructionID := range machine.Insts {
			operands := machine.InstructionOperands(uint32(instructionID))
			for index := range operands {
				if operands[index].Reg == producer.Result {
					operands[index].Flags |= railmach.OperandColdRemat
				}
			}
		}
	}
}

// nativeIntegerConstant returns the exact defining constant for a machine SSA
// value. Finalizers use this only for semantics-preserving target rewrites; the
// definition/result checks keep the decision independently auditable instead
// of trusting rematerialization flags alone.
func nativeIntegerConstant(plan *nativeBackendPlan, value railmach.VReg) (uint64, bool) {
	if plan == nil || plan.Machine == nil || value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return 0, false
	}
	definition := plan.Machine.VRegs[value].Def
	if definition < 3 || (definition-3)%6 != 0 {
		return 0, false
	}
	instructionID := (definition - 3) / 6
	if int(instructionID) >= len(plan.Machine.Insts) {
		return 0, false
	}
	instruction := plan.Machine.Insts[instructionID]
	if instruction.Result != value || instruction.Op != wasm.InstrI32Const && instruction.Op != wasm.InstrI64Const {
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

func nativeScheduleScoreBetter(objective corecompiler.OptimizationObjective, instructions int, candidate, retained railmach.ScheduleScore) bool {
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
	return candidate.BetterThan(retained)
}

func railMachPhysicalLiveAcross(plan *nativeBackendPlan, instructionID uint32, bank railmach.Bank, physical uint16) bool {
	position := plan.Allocation.InstructionPositions[instructionID]*6 + 2
	for _, interval := range plan.Allocation.Intervals {
		location := plan.Allocation.Locations[interval.Reg]
		if interval.Bank == bank && location.Kind == railmach.LocationRegister && location.Index == physical && interval.Start < position && interval.End > position {
			return true
		}
	}
	return false
}

func nativeExternalCallFPRMask(stack *railssa.StackFunc, machine *railmach.Func, allocation *railmach.GreedyAllocation) uint64 {
	if stack == nil || machine == nil || allocation == nil || machine.Target != railmach.TargetAMD64 {
		return 0
	}
	var mask uint64
	for instructionID, instruction := range machine.Insts {
		external := instruction.Op != wasm.InstrCall && railmach.IsCall(instruction.Op) || instruction.Op == wasm.InstrCall && uint32(instruction.Aux) < stack.ImportedFuncs
		if !external {
			continue
		}
		position := allocation.InstructionPositions[instructionID]*6 + 2
		for _, interval := range allocation.Intervals {
			if interval.Bank != railmach.BankFPR || interval.Start >= position || interval.End <= position {
				continue
			}
			location := allocation.Locations[interval.Reg]
			if location.Kind == railmach.LocationRegister && location.Index < 64 {
				mask |= uint64(1) << location.Index
			}
		}
	}
	return mask
}

func nativeMemoryAccess(kind wasm.InstrKind) (size int, signed, store, ok bool) {
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
	applyNativeARM64ShiftImmediateRematerialization(machine)
	dag, err := railmach.BuildDependencyDAG(machine, selection, metadata, &p.dag)
	if err != nil {
		return nil, err
	}
	defaultGreedy := railmach.DefaultGreedyConfig(machineTarget)
	defaultGreedy.CallClobbers = nativeCallClobberOverrides(machine, stack.ImportedFuncs, moduleContracts, components, refinedRecursive, localIndex, defaultGreedy)
	bestGreedy := defaultGreedy
	var best railmach.ScheduleScore
	haveBest := false
	bestIndex := 0
	parallelCandidates := p.parallelCandidates && len(machine.Insts) >= 1024
	// Evaluate the commonly retained source-stable candidate last. Candidate
	// scoring is order-independent (Kind is the deterministic final tie-break),
	// so its verified products can be consumed directly when it wins instead of
	// rebuilding a fourth identical schedule/allocation/exit chain.
	kinds := [3]railmach.ScheduleKind{railmach.ScheduleKindLatencyFusion, railmach.ScheduleKindPressure, railmach.ScheduleKindSourceStable}
	if parallelCandidates {
		scores, candidateErrs := p.evaluateScheduleCandidates(machine, selection, dag, pressure, defaultGreedy, kinds, true)
		for index, score := range scores {
			if candidateErrs[index] != nil {
				return nil, candidateErrs[index]
			}
			if !haveBest || nativeScheduleScoreBetter(objective, len(machine.Insts), score, best) {
				best, bestIndex, haveBest = score, index, true
			}
		}
	} else {
		for _, kind := range kinds {
			candidate, candidateErr := railmach.BuildScheduleWithPressure(machine, selection, dag, kind, pressure, &p.schedule)
			if candidateErr != nil {
				return nil, candidateErr
			}
			candidateAllocation, candidateErr := railmach.AllocateGreedyPForSchedule(machine, candidate, defaultGreedy, &p.allocation)
			if candidateErr != nil {
				return nil, candidateErr
			}
			candidateExit, candidateErr := railmach.LateSSAExitVerifiedAllocation(machine, &candidateAllocation.Allocation, &p.exit)
			if candidateErr != nil {
				return nil, candidateErr
			}
			score, candidateErr := railmach.ScoreVerifiedScheduleCandidate(machine, selection, candidate, candidateAllocation, candidateExit)
			if candidateErr != nil {
				return nil, candidateErr
			}
			if !haveBest || nativeScheduleScoreBetter(objective, len(machine.Insts), score, best) {
				best, haveBest = score, true
			}
		}
	}
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
	if decision := railmach.DecideRetry(0, allocation, exit.Debt); decision.Retry {
		backendAttempts = railmach.MaxBackendAttempts
		retryGreedy := defaultGreedy
		retryGreedy.PreserveGPRCost = 0
		retryGreedy.PreserveFPRCost = 0
		retryBest := best
		retryKind := best.Kind
		retryIndex := 0
		improved := false
		retryKinds := [3]railmach.ScheduleKind{railmach.ScheduleKindSourceStable, railmach.ScheduleKindLatencyFusion, railmach.ScheduleKindPressure}
		if parallelCandidates {
			retryScores, retryErrs := p.evaluateScheduleCandidates(machine, selection, dag, pressure, retryGreedy, retryKinds, true)
			for index, candidateScore := range retryScores {
				if retryErrs[index] != nil {
					return nil, retryErrs[index]
				}
				if nativeScheduleScoreBetter(objective, len(machine.Insts), candidateScore, retryBest) {
					retryBest, retryKind, retryIndex, improved = candidateScore, retryKinds[index], index, true
				}
			}
		} else {
			for _, kind := range retryKinds {
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
				candidateScore, retryErr := railmach.ScoreVerifiedScheduleCandidate(machine, selection, candidate, candidateAllocation, candidateExit)
				if retryErr != nil {
					return nil, retryErr
				}
				if nativeScheduleScoreBetter(objective, len(machine.Insts), candidateScore, retryBest) {
					retryBest, retryKind, improved = candidateScore, kind, true
				}
			}
		}
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
	postRA, err := railmach.PlanPostRAVerifiedAllocation(machineTarget, machine, selection, schedule, allocation, exit, &p.postRA)
	if err != nil {
		return nil, err
	}
	hasPostRARealization := p.preparePostRAScratch(machineTarget, len(machine.Insts), postRA.Rewrites)
	if hasPostRARealization {
		for _, rewrite := range postRA.Rewrites {
			switch rewrite.Kind {
			case railmach.RewriteARM64Pair:
				if machineTarget != railmach.TargetARM64 {
					continue
				}
				if p.postRASkip[rewrite.First] || p.postRAPairWith[rewrite.Second] != 0 || !nativeARM64PairRealizable(machine, allocation, rewrite.First, rewrite.Second) {
					continue
				}
				p.postRAPairWith[rewrite.First] = rewrite.Second + 1
				p.postRASkip[rewrite.Second] = true
			case railmach.RewriteLoadStoreForward:
				if !p.postRASkip[rewrite.First] && !p.postRASkip[rewrite.Second] {
					p.postRAForwardFrom[rewrite.Second] = rewrite.First + 1
				}
			case railmach.RewriteAMD64FusionRepair:
				if machineTarget == railmach.TargetAMD64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) {
					p.postRAFusionWith[rewrite.First] = rewrite.Second + 1
					p.postRAFusionWith[rewrite.Second] = rewrite.First + 1
				}
			case railmach.RewriteARM64CompareBranch:
				if machineTarget == railmach.TargetARM64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) {
					p.postRAFusionWith[rewrite.First] = rewrite.Second + 1
					p.postRAFusionWith[rewrite.Second] = rewrite.First + 1
				}
			case railmach.RewritePhysicalRename:
				if machineTarget == railmach.TargetAMD64 || machineTarget == railmach.TargetARM64 {
					p.postRAFusionWith[rewrite.First] = rewrite.Second + 1
					p.postRAFusionWith[rewrite.Second] = rewrite.First + 1
				}
			case railmach.RewriteARM64PrePostIndex:
				if machineTarget != railmach.TargetARM64 || len(p.postRASkip) != 0 && (p.postRASkip[rewrite.First] || rewrite.Second != ^uint32(0) && p.postRASkip[rewrite.Second]) {
					continue
				}
				if rewrite.Second == ^uint32(0) && (len(p.postRAPostIndexWith) == 0 || p.postRAPostIndexWith[rewrite.First] == 0) {
					p.postRAPreIndex[rewrite.First] = true
				} else if planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) && p.postRAPostIndexWith[rewrite.First] == 0 && p.postRAPostIndexWith[rewrite.Second] == 0 {
					p.postRAPostIndexWith[rewrite.First] = rewrite.Second + 1
					p.postRAPostIndexWith[rewrite.Second] = rewrite.First + 1
					p.postRAPreIndex[rewrite.First] = false
					p.postRAPreIndex[rewrite.Second] = false
				}
			case railmach.RewriteAMD64MemoryFold:
				if machineTarget == railmach.TargetAMD64 && planInstructionsAdjacent(schedule, rewrite.First, rewrite.Second) && !p.postRASkip[rewrite.First] && !p.postRASkip[rewrite.Second] {
					p.postRAMemoryFrom[rewrite.Second] = rewrite.First + 1
					p.postRASkip[rewrite.First] = true
				}
			case railmach.RewriteARM64RepeatedAdd:
				if machineTarget != railmach.TargetARM64 || !nativeARM64RepeatedAddRealizable(machine, schedule, allocation, rewrite.First, rewrite.Second) {
					continue
				}
				p.postRARepeatFirst[rewrite.Second] = rewrite.First + 1
				firstPosition := allocation.InstructionPositions[rewrite.First]
				lastPosition := allocation.InstructionPositions[rewrite.Second]
				for instructionID, position := range allocation.InstructionPositions {
					if position >= firstPosition && position < lastPosition {
						p.postRASkip[instructionID] = true
					}
				}
			}
		}
	}
	contract, calls, err := railmach.AnalyzeVerifiedABI(machine, allocation, metadata, stack.ImportedFuncs)
	if err != nil {
		return nil, err
	}
	localContract := contract
	refinedCalls := refineNativeCallContracts(calls, stack.ImportedFuncs, moduleContracts, components, refinedRecursive, localIndex)
	railmach.PropagateCallClobbers(&contract, calls, defaultGreedy)
	callArgumentBytes := nativeCallArgumentBytes(machine)
	requirements, frame, err := railmach.FrameForAllocation(contract, allocation, callArgumentBytes/8)
	if err != nil {
		return nil, err
	}
	externalCallFPRs := nativeExternalCallFPRMask(stack, machine, allocation)
	if p.rootPlan.SlotCount != 0 || externalCallFPRs != 0 {
		requirements.RootSlots = p.rootPlan.SlotCount
		requirements.CallAreaBytes += uint32(bits.OnesCount64(externalCallFPRs)) * 8
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
		p.coldBlocks = p.coldBlocks[:0]
		p.calleeSaveRegions = p.calleeSaveRegions[:0]
	}
	p.plan = nativeBackendPlan{
		Stack: stack, CFG: cfg, Semantic: semantic,
		Machine: machine, Selection: selection, DAG: dag, Schedule: schedule, Allocation: allocation, Exit: exit, PostRA: postRA,
		Specialize: specialize, Roots: &p.rootPlan, Emission: emission, Pressure: pressure, Remat: remat, Layout: layout, ABI: contract, LocalABI: localContract, Calls: calls, Frame: frame, CalleeSaves: p.calleeSaveRegions, ExternalCallFPRs: externalCallFPRs, CallArgumentBytes: callArgumentBytes, Score: best, BackendAttempts: backendAttempts,
		Simplified: simplified, IPRARefinedCalls: refinedCalls,
		PostRAPairWith: p.postRAPairWith, PostRASkip: p.postRASkip,
		PostRAForwardFrom:   p.postRAForwardFrom,
		PostRAFusionWith:    p.postRAFusionWith,
		PostRAMemoryFrom:    p.postRAMemoryFrom,
		PostRARepeatFirst:   p.postRARepeatFirst,
		PostRAPreIndex:      p.postRAPreIndex,
		PostRAPostIndexWith: p.postRAPostIndexWith,
	}
	p.immediateProducer = resizeNativeSlice(p.immediateProducer, len(machine.Insts))
	p.immediateSkip = resizeNativeSlice(p.immediateSkip, len(machine.Insts))
	p.immediateUses = resizeNativeSlice(p.immediateUses, len(machine.VRegs))
	buildNativeImmediateCombinations(&p.plan, p.immediateProducer, p.immediateSkip, p.immediateUses)
	if machine.Target == railmach.TargetARM64 {
		buildNativeARM64LogicalImmediateCombinations(&p.plan, p.immediateProducer, p.immediateSkip, p.immediateUses)
	}
	p.plan.ImmediateProducer = p.immediateProducer
	p.plan.ImmediateSkip = p.immediateSkip
	for _, transfer := range machine.Transfers {
		p.immediateUses[transfer.Src]++
	}
	for _, result := range machine.Results {
		p.immediateUses[result]++
	}
	buildNativeEdgeConstantRematerialization(&p.plan, p.immediateSkip, p.immediateUses)
	p.deadGCReservations = resizeNativeSlice(p.deadGCReservations, len(machine.Insts))
	clear(p.deadGCReservations)
	for instructionID, instruction := range machine.Insts {
		if instruction.Result == 0 || p.immediateUses[instruction.Result] != 0 {
			continue
		}
		switch instruction.Op {
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
	p.plan.DeadGCReservations = p.deadGCReservations
	p.noBarrierGCStores = resizeNativeSlice(p.noBarrierGCStores, len(machine.Insts))
	clear(p.noBarrierGCStores)
	for instructionID, instruction := range machine.Insts {
		operands := machine.InstructionOperands(uint32(instructionID))
		var child railmach.VReg
		switch instruction.Op {
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
	p.memoryCheckEnds = resizeNativeSlice(p.memoryCheckEnds, len(machine.VRegs))
	clear(p.memoryCheckEnds)
	p.memoryCheckTouched = resizeNativeSlice(p.memoryCheckTouched, len(machine.Insts))[:0]
	p.plan.BlockOffsets = p.blockOffsets
	p.plan.BranchPatches = p.branchPatches
	p.plan.ConditionalPatches = p.conditionalPatches
	p.plan.ColdTrapPatches = p.coldTrapPatches
	p.plan.MemoryCheckEnds = p.memoryCheckEnds
	p.plan.MemoryCheckTouched = p.memoryCheckTouched
	return &p.plan, nil
}

func buildNativeEdgeConstantRematerialization(plan *nativeBackendPlan, skipped []bool, uses []uint32) {
	if plan == nil || plan.Machine == nil || plan.Allocation == nil || plan.Exit == nil {
		return
	}
	for producerID, producer := range plan.Machine.Insts {
		if producer.Result == 0 || (producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const) || skipped[producerID] {
			continue
		}
		transfers := 0
		for _, transfer := range plan.Machine.Transfers {
			if transfer.Src == producer.Result {
				transfers++
			}
		}
		if int(producer.Result) >= len(uses) || uses[producer.Result] != uint32(transfers) {
			continue
		}
		moves := 0
		for _, move := range plan.Exit.Moves {
			if move.Reg != producer.Result {
				continue
			}
			if move.Kind != railmach.MoveCopy || move.Src != plan.Allocation.Locations[producer.Result] {
				moves = -1
				break
			}
			moves++
		}
		if transfers != 0 && moves == transfers {
			skipped[producerID] = true
		}
	}
}

func buildNativeARM64LogicalImmediateCombinations(plan *nativeBackendPlan, producers []uint32, skipped []bool, uses []uint32) {
	if plan == nil || plan.Machine == nil {
		return
	}
	for instructionID, instruction := range plan.Machine.Insts {
		if producers[instructionID] != ^uint32(0) {
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
		if (producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const) || !arm64LogicalImmediateEncodable(instruction.Op, producer.Aux) {
			continue
		}
		producers[instructionID] = producerID
	}
	for producerID, producer := range plan.Machine.Insts {
		if producer.Result == 0 || (producer.Op != wasm.InstrI32Const && producer.Op != wasm.InstrI64Const) || skipped[producerID] || nativeMachineValueEscapes(plan.Machine, producer.Result) {
			continue
		}
		folded := uint32(0)
		for consumerID := range plan.Machine.Insts {
			operands := plan.Machine.InstructionOperands(uint32(consumerID))
			if len(operands) == 2 && operands[1].Reg == producer.Result && producers[consumerID] == uint32(producerID) {
				folded++
			}
		}
		if uses[producer.Result] != 0 && folded == uses[producer.Result] {
			skipped[producerID] = true
		}
	}
}

func arm64LogicalImmediateEncodable(kind wasm.InstrKind, value uint64) bool {
	var probe arm64.Asm
	switch kind {
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

func nativeValueCannotCreateCollectorEdge(machine *railmach.Func, value railmach.VReg) bool {
	if machine == nil || value == 0 || int(value) >= len(machine.VRegs) {
		return false
	}
	definition := machine.VRegs[value].Def
	if definition%6 != 3 || int(definition/6) >= len(machine.Insts) {
		return false
	}
	producer := machine.Insts[definition/6]
	return producer.Result == value && (producer.Op == wasm.InstrRefNull || producer.Op == wasm.InstrRefI31)
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
		case railmach.RewritePhysicalRename:
			needsFusion = target == railmach.TargetAMD64 || target == railmach.TargetARM64 || needsFusion
		case railmach.RewriteAMD64MemoryFold:
			if target == railmach.TargetAMD64 {
				needsSkip, needsMemory = true, true
			}
		case railmach.RewriteARM64RepeatedAdd:
			if target == railmach.TargetARM64 {
				needsSkip, needsRepeat = true, true
			}
		case railmach.RewriteARM64PrePostIndex:
			if target == railmach.TargetARM64 {
				needsPreIndex = true
				needsPostIndex = rewrite.Second != ^uint32(0) || needsPostIndex
			}
		}
	}
	prepare := func(values []uint32, needed bool) []uint32 {
		if !needed {
			return values[:0]
		}
		values = resizeNativeSlice(values, instructions)
		clear(values)
		return values
	}
	prepareBool := func(values []bool, needed bool) []bool {
		if !needed {
			return values[:0]
		}
		values = resizeNativeSlice(values, instructions)
		clear(values)
		return values
	}
	p.postRAPairWith = prepare(p.postRAPairWith, needsPair)
	p.postRASkip = prepareBool(p.postRASkip, needsSkip)
	p.postRAForwardFrom = prepare(p.postRAForwardFrom, needsForward)
	p.postRAFusionWith = prepare(p.postRAFusionWith, needsFusion)
	p.postRAMemoryFrom = prepare(p.postRAMemoryFrom, needsMemory)
	p.postRARepeatFirst = prepare(p.postRARepeatFirst, needsRepeat)
	p.postRAPreIndex = prepareBool(p.postRAPreIndex, needsPreIndex)
	p.postRAPostIndexWith = prepare(p.postRAPostIndexWith, needsPostIndex)
	return needsPair || needsSkip || needsForward || needsFusion || needsMemory || needsRepeat || needsPreIndex || needsPostIndex
}

func nativeCallArgumentBytes(machine *railmach.Func) uint32 {
	maxSlots := uint32(0)
	for instructionID, instruction := range machine.Insts {
		if !railmach.IsCall(instruction.Op) {
			continue
		}
		slots := uint32(len(machine.InstructionOperands(uint32(instructionID))))
		slots = max(slots, instruction.ResultCount())
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
		if instruction.Op != wasm.InstrCall || uint32(instruction.Aux) < imported {
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
			GPR:         contract.GPRClobbers & callerRegisterMask(config.CallerGPRs),
			FPR:         contract.FPRClobbers & callerRegisterMask(config.CallerFPRs),
		})
	}
	return overrides
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
	if machine.Op != wasm.InstrCallIndirect {
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
	body := m.Code[local].BodyBytes
	if len(body) != 6 || body[0] != 0x20 || body[1] != 0 || body[2] != 0x20 || body[3] != 1 || body[5] != 0x0b {
		return wasm.InstrInvalid, false
	}
	switch body[4] {
	case 0x6a:
		return wasm.InstrI32Add, true
	case 0x6b:
		return wasm.InstrI32Sub, true
	case 0x6c:
		return wasm.InstrI32Mul, true
	case 0x71:
		return wasm.InstrI32And, true
	case 0x72:
		return wasm.InstrI32Or, true
	case 0x73:
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

func resizeNativeSlice[T any](values []T, length int) []T {
	if cap(values) < length {
		return make([]T, length)
	}
	return values[:length]
}

func nativeControlInstruction(kind wasm.InstrKind) bool {
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
	gprClobbers, fprClobbers := callerRegisterMask(config.CallerGPRs), callerRegisterMask(config.CallerFPRs)
	for _, call := range plan.Calls {
		if call.Instruction == instructionID && !call.Conservative {
			gprClobbers = call.GPRClobbers & callerRegisterMask(config.CallerGPRs)
			fprClobbers = call.FPRClobbers & callerRegisterMask(config.CallerFPRs)
			break
		}
	}
	for _, interval := range plan.Allocation.Intervals {
		if interval.Start >= position || interval.End <= position {
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
		external := instruction.Op != wasm.InstrCall && railmach.IsCall(instruction.Op) || instruction.Op == wasm.InstrCall && uint32(instruction.Aux) < plan.Stack.ImportedFuncs
		if plan.Machine.Target == railmach.TargetAMD64 && external && plan.ExternalCallFPRs&(uint64(1)<<location.Index) == 0 {
			return false
		}
	}
	return true
}
