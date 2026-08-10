package gc

import (
	"errors"
	"fmt"
	"math/bits"
)

var errThroughputHeapExhausted = errors.New("gc: throughput heap exhausted")

const throughputNoSlot = ^uint32(0)

var throughputClassSizes = [...]uint32{32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 3072, 4096, 8192, 16384, 32768}

type throughputFreeSpan struct {
	off  uint32
	size uint32
}

// throughputSpanNode is an arena-owned AVL node. Free spans of every size share
// this one address index, so adjacent size-class and large-object frees can
// coalesce and immediately serve either allocation path. maxSize augments the
// tree for a lowest-address first-fit search among reconciled/indexed spans
// without walking unrelated spans. Deferred spans are reclamation debt, not
// allocation candidates, until incremental or complete reconciliation indexes
// them.
type throughputSpanNode struct {
	off      uint32
	size     uint32
	maxSize  uint32
	left     uint32
	right    uint32
	nextFree uint32
	height   uint8
	_        [3]byte
}

// throughputAllocTransaction records state that bump allocation or backing
// growth can mutate. Reused-span allocations are unwound in reverse through
// rollbackSuccessfulAlloc; the AVL arena is reusable and does not need a full
// metadata snapshot on the successful path.
type throughputAllocTransaction struct {
	mem  []byte
	bump uint32
}

type throughputAllocRun struct {
	off           uint32
	allocSize     uint32
	lastAllocSize uint32
	class         uint16
}

type throughputHeap struct {
	mem        []byte
	limit      uint32
	pageBytes  uint32
	classLimit uint32
	bump       uint32

	spanRoot     uint32
	freeNodeHead uint32
	spanNodes    []throughputSpanNode
	pendingFree  []throughputFreeSpan
	freeBytes    uint64
	pendingBytes uint64
	spanCount    uint32
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
	h.spanRoot = throughputNoSlot
	h.freeNodeHead = throughputNoSlot
	h.spanNodes = nil
	h.pendingFree = nil
	h.freeBytes = 0
	h.pendingBytes = 0
	h.spanCount = 0
	return nil
}

func (h *throughputHeap) Close() {
	h.mem = nil
	h.spanNodes = nil
	h.pendingFree = nil
	h.spanRoot = throughputNoSlot
	h.freeNodeHead = throughputNoSlot
	h.freeBytes = 0
	h.pendingBytes = 0
	h.spanCount = 0
	h.bump = 0
}

func (h *throughputHeap) bytes(e handleEntry) []byte { return h.mem[e.off : e.off+e.size] }

func (h *throughputHeap) alloc(size uint32, sp spaceKind) (handleEntry, error) {
	if size > ^uint32(0)-15 {
		return handleEntry{}, ErrAllocationTooLarge
	}
	allocSize := Align16(size)
	class := uint16(len(throughputClassSizes))
	if sp != spaceLarge && allocSize <= h.classLimit {
		cls := h.classFor(allocSize)
		if cls < 0 {
			return handleEntry{}, fmt.Errorf("gc: no throughput size class for %d", allocSize)
		}
		allocSize = throughputClassSizes[cls]
		class = uint16(cls)
	}

	// Reconciled spans have priority even when a lower-address suitable span is
	// still pending. Only an indexed miss pays deferred reconciliation; each LIFO
	// debt item is indexed and the lowest-address indexed fit is retried.
	idx := h.findSpan(allocSize)
	for idx == throughputNoSlot && len(h.pendingFree) != 0 {
		if err := h.sweepOnePending(); err != nil {
			return handleEntry{}, err
		}
		idx = h.findSpan(allocSize)
	}
	if idx != throughputNoSlot {
		span := throughputFreeSpan{off: h.spanNodes[idx].off, size: h.spanNodes[idx].size}
		if span.size-allocSize >= 32 {
			// Consuming a prefix leaves the replacement span between the same
			// predecessor and successor. Update its key and max-size path in place
			// instead of removing/reinserting and rebalancing the AVL tree.
			if !h.replaceFreeSpan(span.off, throughputFreeSpan{off: span.off + allocSize, size: span.size - allocSize}) {
				return handleEntry{}, errors.New("gc: throughput free-span index changed during allocation")
			}
			h.freeBytes -= uint64(allocSize)
		} else {
			reusable, ok := h.removeFreeSpan(span.off)
			if !ok {
				return handleEntry{}, errors.New("gc: throughput free-span index changed during allocation")
			}
			allocSize = span.size
			class = uint16(len(throughputClassSizes))
			h.releaseSpanNode(reusable)
		}
		return handleEntry{off: span.off, size: size, allocSize: allocSize, class: class, space: sp}, nil
	}

	off, err := h.grow(allocSize)
	if err != nil {
		return handleEntry{}, err
	}
	return handleEntry{off: off, size: size, allocSize: allocSize, class: class, space: sp}, nil
}

