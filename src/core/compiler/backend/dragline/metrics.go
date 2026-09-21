package dragline

import (
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

const MetricsVersion = 30

// ScheduleDiagnosticKind selects one scheduler for an opt-in metrics compile.
// Zero preserves production selection. A forced kind applies only to functions
// that would normally evaluate schedule alternatives; tiny or dependency-linear
// functions retain their normal source-stable fast path.
type ScheduleDiagnosticKind uint8

const (
	ScheduleDiagnosticAuto ScheduleDiagnosticKind = iota
	ScheduleDiagnosticSourceStable
	ScheduleDiagnosticLatencyFusion
	ScheduleDiagnosticPressure
)

func (kind ScheduleDiagnosticKind) valid() bool {
	return kind <= ScheduleDiagnosticPressure
}

func (kind ScheduleDiagnosticKind) machineKind() railmach.ScheduleKind {
	switch kind {
	case ScheduleDiagnosticSourceStable:
		return railmach.ScheduleKindSourceStable
	case ScheduleDiagnosticLatencyFusion:
		return railmach.ScheduleKindLatencyFusion
	case ScheduleDiagnosticPressure:
		return railmach.ScheduleKindPressure
	default:
		return 0
	}
}

func diagnosticScheduleOverride(metrics *Metrics) railmach.ScheduleKind {
	if metrics == nil {
		return 0
	}
	return metrics.ScheduleOverride.machineKind()
}

// Metrics contains one deterministic row per compiled function plus module
// totals. Timings are observational; all counts and byte sizes are exact for
// the compiler-owned slices tracked by the current pipeline.
type Metrics struct {
	Version           uint32                 `json:"version"`
	TargetFingerprint [32]byte               `json:"target_fingerprint"`
	ScheduleOverride  ScheduleDiagnosticKind `json:"schedule_override,omitempty"`
	Functions         []FunctionMetrics      `json:"functions"`

	FinalizeNanos              int64  `json:"finalize_nanos"`
	TotalNanos                 int64  `json:"total_nanos"`
	PeakLiveBytes              uint64 `json:"peak_live_bytes"`
	NativeBytes                uint64 `json:"native_bytes"`
	TargetSelectedInstructions uint64 `json:"target_selected_instructions"`
	GenericMachineInstructions uint64 `json:"generic_machine_instructions"`

	RailMach   EmitterMetrics `json:"railmach"`
	Structured EmitterMetrics `json:"structured"`
}

// EmitterMetrics aggregates exact compile attribution by the function emitter
// that produced the installed native body. Execution attribution is joined by
// function index in the benchmark harness rather than guessed from module
// identity.
type EmitterMetrics struct {
	Functions   uint32 `json:"functions"`
	BodyBytes   uint64 `json:"body_bytes"`
	NativeBytes uint64 `json:"native_bytes"`
	LowerNanos  int64  `json:"lower_nanos"`
	EmitNanos   int64  `json:"emit_nanos"`
	CacheHits   uint32 `json:"cache_hits"`
}

// NativePlannerCapacityBreakdown attributes the native finalization scratch
// retained at the function's exact planner-capacity high-water mark. The
// categories are disjoint and sum to NativePlannerRetainedBytes.
type NativePlannerCapacityBreakdown struct {
	ControlFlow uint64 `json:"control_flow"`
	Bounds      uint64 `json:"bounds"`
	PostRA      uint64 `json:"postra"`
	Immediates  uint64 `json:"immediates"`
	GC          uint64 `json:"gc"`
	CallsRoots  uint64 `json:"calls_roots"`
}

// Total returns all retained native-planner bytes represented by b.
func (b NativePlannerCapacityBreakdown) Total() uint64 {
	return b.ControlFlow + b.Bounds + b.PostRA + b.Immediates + b.GC + b.CallsRoots
}

// ScheduleCandidateMetrics records one initial schedule candidate after
// allocation and late SSA exit, plus opt-in first-pass post-RA opportunities.
type ScheduleCandidateMetrics struct {
	EstimatedCycles   uint64 `json:"estimated_cycles"`
	ResourceCycles    uint64 `json:"resource_cycles"`
	SelectedBytes     uint64 `json:"selected_bytes"`
	PostRARewrites    uint32 `json:"postra_rewrites"`
	PostRAElisions    uint32 `json:"postra_planned_elisions"`
	PostRAWrapSpills  uint32 `json:"postra_wrap_spills"`
	EliminatedMoves   uint32 `json:"eliminated_moves"`
	Kind              uint8  `json:"kind"`
	Nondominated      bool   `json:"nondominated"`
	WeightedSpillDebt uint64 `json:"weighted_spill_debt"`
	PhysicalCopies    uint32 `json:"physical_copies"`
	CopyCycles        uint32 `json:"copy_cycles"`
	CopyMotion        uint32 `json:"copy_motion"`
	FixedRepairs      uint32 `json:"fixed_repairs"`
	BrokenFusions     uint32 `json:"broken_fusions"`
	LoopInvariantOps  uint32 `json:"loop_invariant_ops"`
}

// FunctionMetrics attributes compiler work to one original Wasm function.
type FunctionMetrics struct {
	Function  uint32 `json:"function"`
	BodyBytes uint32 `json:"body_bytes"`

	LowerNanos int64 `json:"lower_nanos"`
	EmitNanos  int64 `json:"emit_nanos"`

	RailSSAInstructions        uint32                            `json:"railssa_instructions"`
	RailMachInstructions       uint32                            `json:"railmach_instructions"`
	TargetSelectedInstructions uint32                            `json:"target_selected_instructions"`
	GenericMachineInstructions uint32                            `json:"generic_machine_instructions"`
	SemanticArguments          uint32                            `json:"semantic_arguments"`
	StackInstructions          uint32                            `json:"stack_instructions"`
	MemoryAccesses             uint32                            `json:"memory_accesses"`
	BoundsChecksElided         uint32                            `json:"bounds_checks_elided"`
	BoundsChecksReused         uint32                            `json:"bounds_checks_reused"`
	ObligationsElided          uint32                            `json:"obligations_elided"`
	ProofQueries               uint32                            `json:"proof_queries"`
	RailMachFinalized          bool                              `json:"railmach_finalized"`
	StructuredReason           string                            `json:"structured_reason,omitempty"`
	ScheduleKind               uint8                             `json:"schedule_kind"`
	ScheduleForced             bool                              `json:"schedule_forced"`
	BackendAttempts            uint8                             `json:"backend_attempts"`
	ScheduleCandidates         uint8                             `json:"schedule_candidates"`
	InitialScheduleScoreCount  uint8                             `json:"initial_schedule_score_count"`
	InitialScheduleScores      [3]ScheduleCandidateMetrics       `json:"initial_schedule_scores"`
	InitialCandidateFrontier   uint8                             `json:"initial_candidate_frontier"`
	RetryScheduleScoreCount    uint8                             `json:"retry_schedule_score_count"`
	RetryScheduleScores        [3]ScheduleCandidateMetrics       `json:"retry_schedule_scores"`
	RetryCandidateFrontier     uint8                             `json:"retry_candidate_frontier"`
	SelectionCombinations      uint32                            `json:"selection_combinations"`
	Dependencies               uint32                            `json:"dependencies"`
	ScheduleReadySteps         uint32                            `json:"schedule_ready_steps"`
	ScheduleReadyWidthTotal    uint64                            `json:"schedule_ready_width_total"`
	ScheduleReadyWidthMax      uint32                            `json:"schedule_ready_width_max"`
	ScheduleCriticalPathCost   uint64                            `json:"schedule_critical_path_cost"`
	LivenessDebt               railmach.LivenessDebt             `json:"liveness_debt"`
	LiveIntervals              uint32                            `json:"live_intervals"`
	LiveSegments               uint32                            `json:"live_segments"`
	SegmentedRanges            uint32                            `json:"segmented_ranges"`
	SegmentedCandidateRanges   uint32                            `json:"segmented_candidate_ranges"`
	SegmentedBaselineDebt      uint64                            `json:"segmented_baseline_spill_debt"`
	SegmentedCandidateDebt     uint64                            `json:"segmented_candidate_spill_debt"`
	SegmentedBaselineCopies    uint32                            `json:"segmented_baseline_physical_copies"`
	SegmentedCandidateCopies   uint32                            `json:"segmented_candidate_physical_copies"`
	SegmentedAttempted         bool                              `json:"segmented_attempted"`
	SegmentedAdmitted          bool                              `json:"segmented_admitted"`
	AllocationFragments        uint32                            `json:"allocation_fragments"`
	IPRARefinedCalls           uint32                            `json:"ipra_refined_calls"`
	WeightedSpillDebt          uint64                            `json:"weighted_spill_debt"`
	AllocationStage            uint8                             `json:"allocation_stage"`
	Promotions                 uint32                            `json:"promotions"`
	Evictions                  uint32                            `json:"evictions"`
	CalleeSavedRanges          uint32                            `json:"callee_saved_ranges"`
	ShrinkWrappedSaves         uint32                            `json:"shrink_wrapped_saves"`
	PreservationCost           uint64                            `json:"preservation_cost"`
	SpillSlots                 uint32                            `json:"spill_slots"`
	RegionalFragments          uint32                            `json:"regional_fragments"`
	RegionalReloads            uint32                            `json:"regional_reloads"`
	RegionalStores             uint32                            `json:"regional_stores"`
	PhysicalCopies             uint32                            `json:"physical_copies"`
	CoalescedCopies            uint32                            `json:"coalesced_copies"`
	CopyRematerializations     uint32                            `json:"copy_rematerializations"`
	CopyCycles                 uint32                            `json:"copy_cycles"`
	CopyMotion                 uint32                            `json:"copy_motion"`
	EdgeMoves                  uint32                            `json:"edge_moves"`
	LoopBackedgeMoves          uint32                            `json:"loop_backedge_moves"`
	LoopBackedgesWithMoves     uint32                            `json:"loop_backedges_with_moves"`
	MaxEdgeMoveBundle          uint32                            `json:"max_edge_move_bundle"`
	FixedMoves                 uint32                            `json:"fixed_moves"`
	AddressFolds               uint32                            `json:"address_folds"`
	MemoryFolds                uint32                            `json:"memory_folds"`
	ImmediateFolds             uint32                            `json:"immediate_folds"`
	PostRARewrites             uint32                            `json:"postra_rewrites"`
	PostRAByteSavings          int64                             `json:"postra_byte_savings"`
	HostEffectSpecializations  uint32                            `json:"host_effect_specializations"`
	ExactGCTypeSpecializations uint32                            `json:"exact_gc_type_specializations"`
	FreshObjectSpecializations uint32                            `json:"fresh_object_specializations"`
	RootSlots                  uint32                            `json:"root_slots"`
	RootSafepoints             uint32                            `json:"root_safepoints"`
	RootUses                   uint32                            `json:"root_uses"`
	GuardedIndirectCalls       uint32                            `json:"guarded_indirect_calls"`
	ABIClass                   uint8                             `json:"abi_class"`
	ClobberGPR                 uint64                            `json:"clobber_gpr"`
	ClobberFPR                 uint64                            `json:"clobber_fpr"`
	CacheHit                   bool                              `json:"cache_hit"`
	NativeBytes                uint32                            `json:"native_bytes"`
	FrameBytes                 uint32                            `json:"frame_bytes"`
	Relocations                uint32                            `json:"relocations"`
	PeakLiveBytes              uint64                            `json:"peak_live_bytes"`
	RailSSARetainedBytes       uint64                            `json:"railssa_retained_bytes"`
	RailMachRetainedBytes      uint64                            `json:"railmach_retained_bytes"`
	NativePlannerRetainedBytes uint64                            `json:"native_planner_retained_bytes"`
	NativePlannerCapacity      NativePlannerCapacityBreakdown    `json:"native_planner_capacity"`
	RailSSACapacity            railssa.PipelineCapacityBreakdown `json:"railssa_capacity"`

	// liveBaseBytes is retained planner storage that remains live during the
	// current compiler phase. observe adds it to transient storage. A giant
	// function can release planning-only slabs before native finalization, so
	// livePhasePeakBytes is reset at that ownership boundary while PeakLiveBytes
	// remains the function-wide high-water mark.
	liveBaseBytes      uint64
	livePhasePeakBytes uint64
}

func recordSpecializationMetrics(metrics *FunctionMetrics, plan *railssa.SpecializationPlan) {
	if metrics == nil || plan == nil {
		return
	}
	for _, specialization := range plan.Entries {
		switch specialization.Kind {
		case railssa.SpecializeHostEffects:
			metrics.HostEffectSpecializations++
		case railssa.SpecializeExactGCType:
			metrics.ExactGCTypeSpecializations++
		case railssa.SpecializeFreshObject:
			metrics.FreshObjectSpecializations++
		}
	}
}

func recordNativePlanMetrics(metrics *FunctionMetrics, plan *nativeBackendPlan) {
	if metrics == nil || plan == nil {
		return
	}
	metrics.RailMachFinalized = true
	metrics.RailSSAInstructions = uint32(len(plan.Semantic.Insts))
	metrics.SemanticArguments = uint32(len(plan.Semantic.Args))
	metrics.RailMachInstructions = uint32(len(plan.Machine.Insts))
	metrics.MemoryAccesses = uint32(len(plan.Machine.Memory))
	metrics.TargetSelectedInstructions = plan.TargetSelectedInstructions
	metrics.GenericMachineInstructions = plan.GenericMachineInstructions
	metrics.ScheduleKind = uint8(plan.Score.Kind)
	metrics.ScheduleForced = plan.ScheduleForced
	metrics.BackendAttempts = plan.BackendAttempts
	metrics.ScheduleCandidates = plan.ScheduleCandidates
	metrics.InitialScheduleScoreCount = plan.InitialScheduleScoreCount
	metrics.InitialCandidateFrontier = plan.InitialCandidateFrontier
	metrics.RetryScheduleScoreCount = plan.RetryScheduleScoreCount
	metrics.RetryCandidateFrontier = plan.RetryCandidateFrontier
	recordScheduleScores := func(out *[3]ScheduleCandidateMetrics, scores [3]railmach.ScheduleScore, count, frontier uint8) {
		for index := range scores[:count] {
			score := scores[index]
			out[index] = ScheduleCandidateMetrics{
				EstimatedCycles: score.EstimatedCycles, ResourceCycles: score.ResourceCycles, SelectedBytes: score.SelectedBytes,
				PostRARewrites: score.PostRARewrites, PostRAElisions: score.PostRAElisions, PostRAWrapSpills: score.PostRAWrapSpills, EliminatedMoves: score.EliminatedMoves,
				Kind: uint8(score.Kind), Nondominated: frontier&(1<<index) != 0, WeightedSpillDebt: score.WeightedSpillDebt,
				PhysicalCopies: score.PhysicalCopies, CopyCycles: score.CopyCycles,
				CopyMotion: score.CopyMotion, FixedRepairs: score.FixedRepairs,
				BrokenFusions: score.BrokenFusions, LoopInvariantOps: score.LoopInvariantOps,
			}
		}
	}
	recordScheduleScores(&metrics.InitialScheduleScores, plan.InitialScheduleScores, plan.InitialScheduleScoreCount, plan.InitialCandidateFrontier)
	recordScheduleScores(&metrics.RetryScheduleScores, plan.RetryScheduleScores, plan.RetryScheduleScoreCount, plan.RetryCandidateFrontier)
	metrics.SelectionCombinations = uint32(len(plan.Selection.Combinations))
	metrics.Dependencies = uint32(len(plan.DAG.Dependencies))
	if freedom, err := railmach.MeasureScheduleFreedom(plan.Machine, plan.Selection, plan.DAG); err == nil {
		metrics.ScheduleReadySteps = freedom.ReadySteps
		metrics.ScheduleReadyWidthTotal = freedom.ReadyWidthTotal
		metrics.ScheduleReadyWidthMax = freedom.ReadyWidthMax
		metrics.ScheduleCriticalPathCost = freedom.CriticalPathCost
	}
	if debt, err := railmach.MeasureLivenessDebt(plan.Machine, plan.Schedule, plan.Allocation); err == nil {
		metrics.LivenessDebt = debt
	}
	metrics.LiveIntervals = uint32(len(plan.Allocation.Intervals))
	metrics.LiveSegments = uint32(len(plan.Allocation.Intervals) + len(plan.Allocation.LiveSegments) - len(plan.Allocation.LiveSegmentRanges))
	metrics.SegmentedRanges = uint32(len(plan.Allocation.LiveSegmentRanges))
	metrics.SegmentedCandidateRanges = plan.SegmentedCandidateRanges
	metrics.SegmentedBaselineDebt = plan.SegmentedBaselineDebt
	metrics.SegmentedCandidateDebt = plan.SegmentedCandidateDebt
	metrics.SegmentedBaselineCopies = plan.SegmentedBaselineCopies
	metrics.SegmentedCandidateCopies = plan.SegmentedCandidateCopies
	metrics.SegmentedAttempted = plan.SegmentedAttempted
	metrics.SegmentedAdmitted = plan.SegmentedAdmitted
	metrics.AllocationFragments = uint32(len(plan.Allocation.Fragments))
	metrics.IPRARefinedCalls = plan.IPRARefinedCalls
	metrics.WeightedSpillDebt = plan.Score.WeightedSpillDebt
	metrics.AllocationStage = plan.Allocation.Stage
	metrics.Promotions = plan.Allocation.Metrics.Promotions
	metrics.Evictions = plan.Allocation.Metrics.Evictions
	metrics.CalleeSavedRanges = plan.Allocation.Metrics.CalleeSaved
	metrics.PreservationCost = plan.Allocation.Metrics.PreservationCost
	metrics.SpillSlots = uint32(plan.Allocation.SpillSlots)
	metrics.RegionalFragments = plan.Allocation.Metrics.RegionalFragments
	metrics.RegionalReloads = plan.Allocation.Metrics.RegionalReloads
	metrics.RegionalStores = plan.Allocation.Metrics.RegionalStores
	metrics.PhysicalCopies = plan.Exit.Debt.Physical
	metrics.CoalescedCopies = plan.Exit.Debt.Coalesced
	metrics.CopyRematerializations = plan.Exit.Debt.Rematerialized
	metrics.CopyCycles = plan.Exit.Debt.Cycles
	metrics.CopyMotion = plan.Exit.Debt.Motion
	for edgeIndex, moveRange := range plan.Exit.EdgeMoves {
		metrics.EdgeMoves += moveRange.Count
		if moveRange.Count > metrics.MaxEdgeMoveBundle {
			metrics.MaxEdgeMoveBundle = moveRange.Count
		}
		if moveRange.Count == 0 || edgeIndex >= len(plan.Machine.Edges) {
			continue
		}
		edge := plan.Machine.Edges[edgeIndex]
		if int(edge.From) < len(plan.Machine.Blocks) && int(edge.To) < len(plan.Machine.Blocks) &&
			edge.From >= edge.To && plan.Machine.Blocks[edge.To].Flags&railssa.BlockLoopHeader != 0 {
			metrics.LoopBackedgeMoves += moveRange.Count
			metrics.LoopBackedgesWithMoves++
		}
	}
	for _, moveRange := range plan.Exit.FixedMoves {
		metrics.FixedMoves += moveRange.Count
	}
	metrics.AddressFolds = uint32(len(plan.Selection.AddressFolds))
	metrics.ABIClass = uint8(plan.ABI.Class)
	metrics.ClobberGPR = plan.ABI.GPRClobbers
	metrics.ClobberFPR = plan.ABI.FPRClobbers
	metrics.ObligationsElided = plan.Simplified.Metrics.ObligationsRemoved
	if plan.Roots != nil {
		metrics.RootSlots = uint32(plan.Roots.SlotCount)
		metrics.RootSafepoints = uint32(len(plan.Roots.Sites))
		metrics.RootUses = uint32(len(plan.Roots.Roots))
	}
	if plan.Emission != nil {
		metrics.ProofQueries = plan.Emission.ProofQueries
	}
	metrics.FrameBytes = plan.Frame.TotalBytes
	metrics.ShrinkWrappedSaves = uint32(len(plan.CalleeSaves))
}

func (m *Metrics) reset(fingerprint [32]byte) {
	scheduleOverride := m.ScheduleOverride
	functions := m.Functions[:0]
	*m = Metrics{Version: MetricsVersion, TargetFingerprint: fingerprint, ScheduleOverride: scheduleOverride, Functions: functions}
}

func (m *Metrics) observe(bytes uint64) {
	if bytes > m.PeakLiveBytes {
		m.PeakLiveBytes = bytes
	}
}

func (m *Metrics) summarizeEmitters() {
	if m == nil {
		return
	}
	m.RailMach = EmitterMetrics{}
	m.Structured = EmitterMetrics{}
	m.TargetSelectedInstructions = 0
	m.GenericMachineInstructions = 0
	for index := range m.Functions {
		row := &m.Functions[index]
		m.TargetSelectedInstructions += uint64(row.TargetSelectedInstructions)
		m.GenericMachineInstructions += uint64(row.GenericMachineInstructions)
		// Every emitted native function has a non-empty adapter or body. A zero
		// byte row belongs to an unselected function in a tier clone.
		if row.NativeBytes == 0 {
			continue
		}
		total := &m.Structured
		if row.RailMachFinalized {
			total = &m.RailMach
		}
		total.Functions++
		total.BodyBytes += uint64(row.BodyBytes)
		total.NativeBytes += uint64(row.NativeBytes)
		total.LowerNanos += row.LowerNanos
		total.EmitNanos += row.EmitNanos
		if row.CacheHit {
			total.CacheHits++
		}
	}
}

func (m *FunctionMetrics) observe(bytes uint64) {
	if m == nil {
		return
	}
	live := m.liveBaseBytes + bytes
	if live > m.livePhasePeakBytes {
		m.livePhasePeakBytes = live
	}
	if live > m.PeakLiveBytes {
		m.PeakLiveBytes = live
	}
}

func (m *FunctionMetrics) beginLivePhase(base uint64) {
	if m == nil {
		return
	}
	m.liveBaseBytes = base
	m.livePhasePeakBytes = 0
	m.observe(0)
}

func elapsedNanos(start time.Time) int64 { return time.Since(start).Nanoseconds() }

func sliceBytes[T any](values []T) uint64 {
	var value T
	return uint64(cap(values)) * uint64(unsafe.Sizeof(value))
}
