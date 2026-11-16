// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package compactgc

import (
	"errors"
	"sort"
)

type TypeID uint32
type Kind uint8

const (
	KindStruct Kind = iota
	KindArray
)

// Header is a computed value, never a mutable native-header byte view.
// Only TypeID (four bytes) is stored for structs. Arrays also store Length
// (four bytes). Payload padding is descriptor-aligned; all objects stride16.
type Header struct {
	TypeID                                              TypeID
	Length, PayloadOffset, PayloadBytes, Extent, Stride uint32
	IsArray                                             bool
}

// Descriptor is an immutable tracing/layout schema, NOT a Wasm subtype or
// nullability validator. The adapter must enforce semantic reference types.
// RefOffsets are relative to the payload, not the header. Array reference slots
// are exactly four bytes. Numeric storage sizes are1,2,4,8,16 bytes.
type Descriptor struct {
	Kind         Kind
	PayloadBytes uint32
	Align        uint32
	RefOffsets   []uint32
	ElementSize  uint32
	ElementRef   bool
}

var ErrType = errors.New("compactgc: invalid type descriptor")

func cloneDescriptor(d Descriptor) (Descriptor, error) {
	if d.Align != 1 && d.Align != 2 && d.Align != 4 && d.Align != 8 && d.Align != 16 {
		return Descriptor{}, ErrType
	}
	if d.Kind == KindArray {
		if d.PayloadBytes != 0 || len(d.RefOffsets) != 0 {
			return Descriptor{}, ErrType
		}
		switch d.ElementSize {
		case 1, 2, 4, 8:
		case 16:
			if d.Align != 16 {
				return Descriptor{}, ErrType
			}
		default:
			return Descriptor{}, ErrType
		}
		if d.ElementRef && d.ElementSize != 4 {
			return Descriptor{}, ErrType
		}
	} else if d.Kind == KindStruct {
		if d.ElementSize != 0 || d.ElementRef || uint64(d.PayloadBytes)+16 > uint64(^uint32(0)-7) {
			return Descriptor{}, ErrType
		}
		d.RefOffsets = append([]uint32(nil), d.RefOffsets...)
		sort.Slice(d.RefOffsets, func(i, j int) bool { return d.RefOffsets[i] < d.RefOffsets[j] })
		for i, o := range d.RefOffsets {
			if o%4 != 0 || uint64(o)+4 > uint64(d.PayloadBytes) || (i > 0 && d.RefOffsets[i-1] == o) {
				return Descriptor{}, ErrType
			}
		}
	} else {
		return Descriptor{}, ErrType
	}
	return d, nil
}
func NewWithTypes(nurseryBytes, oldBytes uint32, types []Descriptor) (*Collector, error) {
	c, e := New(nurseryBytes, oldBytes)
	if e != nil {
		return nil, e
	}
	if _, e = c.AddTypes(types); e != nil {
		return nil, e
	}
	return c, nil
}

// AddTypes validates/deep-copies a complete batch before publishing. Registered
// descriptors never change. This explicit setup operation may allocate Go memory.
func (c *Collector) AddTypes(types []Descriptor) (TypeID, error) {
	if uint64(len(c.types))+uint64(len(types)) > uint64(^uint32(0)) {
		return 0, ErrType
	}
	copyTypes := make([]Descriptor, len(types))
	for i, d := range types {
		var e error
		copyTypes[i], e = cloneDescriptor(d)
		if e != nil {
			return 0, e
		}
	}
	first := TypeID(len(c.types))
	c.types = append(c.types, copyTypes...)
	return first, nil
}
func (c *Collector) NewStruct(id TypeID) (Ref, error) {
	if uint64(id) >= uint64(len(c.types)) || c.types[id].Kind != KindStruct {
		return 0, ErrType
	}
	return c.allocate(id, uint64(c.types[id].PayloadBytes), 0)
}
func (c *Collector) NewArray(id TypeID, length uint32) (Ref, error) {
	if uint64(id) >= uint64(len(c.types)) || c.types[id].Kind != KindArray {
		return 0, ErrType
	}
	return c.allocate(id, uint64(c.types[id].ElementSize)*uint64(length), length)
}
func payloadOffset(d Descriptor) uint32 {
	h := uint32(4)
	if d.Kind == KindArray {
		h = 8
	}
	return (h + d.Align - 1) &^ (d.Align - 1)
}
func (c *Collector) allocate(id TypeID, payload uint64, length uint32) (Ref, error) {
	d := c.types[id]
	extent := uint64(payloadOffset(d)) + payload
	stride := (extent + 15) &^ uint64(15)
	if stride > uint64(^uint32(0)) {
		return 0, ErrOOM
	}
	off := c.nurseryBump
	old := stride > uint64(c.nurseryEnd-c.nurseryStart)
	if old {
		if stride > uint64(c.oldStart[c.active]+c.oldSize-c.oldBump) {
			return 0, ErrOOM
		}
	} else if stride > uint64(c.nurseryEnd-c.nurseryBump) {
		return 0, ErrOOM
	}
	if old {
		off = c.oldBump
	}
	clear(c.arena[off : off+uint32(stride)])
	c.put32(off, uint32(id))
	if d.Kind == KindArray {
		c.put32(off+4, length)
	}
	c.starts[off/8] = true
	if old {
		c.oldBump += uint32(stride)
		c.oldAllocations++
		if cohortDiagnostics {
			c.recordOversized(uint32(stride))
		}
	} else {
		c.nurseryBump += uint32(stride)
	}
	return Ref(off), nil
}

