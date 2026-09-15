package gc

import raw "github.com/wago-org/wago/src/core/runtime/gc/native"

// Telemetry contains bounded diagnostic counters, never native references.
// Collection timing is available only in builds tagged wago_gcstats.
type Telemetry = raw.Telemetry
type TelemetrySnapshot = raw.TelemetrySnapshot

func TelemetryAvailable() bool { return raw.TelemetryAvailable() }

// TelemetrySnapshot follows the Collector's external synchronization contract.
// Closed collectors and ordinary builds return false.
func (c *Collector) TelemetrySnapshot() (TelemetrySnapshot, bool) {
	if c.available() != nil {
		return TelemetrySnapshot{}, false
	}
	return c.heap.TelemetrySnapshot()
}

func (c *Collector) ResetTelemetry() bool {
	return c.available() == nil && c.heap.ResetTelemetry()
}
