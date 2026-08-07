package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type tinyGCState uint8

const (
	tinyIdle tinyGCState = iota
	tinyMark
	tinyRemark
	tinySweep
)

type tinyColor uint8

const (
	tinyWhite tinyColor = iota
	tinyGray
	tinyBlack
)

type tinyGC struct {
	state     tinyGCState
	color     []tinyColor
	grayStack []uint32
	sweep     uint32
	cycles    uint64
}

// Step performs one bounded unit of Tiny incremental tri-color collection. When
// called while idle it starts a new cycle by graying the supplied roots.
func (c *Collector) Step(roots RootSet) error {
	if c.cfg.Profile != ProfileTiny {
		return c.CollectMinor(roots)
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if c.tinyGC.state == tinyIdle {
		c.tinyStartMark(roots)
		return nil
	}
	if c.tinyGC.state == tinyMark {
		if len(c.tinyGC.grayStack) == 0 {
			c.tinyGC.state = tinyRemark
			return nil
		}
		n := len(c.tinyGC.grayStack) - 1
		h := c.tinyGC.grayStack[n]
		c.tinyGC.grayStack = c.tinyGC.grayStack[:n]
		if int(h) < len(c.handles) && c.handles[h].space == spaceTiny {
			c.tinySetColor(h, tinyBlack)
			c.scanObjectRefs(h, c.tinyMarkRef)
		}
		return nil
	}
	if c.tinyGC.state == tinyRemark {
		if len(c.tinyGC.grayStack) > 0 {
			c.tinyGC.state = tinyMark
			return nil
		}
		c.tinyMarkRoots(roots)
		if len(c.tinyGC.grayStack) > 0 {
			c.tinyGC.state = tinyMark
			return nil
		}
		c.tinyGC.state = tinySweep
		c.tinyGC.sweep = 1
		return nil
	}
	if c.tinyGC.sweep >= uint32(len(c.handles)) {
		c.tinyFinishCycle()
		return nil
	}
	h := c.tinyGC.sweep
	c.tinyGC.sweep++
	if c.handles[h].space == spaceTiny {
		switch c.tinyColorOf(h) {
		case tinyWhite:
			c.free(h)
		case tinyBlack:
			c.tinySetColor(h, tinyWhite)
		case tinyGray:
			return fmt.Errorf("gc: gray object %d reached tiny sweep", h)
		default:
			return fmt.Errorf("gc: invalid tiny color for handle %d", h)
		}
	}
	return nil
}

func (c *Collector) tinyCollectFull(roots RootSet) error {
	c.tinyStartMark(roots)
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(roots); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) tinyStartMark(roots RootSet) {
	if len(c.tinyGC.color) < len(c.handles) {
		more := make([]tinyColor, len(c.handles)-len(c.tinyGC.color))
		c.tinyGC.color = append(c.tinyGC.color, more...)
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space == spaceTiny {
			c.tinySetColor(h, tinyWhite)
		}
	}
	c.tinyGC.grayStack = c.tinyGC.grayStack[:0]
	c.tinyGC.state = tinyMark
	c.tinyGC.sweep = 1
	c.tinyMarkRoots(roots)
}

func (c *Collector) tinyMarkRoots(roots RootSet) {
	if direct, ok := roots.(DirectRootRefSet); ok {
		c.markDirectRoots(direct, rootMarkTiny)
	} else if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.tinyMarkRef(r); return true }) {
		roots.RangeRoots(func(s RootSlot) bool { c.tinyMarkRef(s.GetRef()); return true })
	}
	for _, r := range c.globalSlots {
		c.tinyMarkRef(r)
	}
	for _, r := range c.tableSlots {
		c.tinyMarkRef(r)
	}
}

func (c *Collector) tinyFinishCycle() {
	c.tinyGC.grayStack = c.tinyGC.grayStack[:0]
	c.tinyGC.state = tinyIdle
	c.tinyGC.sweep = 1
	c.tinyGC.cycles++
}

func (c *Collector) tinyMarkRef(r Ref) {
	if !r.IsObj() {
		return
	}
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space != spaceTiny {
		return
	}
	if c.tinyColorOf(h) != tinyWhite {
		return
	}
	c.tinyGrayHandle(h)
}

func (c *Collector) tinyMarkRefNow(r Ref) {
	c.tinyMarkRef(r)
	for len(c.tinyGC.grayStack) > 0 {
		n := len(c.tinyGC.grayStack) - 1
		h := c.tinyGC.grayStack[n]
		c.tinyGC.grayStack = c.tinyGC.grayStack[:n]
		if int(h) >= len(c.handles) || c.handles[h].space != spaceTiny || c.tinyColorOf(h) != tinyGray {
			continue
		}
		c.tinySetColor(h, tinyBlack)
		c.scanObjectRefs(h, c.tinyMarkRef)
	}
}

func (c *Collector) tinyGrayHandle(h uint32) {
	if c.tinyColorOf(h) == tinyGray {
		return
	}
	c.tinySetColor(h, tinyGray)
	c.tinyGC.grayStack = append(c.tinyGC.grayStack, h)
}

func (c *Collector) tinyColorOf(h uint32) tinyColor {
	if int(h) >= len(c.tinyGC.color) {
		return tinyWhite
	}
	return c.tinyGC.color[h]
}

func (c *Collector) tinySetColor(h uint32, color tinyColor) {
	for int(h) >= len(c.tinyGC.color) {
		c.tinyGC.color = append(c.tinyGC.color, tinyWhite)
	}
	c.tinyGC.color[h] = color
}