func (h *throughputHeap) beginAllocTransaction() throughputAllocTransaction {
	return throughputAllocTransaction{mem: h.mem, bump: h.bump}
}

func (h *throughputHeap) restoreAllocTransaction(tx throughputAllocTransaction) {
	h.mem = tx.mem
	h.bump = tx.bump
}

// rollbackSuccessfulAlloc reverses one successful allocation while a larger
// allocation transaction is being unwound in LIFO order. Spans below the
// transaction's initial bump came from the shared free-span index; bump
// allocations require no per-allocation restoration because the transaction
// restores the bump and backing slice after all indexed allocations are freed.
func (h *throughputHeap) rollbackSuccessfulAlloc(e handleEntry, initialBump uint32) {
	if e.off >= initialBump {
		return
	}
	if err := h.free(e); err != nil {
		panic("gc: cannot roll back successful throughput allocation: " + err.Error())
	}
}

func (h *throughputHeap) free(e handleEntry) error {
	if e.allocSize == 0 || e.off%16 != 0 || e.off+e.allocSize < e.off || e.off+e.allocSize > h.bump {
		return errors.New("gc: invalid throughput free span")
	}
	return h.insertFreeSpan(throughputFreeSpan{off: e.off, size: e.allocSize})
}

func (h *throughputHeap) deferFree(e handleEntry) error {
	if e.allocSize == 0 || e.off%16 != 0 || e.off+e.allocSize < e.off || e.off+e.allocSize > h.bump {
		return errors.New("gc: invalid deferred throughput free span")
	}
	h.pendingFree = append(h.pendingFree, throughputFreeSpan{off: e.off, size: e.allocSize})
	h.pendingBytes += uint64(e.allocSize)
	return nil
}

func (h *throughputHeap) sweepOnePending() error {
	last := len(h.pendingFree) - 1
	span := h.pendingFree[last]
	// Pending debt remains authoritative until index insertion succeeds. On any
	// validation, overlap, metadata, or injected failure, a later allocation can
	// retry the exact same span instead of silently losing reclaimable space.
	if err := injectFailure(h, failThroughputReconciliation); err != nil {
		return err
	}
	if err := h.insertFreeSpan(span); err != nil {
		return err
	}
	h.pendingFree[last] = throughputFreeSpan{}
	h.pendingFree = h.pendingFree[:last]
	h.pendingBytes -= uint64(span.size)
	return nil
}

func (h *throughputHeap) sweepAllPending() error {
	// Full collection appends frees in handle order, which is commonly also
	// ascending allocation order. Collapse those runs before touching the AVL
	// index so a dead contiguous old heap becomes one insertion rather than one
	// insertion and coalescing operation per object. Arbitrary handle reuse can
	// break address order; leave that case to the ordinary exact path.
	if len(h.pendingFree) > 1 {
		ascending, descending := true, true
		for i := 1; i < len(h.pendingFree); i++ {
			previous, current := h.pendingFree[i-1], h.pendingFree[i]
			if current.off < previous.off+previous.size {
				ascending = false
			}
			if current.off+current.size > previous.off {
				descending = false
			}
			if !ascending && !descending {
				break
			}
		}
		if ascending || descending {
			spans := h.pendingFree
			out := spans[:1]
			for _, span := range spans[1:] {
				last := &out[len(out)-1]
				if ascending && last.off+last.size == span.off {
					last.size += span.size
				} else if descending && span.off+span.size == last.off {
					last.off = span.off
					last.size += span.size
				} else {
					out = append(out, span)
				}
			}
			clear(spans[len(out):])
			h.pendingFree = out
		}
	}
	for len(h.pendingFree) != 0 {
		if err := h.sweepOnePending(); err != nil {
			return err
		}
	}
	return nil
}

