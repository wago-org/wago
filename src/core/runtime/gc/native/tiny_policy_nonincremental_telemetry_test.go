//go:build wago_gcstats && wago_tiny_nonincremental

package gc

import "testing"

func TestTinyNonIncrementalTelemetryReportsSynchronousPolicy(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{
		Profile:        ProfileTiny,
		TinyHeapBytes:  4096,
		TinyBlockBytes: 16,
		Telemetry:      new(Telemetry),
	}, []TypeDesc{leaf})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("Tiny nonincremental telemetry snapshot unavailable")
	}
	if snapshot.Tiny.IncrementalBuild {
		t.Fatalf("Tiny policy telemetry = %+v, want synchronous build", snapshot.Tiny)
	}
}
