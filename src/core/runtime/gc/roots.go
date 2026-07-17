package gc

// RootSlot is the mutable root slot abstraction used by the collector to update
// references after moving nursery collection. Generated stack maps will expose
// frame slots through an allocation-free equivalent later.
type RootSlot interface {
	GetRef() Ref
	SetRef(Ref)
}

type RootSet interface{ RangeRoots(func(RootSlot) bool) }

// EmptyRoots is an explicit non-nil root set for may-collect operations that
// have proven no live refs. Its zero-sized value avoids allocating a slice
// header merely to distinguish an exact empty set from a missing root set.
type EmptyRoots struct{}

func (EmptyRoots) RangeRoots(func(RootSlot) bool) {}

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

type combinedRootSet struct {
	first  RootSet
	second RootSet
}

type valueRootSet struct {
	values []Value
	fields []FieldDesc
	all    bool
}

type valueRootSlot struct {
	values []Value
	idx    int
}

func (s valueRootSlot) GetRef() Ref  { return s.values[s.idx].Ref }
func (s valueRootSlot) SetRef(r Ref) { s.values[s.idx].Ref = r }

func (s valueRootSet) RangeRoots(fn func(RootSlot) bool) {
	for i := range s.values {
		if !s.all && (i >= len(s.fields) || !isCollectorRefKind(s.fields[i].Kind)) {
			continue
		}
		if !fn(valueRootSlot{values: s.values, idx: i}) {
			return
		}
	}
}

func rangeRootRefs(roots RootSet, fn func(Ref) bool) {
	if roots == nil {
		return
	}
	switch s := roots.(type) {
	case EmptyRoots:
		return
	case valueRootSet:
		for i := range s.values {
			if !s.all && (i >= len(s.fields) || !isCollectorRefKind(s.fields[i].Kind)) {
				continue
			}
			if !fn(s.values[i].Ref) {
				return
			}
		}
	case combinedRootSet:
		keepGoing := true
		rangeRootRefs(s.first, func(r Ref) bool {
			keepGoing = fn(r)
			return keepGoing
		})
		if keepGoing {
			rangeRootRefs(s.second, fn)
		}
	case extraRootSet:
		keepGoing := true
		rangeRootRefs(s.roots, func(r Ref) bool {
			keepGoing = fn(r)
			return keepGoing
		})
		if keepGoing && s.extra != nil {
			fn(s.extra.GetRef())
		}
	case RefSliceRoots:
		for i := range s {
			if !fn(s[i]) {
				return
			}
		}
	default:
		roots.RangeRoots(func(slot RootSlot) bool { return fn(slot.GetRef()) })
	}
}

func combineRootSets(first, second RootSet) RootSet {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return combinedRootSet{first: first, second: second}
}

func (s combinedRootSet) RangeRoots(fn func(RootSlot) bool) {
	keepGoing := true
	s.first.RangeRoots(func(slot RootSlot) bool {
		keepGoing = fn(slot)
		return keepGoing
	})
	if keepGoing {
		s.second.RangeRoots(fn)
	}
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

func slotIndexOK(i uint32, n int) bool { return uint64(i) < uint64(n) }

func (c *Collector) newRootSlot(slots *[]Ref, initial Ref) (uint32, error) {
	if err := c.errIfClosed(); err != nil {
		return 0, err
	}
	if err := c.validateStoredRef(initial, true); err != nil {
		return 0, err
	}
	*slots = append(*slots, initial)
	return uint32(len(*slots) - 1), nil
}

// NewGlobalSlot creates a nullable global root slot for trusted/test setup. It
// panics if initial is not null, i31, or a live object ref owned by this
// collector; production decoding/instantiation paths must use
// NewCheckedGlobalSlot so invalid refs are reported as errors.
func (c *Collector) NewGlobalSlot(initial Ref) uint32 {
	i, err := c.NewCheckedGlobalSlot(initial)
	if err != nil {
		panic("gc: invalid initial global ref: " + err.Error())
	}
	return i
}

// NewCheckedGlobalSlot creates a nullable global root slot after validating the
// initial ref. Rejected refs do not append a slot.
func (c *Collector) NewCheckedGlobalSlot(initial Ref) (uint32, error) {
	return c.newRootSlot(&c.globalSlots, initial)
}
func (c *Collector) SetGlobalSlot(i uint32, r Ref) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if !slotIndexOK(i, len(c.globalSlots)) {
		return errRange
	}
	if err := c.validateStoredRef(r, true); err != nil {
		return err
	}
	c.WriteBarrierSlot(SlotGlobal, i, r)
	c.pruneSlotCardUnlessNursery(SlotGlobal, i, r)
	c.globalSlots[i] = r
	return nil
}

// GlobalSlot returns the current global root value. Invalid indexes return
// null; use CheckedGlobalSlot when the caller must distinguish null from an
// out-of-range slot.
func (c *Collector) GlobalSlot(i uint32) Ref {
	if !slotIndexOK(i, len(c.globalSlots)) {
		return Null()
	}
	return c.globalSlots[i]
}

func (c *Collector) CheckedGlobalSlot(i uint32) (Ref, error) {
	if err := c.errIfClosed(); err != nil {
		return Null(), err
	}
	if !slotIndexOK(i, len(c.globalSlots)) {
		return Null(), errRange
	}
	return c.globalSlots[i], nil
}

// NewTableSlot creates a nullable table root slot for trusted/test setup. It
// panics if initial is not null, i31, or a live object ref owned by this
// collector; production decoding/instantiation paths must use NewCheckedTableSlot
// so invalid refs are reported as errors.
func (c *Collector) NewTableSlot(initial Ref) uint32 {
	i, err := c.NewCheckedTableSlot(initial)
	if err != nil {
		panic("gc: invalid initial table ref: " + err.Error())
	}
	return i
}

// NewCheckedTableSlot creates a nullable table root slot after validating the
// initial ref. Rejected refs do not append a slot.
func (c *Collector) NewCheckedTableSlot(initial Ref) (uint32, error) {
	return c.newRootSlot(&c.tableSlots, initial)
}
func (c *Collector) SetTableSlot(i uint32, r Ref) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if !slotIndexOK(i, len(c.tableSlots)) {
		return errRange
	}
	if err := c.validateStoredRef(r, true); err != nil {
		return err
	}
	c.WriteBarrierSlot(SlotTable, i, r)
	c.pruneSlotCardUnlessNursery(SlotTable, i, r)
	c.tableSlots[i] = r
	return nil
}

// TableSlot returns the current table root value. Invalid indexes return null;
// use CheckedTableSlot when the caller must distinguish null from an out-of-range
// slot.
func (c *Collector) TableSlot(i uint32) Ref {
	if !slotIndexOK(i, len(c.tableSlots)) {
		return Null()
	}
	return c.tableSlots[i]
}

func (c *Collector) CheckedTableSlot(i uint32) (Ref, error) {
	if err := c.errIfClosed(); err != nil {
		return Null(), err
	}
	if !slotIndexOK(i, len(c.tableSlots)) {
		return Null(), errRange
	}
	return c.tableSlots[i], nil
}