// Keep the mapper out of line: duplicating it in allocator callers grows hot
// code and regresses promotion despite faster isolated lookups.
//
//go:noinline
func (h *throughputHeap) classFor(size uint32) int {
	if size <= 128 {
		class := 0
		switch {
		case size <= 32:
		case size <= 64:
			class = int((size-1)>>4) - 1
		case size <= 96:
			class = 3
		default:
			class = 4
		}
		if throughputClassSizes[class] <= h.classLimit {
			return class
		}
		return -1
	}
	if size > 32768 {
		return -1
	}

	exponent := bits.Len32(size - 1)
	class := exponent + 2
	if size <= 4096 {
		// Classes through 4 KiB alternate powers of two and 1.5x.
		class = 2 * (exponent - 5)
		if size <= uint32(3)<<(exponent-2) {
			class--
		}
	}
	if throughputClassSizes[class] <= h.classLimit {
		return class
	}
	return -1
}

func (h *throughputHeap) promotionAllocSize(size uint32) uint32 {
	allocSize := Align16(size)
	if allocSize <= h.classLimit {
		if cls := h.classFor(allocSize); cls >= 0 {
			return throughputClassSizes[cls]
		}
	}
	return allocSize
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

func (h *throughputHeap) largestFree() uint32 {
	if h.spanRoot == throughputNoSlot {
		return 0
	}
	return h.spanNodes[h.spanRoot].maxSize
}

func (h *throughputHeap) findSpan(size uint32) uint32 {
	idx := h.spanRoot
	if idx == throughputNoSlot || h.spanNodes[idx].maxSize < size {
		return throughputNoSlot
	}
	for idx != throughputNoSlot {
		n := &h.spanNodes[idx]
		if n.left != throughputNoSlot && h.spanNodes[n.left].maxSize >= size {
			idx = n.left
			continue
		}
		if n.size >= size {
			return idx
		}
		idx = n.right
	}
	return throughputNoSlot
}

func (h *throughputHeap) findSpanCounted(size uint32) (uint32, uint32) {
	idx := h.spanRoot
	if idx == throughputNoSlot || h.spanNodes[idx].maxSize < size {
		return throughputNoSlot, 1
	}
	var steps uint32
	for idx != throughputNoSlot {
		steps++
		n := &h.spanNodes[idx]
		if n.left != throughputNoSlot && h.spanNodes[n.left].maxSize >= size {
			idx = n.left
			continue
		}
		if n.size >= size {
			return idx, steps
		}
		idx = n.right
	}
	return throughputNoSlot, steps
}

func (h *throughputHeap) findSpanByOffset(off uint32) uint32 {
	for idx := h.spanRoot; idx != throughputNoSlot; {
		n := &h.spanNodes[idx]
		switch {
		case off < n.off:
			idx = n.left
		case off > n.off:
			idx = n.right
		default:
			return idx
		}
	}
	return throughputNoSlot
}

func (h *throughputHeap) spanNeighbors(off uint32) (pred, succ uint32) {
	pred, succ = throughputNoSlot, throughputNoSlot
	for idx := h.spanRoot; idx != throughputNoSlot; {
		n := &h.spanNodes[idx]
		if off <= n.off {
			succ = idx
			idx = n.left
		} else {
			pred = idx
			idx = n.right
		}
	}
	return pred, succ
}

func (h *throughputHeap) insertFreeSpan(span throughputFreeSpan) error {
	if span.size == 0 || span.off%16 != 0 || span.size%16 != 0 || span.off+span.size < span.off || span.off+span.size > h.bump {
		return errors.New("gc: invalid throughput free span")
	}
	pred, succ := h.spanNeighbors(span.off)
	if pred != throughputNoSlot {
		p := h.spanNodes[pred]
		if p.off+p.size > span.off {
			return errors.New("gc: overlapping throughput free span")
		}
	}
	if succ != throughputNoSlot && span.off+span.size > h.spanNodes[succ].off {
		return errors.New("gc: overlapping throughput free span")
	}

	predAdjacent := pred != throughputNoSlot && h.spanNodes[pred].off+h.spanNodes[pred].size == span.off
	succAdjacent := succ != throughputNoSlot && span.off+span.size == h.spanNodes[succ].off
	if predAdjacent != succAdjacent {
		merged := span
		oldOff := uint32(0)
		if predAdjacent {
			p := h.spanNodes[pred]
			oldOff = p.off
			merged.off = p.off
			merged.size += p.size
		} else {
			s := h.spanNodes[succ]
			oldOff = s.off
			merged.size += s.size
		}
		if merged.off+merged.size != h.bump {
			// A one-neighbor merge also remains between the same surrounding
			// spans. Mutate the existing node and preserve its tree position.
			if !h.replaceFreeSpan(oldOff, merged) {
				return errors.New("gc: throughput free-span index changed during coalescing")
			}
			h.freeBytes += uint64(span.size)
			return nil
		}
	}

	reuse, extra := throughputNoSlot, throughputNoSlot
	if pred != throughputNoSlot {
		p := h.spanNodes[pred]
		if p.off+p.size == span.off {
			span.off = p.off
			span.size += p.size
			reuse, _ = h.removeFreeSpan(p.off)
		}
	}
	if succ != throughputNoSlot {
		s := h.spanNodes[succ]
		if span.off+span.size == s.off {
			span.size += s.size
			extra, _ = h.removeFreeSpan(s.off)
		}
	}

	if span.off+span.size == h.bump {
		h.bump = span.off
		if reuse != throughputNoSlot {
			h.releaseSpanNode(reuse)
		}
		if extra != throughputNoSlot {
			h.releaseSpanNode(extra)
		}
		return nil
	}
	if reuse == throughputNoSlot {
		reuse = extra
		extra = throughputNoSlot
	}
	if reuse == throughputNoSlot {
		var err error
		reuse, err = h.acquireSpanNode()
		if err != nil {
			return err
		}
	}
	if extra != throughputNoSlot {
		h.releaseSpanNode(extra)
	}
	h.insertFreeSpanNode(span, reuse)
	return nil
}

func (h *throughputHeap) insertFreeSpanNode(span throughputFreeSpan, idx uint32) {
	h.spanNodes[idx] = throughputSpanNode{
		off: span.off, size: span.size, maxSize: span.size,
		left: throughputNoSlot, right: throughputNoSlot, nextFree: throughputNoSlot, height: 1,
	}
	h.spanRoot = h.insertAVL(h.spanRoot, idx)
	h.freeBytes += uint64(span.size)
	h.spanCount++
}

func (h *throughputHeap) removeFreeSpan(off uint32) (uint32, bool) {
	idx := h.findSpanByOffset(off)
	if idx == throughputNoSlot {
		return throughputNoSlot, false
	}
	size := h.spanNodes[idx].size
	var removed uint32
	var ok bool
	h.spanRoot, removed, ok = h.removeAVL(h.spanRoot, off)
	if !ok {
		return throughputNoSlot, false
	}
	h.freeBytes -= uint64(size)
	h.spanCount--
	return removed, true
}

// replaceFreeSpan changes one indexed span without changing its in-order
// neighbors. Prefix consumption and one-neighbor coalescing satisfy that
// condition, so only maxSize summaries on the search path need refreshing.
func (h *throughputHeap) replaceFreeSpan(oldOff uint32, span throughputFreeSpan) bool {
	var ok bool
	h.spanRoot, ok = h.replaceFreeSpanAVL(h.spanRoot, oldOff, span)
	return ok
}

func (h *throughputHeap) replaceFreeSpanAVL(root, oldOff uint32, span throughputFreeSpan) (uint32, bool) {
	if root == throughputNoSlot {
		return root, false
	}
	n := &h.spanNodes[root]
	var ok bool
	switch {
	case oldOff < n.off:
		n.left, ok = h.replaceFreeSpanAVL(n.left, oldOff, span)
	case oldOff > n.off:
		n.right, ok = h.replaceFreeSpanAVL(n.right, oldOff, span)
	default:
		n.off, n.size = span.off, span.size
		ok = true
	}
	if ok {
		h.updateNode(root)
	}
	return root, ok
}

func (h *throughputHeap) acquireSpanNode() (uint32, error) {
	if h.freeNodeHead != throughputNoSlot {
		idx := h.freeNodeHead
		h.freeNodeHead = h.spanNodes[idx].nextFree
		h.spanNodes[idx] = throughputSpanNode{}
		return idx, nil
	}
	if uint64(len(h.spanNodes)) >= uint64(throughputNoSlot) {
		return 0, errors.New("gc: throughput free-span metadata exhausted")
	}
	idx := uint32(len(h.spanNodes))
	h.spanNodes = append(h.spanNodes, throughputSpanNode{})
	return idx, nil
}

func (h *throughputHeap) releaseSpanNode(idx uint32) {
	h.spanNodes[idx] = throughputSpanNode{left: throughputNoSlot, right: throughputNoSlot, nextFree: h.freeNodeHead}
	h.freeNodeHead = idx
}

func (h *throughputHeap) nodeHeight(idx uint32) uint8 {
	if idx == throughputNoSlot {
		return 0
	}
	return h.spanNodes[idx].height
}

func (h *throughputHeap) nodeMax(idx uint32) uint32 {
	if idx == throughputNoSlot {
		return 0
	}
	return h.spanNodes[idx].maxSize
}

func (h *throughputHeap) updateNode(idx uint32) {
	n := &h.spanNodes[idx]
	leftHeight, rightHeight := h.nodeHeight(n.left), h.nodeHeight(n.right)
	n.height = max(leftHeight, rightHeight) + 1
	n.maxSize = max(n.size, max(h.nodeMax(n.left), h.nodeMax(n.right)))
}

func (h *throughputHeap) rotateLeft(root uint32) uint32 {
	right := h.spanNodes[root].right
	middle := h.spanNodes[right].left
	h.spanNodes[right].left = root
	h.spanNodes[root].right = middle
	h.updateNode(root)
	h.updateNode(right)
	return right
}

func (h *throughputHeap) rotateRight(root uint32) uint32 {
	left := h.spanNodes[root].left
	middle := h.spanNodes[left].right
	h.spanNodes[left].right = root
	h.spanNodes[root].left = middle
	h.updateNode(root)
	h.updateNode(left)
	return left
}

func (h *throughputHeap) rebalance(root uint32) uint32 {
	if root == throughputNoSlot {
		return root
	}
	h.updateNode(root)
	n := &h.spanNodes[root]
	balance := int(h.nodeHeight(n.left)) - int(h.nodeHeight(n.right))
	if balance > 1 {
		left := n.left
		if h.nodeHeight(h.spanNodes[left].left) < h.nodeHeight(h.spanNodes[left].right) {
			n.left = h.rotateLeft(left)
		}
		return h.rotateRight(root)
	}
	if balance < -1 {
		right := n.right
		if h.nodeHeight(h.spanNodes[right].right) < h.nodeHeight(h.spanNodes[right].left) {
			n.right = h.rotateRight(right)
		}
		return h.rotateLeft(root)
	}
	return root
}

func (h *throughputHeap) insertAVL(root, idx uint32) uint32 {
	if root == throughputNoSlot {
		return idx
	}
	if h.spanNodes[idx].off < h.spanNodes[root].off {
		h.spanNodes[root].left = h.insertAVL(h.spanNodes[root].left, idx)
	} else {
		h.spanNodes[root].right = h.insertAVL(h.spanNodes[root].right, idx)
	}
	return h.rebalance(root)
}

func (h *throughputHeap) removeAVL(root, off uint32) (uint32, uint32, bool) {
	if root == throughputNoSlot {
		return root, throughputNoSlot, false
	}
	n := &h.spanNodes[root]
	if off < n.off {
		var removed uint32
		var ok bool
		n.left, removed, ok = h.removeAVL(n.left, off)
		if !ok {
			return root, removed, false
		}
		return h.rebalance(root), removed, true
	}
	if off > n.off {
		var removed uint32
		var ok bool
		n.right, removed, ok = h.removeAVL(n.right, off)
		if !ok {
			return root, removed, false
		}
		return h.rebalance(root), removed, true
	}
	if n.left == throughputNoSlot {
		return n.right, root, true
	}
	if n.right == throughputNoSlot {
		return n.left, root, true
	}
	succ := n.right
	for h.spanNodes[succ].left != throughputNoSlot {
		succ = h.spanNodes[succ].left
	}
	n.off, n.size = h.spanNodes[succ].off, h.spanNodes[succ].size
	var removed uint32
	n.right, removed, _ = h.removeAVL(n.right, n.off)
	return h.rebalance(root), removed, true
}

func (h *throughputHeap) freeSpans() []throughputFreeSpan {
	out := make([]throughputFreeSpan, 0, h.spanCount)
	var walk func(uint32)
	walk = func(idx uint32) {
		if idx == throughputNoSlot {
			return
		}
		n := h.spanNodes[idx]
		walk(n.left)
		out = append(out, throughputFreeSpan{off: n.off, size: n.size})
		walk(n.right)
	}
	walk(h.spanRoot)
	return out
}

func (h *throughputHeap) verify(handles []handleEntry) error {
	if h.limit == 0 {
		return errors.New("gc: throughput heap is not initialized")
	}
	memLen := uint32(len(h.mem))
	if h.bump > memLen || h.bump > h.limit {
		return errors.New("gc: throughput bump out of bounds")
	}
	live := make([]throughputFreeSpan, 0)
	for i, e := range handles {
		if i == 0 || e.space == spaceFree || e.space == spaceNursery || e.space == spaceTiny {
			continue
		}
		if e.space != spaceOld && e.space != spaceLarge {
			return fmt.Errorf("gc: invalid throughput space for handle %d", i)
		}
		if e.allocSize == 0 || e.size > e.allocSize || e.off%16 != 0 || e.off+e.allocSize < e.off || e.off+e.allocSize > h.bump {
			return fmt.Errorf("gc: throughput handle %d out of bounds", i)
		}
		for _, s := range live {
			if spansOverlap(e.off, e.allocSize, s.off, s.size) {
				return fmt.Errorf("gc: throughput live span overlap at handle %d", i)
			}
		}
		live = append(live, throughputFreeSpan{off: e.off, size: e.allocSize})
	}

	seen := make([]uint8, len(h.spanNodes))
	var spanCount uint32
	var freeBytes uint64
	var verifyTree func(uint32, uint32, uint32) (uint8, uint32, error)
	verifyTree = func(idx, minOff, maxOff uint32) (uint8, uint32, error) {
		if idx == throughputNoSlot {
			return 0, 0, nil
		}
		if int(idx) >= len(h.spanNodes) || seen[idx] != 0 {
			return 0, 0, errors.New("gc: malformed throughput free-span tree")
		}
		seen[idx] = 1
		n := h.spanNodes[idx]
		if n.size == 0 || n.off%16 != 0 || n.size%16 != 0 || n.off+n.size < n.off || n.off+n.size > h.bump {
			return 0, 0, errors.New("gc: throughput free span out of bounds")
		}
		if n.off < minOff || (maxOff != 0 && n.off >= maxOff) {
			return 0, 0, errors.New("gc: throughput free-span tree is unordered")
		}
		lh, lm, err := verifyTree(n.left, minOff, n.off)
		if err != nil {
			return 0, 0, err
		}
		rh, rm, err := verifyTree(n.right, n.off+1, maxOff)
		if err != nil {
			return 0, 0, err
		}
		wantHeight := max(lh, rh) + 1
		wantMax := max(n.size, max(lm, rm))
		if n.height != wantHeight || n.maxSize != wantMax || int(lh)-int(rh) < -1 || int(lh)-int(rh) > 1 {
			return 0, 0, errors.New("gc: throughput free-span AVL metadata is stale")
		}
		spanCount++
		freeBytes += uint64(n.size)
		return wantHeight, wantMax, nil
	}
	_, treeMax, err := verifyTree(h.spanRoot, 0, 0)
	if err != nil {
		return err
	}
	for idx := h.freeNodeHead; idx != throughputNoSlot; idx = h.spanNodes[idx].nextFree {
		if int(idx) >= len(h.spanNodes) || seen[idx] != 0 {
			return errors.New("gc: malformed throughput free-node list")
		}
		seen[idx] = 2
	}
	for _, state := range seen {
		if state == 0 {
			return errors.New("gc: orphaned throughput span node")
		}
	}
	if spanCount != h.spanCount || freeBytes != h.freeBytes || treeMax != h.largestFree() {
		return errors.New("gc: throughput free-span summary is stale")
	}

	free := h.freeSpans()
	for i, s := range free {
		if i > 0 && free[i-1].off+free[i-1].size >= s.off {
			return errors.New("gc: throughput free spans are not coalesced")
		}
		for _, object := range live {
			if spansOverlap(s.off, s.size, object.off, object.size) {
				return errors.New("gc: throughput free span overlaps live object")
			}
		}
	}
	var pendingBytes uint64
	for i, span := range h.pendingFree {
		if span.size == 0 || span.off%16 != 0 || span.size%16 != 0 || span.off+span.size < span.off || span.off+span.size > h.bump {
			return errors.New("gc: deferred throughput free span out of bounds")
		}
		pendingBytes += uint64(span.size)
		for _, object := range live {
			if spansOverlap(span.off, span.size, object.off, object.size) {
				return errors.New("gc: deferred throughput free span overlaps live object")
			}
		}
		for _, indexed := range free {
			if spansOverlap(span.off, span.size, indexed.off, indexed.size) {
				return errors.New("gc: deferred throughput free span overlaps indexed free span")
			}
		}
		for j := 0; j < i; j++ {
			if spansOverlap(span.off, span.size, h.pendingFree[j].off, h.pendingFree[j].size) {
				return errors.New("gc: deferred throughput free spans overlap")
			}
		}
	}
	if pendingBytes != h.pendingBytes {
		return errors.New("gc: deferred throughput free-byte summary is stale")
	}
	return nil
}

func spansOverlap(aOff, aSize, bOff, bSize uint32) bool {
	return aOff < bOff+bSize && bOff < aOff+aSize
}

// tryAllocRun reserves one contiguous run only when doing so preserves the
// first-fit choice of repeated individual allocations. It never grows backing:
// cold growth keeps the existing geometric reservation sequence and failure
// injection points, while warmed promotion batches can remove one AVL span or
// advance the bump once for the complete equal-sized run.
func (h *throughputHeap) tryAllocRun(size uint32, count int, sp spaceKind) (throughputAllocRun, bool, error) {
	if count < 2 || size > ^uint32(0)-15 {
		return throughputAllocRun{}, false, nil
	}
	allocSize := Align16(size)
	class := uint16(len(throughputClassSizes))
	if sp != spaceLarge && allocSize <= h.classLimit {
		cls := h.classFor(allocSize)
		if cls < 0 {
			return throughputAllocRun{}, false, fmt.Errorf("gc: no throughput size class for %d", allocSize)
		}
		allocSize = throughputClassSizes[cls]
		class = uint16(cls)
	}
	total := uint64(allocSize) * uint64(count)
	if total > uint64(^uint32(0)) {
		return throughputAllocRun{}, false, nil
	}
	idx := h.findSpan(allocSize)
	if idx != throughputNoSlot {
		span := throughputFreeSpan{off: h.spanNodes[idx].off, size: h.spanNodes[idx].size}
		if uint64(span.size) < total {
			return throughputAllocRun{}, false, nil
		}
		consumed := uint32(total)
		lastAllocSize := allocSize
		if span.size-consumed >= 32 {
			if !h.replaceFreeSpan(span.off, throughputFreeSpan{off: span.off + consumed, size: span.size - consumed}) {
				return throughputAllocRun{}, false, errors.New("gc: throughput free-span index changed during run allocation")
			}
			h.freeBytes -= uint64(consumed)
		} else {
			reusable, ok := h.removeFreeSpan(span.off)
			if !ok {
				return throughputAllocRun{}, false, errors.New("gc: throughput free-span index changed during run allocation")
			}
			lastAllocSize += span.size - consumed
			h.releaseSpanNode(reusable)
		}
		return throughputAllocRun{off: span.off, allocSize: allocSize, lastAllocSize: lastAllocSize, class: class}, true, nil
	}
	if len(h.pendingFree) != 0 {
		return throughputAllocRun{}, false, nil
	}
	off := Align16(h.bump)
	end := uint64(off) + total
	if end > uint64(h.limit) || end > uint64(len(h.mem)) {
		return throughputAllocRun{}, false, nil
	}
	h.bump = uint32(end)
	return throughputAllocRun{off: off, allocSize: allocSize, lastAllocSize: allocSize, class: class}, true, nil
}
