//go:build linux && amd64 && wago_gc_direct_trusted_alloc && !wago_gc_compact_helpers && !(wago_gc_compact_reads || wago_gc_compact_alloc || wago_gc_compact_refstore || wago_gc_compact_shared_checks || wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc || wago_gc_compact_fill || wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2 || wago_gc_compact_range_stride || wago_gc_compact_refstruct)

package wago

import (
	"github.com/wago-org/wago/experiments/directgc"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"strconv"
	"testing"
)

func TestGCBridgeGeometry(t *testing.T) {
	defaults, err := gcBridgeGeometry("direct", "", "", false)
	if err != nil || defaults.NurseryBytes != 64<<10 || defaults.ThroughputHeapBytes != 8<<20 {
		t.Fatal(defaults, err)
	}
	for _, split := range [][2]uint32{{2, 8}, {4, 7}, {6, 6}} {
		young, old := split[0]<<20, split[1]<<20
		cfg, err := gcBridgeGeometry("direct", strconv.FormatUint(uint64(young), 10), strconv.FormatUint(uint64(old), 10), false)
		if err != nil {
			t.Fatal(err)
		}
		c, err := gc.NewExperimentalDirectCollector(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		v := c.DirectView()
		if v == nil || v.NurseryEnd-v.NurseryStart != young || v.OldStart != 16+young || v.ArenaBytes != 16+(18<<20) {
			t.Fatalf("actual geometry: %+v", v)
		}
		c.Close()
		core, err := directgc.New(young, old)
		if err != nil {
			t.Fatal(err)
		}
		if got := gcBridgeFixedBytes(young, old); got != core.FixedBytes() || got != 28463151 {
			t.Fatalf("fixed bytes %d actual %d", got, core.FixedBytes())
		}
	}
	for _, tc := range []struct {
		mode, young, old string
		age2             bool
	}{
		{"direct", "0", "", false}, {"direct", "", "0", false}, {"direct", "17", "", false}, {"direct", "", "17", false},
		{"direct", "4294967296", "", false}, {"direct", "4294967280", "16", false}, {"direct", "16", "4294967280", false},
		{"direct", "-16", "", false}, {"direct", "", "abc", false}, {"compact", "", "16", false}, {"handles", "", "16", false}, {"direct", "", "16", true},
	} {
		if _, err := gcBridgeGeometry(tc.mode, tc.young, tc.old, tc.age2); err == nil {
			t.Fatalf("accepted invalid geometry %+v", tc)
		}
	}
}
