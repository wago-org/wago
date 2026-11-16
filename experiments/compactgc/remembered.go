// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package compactgc

// rememberParent requires a valid old object. The bit deduplicates queue entries;
// overflow switches both tracing and rewriting to the complete old-region walk.
// No barrier can be dropped when the bounded queue fills. This operation never
// allocates: construction reserves the queue's maximum capacity.
//
// A future native store must enqueue consistently or set rememberedOverflow.
// Merely writing a dirty byte is NOT a complete barrier protocol anymore.
func (c *Collector) rememberParent(r Ref) {
	slot := uint32(r) / 8
	if c.dirty[slot] {
		return
	}
	c.dirty[slot] = true
	if c.rememberedOverflow {
		return
	}
	if len(c.remembered) == cap(c.remembered) {
		c.rememberedOverflow = true
		return
	}
	c.remembered = append(c.remembered, r)
}
func (c *Collector) rangeRemembered(fn func(Ref) error) error {
	if c.rememberedOverflow {
		for i := c.oldStart[c.active] / 8; i < c.oldBump/8; i++ {
			if c.starts[i] && c.dirty[i] {
				if e := fn(Ref(i * 8)); e != nil {
					return e
				}
			}
		}
		return nil
	}
	for _, r := range c.remembered {
		if e := fn(r); e != nil {
			return e
		}
	}
	return nil
}

// resetRemembered is called only after a successful collection. OOM or invalid
// roots never consume a queue entry or clear a dirty bit. Promotion-all means no
// old-to-young edge remains after a successful minor. Full resets both spaces.
func (c *Collector) resetRemembered(full bool) {
	if full {
		clear(c.dirty)
	} else {
		_ = c.rangeRemembered(func(r Ref) error { c.dirty[uint32(r)/8] = false; return nil })
	}
	c.remembered = c.remembered[:0]
	c.rememberedOverflow = false
}
