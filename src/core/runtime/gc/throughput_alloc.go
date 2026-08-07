package gc

import (
	"errors"
	"fmt"
)

var errThroughputHeapExhausted = errors.New("gc: throughput heap exhausted")

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

// throughputAllocCheckpoint records only allocator state that one allocation
// can mutate. Promotion planning restores checkpoints in reverse order on
// failure, without copying allocator metadata on the successful path.
type throughputAllocTransaction struct {
	mem              []byte
	bump             uint32
	largestFree      uint32
	largestFreeDirty bool
}

type throughputAllocCheckpoint struct {
	class          int
	freeHead       uint32
	freeRecordHead uint32
	freeSlotsLen   int
	freeHeadSlot   throughputFreeSlot
	largeIndex     int
	largeSpan      throughputLargeFree
	largeRemoved   bool
}

type throughputHeap struct {
	mem              []byte
	limit            uint32
	pageBytes        uint32
	classLimit       uint32
	bump             uint32
	freeHeads        []uint32
	freeRecordHeads  []uint32 // reusable metadata records popped from freeHeads
	freeSlots        [][]throughputFreeSlot
	largeFree        []throughputLargeFree
	largestFree      uint32
	largestFreeDirty bool
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
	h.freeRecordHeads = make([]uint32, len(throughputClassSizes))
	h.freeSlots = make([][]throughputFreeSlot, len(throughputClassSizes))
	h.largeFree = nil
	h.largestFree = 0
	h.largestFreeDirty = false
	for i := range h.freeHeads {
		h.freeHeads[i] = throughputNoSlot
		h.freeRecordHeads[i] = throughputNoSlot
	}
	return nil
}

func (h *throughputHeap) Close() {
	h.mem = nil
	h.freeHeads = nil
	h.freeRecordHeads = nil
	h.freeSlots = nil
	h.largeFree = nil
	h.largestFree = 0
	h.largestFreeDirty = false
	h.bump = 0
}

func (h *throughputHeap) bytes(e handleEntry) []byte { return h.mem[e.off : e.off+e.size] }

