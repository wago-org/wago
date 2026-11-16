// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package directgc is an experimental, standalone moving offset collector.
// It is not a Wago runtime collector. All use must be serialized. References
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

type Collector struct {
	arena                                 []byte
	starts, dirty                         []bool
	forward                               []Ref
	queue                                 []Ref
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
	c := &Collector{arena: arena, starts: make([]bool, int(total/8)), dirty: make([]bool, int(total/8)), forward: make([]Ref, int(total/8)), queue: make([]Ref, int(total/uint64(header))), nurseryStart: 16, nurseryEnd: 16 + nurseryBytes, nurseryBump: 16, oldSize: oldBytes}
	c.oldStart = [2]uint32{c.nurseryEnd, c.nurseryEnd + oldBytes}
	c.oldBump = c.oldStart[0]
	return c, nil
}

// FixedBytes reports allocated arena (including alignment slack) and scalar
// tracing metadata bytes, excluding registered descriptors/generic shapes and
// Go slice/object headers. Scratch deliberately favors a simple bounded proof of
// correctness, not low memory overhead.
func (c *Collector) FixedBytes() uint64 {
	return uint64(len(c.arena)) + 15 + uint64(len(c.starts)) + uint64(len(c.dirty)) + 4*uint64(len(c.forward)) + 4*uint64(len(c.queue))
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
		c.dirty[uint32(r)/8] = true
	}
	return nil
}
func (c *Collector) stride(off uint32) uint32 { return (c.u32(off+4) + 15) &^ 15 }

// Minor evacuates all reachable nursery objects into active old space. Dirty
// old objects are conservative roots even when no longer globally reachable.
func (c *Collector) Minor(roots []*Ref) error { return c.collect(roots, false) }

// Full evacuates the reachable graph into the other old semispace. This prototype
// promotes every survivor and has no survivor age or large-object policy.
func (c *Collector) Full(roots []*Ref) error { return c.collect(roots, true) }

func (c *Collector) collect(roots []*Ref, full bool) error {
	// Scratch is not authoritative heap state and is cleared on ALL outcomes.
	clear(c.forward)
	defer clear(c.forward)
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
		if c.forward[uint32(r)/8] != 0 {
			return nil
		}
		size := c.stride(uint32(r))
		if size > limit-next {
			return ErrOOM
		}
		if count >= len(c.queue) {
			return ErrOOM
		} // header bounds make this unreachable.
		c.forward[uint32(r)/8] = Ref(next)
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
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.stride(off) {
			if c.dirty[off/8] {
				if err := scan(Ref(off)); err != nil {
					return err
				}
			}
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
		if f := c.forward[uint32(r)/8]; f != 0 {
			return f
		}
		return r
	}
	for i := 0; i < count; i++ {
		src := uint32(c.queue[i])
		dst := uint32(c.forward[src/8])
		copy(c.arena[dst:dst+c.stride(src)], c.arena[src:src+c.stride(src)])
		c.put32(dst+12, c.u32(dst+12)|FlagOld)
	}
	fix := func(off uint32) {
		_ = c.eachRef(off, func(at uint32) error { c.put32(at, uint32(rewrite(Ref(c.u32(at))))); return nil })
	}

	for i := 0; i < count; i++ {
		fix(uint32(c.forward[uint32(c.queue[i])/8]))
	}
	if !full {
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.stride(off) {
			if c.dirty[off/8] {
				fix(off)
			}
		}
	}
	// Duplicate root pointers are idempotent: destination offsets have no forward
	// entry because all targets are outside the precollection live source ranges.
	for _, p := range roots {
		*p = rewrite(*p)
	}
	for off := c.nurseryStart; off < c.nurseryBump; off += c.stride(off) {
		c.starts[off/8] = false
	}
	if full {
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.stride(off) {
			c.starts[off/8] = false
		}
	}
	for i := 0; i < count; i++ {
		c.starts[uint32(c.forward[uint32(c.queue[i])/8])/8] = true
	}
	clear(c.dirty)
	c.nurseryBump = c.nurseryStart
	c.oldBump = next
	c.active = target
	return nil
}
