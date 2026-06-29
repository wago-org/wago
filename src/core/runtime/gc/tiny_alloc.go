package gc

import (
	"errors"
	"fmt"
)

const (
	defaultTinyHeapBytes  = 64 << 10
	defaultTinyBlockBytes = 16
	defaultTinyStepBudget = 1
	tinyNoBlock           = ^uint32(0)
)

type tinyBlockIndex uint32

type tinyBlock struct {
	next uint32
	prev uint32
	size uint32
	used bool
}

type tinyHeap struct {
	mem        []byte
	blocks     []tinyBlock
	blockBytes uint32
	freeHead   uint32
}

func newTinyCollector(config Config, types []TypeDesc) (*Collector, error) {
	if config.TinyHeapBytes == 0 {
		config.TinyHeapBytes = defaultTinyHeapBytes
	}
	if config.TinyBlockBytes == 0 {
		config.TinyBlockBytes = defaultTinyBlockBytes
	}
	if config.TinyStepBudget == 0 {
		config.TinyStepBudget = defaultTinyStepBudget
	}
	if err := validateTinyConfig(config); err != nil {
		return nil, err
	}
	if err := ValidateTypeDescs(types); err != nil {
		return nil, err
	}
	c := &Collector{cfg: config, types: append([]TypeDesc(nil), types...), handles: []handleEntry{{}}}
	if err := c.initTypeIndex(); err != nil {
		return nil, err
	}
	blocks := config.TinyHeapBytes / config.TinyBlockBytes
	c.tiny = tinyHeap{mem: make([]byte, config.TinyHeapBytes), blocks: make([]tinyBlock, blocks), blockBytes: config.TinyBlockBytes, freeHead: 0}
	c.tiny.blocks[0] = tinyBlock{next: tinyNoBlock, prev: tinyNoBlock, size: blocks}
	c.tinyGC.state = tinyIdle
	c.tinyGC.sweep = 1
	c.tinyGC.color = []tinyColor{tinyWhite}
	return c, nil
}

func (c *Collector) initTypeIndex() error {
	var max TypeID
	for _, d := range c.types {
		if d.ID > max {
			max = d.ID
		}
	}
	c.typeIndex = make([]int, int(max)+1)
	for i := range c.typeIndex {
		c.typeIndex[i] = -1
	}
	for i, d := range c.types {
		if int(d.ID) >= len(c.typeIndex) {
			return errors.New("gc: internal type index error")
		}
		if c.typeIndex[d.ID] != -1 {
			return fmt.Errorf("gc: duplicate type id %d", d.ID)
		}
		c.typeIndex[d.ID] = i
	}
	return nil
}

func validateTinyConfig(config Config) error {
	if config.TinyHeapBytes == 0 {
		return errors.New("gc: tiny heap size is zero")
	}
	if config.TinyBlockBytes == 0 || config.TinyBlockBytes&(config.TinyBlockBytes-1) != 0 {
		return errors.New("gc: tiny block size must be a power of two")
	}
	if config.TinyBlockBytes < 8 {
		return errors.New("gc: tiny block size is smaller than object alignment")
	}
	if config.TinyHeapBytes%config.TinyBlockBytes != 0 {
		return errors.New("gc: tiny heap size must be a multiple of block size")
	}
	blocks := config.TinyHeapBytes / config.TinyBlockBytes
	if blocks == 0 || blocks == tinyNoBlock {
		return errors.New("gc: tiny heap block count out of range")
	}
	return nil
}

func (h *tinyHeap) Close() {
	h.mem = nil
	h.blocks = nil
	h.freeHead = tinyNoBlock
}

func (h *tinyHeap) bytes(off, size uint32) []byte { return h.mem[off : off+size] }

func (h *tinyHeap) alloc(size uint32) (uint32, uint32, error) {
	need := (size + h.blockBytes - 1) / h.blockBytes
	for b := h.freeHead; b != tinyNoBlock; b = h.blocks[b].next {
		span := h.blocks[b].size
		if span < need {
			continue
		}
		if span == need {
			h.unlinkFree(b)
			h.blocks[b].used = true
			return b * h.blockBytes, span * h.blockBytes, nil
		}
		rem := b + need
		h.blocks[rem] = tinyBlock{next: h.blocks[b].next, prev: h.blocks[b].prev, size: span - need}
		if h.blocks[rem].next != tinyNoBlock {
			h.blocks[h.blocks[rem].next].prev = rem
		}
		if h.blocks[rem].prev != tinyNoBlock {
			h.blocks[h.blocks[rem].prev].next = rem
		} else {
			h.freeHead = rem
		}
		h.blocks[b] = tinyBlock{next: tinyNoBlock, prev: tinyNoBlock, size: need, used: true}
		return b * h.blockBytes, need * h.blockBytes, nil
	}
	return 0, 0, errors.New("gc: tiny heap exhausted")
}

