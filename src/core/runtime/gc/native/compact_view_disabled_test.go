//go:build !wago_gc_compact_helpers && !(wago_gc_compact_reads || (wago_gc_compact_alloc || ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || (wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || wago_gc_compact_range_sse2) || wago_gc_compact_range_stride))))))) || wago_gc_compact_refstruct)

package gc

import (
	"testing"
	"unsafe"
)

func TestCompactViewDefaultStateIsEmpty(t *testing.T) {
	if unsafe.Sizeof(compactViewState{}) != 0 {
		t.Fatal("compact view adds default state")
	}
	c := new(Collector)
	if c.CompactView() != nil {
		t.Fatal("default exposes compact view")
	}
	if _, e := BuildCompactInstanceView(c, nil); e == nil {
		t.Fatal("default admits compact instance")
	}
}
