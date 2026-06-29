package gc

// RootSlot is the mutable root slot abstraction used by the collector to update
// references after moving nursery collection. Generated stack maps will expose
// frame slots through an allocation-free equivalent later.
type RootSlot interface {
	GetRef() Ref
	SetRef(Ref)
}

type RootSet interface{ RangeRoots(func(RootSlot) bool) }

type Root Ref

func (r *Root) GetRef() Ref  { return Ref(*r) }
func (r *Root) SetRef(v Ref) { *r = Root(v) }

type Slots []RootSlot

func (s Slots) RangeRoots(fn func(RootSlot) bool) {
	for _, slot := range s {
		if !fn(slot) {
			return
		}
	}
}

type RefSliceRoots []Ref

func withExtraRoot(roots RootSet, extra RootSlot) RootSet {
	return extraRootSet{roots: roots, extra: extra}
}

type extraRootSet struct {
	roots RootSet
	extra RootSlot
}

func (s extraRootSet) RangeRoots(fn func(RootSlot) bool) {
	keepGoing := true
	if s.roots != nil {
		s.roots.RangeRoots(func(slot RootSlot) bool {
			keepGoing = fn(slot)
			return keepGoing
		})
	}
	if keepGoing && s.extra != nil {
		fn(s.extra)
	}
}

func (s RefSliceRoots) RangeRoots(fn func(RootSlot) bool) {
	for i := range s {
		slot := sliceRootSlot{slice: s, idx: i}
		if !fn(slot) {
			return
		}
	}
}

type sliceRootSlot struct {
	slice []Ref
	idx   int
}

func (s sliceRootSlot) GetRef() Ref  { return s.slice[s.idx] }
func (s sliceRootSlot) SetRef(r Ref) { s.slice[s.idx] = r }

func (c *Collector) NewGlobalSlot(initial Ref) uint32 {
	c.globalSlots = append(c.globalSlots, initial)
	return uint32(len(c.globalSlots) - 1)
}
func (c *Collector) SetGlobalSlot(i uint32, r Ref) error {
	if int(i) >= len(c.globalSlots) {
		return errRange
	}
	if err := c.validateStoredRef(r, true); err != nil {
		return err
	}
	c.WriteBarrierSlot(SlotGlobal, i, r)
	c.globalSlots[i] = r
	return nil
}
func (c *Collector) GlobalSlot(i uint32) Ref { return c.globalSlots[i] }
func (c *Collector) NewTableSlot(initial Ref) uint32 {
	c.tableSlots = append(c.tableSlots, initial)
	return uint32(len(c.tableSlots) - 1)
}
func (c *Collector) SetTableSlot(i uint32, r Ref) error {
	if int(i) >= len(c.tableSlots) {
		return errRange
	}
	if err := c.validateStoredRef(r, true); err != nil {
		return err
	}
	c.WriteBarrierSlot(SlotTable, i, r)
	c.tableSlots[i] = r
	return nil
}
func (c *Collector) TableSlot(i uint32) Ref { return c.tableSlots[i] }
