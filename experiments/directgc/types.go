// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package directgc

import (
	"encoding/binary"
	"errors"
	"sort"
)

type TypeID uint32
type Kind uint8

const (
	KindStruct Kind = iota
	KindArray
)
const (
	HeaderSize    uint32 = 16
	PayloadOffset        = HeaderSize
)
const (
	FlagPointerFree uint32 = 1 << iota
	FlagForwarded
	FlagMarked
	FlagOld
	FlagLarge
)

// ObjHeader has the native collector's little-endian field order. Size is the
// 8-byte-rounded object extent; physical allocation stride rounds it to16 here.
type ObjHeader struct{ TypeID, Size, Aux, Flags uint32 }

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

var ErrType = errors.New("directgc: invalid type descriptor")

func cloneDescriptor(d Descriptor) (Descriptor, error) {
	if d.Align != 8 && d.Align != 16 {
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
func (c *Collector) allocate(id TypeID, payload uint64, aux uint32) (Ref, error) {
	size := (uint64(header) + payload + 7) &^ uint64(7)
	stride := (size + 15) &^ uint64(15)
	off := c.nurseryBump
	flags := uint32(0)
	if stride > uint64(c.nurseryEnd-c.nurseryStart) {
		// Only inherently oversized objects bypass Eden. A normally sized object
		// with a full nursery still asks its caller to collect and retry.
		if stride > uint64(c.oldStart[c.active]+c.oldSize-c.oldBump) {
			return 0, ErrOOM
		}
		off = c.oldBump
		c.oldBump += uint32(stride)
		flags |= FlagOld | FlagLarge
	} else {
		if stride > uint64(c.nurseryEnd-c.nurseryBump) {
			return 0, ErrOOM
		}
		c.nurseryBump += uint32(stride)
	}
	clear(c.arena[off : off+uint32(stride)])
	d := c.types[id]
	if !d.ElementRef && len(d.RefOffsets) == 0 {
		flags |= FlagPointerFree
	}
	c.put32(off, uint32(id))
	c.put32(off+4, uint32(size))
	c.put32(off+8, aux)
	c.put32(off+12, flags)
	c.starts[off/8] = true
	if flags&FlagOld != 0 {
		c.oldAllocations++
	}
	return Ref(off), nil
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
	return c.u32(uint32(r) + 8), nil
}
func (c *Collector) IsValid(r Ref) bool { return r == 0 || r.IsI31() || c.valid(r) }
func (c *Collector) Header(r Ref) (ObjHeader, error) {
	if !c.valid(r) {
		return ObjHeader{}, ErrRef
	}
	x := uint32(r)
	return ObjHeader{c.u32(x), c.u32(x + 4), c.u32(x + 8), c.u32(x + 12)}, nil
}
func (c *Collector) logicalPayload(r Ref) uint32 {
	d := c.types[c.u32(uint32(r))]
	if d.Kind == KindArray {
		return d.ElementSize * c.u32(uint32(r)+8)
	}
	return d.PayloadBytes
}

// Payload borrows mutable logical payload bytes (not header or padding). It is
// invalid after ANY collection. Trusted adapters must validate reference values
// and call Remember after raw reference writes, and may not use a retained slice
// across allocation safepoints. Prefer checked SetRef/ArraySetRef when possible.
func (c *Collector) Payload(r Ref) ([]byte, error) {
	if !c.valid(r) {
		return nil, ErrRef
	}
	x := uint32(r) + header
	return c.arena[x : x+c.logicalPayload(r) : x+c.logicalPayload(r)], nil
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
		if !d.ElementRef || index >= c.u32(uint32(r)+8) {
			return 0, ErrField
		}
		return uint32(r) + header + index*4, nil
	}
	if uint64(index) >= uint64(len(d.RefOffsets)) {
		return 0, ErrField
	}
	return uint32(r) + header + d.RefOffsets[index], nil
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
			for i, n := uint32(0), c.u32(off+8); i < n; i++ {
				if e := fn(off + header + i*4); e != nil {
					return e
				}
			}
		}
	} else {
		for _, o := range d.RefOffsets {
			if e := fn(off + header + o); e != nil {
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
		o := uint64(at - uint32(r) - header)
		if uint64(offset) < o+4 && o < end {
			return ErrField
		}
		return nil
	}); e != nil {
		return e
	}
	copy(c.arena[uint32(r)+header+offset:uint32(r)+header+uint32(end)], data)
	return nil
}
func Null() Ref               { return 0 }
func (r Ref) IsNull() bool    { return r == 0 }
func (r Ref) IsI31() bool     { return r&1 != 0 }
func (r Ref) IsObj() bool     { return r != 0 && r&1 == 0 }
func I31New(v int32) Ref      { return Ref(uint32(v)<<1) | 1 }
func (r Ref) I31GetU() uint32 { return uint32(r) >> 1 }
func (r Ref) I31GetS() int32  { return int32(uint32(r)) >> 1 }

// NewObject and Word are shape-based convenience helpers retained for the
// substrate graph tests. Real adapters should register descriptors explicitly.
func (c *Collector) NewObject(refs, words uint32) (Ref, error) {
	size := objectSize(refs, words)
	if size > uint64(c.nurseryEnd-c.nurseryBump) {
		return 0, ErrOOM
	}
	key := [2]uint32{refs, words}
	id, ok := c.generic[key]
	if !ok {
		d := Descriptor{Kind: KindStruct, PayloadBytes: uint32(size) - header, Align: 8, RefOffsets: make([]uint32, refs)}
		for i := range d.RefOffsets {
			d.RefOffsets[i] = uint32(i) * 4
		}
		var e error
		id, e = c.AddTypes([]Descriptor{d})
		if e != nil {
			return 0, e
		}
		if c.generic == nil {
			c.generic = make(map[[2]uint32]TypeID)
		}
		c.generic[key] = id
	}
	return c.NewStruct(id)
}
func (c *Collector) wordOffset(r Ref, index uint32) (uint32, error) {
	if !c.valid(r) {
		return 0, ErrRef
	}
	id := TypeID(c.u32(uint32(r)))
	for key, shape := range c.generic {
		if shape == id {
			if index >= key[1] {
				return 0, ErrField
			}
			return uint32(r) + header + ((key[0]*4 + 7) &^ 7) + index*8, nil
		}
	}
	return 0, ErrField
}
func (c *Collector) Word(r Ref, index uint32) (uint64, error) {
	off, e := c.wordOffset(r, index)
	if e != nil {
		return 0, e
	}
	return binary.LittleEndian.Uint64(c.arena[off : off+8]), nil
}
func (c *Collector) SetWord(r Ref, index uint32, v uint64) error {
	off, e := c.wordOffset(r, index)
	if e != nil {
		return e
	}
	binary.LittleEndian.PutUint64(c.arena[off:off+8], v)
	return nil
}

// ObjectBytes borrows the complete native Size extent, including header and
// 8-byte padding (not physical stride padding). This is a trusted adapter lease:
// any collection invalidates it. The adapter must not change TypeID, Size, Aux,
// or tracing flags, must enforce reference types, and must Remember raw writes.
func (c *Collector) ObjectBytes(r Ref) ([]byte, error) {
	if !c.valid(r) {
		return nil, ErrRef
	}
	off := uint32(r)
	size := c.u32(off + 4)
	if size < header || uint64(off)+uint64(size) > uint64(len(c.arena)) {
		return nil, ErrRef
	}
	return c.arena[off : off+size : off+size], nil
}
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
