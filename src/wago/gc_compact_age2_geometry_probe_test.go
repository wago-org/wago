//go:build wago_gc_compact_age2

package wago

import (
	"encoding/json"
	compact "github.com/wago-org/wago/experiments/compactgc"
	"os"
	"testing"
)

func TestGCCompactAge2GeometryProbe(t *testing.T) {
	if os.Getenv("GC_GEOMETRY_PROBE") != "1" {
		t.Skip("opt-in geometry")
	}
	ds := []compact.Descriptor{{Kind: compact.KindStruct, PayloadBytes: 4, Align: 4}, {Kind: compact.KindArray, ElementSize: 4, Align: 4}, {Kind: compact.KindStruct, PayloadBytes: 12, Align: 4, RefOffsets: []uint32{4, 8}}}
	base, e := compact.NewWithTypes(2<<20, 8<<20, ds)
	if e != nil {
		t.Fatal(e)
	}
	age, e := compact.NewAge2WithTypes(2<<20, 8<<20, 512<<10, ds)
	if e != nil {
		t.Fatal(e)
	}
	if base.FixedBytes() != age.FixedBytes() {
		t.Fatal("unexpected fixed backing difference")
	}
	b, e := json.Marshal(map[string]any{"baseline": base.MemoryUsage(), "age2": age.MemoryUsage(), "fixed_backing_bytes": base.FixedBytes(), "total_young_bytes": 2 << 20, "baseline_active_bytes": 2 << 20, "age2_active_bytes": 1 << 20, "age2_survivor_cap_bytes": 512 << 10, "scope": "independent equal descriptors; excludes runtime adapter metadata, see workload Go heap/RSS"})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("AGE2_GEOMETRY_RESULT " + string(b))
}
