//go:build !wago_gc_compact_helpers && !(wago_gc_compact_reads || (wago_gc_compact_alloc || ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || (wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum)) || wago_gc_compact_range_stride))))))) || wago_gc_compact_refstruct)

package gc

import (
	"errors"
	directgc "github.com/wago-org/wago/experiments/directgc"
)

type directHeap = directgc.Collector
type directHeapRef = directgc.Ref
type directHeapTypeID = directgc.TypeID
type directHeapDescriptor = directgc.Descriptor

const directHeapKindStruct = directgc.KindStruct
const directHeapKindArray = directgc.KindArray
const directHeapCompact = false

var errDirectHeapOOM = directgc.ErrOOM

func newDirectHeap(n, o uint32, d []directHeapDescriptor, age2 bool) (*directHeap, error) {
	if age2 {
		return nil, errors.New("gc: age2 requires compact layout")
	}
	return directgc.NewWithTypes(n, o, d)
}
func directDescriptorAlign(a uint32) uint32                { return max(a, 8) }
func directRawBytes(h *directHeap, r directHeapRef) []byte { b, _ := h.ObjectBytes(r); return b }
func heapReadSnapshot(h *directHeap) DirectCollectorView {
	x := h.ReadView()
	return DirectCollectorView{Arena: x.Arena, ArenaBytes: x.ArenaBytes, Starts: x.Starts, StartCount: x.StartCount, NurseryStart: x.NurseryStart, NurseryBump: x.NurseryBump, OldStart: x.OldStart, OldBump: x.OldBump, Dirty: x.Dirty, DirtyCount: x.DirtyCount}
}
func heapAllocationSnapshot(h *directHeap) directAllocationWords {
	x := h.AllocationWords()
	return directAllocationWords{x.Bump, x.End}
}
func (c *Collector) compactRefDesc(r Ref) (*TypeDesc, error) {
	return nil, errors.New("gc: compact backend unavailable")
}
func (c *Collector) compactHeaderPayload(r Ref) (ObjHeader, []byte, error) {
	return ObjHeader{}, nil, errors.New("gc: compact backend unavailable")
}