func (h *throughputHeap) alloc(size uint32, sp spaceKind) (handleEntry, error) {
	if size > ^uint32(0)-15 {
		return handleEntry{}, ErrAllocationTooLarge
	}
	allocSize := Align16(size)
	if sp != spaceLarge && allocSize <= h.classLimit {
		cls := h.classFor(allocSize)
		if cls < 0 {
			return handleEntry{}, fmt.Errorf("gc: no throughput size class for %d", allocSize)
		}
		classSize := throughputClassSizes[cls]
		if head := h.freeHeads[cls]; head != throughputNoSlot {
			slot := h.freeSlots[cls][head]
			h.freeHeads[cls] = slot.next
			h.freeSlots[cls][head] = throughputFreeSlot{next: h.freeRecordHeads[cls]}
			h.freeRecordHeads[cls] = head
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
		if span.size == h.largestFree {
			h.largestFreeDirty = true
		}
		return handleEntry{off: off, size: size, allocSize: allocSize, class: uint16(len(throughputClassSizes)), space: sp}, nil
	}
	off, err := h.grow(allocSize)
	if err != nil {
		return handleEntry{}, err
	}
	return handleEntry{off: off, size: size, allocSize: allocSize, class: uint16(len(throughputClassSizes)), space: sp}, nil
}

func (h *throughputHeap) checkpointAlloc(size uint32, sp spaceKind) throughputAllocCheckpoint {
	cp := throughputAllocCheckpoint{
		class:      -1,
		largeIndex: -1,
	}
	if size <= ^uint32(0)-15 {
		allocSize := Align16(size)
		if sp != spaceLarge && allocSize <= h.classLimit {
			if cls := h.classFor(allocSize); cls >= 0 {
				cp.class = cls
				cp.freeHead = h.freeHeads[cls]
				cp.freeRecordHead = h.freeRecordHeads[cls]
				cp.freeSlotsLen = len(h.freeSlots[cls])
				if cp.freeHead != throughputNoSlot && int(cp.freeHead) < cp.freeSlotsLen {
					cp.freeHeadSlot = h.freeSlots[cls][cp.freeHead]
				}
				return cp
			}
		}
	}
	if size <= ^uint32(0)-15 {
		allocSize := Align16(size)
		for i, span := range h.largeFree {
			if span.size >= allocSize {
				cp.largeIndex = i
				cp.largeSpan = span
				cp.largeRemoved = span.size-allocSize < 32
				break
			}
		}
	}
	return cp
}

func (h *throughputHeap) restoreAlloc(cp throughputAllocCheckpoint) {
	if cp.class >= 0 {
		cls := cp.class
		h.freeHeads[cls] = cp.freeHead
		h.freeRecordHeads[cls] = cp.freeRecordHead
		h.freeSlots[cls] = h.freeSlots[cls][:cp.freeSlotsLen]
		if cp.freeHead != throughputNoSlot {
			h.freeSlots[cls][cp.freeHead] = cp.freeHeadSlot
		}
		return
	}
	if cp.largeIndex < 0 {
		return
	}
	if !cp.largeRemoved {
		h.largeFree[cp.largeIndex] = cp.largeSpan
		return
	}
	h.largeFree = append(h.largeFree, throughputLargeFree{})
	copy(h.largeFree[cp.largeIndex+1:], h.largeFree[cp.largeIndex:])
	h.largeFree[cp.largeIndex] = cp.largeSpan
}

func (h *throughputHeap) beginAllocTransaction() throughputAllocTransaction {
	return throughputAllocTransaction{
		mem:              h.mem,
		bump:             h.bump,
		largestFree:      h.largestFree,
		largestFreeDirty: h.largestFreeDirty,
	}
}

func (h *throughputHeap) restoreAllocTransaction(tx throughputAllocTransaction) {
	h.mem = tx.mem
	h.bump = tx.bump
	h.largestFree = tx.largestFree
	h.largestFreeDirty = tx.largestFreeDirty
}

// rollbackSuccessfulAlloc reverses one successful allocation while a larger
// allocation transaction is being unwound in LIFO order. Spans below the
// transaction's initial bump came from a free list; bump allocations require no
// per-allocation metadata restoration because restoreAllocTransaction resets the
// bump and backing slice once all free-list allocations have been returned.
func (h *throughputHeap) rollbackSuccessfulAlloc(e handleEntry, initialBump uint32) {
	if e.off >= initialBump {
		return
	}
	if err := h.free(e); err != nil {
		panic("gc: cannot roll back successful throughput allocation: " + err.Error())
	}
}

func (h *throughputHeap) free(e handleEntry) error {
	if e.allocSize == 0 || e.off+e.allocSize > uint32(len(h.mem)) {
		return errors.New("gc: invalid throughput free span")
	}
	if int(e.class) < len(throughputClassSizes) && e.allocSize == throughputClassSizes[e.class] {
		idx := h.freeRecordHeads[e.class]
		if idx != throughputNoSlot {
			h.freeRecordHeads[e.class] = h.freeSlots[e.class][idx].next
			h.freeSlots[e.class][idx] = throughputFreeSlot{off: e.off, next: h.freeHeads[e.class]}
		} else {
			idx = uint32(len(h.freeSlots[e.class]))
			h.freeSlots[e.class] = append(h.freeSlots[e.class], throughputFreeSlot{off: e.off, next: h.freeHeads[e.class]})
		}
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
	off := Align16(h.bump)
	end := uint64(off) + uint64(size)
	if end > uint64(h.limit) || end > uint64(^uint32(0)) {
		return 0, errThroughputHeapExhausted
	}
	needLen, err := throughputReservationLen(end, h.pageBytes, h.limit)
	if err != nil {
		return 0, err
	}
	if needLen > uint64(len(h.mem)) {
		if err := h.growBacking(needLen); err != nil {
			return 0, err
		}
	}
	h.bump = uint32(end)
	return off, nil
}

//go:noinline
func (h *throughputHeap) growBacking(needed uint64) error {
	if err := injectFailure(h, failBackingGrowth); err != nil {
		return err
	}
	reserve := needed
	current, pageBytes, limit := uint64(len(h.mem)), uint64(h.pageBytes), uint64(h.limit)
	if current >= pageBytes {
		step := current / 2
		if step < pageBytes {
			step = pageBytes
		}
		grown := align64(current+step, pageBytes)
		if grown > limit {
			grown = limit
		}
		if grown > reserve {
			reserve = grown
		}
	}
	if reserve > limit || reserve > uint64(^uint32(0)) || reserve > uint64(int(^uint(0)>>1)) {
		return errors.New("gc: throughput heap reservation too large")
	}
	newMem := makeAlignedBytes(uint32(reserve), 16)
	copy(newMem, h.mem)
	h.mem = newMem
	return nil
}

func throughputReservationLen(end uint64, pageBytes, limit uint32) (uint64, error) {
	needLen := align64(end, uint64(pageBytes))
	if needLen > uint64(limit) {
		needLen = end
	}
	if needLen > uint64(^uint32(0)) || needLen > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("gc: throughput heap reservation too large")
	}
	return needLen, nil
}

func align64(v, a uint64) uint64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) &^ (a - 1)
}

