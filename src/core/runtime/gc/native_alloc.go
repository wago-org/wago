package gc

// nativeStructHandleBatch bounds collector-owned unpublished handle identities
// exposed to generated code. Reserving identities does not consume nursery bytes
// or publish a live object; native code advances the real nursery bump only after
// validating a complete constructor.
const (
	nativeStructHandleBatch         = 32
	NativeStructAllocHandleCapacity = nativeStructHandleBatch
)

type nativeStructAllocState struct {
	Epoch   uint32
	Cursor  uint32
	Count   uint32
	_       uint32
	Handles [nativeStructHandleBatch]uint32
}

const (
	NativeStructAllocEpochOffset   = 0
	NativeStructAllocCursorOffset  = 4
	NativeStructAllocCountOffset   = 8
	NativeStructAllocHandlesOffset = 16
	NativeStructAllocStateSize     = 16 + nativeStructHandleBatch*4
)

// PrepareNativeStructHandles publishes a fresh bounded batch of free handle
// identities for native nursery allocation. Existing unconsumed reservations are
// retained; callers refill only after generated code has exhausted the batch.
// The ordinary rooted helper remains responsible for collection and retry.
func (c *Collector) PrepareNativeStructHandles() bool {
	if c == nil || c.closed || c.cfg.Profile != ProfileThroughput || c.cfg.DisableCollection || c.cfg.CollectEveryAlloc {
		return false
	}
	s := &c.nativeStructAlloc
	if s.Cursor < s.Count {
		return true
	}
	clear(s.Handles[:])
	s.Cursor = 0
	s.Count = 0
	for i := range s.Handles {
		h := c.newHandle(handleEntry{})
		s.Handles[i] = h
		// Reserved free handles participate in this dense list before activation.
		// Collection ignores spaceFree entries and compacts them after cancellation.
		c.nurseryHandles = append(c.nurseryHandles, h)
		s.Count++
	}
	s.Epoch = c.nativeAllocEpoch
	if c.telemetryEnabled() {
		c.cfg.Telemetry.paths.HandleRefills++
		c.cfg.Telemetry.paths.ConditionalMediumPaths++
	}
	c.refreshNativeHandles()
	return true
}

// discardNativeStructHandles transactionally returns every unconsumed identity
// before collection or shutdown. Consumed entries are live nursery handles and
// remain owned by the normal collector lifecycle.
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
	for i := s.Cursor; i < end; i++ {
		h := s.Handles[i]
		if h != 0 && int(h) < len(c.handles) && c.handles[h].space == spaceFree {
			c.freeHandles = append(c.freeHandles, h)
			canceled = true
		}
	}
	if canceled {
		// Remove canceled identities immediately. An explicit minor collection may
		// fail promotion and return without reaching the normal nursery compaction;
		// leaving free identities in the dense set would let later reuse append a
		// duplicate handle.
		c.compactNurseryHandles()
	}
	clear(s.Handles[:])
	s.Cursor = 0
	s.Count = 0
	c.nativeAllocEpoch++
	if c.nativeAllocEpoch == 0 {
		c.nativeAllocEpoch = 1
	}
	s.Epoch = c.nativeAllocEpoch
}
