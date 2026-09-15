package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
)

const (
	defaultTinyHeapBytes  = 64 << 10
	defaultTinyBlockBytes = 16
	defaultTinyStepBudget = 1
	tinyNoBlock           = ^uint32(0)
	tinyExactBins         = 64
	tinySecondLevelBits   = 3
	tinySecondLevelBins   = 1 << tinySecondLevelBits
	tinyFreeLinkBytes     = 8
)

var errTinyHeapExhausted = errors.New("gc: tiny heap exhausted")

type tinyHeap struct {
	mem []byte

	// span16/span32 contain a span's block count at its first and last
	// blocks; interior entries are zero. Default heaps use 16-bit tags.
	span16 []uint16
	span32 []uint32
	// usedStarts distinguishes allocated span starts from free span starts.
	usedStarts []uint64

	// Free spans are grouped into exact small bins and logarithmic large
	// bins. Their first eight payload bytes hold next/previous block indexes.
	// binWords and binSummary find the next occupied size class without
	// walking unrelated free spans.
	binHeads    []uint32
	binWords    []uint64
	binSummary  uint64
	blockCount  uint32
	blockBytes  uint32
	poisonFreed bool
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
	if config.TinyPacingStepLimit == 0 {
		config.TinyPacingStepLimit = 1
	}
	if err := validateTinyConfig(config); err != nil {
		return nil, err
	}
	if err := ValidateTypeDescs(types); err != nil {
		return nil, err
	}
	requiredAlign := requiredObjectAlignment(types)
	objectAlign := requiredAlign
	if objectAlign < 16 && config.TinyBlockBytes >= 16 {
		objectAlign = 16
	}
	if config.TinyBlockBytes < objectAlign {
		return nil, fmt.Errorf("gc: tiny block size %d is smaller than required object alignment %d", config.TinyBlockBytes, objectAlign)
	}
	c := &Collector{cfg: config, types: append([]TypeDesc(nil), types...), objectAlign: objectAlign, handles: []handleEntry{{}}}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.attach(config.Profile, 0)
	}
	if err := c.initSubtypeIntervals(); err != nil {
		return nil, err
	}
	blocks := config.TinyHeapBytes / config.TinyBlockBytes
	c.tiny = newTinyHeap(makeAlignedBytes(config.TinyHeapBytes, uintptr(objectAlign)), blocks, config.TinyBlockBytes, config.PoisonFreed)
	c.tinyGC.state = tinyIdle
	c.tinyGC.sweep = 1
	c.tinyGC.color = []tinyMarkState{tinyEncodeMarkState(c.tinyGC.markEpoch, tinyWhite)}
	c.initNativeView()
	return c, nil
}

