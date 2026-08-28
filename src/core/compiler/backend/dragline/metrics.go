package dragline

import (
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

const MetricsVersion = 16

// Metrics contains one deterministic row per compiled function plus module
// totals. Timings are observational; all counts and byte sizes are exact for
// the compiler-owned slices tracked by the current pipeline.
type Metrics struct {
	Version           uint32            `json:"version"`
	TargetFingerprint [32]byte          `json:"target_fingerprint"`
	Functions         []FunctionMetrics `json:"functions"`

	FinalizeNanos int64  `json:"finalize_nanos"`
	TotalNanos    int64  `json:"total_nanos"`
	PeakLiveBytes uint64 `json:"peak_live_bytes"`
	NativeBytes   uint64 `json:"native_bytes"`
}

// FunctionMetrics attributes compiler work to one original Wasm function.
type FunctionMetrics struct {
	Function  uint32 `json:"function"`
	BodyBytes uint32 `json:"body_bytes"`

	LowerNanos int64 `json:"lower_nanos"`
	EmitNanos  int64 `json:"emit_nanos"`

	RailSSAInstructions        uint32                            `json:"railssa_instructions"`
	RailMachInstructions       uint32                            `json:"railmach_instructions"`
	SemanticArguments          uint32                            `json:"semantic_arguments"`
	StackInstructions          uint32                            `json:"stack_instructions"`
	BoundsChecksElided         uint32                            `json:"bounds_checks_elided"`
	ObligationsElided          uint32                            `json:"obligations_elided"`
	ProofQueries               uint32                            `json:"proof_queries"`
	RailMachFinalized          bool                              `json:"railmach_finalized"`
	ScheduleKind               uint8                             `json:"schedule_kind"`
	BackendAttempts            uint8                             `json:"backend_attempts"`
	ScheduleCandidates         uint8                             `json:"schedule_candidates"`
	SelectionCombinations      uint32                            `json:"selection_combinations"`
	Dependencies               uint32                            `json:"dependencies"`
	LiveSegments               uint32                            `json:"live_segments"`
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
	RailSSACapacity            railssa.PipelineCapacityBreakdown `json:"railssa_capacity"`

	// liveBaseBytes is retained planner storage that remains live during native
	// finalization. observe adds it to transient emitter storage.
	liveBaseBytes uint64
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
	metrics.ScheduleKind = uint8(plan.Score.Kind)
	metrics.BackendAttempts = plan.BackendAttempts
	metrics.ScheduleCandidates = plan.BackendAttempts * 3
	metrics.SelectionCombinations = uint32(len(plan.Selection.Combinations))
	metrics.Dependencies = uint32(len(plan.DAG.Dependencies))
	metrics.LiveSegments = uint32(len(plan.Allocation.Intervals) + len(plan.Allocation.Fragments))
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
	functions := m.Functions[:0]
	*m = Metrics{Version: MetricsVersion, TargetFingerprint: fingerprint, Functions: functions}
}

func (m *Metrics) observe(bytes uint64) {
	if bytes > m.PeakLiveBytes {
		m.PeakLiveBytes = bytes
	}
}

func (m *FunctionMetrics) observe(bytes uint64) {
	if m != nil && m.liveBaseBytes+bytes > m.PeakLiveBytes {
		m.PeakLiveBytes = m.liveBaseBytes + bytes
	}
}

func elapsedNanos(start time.Time) int64 { return time.Since(start).Nanoseconds() }

func sliceBytes[T any](values []T) uint64 {
	var value T
	return uint64(cap(values)) * uint64(unsafe.Sizeof(value))
}