func (h *throughputHeap) findLarge(size uint32) int {
	if size > h.largestFree {
		return -1
	}
	if h.largestFreeDirty {
		return h.findLargeDirty(size)
	}
	for i, span := range h.largeFree {
		if span.size >= size {
			return i
		}
	}
	return -1
}

//go:noinline
func (h *throughputHeap) findLargeDirty(size uint32) int {
	largestFree := uint32(0)
	for i, span := range h.largeFree {
		if span.size > largestFree {
			largestFree = span.size
		}
		if span.size >= size {
			return i
		}
	}
	h.largestFree = largestFree
	h.largestFreeDirty = false
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
	if h.largeFree[pos].size >= h.largestFree {
		h.largestFree = h.largeFree[pos].size
		h.largestFreeDirty = false
	}
}

func (h *throughputHeap) recomputeLargestFree() {
	h.largestFree = 0
	for _, span := range h.largeFree {
		if span.size > h.largestFree {
			h.largestFree = span.size
		}
	}
	h.largestFreeDirty = false
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
		if e.allocSize == 0 || e.size > e.allocSize || e.off%16 != 0 || e.off+e.allocSize < e.off || e.off+e.allocSize > memLen {
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
			if slot.off%16 != 0 || slot.off+classSize < slot.off || slot.off+classSize > memLen {
				return errors.New("gc: throughput class free span out of bounds")
			}
			if classSize > h.classLimit || h.classFor(classSize) != cls {
				return errors.New("gc: throughput class free span has wrong class")
			}
			free = append(free, throughputLargeFree{off: slot.off, size: classSize})
		}
		for idx := h.freeRecordHeads[cls]; idx != throughputNoSlot; idx = h.freeSlots[cls][idx].next {
			if int(idx) >= len(h.freeSlots[cls]) || seenIdx[idx] {
				return errors.New("gc: malformed throughput free-record list")
			}
			seenIdx[idx] = true
		}
	}
	for i, s := range h.largeFree {
		if s.size == 0 || s.off%16 != 0 || s.off+s.size < s.off || s.off+s.size > memLen {
			return errors.New("gc: throughput large free span out of bounds")
		}
		if i > 0 && h.largeFree[i-1].off+h.largeFree[i-1].size >= s.off {
			return errors.New("gc: throughput large free spans not sorted and coalesced")
		}
		free = append(free, s)
	}
	largestFree := uint32(0)
	for _, span := range h.largeFree {
		if span.size > largestFree {
			largestFree = span.size
		}
	}
	if (!h.largestFreeDirty && h.largestFree != largestFree) ||
		(h.largestFreeDirty && (h.largestFree < largestFree || h.largestFree > memLen)) {
		return errors.New("gc: throughput largest free span is stale")
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
