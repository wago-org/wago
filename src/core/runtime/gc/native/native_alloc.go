package gc

import "slices"

// Native allocation keeps the retained 32-handle batch baseline. A refill also
// reserves a bounded nursery chunk so generated code can publish several objects
// without mutating the collector-wide nursery bump for every object.
const (
	nativeStructHandleBatch                  = 32
	NativeAllocationChunkBytes        uint32 = 4096
	NativeStructAllocHandleCapacity          = nativeStructHandleBatch
	NativeArrayAllocMaxBytes          uint32 = 256
	nativeArrayGenericRefillThreshold uint8  = 9
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

func (c *Collector) nativeAllocationAllowed(minObjectBytes uint32) bool {
	return c != nil && !c.closed && c.cfg.Profile == ProfileThroughput && !c.cfg.DisableCollection && !c.cfg.CollectEveryAlloc &&
		minObjectBytes < c.cfg.LargeObjectBytes && minObjectBytes <= NativeAllocationChunkBytes && minObjectBytes <= ^uint32(0)-15
}

func (c *Collector) reserveNativeHandles() {
	s := &c.nativeStructAlloc
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
	s.Epoch = c.nativeAllocEpoch
	if c.telemetryEnabled() {
		c.cfg.Telemetry.paths.HandleRefills++
		c.cfg.Telemetry.paths.ConditionalMediumPaths++
	}
	c.refreshNativeHandles()
}

// PrepareNativeStructAllocation retains the original direct nursery-bump struct
// path. Array chunks therefore add no cursor checks or reserved-byte gaps to the
// established native struct hot path.
func (c *Collector) PrepareNativeStructAllocation(minObjectBytes uint32) bool {
	if !c.nativeAllocationAllowed(minObjectBytes) {
		return false
	}
	s := &c.nativeStructAlloc
	if s.Cursor < s.Count && s.ChunkEnd == 0 {
		return true
	}
	c.discardNativeStructHandles()
	c.reserveNativeHandles()
	return true
}

// PrepareNativeAllocation publishes a fresh bounded handle batch and reserves a
// nursery chunk for native arrays. Existing usable array reservations are kept.
func (c *Collector) PrepareNativeAllocation(minObjectBytes uint32) bool {
	if !c.nativeAllocationAllowed(minObjectBytes) {
		return false
	}
	s := &c.nativeStructAlloc
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
	if chunkBytes > limit-start {
		chunkBytes = limit - start
	}
	end := start + chunkBytes

	c.reserveNativeHandles()
	s.ChunkStart = start
	s.ChunkCursor = start
	s.ChunkEnd = end
	s.ChunkBump = oldBump
	c.nurseryBump = end
	return true
}

// PrepareNativeArrayAllocation retains only arrays that fit in the measured
// fixed chunk. Larger constructors stay on the rooted helper path; alternating a
// helper and one oversized native reservation regresses sustained throughput.
func (c *Collector) PrepareNativeArrayAllocation(objectBytes uint32) bool {
	if objectBytes > NativeArrayAllocMaxBytes {
		return false
	}
	// Generic public calls collect at the next boundary. Measurements put the
	// refill break-even above eight constructors, so short calls stay helper-only
	// and a ninth slow allocation reserves for the remaining sequence.
	if c.arraySlow < nativeArrayGenericRefillThreshold-1 {
		c.arraySlow++
		return false
	}
	return c.PrepareNativeAllocation(objectBytes)
}

// PrepareNativeArrayAllocationImmediate is for products without a mandatory
// collection before every public invocation. Their batch survives the boundary,
// so the first rooted constructor can profitably refill for later invocations.
func (c *Collector) PrepareNativeArrayAllocationImmediate(objectBytes uint32) bool {
	if objectBytes > NativeArrayAllocMaxBytes {
		return false
	}
	return c.PrepareNativeAllocation(objectBytes)
}

// PrepareNativeStructHandles is retained for low-level callers and tests. New
// runtime integration should pass the known minimum object size to
// PrepareNativeAllocation.
func (c *Collector) PrepareNativeStructHandles() bool { return c.PrepareNativeStructAllocation(16) }

// CancelNativeAllocationBatch invalidates unpublished native identities and
// returns an unused top chunk before a Go allocation slow path.
func (c *Collector) CancelNativeAllocationBatch() {
	if c == nil {
		return
	}
	slowCount := c.arraySlow
	c.discardNativeStructHandles()
	c.arraySlow = slowCount
}

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
		// Reserved handles are appended as one tail batch and no Go allocation or
		// collection can append another nursery identity before cancellation. Trim
		// that bounded tail directly and retain only consumed live handles. Keep the
		// full compaction as a fail-closed fallback if the invariant is not exact.
		trimmed := false
		if int(end) <= len(c.nurseryHandles) {
			start := len(c.nurseryHandles) - int(end)
			trimmed = true
			for i := uint32(0); i < end; i++ {
				if c.nurseryHandles[start+int(i)] != s.Handles[i] {
					trimmed = false
					break
				}
			}
			if trimmed {
				out := c.nurseryHandles[:start]
				for i := uint32(0); i < end; i++ {
					h := s.Handles[i]
					if h != 0 && int(h) < len(c.handles) && c.handles[h].space != spaceFree {
						out = append(out, h)
					}
				}
				c.nurseryHandles = out
			}
		}
		if !trimmed {
			// A failed collection can return before normal nursery compaction, and
			// later handle reuse must not append a duplicate identity to the dense
			// young set.
			c.compactNurseryHandles()
		}
	}
	clear(s.Handles[:])
	s.Cursor = 0
	s.Count = 0
	s.HandleBase = 0
	s.ChunkStart = 0
	s.ChunkCursor = 0
	s.ChunkEnd = 0
	s.ChunkBump = 0
	c.arraySlow = 0
	c.nativeAllocEpoch++
	if c.nativeAllocEpoch == 0 {
		c.nativeAllocEpoch = 1
	}
	s.Epoch = c.nativeAllocEpoch
}
