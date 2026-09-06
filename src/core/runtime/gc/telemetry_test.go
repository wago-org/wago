package gc_test

import (
	"github.com/wago-org/wago/src/core/runtime/gc"
	"testing"
)

func TestCheckedTelemetrySurface(t *testing.T) {
	recorder := new(gc.Telemetry)
	c, err := gc.NewCollector(gc.Config{Telemetry: recorder}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	_, available := c.TelemetrySnapshot()
	if available != gc.TelemetryAvailable() {
		t.Fatalf("snapshot available=%v, build=%v", available, gc.TelemetryAvailable())
	}
	if c.ResetTelemetry() != available {
		t.Fatal("reset availability differs")
	}
	c.Close()
	if _, ok := c.TelemetrySnapshot(); ok {
		t.Fatal("closed collector reports telemetry")
	}
	if c.ResetTelemetry() {
		t.Fatal("closed collector reset succeeded")
	}
	var absent *gc.Collector
	if _, ok := absent.TelemetrySnapshot(); ok || absent.ResetTelemetry() {
		t.Fatal("nil collector reports telemetry")
	}
}