func (c *Collector) initSubtypeIntervals() error {
	intervals, err := buildSubtypeIntervals(c.types)
	if err != nil {
		return err
	}
	c.subtypeIntervals = intervals
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
	if config.TinyPacingStepLimit > tinyNearExhaustionStepLimit {
		return errors.New("gc: Tiny pacing step limit must not exceed 32")
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
	h.span16 = nil
	h.span32 = nil
	h.usedStarts = nil
	h.binHeads = nil
	h.binWords = nil
	h.binSummary = 0
	h.blockCount = 0
}

func (h *tinyHeap) bytes(off, size uint32) []byte { return h.mem[off : off+size] }

func (h *tinyHeap) alloc(size uint32) (uint32, uint32, error) {
	if h.blockBytes == 0 {
		return 0, 0, errors.New("gc: invalid tiny block size")
	}
	need64 := (uint64(size) + uint64(h.blockBytes) - 1) / uint64(h.blockBytes)
	if need64 == 0 || need64 > uint64(^uint32(0)) {
		return 0, 0, errors.New("gc: tiny allocation size overflow")
	}
	need := uint32(need64)
	if need > h.blockCount {
		return 0, 0, errTinyHeapExhausted
	}
	b := h.findFreeSpan(need)
	if b == tinyNoBlock {
		return 0, 0, errTinyHeapExhausted
	}
	span := h.spanSize(b)
	h.removeFree(b, span)
	if span == need {
		h.setUsedStart(b, true)
	} else {
		h.clearSpan(b, span)
		h.setSpan(b, need, true)
		rem := b + need
		h.setSpan(rem, span-need, false)
		h.insertFree(rem, span-need)
	}
	return b * h.blockBytes, need * h.blockBytes, nil
}

func (h *tinyHeap) free(off uint32) error { return h.freeSpan(off, h.poisonFreed) }

func (h *tinyHeap) freeWithoutPoison(off uint32) error { return h.freeSpan(off, false) }

func (h *tinyHeap) freeSpan(off uint32, poison bool) error {
	if h.blockBytes == 0 || off%h.blockBytes != 0 {
		return errors.New("gc: invalid tiny free offset")
	}
	b := off / h.blockBytes
	if b >= h.blockCount || !h.isUsedStart(b) || h.spanSize(b) == 0 {
		return errors.New("gc: invalid tiny free span")
	}
	span := h.spanSize(b)
	if poison {
		freed := h.mem[b*h.blockBytes : (b+span)*h.blockBytes]
		for i := range freed {
			freed[i] = 0xdd
		}
	}
	h.setUsedStart(b, false)
	if b > 0 {
		prevSize := h.spanSize(b - 1)
		if prevSize > 0 && prevSize <= b {
			prev := b - prevSize
			if !h.isUsedStart(prev) && h.spanSize(prev) == prevSize && prev+prevSize == b {
				h.removeFree(prev, prevSize)
				if h.poisonFreed {
					for i := range h.mem[prev*h.blockBytes : prev*h.blockBytes+tinyFreeLinkBytes] {
						h.mem[prev*h.blockBytes+uint32(i)] = 0xdd
					}
				}
				h.clearSpan(prev, prevSize)
				h.clearSpan(b, span)
				b, span = prev, prevSize+span
				h.setSpan(b, span, false)
			}
		}
	}
	if next := b + span; next < h.blockCount {
		nextSize := h.spanSize(next)
		if nextSize > 0 && nextSize <= h.blockCount-next && !h.isUsedStart(next) {
			h.removeFree(next, nextSize)
			if h.poisonFreed {
				for i := range h.mem[next*h.blockBytes : next*h.blockBytes+tinyFreeLinkBytes] {
					h.mem[next*h.blockBytes+uint32(i)] = 0xdd
				}
			}
			h.clearSpan(b, span)
			h.clearSpan(next, nextSize)
			span += nextSize
			h.setSpan(b, span, false)
		}
	}
	h.insertFree(b, span)
	return nil
}

func newTinyHeap(mem []byte, blocks, blockBytes uint32, poisonFreed bool) tinyHeap {
	h := tinyHeap{mem: mem, blockCount: blocks, blockBytes: blockBytes, poisonFreed: poisonFreed}
	if blocks <= uint32(^uint16(0)) {
		h.span16 = make([]uint16, blocks)
	} else {
		h.span32 = make([]uint32, blocks)
	}
	h.usedStarts = make([]uint64, (uint64(blocks)+63)/64)
	binCount := tinyBinForSize(blocks) + 1
	h.binHeads = make([]uint32, binCount)
	for i := range h.binHeads {
		h.binHeads[i] = tinyNoBlock
	}
	h.binWords = make([]uint64, (uint64(binCount)+63)/64)
	h.setSpan(0, blocks, false)
	h.insertFree(0, blocks)
	return h
}

func (h *tinyHeap) spanSize(b uint32) uint32 {
	if b >= h.blockCount {
		return 0
	}
	if h.span16 != nil {
		return uint32(h.span16[b])
	}
	return h.span32[b]
}

func (h *tinyHeap) putSpanSize(b, size uint32) {
	if h.span16 != nil {
		h.span16[b] = uint16(size)
	} else {
		h.span32[b] = size
	}
}

func (h *tinyHeap) setSpan(b, size uint32, used bool) {
	h.putSpanSize(b, size)
	h.putSpanSize(b+size-1, size)
	h.setUsedStart(b, used)
}

func (h *tinyHeap) clearSpan(b, size uint32) {
	h.putSpanSize(b, 0)
	if size > 1 {
		h.putSpanSize(b+size-1, 0)
	}
	h.setUsedStart(b, false)
}

func (h *tinyHeap) isUsedStart(b uint32) bool {
	return h.usedStarts[b>>6]&(uint64(1)<<(b&63)) != 0
}

func (h *tinyHeap) setUsedStart(b uint32, used bool) {
	word, bit := b>>6, uint64(1)<<(b&63)
	if used {
		h.usedStarts[word] |= bit
	} else {
		h.usedStarts[word] &^= bit
	}
}

func tinyBinForSize(size uint32) uint32 {
	if size <= tinyExactBins {
		return size - 1
	}
	fl := uint32(bits.Len32(size) - 1)
	base := uint32(1) << fl
	width := base >> tinySecondLevelBits
	sl := (size - base) / width
	return tinyExactBins + (fl-6)*tinySecondLevelBins + sl
}

func (h *tinyHeap) freeLinks(b uint32) (next, prev uint32) {
	off := b * h.blockBytes
	return binary.LittleEndian.Uint32(h.mem[off:]), binary.LittleEndian.Uint32(h.mem[off+4:])
}

func (h *tinyHeap) setFreeLinks(b, next, prev uint32) {
	off := b * h.blockBytes
	binary.LittleEndian.PutUint32(h.mem[off:], next)
	binary.LittleEndian.PutUint32(h.mem[off+4:], prev)
}

func (h *tinyHeap) insertFree(b, size uint32) {
	bin := tinyBinForSize(size)
	head := h.binHeads[bin]
	h.setFreeLinks(b, head, tinyNoBlock)
	if head != tinyNoBlock {
		next, _ := h.freeLinks(head)
		h.setFreeLinks(head, next, b)
	}
	h.binHeads[bin] = b
	h.markBin(bin, true)
}

func (h *tinyHeap) removeFree(b, size uint32) {
	bin := tinyBinForSize(size)
	next, prev := h.freeLinks(b)
	if prev == tinyNoBlock {
		h.binHeads[bin] = next
	} else {
		_, pp := h.freeLinks(prev)
		h.setFreeLinks(prev, next, pp)
	}
	if next != tinyNoBlock {
		nn, _ := h.freeLinks(next)
		h.setFreeLinks(next, nn, prev)
	}
	if h.binHeads[bin] == tinyNoBlock {
		h.markBin(bin, false)
	}
}

func (h *tinyHeap) markBin(bin uint32, nonempty bool) {
	word, bit := bin>>6, uint64(1)<<(bin&63)
	if nonempty {
		h.binWords[word] |= bit
		h.binSummary |= uint64(1) << word
		return
	}
	h.binWords[word] &^= bit
	if h.binWords[word] == 0 {
		h.binSummary &^= uint64(1) << word
	}
}

func (h *tinyHeap) findFreeSpan(need uint32) uint32 {
	bin := tinyBinForSize(need)
	// Large bins cover a narrow size range, so only the request's own bin
	// can contain undersized spans. Every later occupied bin is a fit.
	for b := h.binHeads[bin]; b != tinyNoBlock; {
		next, _ := h.freeLinks(b)
		if h.spanSize(b) >= need {
			return b
		}
		b = next
	}
	return h.firstSpanAfterBin(bin)
}

func (h *tinyHeap) firstSpanAfterBin(bin uint32) uint32 {
	word := bin >> 6
	shift := (bin & 63) + 1
	var candidates uint64
	if shift < 64 {
		candidates = h.binWords[word] & (^uint64(0) << shift)
	}
	if candidates != 0 {
		return h.binHeads[word*64+uint32(bits.TrailingZeros64(candidates))]
	}
	remaining := h.binSummary & (^uint64(0) << (word + 1))
	if remaining == 0 {
		return tinyNoBlock
	}
	word = uint32(bits.TrailingZeros64(remaining))
	return h.binHeads[word*64+uint32(bits.TrailingZeros64(h.binWords[word]))]
}

func (h *tinyHeap) metadataBytes() uintptr {
	return uintptr(len(h.span16))*2 + uintptr(len(h.span32))*4 +
		uintptr(len(h.usedStarts))*8 + uintptr(len(h.binHeads))*4 + uintptr(len(h.binWords))*8 + 8
}

func (c *Collector) tinyPayAllocationDebt(roots RootSet) error {
	steps := c.tinyGC.allocationDebt / tinyAllocationDebtBytes
	if steps > c.cfg.TinyPacingStepLimit {
		steps = c.cfg.TinyPacingStepLimit
	}
	for i := uint32(0); i < steps; i++ {
		if err := c.tinyPacingStep(roots); err != nil {
			return err
		}
		c.tinyGC.allocationDebt -= tinyAllocationDebtBytes
	}
	return nil
}

func (c *Collector) tinyPacingStep(roots RootSet) error {
	wasActive := tinyIncrementalBuild && c.tinyGC.state != tinyIdle
	if err := c.Step(roots); err != nil {
		return err
	}
	if wasActive && c.tinyGC.state == tinyIdle {
		c.stats.FullCollections++
	}
	return nil
}

func (c *Collector) tinyAddAllocationDebt(bytes uint32) {
	if ^uint32(0)-c.tinyGC.allocationDebt < bytes {
		c.tinyGC.allocationDebt = ^uint32(0)
		return
	}
	c.tinyGC.allocationDebt += bytes
}

func (c *Collector) tinyAssistNearExhaustion(size uint32, roots RootSet) (uint32, uint32, error) {
	limit := c.cfg.TinyPacingStepLimit * tinyNearExhaustionFactor
	if limit < c.cfg.TinyPacingStepLimit || limit > tinyNearExhaustionStepLimit {
		limit = tinyNearExhaustionStepLimit
	}
	if limit == 0 {
		limit = 1
	}
	for i := uint32(0); i < limit; i++ {
		if err := c.tinyPacingStep(roots); err != nil {
			return 0, 0, err
		}
		if c.tinyGC.allocationDebt >= tinyAllocationDebtBytes {
			c.tinyGC.allocationDebt -= tinyAllocationDebtBytes
		} else {
			c.tinyGC.allocationDebt = 0
		}
		off, allocated, err := c.tiny.alloc(size)
		if err == nil {
			return off, allocated, nil
		}
	}
	return 0, 0, errors.New("gc: tiny heap exhausted after bounded pacing assist")
}

func (c *Collector) tinyAlloc(d TypeDesc, size, aux uint32, roots RootSet) (Ref, error) {
	if err := c.errIfClosed(); err != nil {
		return Null(), err
	}
	paced := !c.cfg.CollectEveryAlloc && !c.cfg.TinyCollectEveryAlloc && !c.cfg.TinyStepEveryAlloc
	if paced && !tinyIncrementalBuild && roots != nil {
		if err := c.tinyPayAllocationDebt(roots); err != nil {
			return Null(), err
		}
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
	if err := injectFailure(c, failHandlePublication); err != nil {
		return Null(), err
	}
	off, allocatedBytes, err := c.tiny.alloc(size)
	if err != nil {
		if roots == nil {
			return Null(), errors.New("gc: tiny heap exhausted and no roots were supplied")
		}
		off, allocatedBytes, err = c.tinyAssistNearExhaustion(size, roots)
		if err != nil {
			return Null(), err
		}
	}
	if paced && tinyIncrementalBuild && roots != nil {
		// The allocator reservation is invisible to tracing until its handle is
		// published, so debt work can run here even during sweep without exposing
		// an uninitialized object or forfeiting already swept space.
		if err := c.tinyPayAllocationDebt(roots); err != nil {
			if freeErr := c.tiny.free(off); freeErr != nil {
				panic("gc: failed to roll back unpublished Tiny reservation: " + freeErr.Error())
			}
			return Null(), err
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
	if paced {
		c.tinyAddAllocationDebt(allocatedBytes)
	}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.paths.GoAllocationPaths++
	}
	c.refreshNativeView()
	return r, nil
}

func (c *Collector) tinyPostAlloc(r Ref, d TypeDesc) {
	if !r.IsObj() {
		return
	}
	h := handleOf(r)
	if d.HasRefs && (c.tinyGC.state == tinyMark || c.tinyGC.state == tinyRemark || c.tinySweepActive()) {
		// The handle is newly published and cannot already be queued, so avoid
		// the general duplicate-gray color check on this allocation path. During
		// sweep, constructors populate the payload after allocation; keeping the
		// object gray makes those initialized edges part of bounded barrier work.
		c.tinyQueueGrayHandle(h)
		if c.tinyGC.state == tinySweep && c.tinyGC.scan.handle == 0 {
			c.tinyGC.state = tinyMark
			c.tinyGC.rootPhase = tinyRootsSweepBarrier
		}
		return
	}
	// Idle objects and pointer-free allocations protected from the current
	// mark/sweep are black in the current epoch. The next cycle's epoch advance
	// makes them white.
	c.tinySetBlack(h)
}
