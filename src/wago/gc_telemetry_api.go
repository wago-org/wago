package wago

// GCTelemetrySnapshot returns the current opt-in collector telemetry for this
// instance's shared Runtime GC domain. The bool is false when the instance has no
// collector, callback-scoped guest storage is borrowed, GCConfig.Telemetry was
// nil, or wago_gcstats was not compiled in.
func (in *Instance) GCTelemetrySnapshot() (GCTelemetrySnapshot, bool) {
	if in == nil || in.gc == nil || in.guestStorageBorrowed() {
		return GCTelemetrySnapshot{}, false
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	return in.gc.TelemetrySnapshot()
}

// ResetGCTelemetry clears the shared collector-domain recorder. It returns false
// when telemetry is unavailable, callback-scoped guest storage is borrowed, or a
// Tiny incremental cycle is active.
func (in *Instance) ResetGCTelemetry() bool {
	if in == nil || in.gc == nil || in.guestStorageBorrowed() {
		return false
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	return in.gc.ResetTelemetry()
}