func (c *Collector) scanObjectRefs(h uint32, visit func(Ref)) {
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs {
		return
	}
	hdr := c.header(r)
	b := c.bytes(r)
	if d.Kind == KindStruct {
		for _, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				visit(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
			}
		}
	} else if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < hdr.Aux; i++ {
			off := PayloadOffset + i*d.ElemSize
			visit(Ref(binary.LittleEndian.Uint32(b[off:])))
		}
	}
}

func (c *Collector) verifyTiny(roots RootSet) error {
	if c.tiny.blockBytes == 0 || len(c.tiny.mem) == 0 {
		return errors.New("gc: tiny heap is not initialized")
	}
	liveBlocks := make([]bool, c.tiny.blockCount)
	for h := uint32(1); int(h) < len(c.handles); h++ {
		e := c.handles[h]
		if e.space == spaceFree {
			continue
		}
		if e.space != spaceTiny {
			return fmt.Errorf("gc: non-tiny handle %d in tiny collector", h)
		}
		memBytes := uint32(len(c.tiny.mem))
		if e.off%c.tiny.blockBytes != 0 || e.off > memBytes || e.size > memBytes-e.off {
			return fmt.Errorf("gc: tiny handle %d out of bounds", h)
		}
		bi := e.off / c.tiny.blockBytes
		if bi >= c.tiny.blockCount || !c.tiny.isUsedStart(bi) {
			return fmt.Errorf("gc: tiny handle %d points to free span", h)
		}
		spanBlocks := c.tiny.spanSize(bi)
		if spanBlocks == 0 || spanBlocks > c.tiny.blockCount-bi {
			return fmt.Errorf("gc: tiny handle %d has invalid allocation span", h)
		}
		spanBytes := spanBlocks * c.tiny.blockBytes
		if e.size > spanBytes {
			return fmt.Errorf("gc: tiny handle %d exceeds allocation span", h)
		}
		for i := bi; i < bi+spanBlocks; i++ {
			if int(i) >= len(liveBlocks) || liveBlocks[i] {
				return fmt.Errorf("gc: tiny live span overlap at block %d", i)
			}
			liveBlocks[i] = true
		}
		if col := c.tinyColorOf(h); col != tinyWhite && col != tinyGray && col != tinyBlack {
			return fmt.Errorf("gc: invalid tiny color for handle %d", h)
		}
	}
	if err := c.verifyTinyFreeList(liveBlocks); err != nil {
		return err
	}
	return nil
}

func (c *Collector) verifyTinyFreeList(live []bool) error {
	seenStarts := make([]bool, c.tiny.blockCount)
	freeBlocks := make([]bool, c.tiny.blockCount)
	var summary uint64
	for word, occupied := range c.tiny.binWords {
		if occupied != 0 {
			summary |= uint64(1) << uint32(word)
		}
	}
	if summary != c.tiny.binSummary {
		return errors.New("gc: tiny free-bin summary disagrees with occupancy")
	}
	if lastBits := uint32(len(c.tiny.binHeads)) & 63; lastBits != 0 {
		valid := (uint64(1) << lastBits) - 1
		if c.tiny.binWords[len(c.tiny.binWords)-1]&^valid != 0 {
			return errors.New("gc: tiny free-bin bitmap marks an invalid bin")
		}
	}
	for bin, head := range c.tiny.binHeads {
		marked := c.tiny.binWords[uint32(bin)>>6]&(uint64(1)<<(uint32(bin)&63)) != 0
		if (head != tinyNoBlock) != marked {
			return errors.New("gc: tiny free-bin bitmap disagrees with head")
		}
		prev := tinyNoBlock
		for b := head; b != tinyNoBlock; {
			if b >= c.tiny.blockCount || seenStarts[b] {
				return errors.New("gc: tiny free list out of bounds or cyclic")
			}
			seenStarts[b] = true
			size := c.tiny.spanSize(b)
			if size == 0 || size > c.tiny.blockCount-b || c.tiny.isUsedStart(b) || tinyBinForSize(size) != uint32(bin) {
				return errors.New("gc: malformed tiny free span")
			}
			next, recordedPrev := c.tiny.freeLinks(b)
			if recordedPrev != prev {
				return errors.New("gc: malformed tiny free links")
			}
			for i := b; i < b+size; i++ {
				if live[i] || freeBlocks[i] {
					return errors.New("gc: tiny free span overlaps another span")
				}
				freeBlocks[i] = true
			}
			prev, b = b, next
		}
	}
	var previousFree bool
	for b := uint32(0); b < c.tiny.blockCount; {
		size := c.tiny.spanSize(b)
		if size == 0 || size > c.tiny.blockCount-b || c.tiny.spanSize(b+size-1) != size {
			return errors.New("gc: malformed tiny span boundaries")
		}
		used := c.tiny.isUsedStart(b)
		if used == seenStarts[b] {
			return errors.New("gc: tiny span missing from allocation or free metadata")
		}
		if !used && previousFree {
			return errors.New("gc: adjacent tiny free spans not coalesced")
		}
		for i := b; i < b+size; i++ {
			if used != live[i] || (!used) != freeBlocks[i] {
				return errors.New("gc: tiny span coverage disagrees with handles")
			}
			if i != b && c.tiny.isUsedStart(i) {
				return errors.New("gc: tiny allocation-start bitmap marks a span interior")
			}
		}
		previousFree = !used
		b += size
	}
	return nil
}
