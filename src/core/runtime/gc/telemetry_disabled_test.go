//go:build !wago_gcstats

package gc

import (
	"bytes"
	"strings"
	"testing"
)

func TestCollectorTelemetryRequiresBuildTag(t *testing.T) {
	if TelemetryAvailable() {
		t.Fatal("ordinary release build reports collector telemetry available")
	}
	c, err := NewCollector(Config{Telemetry: new(Telemetry)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, ok := c.TelemetrySnapshot(); ok {
		t.Fatal("ordinary release build unexpectedly enabled collector telemetry")
	}
	if c.ResetTelemetry() {
		t.Fatal("ordinary release build unexpectedly reset collector telemetry")
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		if err := c.CollectMinor(nil); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("disabled telemetry minor collection allocations = %v", allocs)
	}
	var out bytes.Buffer
	if err := NewBenchmarkTelemetryReport("disabled").WriteJSON(&out); err == nil || !strings.Contains(err.Error(), "wago_gcstats") {
		t.Fatalf("disabled JSON error = %v", err)
	}
}
