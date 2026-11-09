package gc

import (
	"strings"
	"testing"
)

// CollectMinor keeps its attempt/unwind contract: a successful attempt is
// recorded once, a root panic propagates unchanged after being recorded as an
// unwound failure, and the native view is refreshed on every exit.
func TestCollectMinorAttemptContract(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 1 << 20})
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	view := c.NativeView()
	if view == nil {
		t.Fatal("collector has no native view")
	}

	before := c.stats.MinorCollections
	view.HandleCount = ^uint32(0)
	if err := c.CollectMinor(RefSliceRoots{ref}); err != nil {
		t.Fatal(err)
	}
	if got := c.stats.MinorCollections; got != before+1 {
		t.Fatalf("minor collections %d, want %d", got, before+1)
	}
	if d := c.CollectionDiagnostics(); d.Kind != "minor" || d.Result != "success" || d.RequiredCause != nil {
		t.Fatalf("successful attempt diagnostics %+v", d)
	}
	if view.HandleCount != uint32(len(c.handles)) {
		t.Fatal("native view not refreshed after a successful minor collection")
	}

	before = c.stats.MinorCollections
	view.HandleCount = ^uint32(0)
	func() {
		defer func() {
			if got := recover(); got != "root enumeration failed" {
				t.Fatalf("root walk panic = %v", got)
			}
		}()
		_ = c.CollectMinor(&panickingMarkRoots{root: Root(ref)})
		t.Fatal("panicking root walk returned")
	}()
	if got := c.stats.MinorCollections; got != before+1 {
		t.Fatalf("minor collections after unwind %d, want %d", got, before+1)
	}
	d := c.CollectionDiagnostics()
	if d.Kind != "minor" || d.Result != "failed" || !d.Pressure || d.RequiredCause == nil ||
		!strings.Contains(d.RequiredCause.Error(), "gc: collection unwound: root enumeration failed") {
		t.Fatalf("unwound attempt diagnostics %+v", d)
	}
	if view.HandleCount != uint32(len(c.handles)) {
		t.Fatal("native view not refreshed after an unwound minor collection")
	}

	c.Close()
	before = c.stats.MinorCollections
	if err := c.CollectMinor(nil); err == nil {
		t.Fatal("closed collector accepted a minor collection")
	}
	if c.stats.MinorCollections != before {
		t.Fatal("rejected minor collection was counted")
	}
}
