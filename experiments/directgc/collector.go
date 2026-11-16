// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package directgc is an experimental moving offset collector substrate.
// Wago supplies a separately admitted runtime adapter. All use must be serialized. References
// copied outside declared object fields and supplied mutable roots become stale
// after collection; offsets carry neither ownership nor generation tags.
package directgc

import (
	"encoding/binary"
	"errors"
	"unsafe"
)

type Ref uint32

var ErrOOM = errors.New("directgc: bounded arena exhausted")
var ErrRef = errors.New("directgc: invalid object reference")
var ErrField = errors.New("directgc: invalid field")
var ErrConfig = errors.New("directgc: invalid arena configuration")

const header = uint32(16)
const rememberedParentCapacity = 1024

type Collector struct {
	retiredNurseryAllocations             uint64
	oldAllocations                        uint64
	arena                                 []byte
	starts, dirty                         []bool
	mark                                  []uint64
	queue                                 []Ref
	remembered                            []Ref
	rememberedOverflow                    bool
	retirementDense                       bool
	nurseryStart, nurseryEnd, nurseryBump uint32
	oldStart                              [2]uint32
	oldSize, oldBump                      uint32
	active                                int
	types                                 []Descriptor
	generic                               map[[2]uint32]TypeID
}

// New allocates all arena and tracing scratch upfront. OldBytes is usable
// capacity of EACH old semispace; the total arena is 16+NurseryBytes+2*OldBytes.
// Go allocation failures are not recovered; ErrOOM refers to arena capacity.
func New(nurseryBytes, oldBytes uint32) (*Collector, error) {
	total := uint64(16) + uint64(nurseryBytes) + 2*uint64(oldBytes)
	if nurseryBytes < header || oldBytes < header || nurseryBytes%16 != 0 || oldBytes%16 != 0 || total > uint64(^uint32(0)) || total > uint64(^uint(0)>>1)-15 {
		return nil, ErrConfig
	}
	raw := make([]byte, int(total)+15)
	start := int((16 - uintptr(unsafe.Pointer(&raw[0]))%16) % 16)
	arena := raw[start : start+int(total) : start+int(total)]
	c := &Collector{arena: arena, starts: make([]bool, int(total/8)), dirty: make([]bool, int(total/8)), mark: make([]uint64, int((total/16+63)/64)), queue: make([]Ref, int(total/uint64(header))), remembered: make([]Ref, 0, min(rememberedParentCapacity, int(oldBytes/header))), nurseryStart: 16, nurseryEnd: 16 + nurseryBytes, nurseryBump: 16, oldSize: oldBytes}
	c.oldStart = [2]uint32{c.nurseryEnd, c.nurseryEnd + oldBytes}
	c.oldBump = c.oldStart[0]
	return c, nil
}

// FixedBytes reports allocated arena (including alignment slack) and scalar
// tracing metadata bytes, excluding registered descriptors/generic shapes and
// Go slice/object headers. Scratch deliberately favors a simple bounded proof of
// correctness, not low memory overhead.
func (c *Collector) FixedBytes() uint64 {
	return uint64(len(c.arena)) + 15 + uint64(len(c.starts)) + uint64(len(c.dirty)) + 8*uint64(len(c.mark)) + 4*uint64(len(c.queue)) + 4*uint64(cap(c.remembered))
}
func (c *Collector) young(r Ref) bool {
	return uint32(r) >= c.nurseryStart && uint32(r) < c.nurseryBump
}
func (c *Collector) valid(r Ref) bool {
	x := uint32(r)
	return x != 0 && x%16 == 0 && int(x/8) < len(c.starts) && c.starts[x/8] && (c.young(r) || (x >= c.oldStart[c.active] && x < c.oldBump))
}
func (c *Collector) u32(off uint32) uint32 { return binary.LittleEndian.Uint32(c.arena[off : off+4]) }
func (c *Collector) put32(off, v uint32)   { binary.LittleEndian.PutUint32(c.arena[off:off+4], v) }
func objectSize(refs, words uint32) uint64 {
	refEnd := (uint64(header) + uint64(refs)*4 + 7) &^ uint64(7)
	return refEnd + uint64(words)*8
}
func (c *Collector) RefField(r Ref, index uint32) (Ref, error) {
	at, e := c.refOffset(r, index)
	if e != nil {
		return 0, e
	}
	return Ref(c.u32(at)), nil
}
func (c *Collector) SetRef(r Ref, index uint32, v Ref) error {
	at, e := c.refOffset(r, index)
	if e != nil {
		return e
	}
	if !c.IsValid(v) {
		return ErrRef
	}
	c.put32(at, uint32(v))
	if !c.young(r) && v.IsObj() && c.young(v) {
		c.rememberParent(r)
	}
	return nil
}
func (c *Collector) stride(off uint32) uint32 { return (c.u32(off+4) + 15) &^ 15 }

