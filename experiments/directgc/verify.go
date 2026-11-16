package directgc

import "fmt"

// Verify checks the entire occupied heap, including unreachable objects and the
// remembered-set invariant. It is a serialized, read-only diagnostic: private
// temporary maps do not reuse or modify collection scratch. References carry no
// ownership/generation tags; a stale offset reused by a new object is not detectable.
func (c *Collector) Verify(roots []*Ref) error {
	bad := func(s string) error { return fmt.Errorf("directgc: verify: %s", s) }
	if c == nil || c.active < 0 || c.active > 1 {
		return bad("collector geometry")
	}
	total := uint64(c.nurseryEnd) + 2*uint64(c.oldSize)
	if c.nurseryStart != 16 || c.nurseryEnd <= 16 || c.nurseryEnd%16 != 0 || c.oldSize == 0 || c.oldSize%16 != 0 || total != uint64(len(c.arena)) || uint64(c.oldStart[0]) != uint64(c.nurseryEnd) || uint64(c.oldStart[1]) != uint64(c.nurseryEnd)+uint64(c.oldSize) || len(c.starts) != len(c.arena)/8 || len(c.dirty) != len(c.starts) {
		return bad("collector geometry")
	}
	if c.nurseryBump < c.nurseryStart || c.nurseryBump > c.nurseryEnd || c.nurseryBump%16 != 0 || c.oldBump < c.oldStart[c.active] || uint64(c.oldBump) > uint64(c.oldStart[c.active])+uint64(c.oldSize) || c.oldBump%16 != 0 {
		return bad("occupied ranges")
	}
	for _, d := range c.types {
		if _, err := cloneDescriptor(d); err != nil {
			return bad("descriptor")
		}
	}
	objects := make(map[Ref]bool)
	for _, region := range [][2]uint32{{c.nurseryStart, c.nurseryBump}, {c.oldStart[c.active], c.oldBump}} {
		for off := region[0]; off < region[1]; {
			if region[1]-off < header || !c.starts[off/8] {
				return bad("missing object start")
			}
			id, size, aux, flags := c.u32(off), c.u32(off+4), c.u32(off+8), c.u32(off+12)
			if uint64(id) >= uint64(len(c.types)) {
				return bad("object descriptor")
			}
			d := c.types[id]
			payload := uint64(d.PayloadBytes)
			if d.Kind == KindArray {
				payload = uint64(d.ElementSize) * uint64(aux)
			} else if aux != 0 {
				return bad("struct auxiliary word")
			}
			expected := (uint64(header) + payload + 7) &^ uint64(7)
			stride := (expected + 15) &^ uint64(15)
			if uint64(size) != expected || stride > uint64(region[1]-off) {
				return bad("object extent")
			}
			old := off >= c.oldStart[c.active] && off < c.oldBump
			pointerFree := !d.ElementRef && len(d.RefOffsets) == 0
			if flags & ^uint32(FlagPointerFree|FlagOld|FlagLarge) != 0 || (flags&FlagOld != 0) != old || (flags&FlagPointerFree != 0) != pointerFree || (flags&FlagLarge != 0 && !old) {
				return bad("object flags")
			}
			objects[Ref(off)] = old
			off += uint32(stride)
		}
	}
	for i, start := range c.starts {
		_, exists := objects[Ref(uint32(i)*8)]
		if start != exists {
			return bad("extraneous object start")
		}
	}
	valid := func(r Ref) bool {
		if r == 0 || r.IsI31() {
			return true
		}
		_, ok := objects[r]
		return ok
	}
	queued := make(map[Ref]bool)
	if c.rememberedOverflow && len(c.remembered) != cap(c.remembered) {
		return bad("remembered overflow")
	}
	for _, r := range c.remembered {
		old, ok := objects[r]
		if !ok || !old || !c.dirty[uint32(r)/8] || queued[r] {
			return bad("remembered entry")
		}
		queued[r] = true
	}
	for i, dirty := range c.dirty {
		if !dirty {
			continue
		}
		r := Ref(uint32(i) * 8)
		old, ok := objects[r]
		if !ok || !old || (!c.rememberedOverflow && !queued[r]) {
			return bad("dirty entry")
		}
	}
	for r, old := range objects {
		if err := c.eachRef(uint32(r), func(at uint32) error {
			v := Ref(c.u32(at))
			if !valid(v) {
				return bad("object edge")
			}
			if old && v.IsObj() && c.young(v) && !c.dirty[uint32(r)/8] {
				return bad("unremembered old-to-young edge")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, p := range roots {
		if p == nil || !valid(*p) {
			return bad("root")
		}
	}
	return nil
}
