package gc

import (
	"math/bits"
	"slices"
)

// One bit covers 256 handles. Go allocations are indexed eagerly. Native nursery
// objects are covered from nurseryHandles before full sweep, or at promotion
// before leaving that ledger. Only a full sweep proves a block empty.
const majorBlockShift = 8
const majorWordShift = majorBlockShift + 6

func (c *Collector) noteMajorHandle(h uint32) {
	if len(c.majorBlocks) == 0 {
		return
	}
	word := int(h >> majorWordShift)
	if word >= len(c.majorBlocks) {
		c.majorBlocks = append(c.majorBlocks, make([]uint64, word+1-len(c.majorBlocks))...)
	}
	c.majorBlocks[word] |= uint64(1) << ((h >> majorBlockShift) & 63)
}

func (c *Collector) noteMajorHandles(handles []uint32) {
	if len(c.majorBlocks) == 0 || len(handles) == 0 {
		return
	}
	// Merge a run into one word update. Revisiting a word ORs into its earlier
	// mask, including when a scattered batch crosses the same word repeatedly.
	word := int(handles[0] >> majorWordShift)
	var mask uint64
	for _, h := range handles {
		next := int(h >> majorWordShift)
		if next != word {
			c.noteMajorWord(word, mask)
			word, mask = next, 0
		}
		mask |= uint64(1) << ((h >> majorBlockShift) & 63)
	}
	c.noteMajorWord(word, mask)
}

func (c *Collector) noteMajorWord(word int, mask uint64) {
	if word >= len(c.majorBlocks) {
		c.majorBlocks = append(c.majorBlocks, make([]uint64, word+1-len(c.majorBlocks))...)
	}
	c.majorBlocks[word] |= mask
}

// Enable the index only after the existing sweep has made the table sparse.
// The free-list signal may underestimate sparsity (retired identities are not
// reusable), but affects performance selection only. Re-enabling always sets
// every bit, so it never relies on a stale density estimate for correctness.
func (c *Collector) prepareMajorBlocks() {
	if len(c.handles) < 4096 || len(c.freeHandles) < len(c.handles)-len(c.handles)/16 {
		c.majorBlocks = c.majorBlocks[:0]
		c.majorYoungDirty = false
		return
	}
	if len(c.majorBlocks) != 0 {
		return
	}
	c.majorYoungDirty = false // the all-set index covers every existing handle
	words := (len(c.handles) + (1 << majorWordShift) - 1) >> majorWordShift
	c.majorBlocks = slices.Grow(c.majorBlocks, words)[:words]
	for i := range c.majorBlocks {
		c.majorBlocks[i] = ^uint64(0)
	}
}

func (c *Collector) sweepMajorBlocks() {
	for word, occupied := range c.majorBlocks {
		var live uint64
		for occupied != 0 {
			bit := uint(bits.TrailingZeros64(occupied))
			mask := uint64(1) << bit
			occupied &^= mask
			start := (word*64 + int(bit)) << majorBlockShift
			end := min(start+(1<<majorBlockShift), len(c.handles))
			h := max(start, 1)
			// One marked object keeps the block occupied. The remaining entries
			// use the original sweep loop without repeating the occupancy update.
			for ; h < end; h++ {
				space := c.handles[h].space
				if space == spaceFree {
					continue
				}
				if c.mark[h] {
					live |= mask
					h++
					break
				}
				if space == spaceOld || space == spaceLarge {
					c.deferThroughputFree(uint32(h))
				} else {
					c.free(uint32(h))
				}
			}
			for ; h < end; h++ {
				if c.handles[h].space != spaceFree && !c.mark[h] {
					if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
						c.deferThroughputFree(uint32(h))
					} else {
						c.free(uint32(h))
					}
				}
			}
		}
		c.majorBlocks[word] = live
	}
	c.compactNurseryHandles()
}
