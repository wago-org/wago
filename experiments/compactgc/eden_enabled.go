//go:build wago_gc_compact_eden

package compactgc

// edenState is scalar-only; default builds retain an empty embedded state.
type edenState struct {
	eden                               bool
	edenSurvivorStart                  [2]uint32
	edenSurvivorSize, edenSurvivorBump uint32
	edenSurvivorActive                 int
}

// NewEdenWithTypes reserves Eden and two equal survivor spaces in one arena.
// Core-only: native snapshots are deliberately unavailable for this policy.
func NewEdenWithTypes(edenBytes, survivorBytes, oldBytes uint32, types []Descriptor) (*Collector, error) {
	total := uint64(edenBytes) + 2*uint64(survivorBytes)
	if !edenCompatible || edenBytes < 16 || survivorBytes < 16 || edenBytes%16 != 0 || survivorBytes%16 != 0 || total > uint64(^uint32(0)) {
		return nil, ErrConfig
	}
	c, e := NewWithTypes(uint32(total), oldBytes, types)
	if e != nil {
		return nil, e
	}
	c.eden = true
	c.nurseryEnd = c.nurseryStart + edenBytes
	c.edenSurvivorStart = [2]uint32{c.nurseryEnd, c.nurseryEnd + survivorBytes}
	c.edenSurvivorSize = survivorBytes
	c.edenSurvivorBump = c.edenSurvivorStart[0]
	return c, nil
}
func (c *Collector) EdenEnabled() bool { return c != nil && c.eden }
func (c *Collector) edenYoung(r Ref) bool {
	return c.eden && uint32(r) >= c.edenSurvivorStart[c.edenSurvivorActive] && uint32(r) < c.edenSurvivorBump
}

// EdenBudget reports total young backing, fixed fresh capacity and each survivor capacity.
func (c *Collector) EdenBudget() (total, fresh, survivor uint32) {
	if !c.EdenEnabled() {
		return 0, 0, 0
	}
	fresh = c.nurseryEnd - c.nurseryStart
	survivor = c.edenSurvivorSize
	return fresh + 2*survivor, fresh, survivor
}

