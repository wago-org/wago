package gc

import (
	"errors"
	"fmt"
)

const throughputNoSlot = ^uint32(0)

var throughputClassSizes = [...]uint32{32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 3072, 4096, 8192, 16384, 32768}

type throughputFreeSlot struct {
	off  uint32
	next uint32
}

type throughputLargeFree struct {
	off  uint32
	size uint32
}

type throughputHeap struct {
	mem        []byte
	limit      uint32
	pageBytes  uint32
	classLimit uint32
	bump       uint32
	freeHeads  []uint32
	freeSlots  [][]throughputFreeSlot
	largeFree  []throughputLargeFree
}

func (h *throughputHeap) Init(cfg Config) error {
	if cfg.ThroughputHeapBytes == 0 || cfg.ThroughputPageBytes == 0 || cfg.ThroughputClassLimit == 0 {
		return errors.New("gc: invalid throughput heap defaults")
	}
	if cfg.ThroughputPageBytes < 4096 || cfg.ThroughputPageBytes&(cfg.ThroughputPageBytes-1) != 0 {
		return errors.New("gc: throughput page size must be a power of two at least 4KiB")
	}
	if cfg.ThroughputHeapBytes < cfg.ThroughputPageBytes {
		return errors.New("gc: throughput heap limit smaller than page size")
	}
	if !supportedThroughputClassLimit(cfg.ThroughputClassLimit) {
		return fmt.Errorf("gc: throughput class limit %d is not a supported size class", cfg.ThroughputClassLimit)
	}
	h.limit = cfg.ThroughputHeapBytes
	h.pageBytes = cfg.ThroughputPageBytes
	h.classLimit = cfg.ThroughputClassLimit
	h.mem = make([]byte, 0)
	h.bump = 0
	h.freeHeads = make([]uint32, len(throughputClassSizes))
	h.freeSlots = make([][]throughputFreeSlot, len(throughputClassSizes))
	for i := range h.freeHeads {
		h.freeHeads[i] = throughputNoSlot
	}
	return nil
}

func (h *throughputHeap) Close() {
	h.mem = nil
	h.freeHeads = nil
	h.freeSlots = nil
	h.largeFree = nil
	h.bump = 0
}

func (h *throughputHeap) bytes(e handleEntry) []byte { return h.mem[e.off : e.off+e.size] }

func (h *throughputHeap) alloc(size uint32, sp spaceKind) (handleEntry, error) {
	allocSize := Align8(size)
	if sp != spaceLarge && allocSize <= h.classLimit {
		cls := h.classFor(allocSize)
		if cls < 0 {
			return handleEntry{}, fmt.Errorf("gc: no throughput size class for %d", allocSize)
		}
		classSize := throughputClassSizes[cls]
		if head := h.freeHeads[cls]; head != throughputNoSlot {
			slot := h.freeSlots[cls][head]
			h.freeHeads[cls] = slot.next
			return handleEntry{off: slot.off, size: size, allocSize: classSize, class: uint16(cls), space: sp}, nil
		}
		off, err := h.grow(classSize)
		if err != nil {
			return handleEntry{}, err
		}
		return handleEntry{off: off, size: size, allocSize: classSize, class: uint16(cls), space: sp}, nil
	}
	if idx := h.findLarge(allocSize); idx >= 0 {
		span := h.largeFree[idx]
		off := span.off
		if span.size-allocSize >= 32 {
			h.largeFree[idx].off += allocSize
			h.largeFree[idx].size -= allocSize
		} else {
			allocSize = span.size
			h.largeFree = append(h.largeFree[:idx], h.largeFree[idx+1:]...)
		}
		return handleEntry{off: off, size: size, allocSize: allocSize, class: uint16(len(throughputClassSizes)), space: sp}, nil
	}
	off, err := h.grow(allocSize)
	if err != nil {
		return handleEntry{}, err
	}
	return handleEntry{off: off, size: size, allocSize: allocSize, class: uint16(len(throughputClassSizes)), space: sp}, nil
}

func (h *throughputHeap) free(e handleEntry) error {
	if e.allocSize == 0 || e.off+e.allocSize > uint32(len(h.mem)) {
		return errors.New("gc: invalid throughput free span")
	}
	if int(e.class) < len(throughputClassSizes) && e.allocSize == throughputClassSizes[e.class] {
		idx := uint32(len(h.freeSlots[e.class]))
		h.freeSlots[e.class] = append(h.freeSlots[e.class], throughputFreeSlot{off: e.off, next: h.freeHeads[e.class]})
		h.freeHeads[e.class] = idx
		return nil
	}
	h.insertLargeFree(throughputLargeFree{off: e.off, size: e.allocSize})
	return nil
}

func (h *throughputHeap) classFor(size uint32) int {
	for i, sz := range throughputClassSizes {
		if size <= sz && sz <= h.classLimit {
			return i
		}
	}
	return -1
}

