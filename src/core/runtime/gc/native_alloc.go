package gc

import "slices"

// Native allocation keeps the retained 32-handle batch baseline. A refill also
// reserves a bounded nursery chunk so generated code can publish several objects
// without mutating the collector-wide nursery bump for every object.
const (
	nativeStructHandleBatch                = 32
	NativeAllocationChunkBytes      uint32 = 4096
	NativeStructAllocHandleCapacity        = nativeStructHandleBatch
	NativeArrayAllocMaxBytes        uint32 = 256
)

// nativeStructAllocState retains its historical name because its address is part
// of the native collector ABI. The state is now shared by admitted struct and
// array constructors.
type nativeStructAllocState struct {
	Epoch       uint32
	Cursor      uint32
	Count       uint32
	HandleBase  uint32
	ChunkStart  uint32
	ChunkCursor uint32
	ChunkEnd    uint32
	ChunkBump   uint32
	Handles     [nativeStructHandleBatch]uint32
}

const (
	NativeStructAllocEpochOffset       = 0
	NativeStructAllocCursorOffset      = 4
	NativeStructAllocCountOffset       = 8
	NativeStructAllocHandleBaseOffset  = 12
	NativeStructAllocChunkStartOffset  = 16
	NativeStructAllocChunkCursorOffset = 20
	NativeStructAllocChunkEndOffset    = 24
	NativeStructAllocChunkBumpOffset   = 28
	NativeStructAllocHandlesOffset     = 32
	NativeStructAllocStateSize         = 32 + nativeStructHandleBatch*4
)

// PrepareNativeAllocation publishes a fresh bounded batch of free handle
// identities and reserves a nursery chunk for native constructors. Existing
// usable reservations are retained. Reserving identities and bytes does not
// publish an object or increment semantic allocation counters.
func (c *Collector) PrepareNativeAllocation(minObjectBytes uint32) bool {
	if c == nil || c.closed || c.cfg.Profile != ProfileThroughput || c.cfg.DisableCollection || c.cfg.CollectEveryAlloc {
		return false
	}
	s := &c.nativeStructAlloc
	if minObjectBytes >= c.cfg.LargeObjectBytes || minObjectBytes > NativeAllocationChunkBytes || minObjectBytes > ^uint32(0)-15 {
		return false
	}
	need := Align16(minObjectBytes)
	if need == 0 {
		need = 16
	}
	if s.Cursor < s.Count && s.ChunkCursor <= s.ChunkEnd && need <= s.ChunkEnd-s.ChunkCursor {
		return true
	}
	c.discardNativeStructHandles()

	oldBump := c.nurseryBump
	start := Align16(oldBump)
	limit := c.edenBytes()
	if start > limit || need > limit-start {
		return false
	}
	chunkBytes := NativeAllocationChunkBytes
	if chunkBytes < need {
		chunkBytes = need
	}
	if chunkBytes > limit-start {
		chunkBytes = limit - start
	}
	end := start + chunkBytes

	clear(s.Handles[:])
	s.Cursor = 0
	s.Count = 0
	s.HandleBase = 0
	newHandles := nativeStructHandleBatch - min(len(c.freeHandles), nativeStructHandleBatch)
	if newHandles != 0 {
		c.handles = slices.Grow(c.handles, newHandles)
		c.mark = slices.Grow(c.mark, newHandles)
		c.tinyGC.color = slices.Grow(c.tinyGC.color, newHandles)
	}
	c.nurseryHandles = slices.Grow(c.nurseryHandles, nativeStructHandleBatch)
	for i := range s.Handles {
		h := c.newHandle(handleEntry{})
		s.Handles[i] = h
		// Reserved free handles participate in this dense list before activation.
		// Collection ignores spaceFree entries and compacts them after cancellation.
		c.nurseryHandles = append(c.nurseryHandles, h)
		s.Count++
	}
	base := s.Handles[0]
	contiguous := base != 0
	for i, h := range s.Handles {
		if h != base+uint32(i) {
			contiguous = false
			break
		}
	}
	if contiguous {
		s.HandleBase = base
	}
	s.ChunkStart = start
	s.ChunkCursor = start
	s.ChunkEnd = end
	s.ChunkBump = oldBump
	c.nurseryBump = end
	s.Epoch = c.nativeAllocEpoch
	if c.telemetryEnabled() {
		c.cfg.Telemetry.paths.HandleRefills++
		c.cfg.Telemetry.paths.ConditionalMediumPaths++
	}
	c.refreshNativeHandles()
	return true
}

// PrepareNativeArrayAllocation retains only arrays that fit in the measured
// fixed chunk. Larger constructors stay on the rooted helper path; alternating a
// helper and one oversized native reservation regresses sustained throughput.
func (c *Collector) PrepareNativeArrayAllocation(objectBytes uint32) bool {
	if objectBytes > NativeArrayAllocMaxBytes {
		return false
	}
	return c.PrepareNativeAllocation(objectBytes)
}

// PrepareNativeStructHandles is retained for low-level callers and tests. New
// runtime integration should pass the known minimum object size to
// PrepareNativeAllocation.
func (c *Collector) PrepareNativeStructHandles() bool { return c.PrepareNativeAllocation(16) }

// CancelNativeAllocationBatch invalidates unpublished native identities and
// returns an unused top chunk before a Go allocation slow path.
func (c *Collector) CancelNativeAllocationBatch() { c.discardNativeStructHandles() }

// discardNativeStructHandles transactionally returns every unconsumed identity
// before a collecting boundary, Go allocation slow path, or shutdown. Consumed
// entries are live nursery handles and remain owned by the collector lifecycle.
func (c *Collector) discardNativeStructHandles() {
	if c == nil {
		return
	}
	s := &c.nativeStructAlloc
	end := s.Count
	if end > uint32(len(s.Handles)) {
		end = uint32(len(s.Handles))
	}
	canceled := false
	if end > s.Cursor {
		c.freeHandles = slices.Grow(c.freeHandles, int(end-s.Cursor))
	}
	for i := s.Cursor; i < end; i++ {
		h := s.Handles[i]
		if s.HandleBase != 0 {
			h = s.HandleBase + i
		}
		if h != 0 && int(h) < len(c.handles) && c.handles[h].space == spaceFree {
			c.freeHandles = append(c.freeHandles, h)
			canceled = true
		}
	}
	// The chunk is exclusively reserved while the collector-wide bump equals its
	// end. Rewind only in that exact state; any intervening allocation keeps the
	// harmless bounded tail gap rather than risking overlap.
	if s.ChunkEnd != 0 && c.nurseryBump == s.ChunkEnd && s.ChunkCursor >= s.ChunkStart && s.ChunkCursor <= s.ChunkEnd {
		rewind := s.ChunkCursor
		if rewind == s.ChunkStart {
			rewind = s.ChunkBump
		}
		c.nurseryBump = rewind
	}
	if canceled {
		// Remove canceled identities immediately. A failed collection can return
		// before normal nursery compaction, and later handle reuse must not append
		// a duplicate identity to the dense young set.
		c.compactNurseryHandles()
	}
	clear(s.Handles[:])
	s.Cursor = 0
	s.Count = 0
	s.HandleBase = 0
	s.ChunkStart = 0
	s.ChunkCursor = 0
	s.ChunkEnd = 0
	s.ChunkBump = 0
	c.nativeAllocEpoch++
	if c.nativeAllocEpoch == 0 {
		c.nativeAllocEpoch = 1
	}
	s.Epoch = c.nativeAllocEpoch
}
