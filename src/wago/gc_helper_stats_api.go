package wago

import corergc "github.com/wago-org/wago/src/core/runtime/gc/native"

// GCHelperStats reports executed synchronous Go helper transitions for one
// tracked collector domain.
type GCHelperStats struct {
	Calls                            uint64
	AllocationCalls                  uint64
	StructAllocationCalls            uint64
	ArrayAllocationCalls             uint64
	StructDefaultAllocationCalls     uint64
	StructInitializedAllocationCalls uint64
	ArrayDefaultAllocationCalls      uint64
	ArrayOtherAllocationCalls        uint64
	MutationCalls                    uint64
	StructMutationCalls              uint64
	ArrayMutationCalls               uint64
	ReferenceMutationCalls           uint64
	ParentNurseryMutationCalls       uint64
	ParentOldMutationCalls           uint64
	ParentLargeMutationCalls         uint64
	ParentTinyMutationCalls          uint64
	OldYoungRememberedCalls          uint64
	OldYoungUnrememberedCalls        uint64
	StructOldYoungRememberedCalls    uint64
	ArrayOldYoungRememberedCalls     uint64
	ArrayCardPresentCalls            uint64
	ArrayCardCoveredCalls            uint64
}

// GCHelperStats returns the current diagnostic helper counters. Only one
// collector domain is tracked process-wide at a time; the zero value means this
// instance's domain is not the active target.
func (in *Instance) GCHelperStats() GCHelperStats {
	if in == nil || in.gc == nil {
		return GCHelperStats{}
	}
	return snapshotGCHelperStats(in.gc)
}

// TelemetryPaths converts build-tagged helper transitions into the common
// machine-readable path schema. Conditional native successes are measured by
// the invoking fixture and deliberately are not inferred from fallback calls.
func (s GCHelperStats) TelemetryPaths() corergc.PathTelemetry {
	return corergc.PathTelemetry{GoHelperTransitions: s.Calls}
}

// SetGCHelperStatsTracking selects or clears this instance's collector domain as
// the one process-wide diagnostic target. Tracking is available only in builds
// compiled with the wago_gcstats tag. Disable it after measurement so the
// diagnostic global does not retain the collector.
func (in *Instance) SetGCHelperStatsTracking(enabled bool) {
	if in == nil || in.gc == nil {
		return
	}
	setGCHelperStatsTracking(in.gc, enabled)
}
