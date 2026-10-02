package wago

// GCTelemetrySnapshot returns the current opt-in collector telemetry for this
// instance's shared Runtime GC domain. The bool is false when the instance has no
// collector, GCConfig.Telemetry was nil, or wago_gcstats was not compiled in.
func (in *Instance) GCTelemetrySnapshot() (GCTelemetrySnapshot, bool) {
	if in == nil || in.gc == nil {
		return GCTelemetrySnapshot{}, false
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	return in.gc.TelemetrySnapshot()
}

// ResetGCTelemetry clears the shared collector-domain recorder. It returns false
// when telemetry is unavailable or a Tiny incremental cycle is active.
func (in *Instance) ResetGCTelemetry() bool {
	if in == nil || in.gc == nil {
		return false
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	return in.gc.ResetTelemetry()
}
