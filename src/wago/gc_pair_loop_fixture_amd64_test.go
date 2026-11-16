//go:build linux && amd64 && (wago_gc_compact_pair_loop || wago_gc_direct_pair_loop || wago_gc_direct_range_loop || wago_gc_compact_range_loop || wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2 || wago_gc_compact_range_stride)

package wago

import "testing"

func pairedLoopInstance(t *testing.T, data []byte) *Instance {
	t.Helper()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	compact := c.gcCompactNativeReceipt() != 0
	if compact {
		if c.gcTrustedDirectVersion != 0 {
			t.Fatal("mixed receipt")
		}
	} else if c.gcTrustedDirectVersion != 6 || c.gcDirectReadABIReceipt() == 0 {
		t.Fatal("missing existing direct receipt")
	}
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: compact, ExperimentalDirectOffsets: !compact, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return in
}