// layoutAt reads only intact original or copied destination headers. Collection
// must never call it on a source after its type word becomes forwarding data.
func (c *Collector) layoutAt(off uint32) (Header, error) {
	if uint64(off)+4 > uint64(len(c.arena)) {
		return Header{}, ErrRef
	}
	id := TypeID(c.u32(off))
	if uint64(id) >= uint64(len(c.types)) {
		return Header{}, ErrType
	}
	d := c.types[id]
	h := Header{TypeID: id, PayloadOffset: payloadOffset(d), PayloadBytes: d.PayloadBytes, IsArray: d.Kind == KindArray}
	payload := uint64(d.PayloadBytes)
	if h.IsArray {
		if uint64(off)+8 > uint64(len(c.arena)) {
			return Header{}, ErrRef
		}
		h.Length = c.u32(off + 4)
		payload = uint64(h.Length) * uint64(d.ElementSize)
	}
	extent := uint64(h.PayloadOffset) + payload
	stride := (extent + 15) &^ uint64(15)
	if stride > uint64(^uint32(0)) || uint64(off)+stride > uint64(len(c.arena)) {
		return Header{}, ErrRef
	}
	h.PayloadBytes = uint32(payload)
	h.Extent = uint32(extent)
	h.Stride = uint32(stride)
	return h, nil
}
func (c *Collector) ObjectType(r Ref) (TypeID, error) {
	if !c.valid(r) {
		return 0, ErrRef
	}
	return TypeID(c.u32(uint32(r))), nil
}
func (c *Collector) ArrayLen(r Ref) (uint32, error) {
	id, e := c.ObjectType(r)
	if e != nil {
		return 0, e
	}
	if c.types[id].Kind != KindArray {
		return 0, ErrType
	}
	return c.u32(uint32(r) + 4), nil
}
func (c *Collector) IsValid(r Ref) bool { return r == 0 || r.IsI31() || c.valid(r) }
func (c *Collector) Header(r Ref) (Header, error) {
	if !c.valid(r) {
		return Header{}, ErrRef
	}
	return c.layoutAt(uint32(r))
}
func (c *Collector) logicalPayload(r Ref) uint32 {
	h, _ := c.layoutAt(uint32(r))
	return h.PayloadBytes
}
func (c *Collector) payloadBase(r Ref) uint32 {
	h, _ := c.layoutAt(uint32(r))
	return uint32(r) + h.PayloadOffset
}

// Payload borrows mutable logical payload bytes (not header or padding). It is
// invalid after ANY collection. Trusted adapters must validate reference values
// and call Remember after raw reference writes, and may not use a retained slice
// across allocation safepoints. Prefer checked SetRef/ArraySetRef when possible.
func (c *Collector) Payload(r Ref) ([]byte, error) {
	h, err := c.Header(r)
	if err != nil {
		return nil, err
	}
	x := uint32(r) + h.PayloadOffset
	end := x + h.PayloadBytes
	return c.arena[x:end:end], nil
}