// Minor evacuates all reachable nursery objects into active old space. Dirty
// old objects are conservative roots even when no longer globally reachable.
func (c *Collector) Minor(roots []*Ref) error { return c.collect(roots, false) }

// Full evacuates the reachable graph into the other old semispace. This prototype
// promotes every survivor and has no survivor age policy. Oversized objects
// allocated directly in old space move with other live objects during Full.
func (c *Collector) Full(roots []*Ref) error { return c.collect(roots, true) }

func (c *Collector) collect(roots []*Ref, full bool) error {
	// Scratch is not authoritative heap state and is cleared on ALL outcomes.
	clear(c.mark)
	defer clear(c.mark)
	target := c.active
	next := c.oldBump
	if full {
		target = 1 - c.active
		next = c.oldStart[target]
	}
	limit := c.oldStart[target] + c.oldSize
	count := 0
	visit := func(r Ref) error {
		if r == 0 || r.IsI31() {
			return nil
		}
		if !c.valid(r) {
			return ErrRef
		}
		if !full && !c.young(r) {
			return nil
		}
		slot := uint32(r) / 16
		if c.mark[slot/64]&(uint64(1)<<(slot%64)) != 0 {
			return nil
		}
		size := c.stride(uint32(r))
		if size > limit-next {
			return ErrOOM
		}
		if count >= len(c.queue) {
			return ErrOOM
		} // header bounds make this unreachable.
		c.mark[slot/64] |= uint64(1) << (slot % 64)
		next += size
		c.queue[count] = r
		count++
		return nil
	}
	scan := func(r Ref) error { return c.eachRef(uint32(r), func(at uint32) error { return visit(Ref(c.u32(at))) }) }

	for _, p := range roots {
		if p == nil {
			return ErrRef
		}
		if err := visit(*p); err != nil {
			return err
		}
	}
	if !full {
		if err := c.rangeRemembered(scan); err != nil {
			return err
		}
	}
	for i := 0; i < count; i++ {
		if err := scan(c.queue[i]); err != nil {
			return err
		}
	}
	// No fallible work follows: all object fields and roots were validated,
	// destinations are disjoint from sources, and capacity is reserved in scratch.
	rewrite := func(r Ref) Ref {
		if r == 0 || r.IsI31() {
			return r
		}
		// Destinations are outside the original live ranges. This also makes
		// repeated root pointers idempotent without consulting overwritten Aux.
		if c.valid(r) && c.u32(uint32(r)+12)&FlagForwarded != 0 {
			return Ref(c.u32(uint32(r) + 8))
		}
		return r
	}
	dst := c.oldBump
	if full {
		dst = c.oldStart[target]
	}
	for i := 0; i < count; i++ {
		src := uint32(c.queue[i])
		stride := c.stride(src)
		// Preserve original Aux (packed array length) in the destination BEFORE
		// overwriting source Aux with its forwarding address. No failures remain.
		copy(c.arena[dst:dst+stride], c.arena[src:src+stride])
		c.put32(dst+12, (c.u32(dst+12)&^FlagForwarded)|FlagOld)
		c.put32(src+8, dst)
		c.put32(src+12, c.u32(src+12)|FlagForwarded)
		dst += stride
	}
	fix := func(off uint32) {
		_ = c.eachRef(off, func(at uint32) error { c.put32(at, uint32(rewrite(Ref(c.u32(at))))); return nil })
	}

	for i := 0; i < count; i++ {
		fix(c.u32(uint32(c.queue[i]) + 8))
	}
	if !full {
		_ = c.rangeRemembered(func(r Ref) error { fix(uint32(r)); return nil })
	}
	// Duplicate root pointers are idempotent: destinations lie outside the
	// precollection live source ranges and cannot expose a source forwarding flag.
	for _, p := range roots {
		*p = rewrite(*p)
	}
	c.retireNursery()
	if full {
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.stride(off) {
			c.starts[off/8] = false
		}
	}
	for i := 0; i < count; i++ {
		c.starts[c.u32(uint32(c.queue[i])+8)/8] = true
	}
	c.resetRemembered(full)
	c.nurseryBump = c.nurseryStart
	c.oldBump = next
	c.active = target
	return nil
}