func supportedThroughputClassLimit(limit uint32) bool {
	for _, sz := range throughputClassSizes {
		if limit == sz {
			return true
		}
	}
	return false
}

func (h *throughputHeap) grow(size uint32) (uint32, error) {
	off := Align8(h.bump)
	end := off + size
	if end < off || end > h.limit {
		return 0, errors.New("gc: throughput heap exhausted")
	}
	needLen := align(end, h.pageBytes)
	if needLen > h.limit {
		needLen = end
	}
	if needLen > uint32(len(h.mem)) {
		newMem := make([]byte, needLen)
		copy(newMem, h.mem)
		h.mem = newMem
	}
	h.bump = end
	return off, nil
}

func (h *throughputHeap) findLarge(size uint32) int {
	for i, s := range h.largeFree {
		if s.size >= size {
			return i
		}
	}
	return -1
}

func (h *throughputHeap) insertLargeFree(s throughputLargeFree) {
	pos := 0
	for pos < len(h.largeFree) && h.largeFree[pos].off < s.off {
		pos++
	}
	h.largeFree = append(h.largeFree, throughputLargeFree{})
	copy(h.largeFree[pos+1:], h.largeFree[pos:])
	h.largeFree[pos] = s
	if pos > 0 && h.largeFree[pos-1].off+h.largeFree[pos-1].size == h.largeFree[pos].off {
		h.largeFree[pos-1].size += h.largeFree[pos].size
		h.largeFree = append(h.largeFree[:pos], h.largeFree[pos+1:]...)
		pos--
	}
	if pos+1 < len(h.largeFree) && h.largeFree[pos].off+h.largeFree[pos].size == h.largeFree[pos+1].off {
		h.largeFree[pos].size += h.largeFree[pos+1].size
		h.largeFree = append(h.largeFree[:pos+1], h.largeFree[pos+2:]...)
	}
}

func (h *throughputHeap) verify(handles []handleEntry) error {
	if h.limit == 0 {
		return errors.New("gc: throughput heap is not initialized")
	}
	memLen := uint32(len(h.mem))
	live := make([]throughputLargeFree, 0)
	for i, e := range handles {
		if i == 0 || e.space == spaceFree || e.space == spaceNursery || e.space == spaceTiny {
			continue
		}
		if e.space != spaceOld && e.space != spaceLarge {
			return fmt.Errorf("gc: invalid throughput space for handle %d", i)
		}
		if e.allocSize == 0 || e.size > e.allocSize || e.off+e.allocSize < e.off || e.off+e.allocSize > memLen {
			return fmt.Errorf("gc: throughput handle %d out of bounds", i)
		}
		for _, s := range live {
			if spansOverlap(e.off, e.allocSize, s.off, s.size) {
				return fmt.Errorf("gc: throughput live span overlap at handle %d", i)
			}
		}
		live = append(live, throughputLargeFree{off: e.off, size: e.allocSize})
	}
	free := make([]throughputLargeFree, 0)
	for cls, head := range h.freeHeads {
		seenIdx := make(map[uint32]bool)
		classSize := throughputClassSizes[cls]
		for idx := head; idx != throughputNoSlot; idx = h.freeSlots[cls][idx].next {
			if int(idx) >= len(h.freeSlots[cls]) || seenIdx[idx] {
				return errors.New("gc: malformed throughput class free list")
			}
			seenIdx[idx] = true
			slot := h.freeSlots[cls][idx]
			if slot.off%8 != 0 || slot.off+classSize < slot.off || slot.off+classSize > memLen {
				return errors.New("gc: throughput class free span out of bounds")
			}
			if classSize > h.classLimit || h.classFor(classSize) != cls {
				return errors.New("gc: throughput class free span has wrong class")
			}
			free = append(free, throughputLargeFree{off: slot.off, size: classSize})
		}
	}
	for i, s := range h.largeFree {
		if s.size == 0 || s.off%8 != 0 || s.off+s.size < s.off || s.off+s.size > memLen {
			return errors.New("gc: throughput large free span out of bounds")
		}
		if i > 0 && h.largeFree[i-1].off+h.largeFree[i-1].size >= s.off {
			return errors.New("gc: throughput large free spans not sorted and coalesced")
		}
		free = append(free, s)
	}
	seenFree := make(map[uint32]bool)
	for i, f := range free {
		if seenFree[f.off] {
			return errors.New("gc: duplicate throughput free slot")
		}
		seenFree[f.off] = true
		for _, s := range live {
			if spansOverlap(f.off, f.size, s.off, s.size) {
				return errors.New("gc: throughput free span overlaps live object")
			}
		}
		for j := 0; j < i; j++ {
			if spansOverlap(f.off, f.size, free[j].off, free[j].size) {
				return errors.New("gc: throughput free span overlaps another free span")
			}
		}
	}
	return nil
}

func spansOverlap(aOff, aSize, bOff, bSize uint32) bool {
	return aOff < bOff+bSize && bOff < aOff+aSize
}