// Remember conservatively records a parent after trusted raw reference writes.
func (c *Collector) Remember(r Ref) error {
	if !c.valid(r) {
		return ErrRef
	}
	if !c.young(r) {
		c.rememberParent(r)
	}
	return nil
}
func (c *Collector) refOffset(r Ref, index uint32) (uint32, error) {
	if !c.valid(r) {
		return 0, ErrRef
	}
	d := c.types[c.u32(uint32(r))]
	if d.Kind == KindArray {
		if !d.ElementRef || index >= c.u32(uint32(r)+4) {
			return 0, ErrField
		}
		return c.payloadBase(r) + index*4, nil
	}
	if uint64(index) >= uint64(len(d.RefOffsets)) {
		return 0, ErrField
	}
	return c.payloadBase(r) + d.RefOffsets[index], nil
}
func (c *Collector) ArrayRef(r Ref, index uint32) (Ref, error) {
	if _, e := c.ArrayLen(r); e != nil {
		return 0, e
	}
	return c.RefField(r, index)
}
func (c *Collector) ArraySetRef(r Ref, index uint32, v Ref) error {
	if _, e := c.ArrayLen(r); e != nil {
		return e
	}
	return c.SetRef(r, index, v)
}

// eachRef visits exact pointer-map slots; headers and descriptors are immutable
// outside collector-owned operations, even when Payload is borrowed.
func (c *Collector) eachRef(off uint32, fn func(uint32) error) error {
	d := c.types[c.u32(off)]
	if d.Kind == KindArray {
		if d.ElementRef {
			for i, n := uint32(0), c.u32(off+4); i < n; i++ {
				if e := fn(off + payloadOffset(d) + i*4); e != nil {
					return e
				}
			}
		}
	} else {
		for _, o := range d.RefOffsets {
			if e := fn(off + payloadOffset(d) + o); e != nil {
				return e
			}
		}
	}
	return nil
}

// WriteBytes writes scalar bytes, rejecting overlap with any reference slot.
func (c *Collector) WriteBytes(r Ref, offset uint32, data []byte) error {
	if !c.valid(r) {
		return ErrRef
	}
	end := uint64(offset) + uint64(len(data))
	if end > uint64(c.logicalPayload(r)) {
		return ErrField
	}
	if len(data) == 0 {
		return nil
	}
	if e := c.eachRef(uint32(r), func(at uint32) error {
		o := uint64(at - c.payloadBase(r))
		if uint64(offset) < o+4 && o < end {
			return ErrField
		}
		return nil
	}); e != nil {
		return e
	}
	copy(c.arena[c.payloadBase(r)+offset:c.payloadBase(r)+uint32(end)], data)
	return nil
}
func Null() Ref               { return 0 }
func (r Ref) IsNull() bool    { return r == 0 }
func (r Ref) IsI31() bool     { return r&1 != 0 }
func (r Ref) IsObj() bool     { return r != 0 && r&1 == 0 }
func I31New(v int32) Ref      { return Ref(uint32(v)<<1) | 1 }
func (r Ref) I31GetU() uint32 { return uint32(r) >> 1 }
func (r Ref) I31GetS() int32  { return int32(uint32(r)) >> 1 }

func (c *Collector) IsYoung(r Ref) bool { return c.valid(r) && c.young(r) }

// LiveObjects counts currently allocated objects, including unreachable objects
// not yet reclaimed. No tracing or collection is performed.
func (c *Collector) LiveObjects() uint32 {
	var n uint32
	for _, live := range c.starts {
		if live {
			n++
		}
	}
	return n
}

// AllocationSnapshot returns exact lifetime allocations and current allocated
// objects without mutating accounting. Pending nursery objects include garbage;
// promotion/relocation never counts as another allocation. Like LiveObjects,
// access is serialized with all mutation, including native publication.
func (c *Collector) AllocationSnapshot() (allocations uint64, live uint32) {
	allocations = c.retiredNurseryAllocations + c.oldAllocations
	first, last := c.nurseryStart/8, c.nurseryBump/8
	for i, yes := range c.starts {
		if yes {
			live++
			if uint32(i) >= first && uint32(i) < last {
				allocations++
			}
		}
	}
	return
}
