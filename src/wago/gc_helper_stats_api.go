package wago

// GCHelperStats reports executed synchronous Go helper transitions for one
// tracked collector domain.
type GCHelperStats struct {
	Calls           uint64
	AllocationCalls uint64
	MutationCalls   uint64
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