func (c *Collector) collectEden(roots []*Ref, full bool) error {
	clear(c.mark)
	defer clear(c.mark)
	oldTarget := c.active
	oldNext := c.oldBump
	if full {
		oldTarget = 1 - c.active
		oldNext = c.oldStart[oldTarget]
	}
	oldLimit := c.oldStart[oldTarget] + c.oldSize
	youngTarget := c.edenSurvivorStart[1-c.edenSurvivorActive]
	youngNext := youngTarget
	// Deterministic destination choice, replayed after preflight. No per-object
	// forwarding metadata beyond the existing mark/queue is required.
	choose := func(src, size uint32, y, o *uint32) (uint32, bool) {
		if !full && src >= c.nurseryStart && src < c.nurseryBump && size <= c.edenSurvivorSize-(*y-youngTarget) {
			dst := *y
			*y += size
			return dst, true
		}
		if size > oldLimit-*o {
			return 0, false
		}
		dst := *o
		*o += size
		return dst, true
	}
	count := 0
	visit := func(r Ref) error {
		if r == 0 || r.IsI31() {
			return nil
		}
		if !c.valid(r) {
			return ErrRef
		}
		if !full && !c.young(r) {
			return nil
		}
		slot := uint32(r) / 16
		bit := uint64(1) << (slot % 64)
		if c.mark[slot/64]&bit != 0 {
			return nil
		}
		h, e := c.layoutAt(uint32(r))
		if e != nil {
			return e
		}
		if count == len(c.queue) {
			return ErrOOM
		}
		if _, ok := choose(uint32(r), h.Stride, &youngNext, &oldNext); !ok {
			return ErrOOM
		}
		c.mark[slot/64] |= bit
		c.queue[count] = r
		count++
		return nil
	}
	scan := func(r Ref) error {
		// Remembered parents were not necessarily visited by visit during a minor.
		if !c.valid(r) {
			return ErrRef
		}
		if _, e := c.layoutAt(uint32(r)); e != nil {
			return e
		}
		return c.eachRef(uint32(r), func(at uint32) error { return visit(Ref(c.u32(at))) })
	}
	for _, p := range roots {
		if p == nil {
			return ErrRef
		}
		if e := visit(*p); e != nil {
			return e
		}
	}
	if !full {
		if e := c.rangeRemembered(scan); e != nil {
			return e
		}
	}
	for i := 0; i < count; i++ {
		if e := scan(c.queue[i]); e != nil {
			return e
		}
	}
	// All fallible work has completed. Destinations are outside source ranges.
	rewrite := func(r Ref) Ref {
		if r.IsObj() && c.valid(r) {
			s := uint32(r) / 16
			if c.mark[s/64]&(uint64(1)<<(s%64)) != 0 {
				return Ref(c.u32(uint32(r)))
			}
		}
		return r
	}
	y, o := youngTarget, c.oldBump
	if full {
		o = c.oldStart[oldTarget]
	}
	for i := 0; i < count; i++ {
		src := uint32(c.queue[i])
		h, _ := c.layoutAt(src)
		dst, _ := choose(src, h.Stride, &y, &o)
		copy(c.arena[dst:dst+h.Stride], c.arena[src:src+h.Stride])
		c.put32(src, dst)
	}
	fix := func(off uint32) bool {
		hasYoung := false
		_ = c.eachRef(off, func(at uint32) error {
			r := rewrite(Ref(c.u32(at)))
			c.put32(at, uint32(r))
			if r.IsObj() && uint32(r) >= youngTarget && uint32(r) < youngNext {
				hasYoung = true
			}
			return nil
		})
		return hasYoung
	}
	// Rebuild only previously dirty parents, then newly promoted parents. Queue
	// compaction writes at or behind the source read cursor; overflow scans starts.
	if full {
		c.resetRemembered(true)
	} else {
		n, overflow := len(c.remembered), c.rememberedOverflow
		c.remembered = c.remembered[:0]
		c.rememberedOverflow = false
		retain := func(off uint32) {
			c.dirty[off/8] = false
			if fix(off) {
				c.rememberParent(Ref(off))
			}
		}
		if overflow {
			for i := c.oldStart[c.active] / 8; i < c.oldBump/8; i++ {
				if c.starts[i] && c.dirty[i] {
					retain(i * 8)
				}
			}
		} else {
			for i := 0; i < n; i++ {
				retain(uint32(c.remembered[:cap(c.remembered)][i]))
			}
		}
	}
	for i := 0; i < count; i++ {
		dst := c.u32(uint32(c.queue[i]))
		hasYoung := fix(dst)
		if !full && dst >= c.oldStart[oldTarget] && dst < oldNext && hasYoung {
			c.rememberParent(Ref(dst))
		}
	}
	for _, p := range roots {
		*p = rewrite(*p)
	}
	for i := c.nurseryStart / 8; i < c.nurseryBump/8; i++ {
		if c.starts[i] {
			c.retiredNurseryAllocations++
		}
	}
	clear(c.starts[c.nurseryStart/8 : c.nurseryBump/8])
	clear(c.starts[c.edenSurvivorStart[c.edenSurvivorActive]/8 : c.edenSurvivorBump/8])
	if full {
		clear(c.starts[c.oldStart[c.active]/8 : c.oldBump/8])
	}
	for i := 0; i < count; i++ {
		c.starts[c.u32(uint32(c.queue[i]))/8] = true
	}
	c.active = oldTarget
	c.oldBump = oldNext
	c.nurseryBump = c.nurseryStart
	c.edenSurvivorActive = 1 - c.edenSurvivorActive
	c.edenSurvivorBump = youngNext
	return nil
}
