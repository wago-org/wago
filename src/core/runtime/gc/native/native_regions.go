package gc

// prepareNativeRegionAllocation owns one bounded Eden interval and a batch of
// identities before returning to generated code. It never collects: failure
// returns to the rooted checked constructor, which may collect and then refill.
// Dead charges deliberately retain nativeAllocationAllowed's region exclusion.
func (c *Collector) prepareNativeRegionAllocation(size uint32) bool {
	if c.closed || c.cfg.Profile != ProfileThroughput || c.cfg.DisableCollection || c.cfg.CollectEveryAlloc ||
		size == 0 || size >= c.cfg.LargeObjectBytes || size > NativeAllocationChunkBytes || len(c.edenRegions) == 0 {
		return false
	}
	need := Align16(size)
	width := uint32(1) << c.cfg.NurseryRegionShift
	if need > width {
		return false
	}
	s := &c.nativeStructAlloc
	if s.Cursor < s.Count && s.ChunkEnd != 0 && s.ChunkCursor <= s.ChunkEnd && need <= s.ChunkEnd-s.ChunkCursor {
		return true
	}
	c.discardNativeStructHandles()
	start, ok := c.regionAllocationOffset(need, 16)
	if !ok {
		return false
	}
	index := start / width
	old := index*width + c.edenRegions[index].bump
	end := start + min(NativeAllocationChunkBytes, width-start%width)
	// No byte ownership changes until the complete handle batch is available.
	c.reserveNativeHandles(nativeArrayHandleBatch)
	s.ChunkStart, s.ChunkCursor, s.ChunkEnd, s.ChunkBump = start, start, end, old
	c.commitRegionAllocation(start, end-start)
	c.nurseryBump = max(c.nurseryBump, end)
	return true
}

// cancelNativeRegionChunk returns only the unused tail of this exact owned
// interval. Native code advances ChunkCursor after initializing each payload;
// no safepoint or trapping instruction occurs during publication. The region
// bump covers published bytes even while its remaining reservation is unused.
func (c *Collector) cancelNativeRegionChunk() {
	s := &c.nativeStructAlloc
	if s.ChunkEnd == 0 || s.ChunkCursor < s.ChunkStart || s.ChunkCursor > s.ChunkEnd {
		return
	}
	width := uint32(1) << c.cfg.NurseryRegionShift
	index := s.ChunkStart / width
	if index >= uint32(len(c.edenRegions)) || index*width+c.edenRegions[index].bump != s.ChunkEnd {
		return // Fail closed: do not reclaim an interval with a later owner.
	}
	rewind := s.ChunkCursor
	if rewind == s.ChunkStart {
		rewind = s.ChunkBump
	}
	c.edenRegions[index].bump = rewind - index*width
	if c.nurseryBump == s.ChunkEnd {
		// This remains a conservative Eden high-water mark. Region membership,
		// never this global counter, authorizes subsequent allocation.
		c.nurseryBump = rewind
	}
}
