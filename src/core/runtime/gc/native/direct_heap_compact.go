//go:build wago_gc_compact_helpers || (wago_gc_compact_reads || (wago_gc_compact_alloc || ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop)))))) || wago_gc_compact_refstruct)

package gc

import (
	"errors"
	"fmt"
	compactgc "github.com/wago-org/wago/experiments/compactgc"
)

type directHeap = compactgc.Collector
type directHeapRef = compactgc.Ref
type directHeapTypeID = compactgc.TypeID
type directHeapDescriptor = compactgc.Descriptor

const directHeapKindStruct = compactgc.KindStruct
const directHeapKindArray = compactgc.KindArray
const directHeapCompact = true

var errDirectHeapOOM = compactgc.ErrOOM

func newDirectHeap(n, o uint32, d []directHeapDescriptor) (*directHeap, error) {
	return compactgc.NewWithTypes(n, o, d)
}
func directDescriptorAlign(a uint32) uint32 { return max(a, 1) }
func directRawBytes(h *directHeap, r directHeapRef) []byte {
	panic("gc: compact heap has no native object-byte ABI")
}
func heapReadSnapshot(h *directHeap) DirectCollectorView         { return DirectCollectorView{} }
func heapAllocationSnapshot(h *directHeap) directAllocationWords { return directAllocationWords{} }
func (c *Collector) compactRefDesc(r Ref) (*TypeDesc, error) {
	if e := c.errIfClosed(); e != nil {
		return nil, e
	}
	if !r.IsObj() {
		return nil, errors.New("gc: ref is not object")
	}
	if !c.validObjectRef(r) {
		return nil, errors.New("gc: invalid object ref")
	}
	id, e := c.direct.ObjectType(directRef(r))
	if e != nil {
		return nil, e
	}
	if uint64(id) >= uint64(len(c.types)) {
		return nil, fmt.Errorf("gc: unknown type id %d", id)
	}
	return &c.types[id], nil
}
func (c *Collector) compactHeaderPayload(r Ref) (ObjHeader, []byte, error) {
	h, e := c.direct.Header(directRef(r))
	if e != nil {
		return ObjHeader{}, nil, e
	}
	p, e := c.direct.Payload(directRef(r))
	if e != nil {
		return ObjHeader{}, nil, e
	}
	flags := uint32(0)
	if !c.types[h.TypeID].HasRefs {
		flags |= FlagPointerFree
	}
	if !c.direct.IsYoung(directRef(r)) {
		flags |= FlagOld
	}
	return ObjHeader{uint32(h.TypeID), h.Extent, h.Length, flags}, p, nil
}