func (h *tinyHeap) free(off uint32) error {
	if h.blockBytes == 0 || off%h.blockBytes != 0 {
		return errors.New("gc: invalid tiny free offset")
	}
	b := off / h.blockBytes
	if int(b) >= len(h.blocks) || !h.blocks[b].used || h.blocks[b].size == 0 {
		return errors.New("gc: invalid tiny free span")
	}
	h.blocks[b].used = false
	h.insertFreeSorted(b)
	h.coalesce(b)
	return nil
}

func (h *tinyHeap) unlinkFree(b uint32) {
	n, p := h.blocks[b].next, h.blocks[b].prev
	if p != tinyNoBlock {
		h.blocks[p].next = n
	} else {
		h.freeHead = n
	}
	if n != tinyNoBlock {
		h.blocks[n].prev = p
	}
	h.blocks[b].next, h.blocks[b].prev = tinyNoBlock, tinyNoBlock
}

func (h *tinyHeap) insertFreeSorted(b uint32) {
	h.blocks[b].next, h.blocks[b].prev = tinyNoBlock, tinyNoBlock
	if h.freeHead == tinyNoBlock || b < h.freeHead {
		h.blocks[b].next = h.freeHead
		if h.freeHead != tinyNoBlock {
			h.blocks[h.freeHead].prev = b
		}
		h.freeHead = b
		return
	}
	cur := h.freeHead
	for h.blocks[cur].next != tinyNoBlock && h.blocks[cur].next < b {
		cur = h.blocks[cur].next
	}
	h.blocks[b].next = h.blocks[cur].next
	h.blocks[b].prev = cur
	if h.blocks[b].next != tinyNoBlock {
		h.blocks[h.blocks[b].next].prev = b
	}
	h.blocks[cur].next = b
}

func (h *tinyHeap) coalesce(b uint32) uint32 {
	if p := h.blocks[b].prev; p != tinyNoBlock && p+h.blocks[p].size == b {
		h.blocks[p].size += h.blocks[b].size
		h.unlinkFree(b)
		h.blocks[b] = tinyBlock{}
		b = p
	}
	if n := h.blocks[b].next; n != tinyNoBlock && b+h.blocks[b].size == n {
		h.blocks[b].size += h.blocks[n].size
		h.unlinkFree(n)
		h.blocks[n] = tinyBlock{}
	}
	return b
}

func (c *Collector) tinyAlloc(d TypeDesc, size, aux uint32, roots RootSet) (Ref, error) {
	if c.closed {
		return Null(), errors.New("gc: collector closed")
	}
	if c.cfg.CollectEveryAlloc || c.cfg.TinyCollectEveryAlloc {
		if roots == nil {
			return Null(), errors.New("gc: allocation-triggered collection requires roots")
		}
		if err := c.CollectFull(roots); err != nil {
			return Null(), err
		}
	} else if c.cfg.TinyStepEveryAlloc {
		if roots == nil {
			return Null(), errors.New("gc: allocation-triggered collection requires roots")
		}
		for i := uint32(0); i < c.cfg.TinyStepBudget; i++ {
			if err := c.Step(roots); err != nil {
				return Null(), err
			}
		}
	}
	if c.tinyGC.state == tinySweep {
		if roots == nil {
			return Null(), errors.New("gc: allocation during tiny sweep requires roots")
		}
		for c.tinyGC.state != tinyIdle {
			if err := c.Step(roots); err != nil {
				return Null(), err
			}
		}
	}
	off, _, err := c.tiny.alloc(size)
	if err != nil {
		if roots == nil {
			return Null(), errors.New("gc: tiny heap exhausted and no roots were supplied")
		}
		if err := c.CollectFull(roots); err != nil {
			return Null(), err
		}
		off, _, err = c.tiny.alloc(size)
		if err != nil {
			return Null(), errors.New("gc: tiny heap exhausted")
		}
	}
	h := c.newHandle(handleEntry{off: off, size: size, space: spaceTiny})
	r := makeObjRef(h)
	flags := uint32(0)
	if !d.HasRefs {
		flags |= FlagPointerFree
	}
	c.writeHeader(r, ObjHeader{TypeID: uint32(d.ID), Size: size, Aux: aux, Flags: flags})
	c.tinyPostAlloc(r, d)
	c.stats.Allocations++
	return r, nil
}

func (c *Collector) tinyPostAlloc(r Ref, d TypeDesc) {
	if !r.IsObj() || c.tinyGC.state == tinyIdle {
		return
	}
	h := handleOf(r)
	if c.tinyGC.state == tinySweep {
		c.tinySetColor(h, tinyBlack)
		return
	}
	if d.HasRefs {
		c.tinyGrayHandle(h)
		return
	}
	c.tinySetColor(h, tinyBlack)
}
