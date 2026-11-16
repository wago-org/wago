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
}

// New allocates all arena and tracing scratch upfront. OldBytes is usable
// capacity of EACH old semispace; the total arena is 8+NurseryBytes+2*OldBytes.
// Go allocation failures are not recovered; ErrOOM refers to arena capacity.
func New(nurseryBytes, oldBytes uint32) (*Collector, error) {
	total := uint64(8) + uint64(nurseryBytes) + 2*uint64(oldBytes)
	if nurseryBytes < header || oldBytes < header || nurseryBytes%8 != 0 || oldBytes%8 != 0 || total > uint64(^uint32(0)) || total > uint64(^uint(0)>>1) {
		return nil, ErrConfig
	}
	c := &Collector{arena: make([]byte, int(total)), starts: make([]bool, int(total/8)), dirty: make([]bool, int(total/8)), forward: make([]Ref, int(total/8)), queue: make([]Ref, int(total/uint64(header))), nurseryStart: 8, nurseryEnd: 8 + nurseryBytes, nurseryBump: 8, oldSize: oldBytes}
	c.oldStart = [2]uint32{c.nurseryEnd, c.nurseryEnd + oldBytes}
	c.oldBump = c.oldStart[0]
	return c, nil
}

// FixedBytes reports allocated arena and scalar tracing metadata bytes, excluding
// Go slice/object headers. Scratch deliberately favors a simple bounded proof of
// correctness, not low memory overhead.
func (c *Collector) FixedBytes() uint64 {
	return uint64(len(c.arena)) + uint64(len(c.starts)) + uint64(len(c.dirty)) + 4*uint64(len(c.forward)) + 4*uint64(len(c.queue))
}
func (c *Collector) young(r Ref) bool {
	return uint32(r) >= c.nurseryStart && uint32(r) < c.nurseryBump
}
func (c *Collector) valid(r Ref) bool {
	x := uint32(r)
	return x != 0 && x%8 == 0 && int(x/8) < len(c.starts) && c.starts[x/8] && (c.young(r) || (x >= c.oldStart[c.active] && x < c.oldBump))
}
func (c *Collector) u32(off uint32) uint32 { return binary.LittleEndian.Uint32(c.arena[off : off+4]) }
func (c *Collector) put32(off, v uint32)   { binary.LittleEndian.PutUint32(c.arena[off:off+4], v) }
func objectSize(refs, words uint32) uint64 {
	refEnd := (uint64(header) + uint64(refs)*4 + 7) &^ uint64(7)
	return refEnd + uint64(words)*8
}
func (c *Collector) NewObject(refs, words uint32) (Ref, error) {
	size := objectSize(refs, words)
	if size > uint64(c.nurseryEnd-c.nurseryBump) {
		return 0, ErrOOM
	}
	off := c.nurseryBump
	c.nurseryBump += uint32(size)
	clear(c.arena[off:c.nurseryBump])
	c.put32(off, uint32(size))
	c.put32(off+4, refs)
	c.put32(off+8, words)
	c.starts[off/8] = true
	return Ref(off), nil
}
func (c *Collector) RefField(r Ref, index uint32) (Ref, error) {
	if !c.valid(r) {
		return 0, ErrRef
	}
	if index >= c.u32(uint32(r)+4) {
		return 0, ErrField
	}
	return Ref(c.u32(uint32(r) + header + index*4)), nil
}
func (c *Collector) SetRef(r Ref, index uint32, v Ref) error {
	if !c.valid(r) || (v != 0 && !c.valid(v)) {
		return ErrRef
	}
	if index >= c.u32(uint32(r)+4) {
		return ErrField
	}
	c.put32(uint32(r)+header+index*4, uint32(v))
	if !c.young(r) && c.young(v) {
		c.dirty[uint32(r)/8] = true
	}
	return nil
}
func (c *Collector) wordOffset(r Ref, index uint32) (uint32, error) {
	if !c.valid(r) {
		return 0, ErrRef
	}
	if index >= c.u32(uint32(r)+8) {
		return 0, ErrField
	}
	return uint32((uint64(r)+uint64(header)+uint64(c.u32(uint32(r)+4))*4+7)&^uint64(7)) + index*8, nil
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
		if r == 0 {
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
		size := c.u32(uint32(r))
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
	scan := func(r Ref) error {
		n := c.u32(uint32(r) + 4)
		for i := uint32(0); i < n; i++ {
			if err := visit(Ref(c.u32(uint32(r) + header + i*4))); err != nil {
				return err
			}
		}
		return nil
	}
	for _, p := range roots {
		if p == nil {
			return ErrRef
		}
		if err := visit(*p); err != nil {
			return err
		}
	}
	if !full {
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.u32(off) {
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
		if r == 0 {
			return 0
		}
		if f := c.forward[uint32(r)/8]; f != 0 {
			return f
		}
		return r
	}
	for i := 0; i < count; i++ {
		src := uint32(c.queue[i])
		dst := uint32(c.forward[src/8])
		copy(c.arena[dst:dst+c.u32(src)], c.arena[src:src+c.u32(src)])
	}
	fix := func(off uint32) {
		for i, n := uint32(0), c.u32(off+4); i < n; i++ {
			at := off + header + i*4
			c.put32(at, uint32(rewrite(Ref(c.u32(at)))))
		}
	}
	for i := 0; i < count; i++ {
		fix(uint32(c.forward[uint32(c.queue[i])/8]))
	}
	if !full {
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.u32(off) {
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
	for off := c.nurseryStart; off < c.nurseryBump; off += c.u32(off) {
		c.starts[off/8] = false
	}
	if full {
		for off := c.oldStart[c.active]; off < c.oldBump; off += c.u32(off) {
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
