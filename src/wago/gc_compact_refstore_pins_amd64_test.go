//go:build linux && amd64 && ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc || wago_gc_compact_fill || (wago_gc_compact_copy || (wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride)))

package wago

import (
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func compactRefStoreWidePinsModule(width byte) []byte {
	init := []byte{0x41, 1, 0xfb, 7, 1, 0x24, 0, 0x0b}
	body := []byte{2, width, 0x7e, 1, 0x7f}
	for i := byte(0); i < width; i++ {
		body = append(body, 0x20, 0, 0x42, i+1, 0x7c, 0x21, i+2)
	}
	idx := width + 2
	body = append(body, 0x03, 0x40, 0x23, 0, 0x41, 0, 0x20, idx, 0xfb, 0, 0, 0xfb, 14, 1, 0x20, idx, 0x41, 1, 0x6a, 0x22, idx, 0x20, 1, 0x49, 0x0d, 0, 0x0b)
	body = append(body, 0x42, 0)
	for i := byte(0); i < width; i++ {
		body = append(body, 0x20, i+2, 0x7c)
	}
	body = append(body, 0x23, 0, 0x41, 0, 0xfb, 11, 1, 0xfb, 2, 0, 0, 0xad, 0x7c, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5e, 0x63, 0, 1}, []byte{0x60, 0, 0}, []byte{0x60, 2, 0x7e, 0x7f, 1, 0x7e})),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestCompactNativeGCRefStoreWidePinnedLocals(t *testing.T) {
	for _, width := range []byte{8, 12} {
		for _, forced := range []bool{false, true} {
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactRefStoreWidePinsModule(width))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: forced, ForceMajorEveryMinor: true, NurseryBytes: 256, ThroughputHeapBytes: 64 << 10}})
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			if _, e := in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			for i := uint64(0); i < 8; i++ {
				n := (uint64(1) << 48) + 91 + i
				got, e := in.Invoke("run", n, 32)
				want := uint64(width)*n + uint64(width)*(uint64(width)+1)/2 + 31
				if e != nil || len(got) != 1 || got[0] != want {
					t.Fatal(width, forced, i, got, e, want)
				}
			}
			if s := in.gc.Stats(); s.Allocations != 257 || s.FullCollections == 0 {
				t.Fatal("missing real allocations/collections", s)
			}
		}
	}
}
