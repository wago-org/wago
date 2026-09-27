//go:build wago_gcstats && !wago_tiny_nonincremental

package gc

import "testing"

func TestTinyRootStagingPanicEndsTelemetry(t *testing.T) {
	for _, remark := range []bool{false, true} {
		name := "initial"
		if remark {
			name = "remark"
		}
		t.Run(name, func(t *testing.T) {
			leaf, err := NewStructDesc(0, nil)
			if err != nil {
				t.Fatal(err)
			}
			c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, Telemetry: new(Telemetry)}, []TypeDesc{leaf})
			object, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if remark {
				if err := c.Step(nil); err != nil {
					t.Fatal(err)
				}
				if err := c.Step(nil); err != nil {
					t.Fatal(err)
				}
				if c.tinyGC.state != tinyRemark {
					t.Fatal("setup did not reach remark")
				}
			}
			func() {
				defer func() {
					if got := recover(); got != "root enumeration failed" {
						t.Fatalf("unexpected panic: %v", got)
					}
				}()
				_ = c.Step(tinyPanickingRoots{ref: object})
			}()
			if c.tinyGC.telemetryOwned || c.cfg.Telemetry.active.active {
				t.Fatal("panicking root walk left incremental telemetry active")
			}
			snapshot, _ := c.TelemetrySnapshot()
			if snapshot.Full.Cycles != 1 || snapshot.Full.FailedCycles != 1 {
				t.Fatalf("panic telemetry cycles=%d failed=%d, want 1/1", snapshot.Full.Cycles, snapshot.Full.FailedCycles)
			}
			if err := c.CollectFull(RefSliceRoots{object}); err != nil {
				t.Fatal(err)
			}
			snapshot, _ = c.TelemetrySnapshot()
			if snapshot.Full.Cycles != 2 || snapshot.Full.FailedCycles != 1 || !c.validObjectRef(object) {
				t.Fatal("retry lost the root or miscounted the failed collection")
			}
			if !c.ResetTelemetry() {
				t.Fatal("completed retry left telemetry active")
			}
		})
	}
}
