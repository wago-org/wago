package gc

import "errors"

// RootSlot is the mutable root slot abstraction used by the collector to update
// references after moving nursery collection. Generated stack maps will expose
// frame slots through an allocation-free equivalent later.
type RootSlot interface {
	GetRef() Ref
	SetRef(Ref)
}

type RootSet interface{ RangeRoots(func(RootSlot) bool) }

// RootRefSink receives immutable root values without constructing RootSlot
// interface values or escaping callback closures. Native frame walkers implement
// DirectRootRefSet for allocation-free marking; moving/rewrite passes continue to
// use RootSet.RangeRoots so they can update the underlying slots.
type RootRefSink interface{ VisitRootRef(Ref) bool }

// DirectRootRefSet returns false when the sink stops enumeration. Composite
// root sets propagate that result without allocating adapter state.
type DirectRootRefSet interface{ RangeRootRefs(RootRefSink) bool }

// ClassifiedRootRefSink receives immutable roots with their exact telemetry
// ownership. It is used only by opt-in telemetry integrations; ordinary
// collection continues through DirectRootRefSet.
type ClassifiedRootRefSink interface {
	VisitClassifiedRootRef(RootClass, Ref) bool
}

// DirectClassifiedRootRefSet lets a runtime attribute roots without allocating
// a RootGroups slice or boxing mutable RootSlot values. It returns false when the
// sink stops enumeration.
type DirectClassifiedRootRefSet interface {
	RangeClassifiedRootRefs(ClassifiedRootRefSink) bool
}

// ClassifiedRoots assigns one telemetry ownership class to an exact root set.
// Collection semantics are unchanged when telemetry is disabled.
type ClassifiedRoots struct {
	Class RootClass
	Roots RootSet
}

func (s ClassifiedRoots) RangeRoots(fn func(RootSlot) bool) {
	if s.Roots != nil {
		s.Roots.RangeRoots(fn)
	}
}

func (s ClassifiedRoots) RangeRootRefs(sink RootRefSink) bool {
	if s.Roots == nil {
		return true
	}
	if direct, ok := s.Roots.(DirectRootRefSet); ok {
		return direct.RangeRootRefs(sink)
	}
	keepGoing := true
	s.Roots.RangeRoots(func(slot RootSlot) bool {
		keepGoing = sink.VisitRootRef(slot.GetRef())
		return keepGoing
	})
	return keepGoing
}

// RootGroup allows one collection to report independently owned root classes.
type RootGroup struct {
	Class RootClass
	Roots RootSet
}

// RootGroups is an exact composite RootSet used by telemetry-aware runtime
// integrations for frames, public tokens, foreign instances, and temporary roots.
type RootGroups []RootGroup

func (groups RootGroups) RangeRoots(fn func(RootSlot) bool) {
	for _, group := range groups {
		if group.Roots == nil {
			continue
		}
		keepGoing := true
		group.Roots.RangeRoots(func(slot RootSlot) bool {
			keepGoing = fn(slot)
			return keepGoing
		})
		if !keepGoing {
			return
		}
	}
}

func (groups RootGroups) RangeRootRefs(sink RootRefSink) bool {
	for _, group := range groups {
		if group.Roots == nil {
			continue
		}
		if direct, ok := group.Roots.(DirectRootRefSet); ok {
			if !direct.RangeRootRefs(sink) {
				return false
			}
			continue
		}
		keepGoing := true
		group.Roots.RangeRoots(func(slot RootSlot) bool {
			keepGoing = sink.VisitRootRef(slot.GetRef())
			return keepGoing
		})
		if !keepGoing {
			return false
		}
	}
	return true
}

// EmptyRoots is an explicit non-nil root set for may-collect operations that
// have proven no live refs. Its zero-sized value avoids allocating a slice
// header merely to distinguish an exact empty set from a missing root set.
type EmptyRoots struct{}

func (EmptyRoots) RangeRoots(func(RootSlot) bool) {}
func (EmptyRoots) RangeRootRefs(RootRefSink) bool { return true }

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

func (s Slots) RangeRootRefs(sink RootRefSink) bool {
	for _, slot := range s {
		if !sink.VisitRootRef(slot.GetRef()) {
			return false
		}
	}
	return true
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

// ArrayInitializerRootScratch is reusable caller-owned root composition for
// reference array constructors. Compact Refs are stable handles, so collection
// only needs their immutable values; the direct visitor avoids RootSlot boxing,
// callback closures, and per-constructor Go allocations. It is not concurrent.
type ArrayInitializerRootScratch struct {
	first   RootSet
	values  []Value
	uniform Ref
	mode    uint8 // 1 uniform, 2 fixed
	active  bool
}

func (s *ArrayInitializerRootScratch) RangeRootRefs(sink RootRefSink) bool {
	if s.first != nil {
		if direct, ok := s.first.(DirectRootRefSet); ok {
			if !direct.RangeRootRefs(sink) {
				return false
			}
		} else {
			keepGoing := true
			if !rangeRootRefs(s.first, func(r Ref) bool {
				keepGoing = sink.VisitRootRef(r)
				return keepGoing
			}) {
				s.first.RangeRoots(func(slot RootSlot) bool {
					keepGoing = sink.VisitRootRef(slot.GetRef())
					return keepGoing
				})
			}
			if !keepGoing {
				return false
			}
		}
	}
	switch s.mode {
	case 1:
		return sink.VisitRootRef(s.uniform)
	case 2:
		for i := range s.values {
			if !sink.VisitRootRef(s.values[i].Ref) {
				return false
			}
		}
	}
	return true
}

func (s *ArrayInitializerRootScratch) RangeRoots(fn func(RootSlot) bool) {
	if s.first != nil {
		keepGoing := true
		s.first.RangeRoots(func(slot RootSlot) bool {
			keepGoing = fn(slot)
			return keepGoing
		})
		if !keepGoing {
			return
		}
	}
	switch s.mode {
	case 1:
		fn(arrayUniformRootSlot{scratch: s})
	case 2:
		for i := range s.values {
			if !fn(valueRootSlot{values: s.values, idx: i}) {
				return
			}
		}
	}
}

type arrayUniformRootSlot struct{ scratch *ArrayInitializerRootScratch }

func (s arrayUniformRootSlot) GetRef() Ref  { return s.scratch.uniform }
func (s arrayUniformRootSlot) SetRef(r Ref) { s.scratch.uniform = r }

func (s *ArrayInitializerRootScratch) prepareUniform(first RootSet, ref Ref) bool {
	if s == nil || s.active {
		return false
	}
	s.first, s.uniform, s.mode, s.active = first, ref, 1, true
	return true
}

func (s *ArrayInitializerRootScratch) prepareFixed(first RootSet, values []Value) bool {
	if s == nil || s.active {
		return false
	}
	s.first, s.values, s.mode, s.active = first, values, 2, true
	return true
}

func (s *ArrayInitializerRootScratch) clear() {
	s.first, s.values, s.uniform, s.mode, s.active = nil, nil, Null(), 0, false
}

// InitializerRootScratch is reusable caller-owned storage for composing exact
// frame roots with struct initializer values across a collection-capable
// allocation. It is configured and cleared by NewStructWithRootScratch and must
// not be used concurrently.
type InitializerRootScratch struct {
	first  RootSet
	values []Value
	fields []FieldDesc
	active bool
}

func (s *InitializerRootScratch) RangeRoots(fn func(RootSlot) bool) {
	keepGoing := true
	if s.first != nil {
		s.first.RangeRoots(func(slot RootSlot) bool {
			keepGoing = fn(slot)
			return keepGoing
		})
	}
	if !keepGoing {
		return
	}
	for i := range s.values {
		if i >= len(s.fields) || !isCollectorRefKind(s.fields[i].Kind) {
			continue
		}
		if !fn(valueRootSlot{values: s.values, idx: i}) {
			return
		}
	}
}

func (s *InitializerRootScratch) prepare(first RootSet, values []Value, fields []FieldDesc) bool {
	if s == nil || s.active {
		return false
	}
	s.first, s.values, s.fields, s.active = first, values, fields, true
	return true
}

func (s *InitializerRootScratch) clear() {
	s.first, s.values, s.fields, s.active = nil, nil, nil, false
}

// InitializerWordRootScratch is the raw-slot counterpart used by the Wasm
// struct.new helper after it has prevalidated every field. Collector references
// remain mutable in the parked helper argument slots across a moving collection.
type InitializerWordRootScratch struct {
	first  RootSet
	words  []uint64
	fields []FieldDesc
	active bool
}

func (s *InitializerWordRootScratch) RangeRoots(fn func(RootSlot) bool) {
	if s.first != nil {
		keepGoing := true
		s.first.RangeRoots(func(slot RootSlot) bool {
			keepGoing = fn(slot)
			return keepGoing
		})
		if !keepGoing {
			return
		}
	}
	cursor := 0
	for _, field := range s.fields {
		if cursor >= len(s.words) {
			return
		}
		if isCollectorRefKind(field.Kind) && !fn(wordRootSlot{words: s.words, idx: cursor}) {
			return
		}
		cursor++
		if field.Kind == StorageV128 {
			cursor++
		}
	}
}

func (s *InitializerWordRootScratch) prepare(first RootSet, words []uint64, fields []FieldDesc) bool {
	if s == nil || s.active {
		return false
	}
	s.first, s.words, s.fields, s.active = first, words, fields, true
	return true
}

func (s *InitializerWordRootScratch) clear() {
	s.first, s.words, s.fields, s.active = nil, nil, nil, false
}

type wordRootSlot struct {
	words []uint64
	idx   int
}

func (s wordRootSlot) GetRef() Ref  { return Ref(uint32(s.words[s.idx])) }
func (s wordRootSlot) SetRef(r Ref) { s.words[s.idx] = uint64(r) }

type valueRootSlot struct {
	values []Value
	idx    int
}

func (s valueRootSlot) GetRef() Ref  { return s.values[s.idx].Ref }
func (s valueRootSlot) SetRef(r Ref) { s.values[s.idx].Ref = r }

func (s valueRootSet) RangeRootRefs(sink RootRefSink) bool {
	for i := range s.values {
		if !s.all && (i >= len(s.fields) || !isCollectorRefKind(s.fields[i].Kind)) {
			continue
		}
		if !sink.VisitRootRef(s.values[i].Ref) {
			return false
		}
	}
	return true
}

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

func rangeRootRefs(roots RootSet, fn func(Ref) bool) bool {
	if roots == nil {
		return true
	}
	visitFallback := func(set RootSet) bool {
		keepGoing := true
		set.RangeRoots(func(slot RootSlot) bool {
			keepGoing = fn(slot.GetRef())
			return keepGoing
		})
		return keepGoing
	}
	switch s := roots.(type) {
	case EmptyRoots:
		return true
	case Slots:
		for _, slot := range s {
			if !fn(slot.GetRef()) {
				return true
			}
		}
		return true
	case valueRootSet:
		for i := range s.values {
			if !s.all && (i >= len(s.fields) || !isCollectorRefKind(s.fields[i].Kind)) {
				continue
			}
			if !fn(s.values[i].Ref) {
				return true
			}
		}
		return true
	case *InitializerRootScratch:
		keepGoing := true
		if s.first != nil && !rangeRootRefs(s.first, func(r Ref) bool {
			keepGoing = fn(r)
			return keepGoing
		}) {
			keepGoing = visitFallback(s.first)
		}
		if keepGoing {
			for i := range s.values {
				if i >= len(s.fields) || !isCollectorRefKind(s.fields[i].Kind) {
					continue
				}
				if !fn(s.values[i].Ref) {
					break
				}
			}
		}
		return true
	case combinedRootSet:
		keepGoing := true
		if !rangeRootRefs(s.first, func(r Ref) bool {
			keepGoing = fn(r)
			return keepGoing
		}) {
			keepGoing = visitFallback(s.first)
		}
		if keepGoing && !rangeRootRefs(s.second, fn) {
			visitFallback(s.second)
		}
		return true
	case extraRootSet:
		keepGoing := true
		if !rangeRootRefs(s.roots, func(r Ref) bool {
			keepGoing = fn(r)
			return keepGoing
		}) {
			keepGoing = visitFallback(s.roots)
		}
		if keepGoing && s.extra != nil {
			fn(s.extra.GetRef())
		}
		return true
	case RefSliceRoots:
		for i := range s {
			if !fn(s[i]) {
				return true
			}
		}
		return true
	default:
		return false
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

func (s RefSliceRoots) RangeRootRefs(sink RootRefSink) bool {
	for _, r := range s {
		if !sink.VisitRootRef(r) {
			return false
		}
	}
	return true
}

type sliceRootSlot struct {
	slice []Ref
	idx   int
}

func (s sliceRootSlot) GetRef() Ref  { return s.slice[s.idx] }
func (s sliceRootSlot) SetRef(r Ref) { s.slice[s.idx] = r }

func slotIndexOK(i uint32, n int) bool { return uint64(i) < uint64(n) }

var errTinyUnsafeSweepRoot = errors.New("gc: Tiny sweep cannot publish an unmarked reference graph")

func (c *Collector) validateTinySweepRootPublication(r Ref) error {
	if !r.IsObj() {
		return nil
	}
	h := handleOf(r)
	if c.tinyGC.state == tinySweep && c.tinyGC.scan.handle == h {
		return errTinyUnsafeSweepRoot
	}
	if !c.tinyIsWhite(h) {
		return nil
	}
	d, err := c.refDesc(r)
	if err != nil {
		return err
	}
	if d.HasRefs {
		// A one-pass sweep cannot reconstruct descendants reclaimed before an
		// omitted white graph is published. Checked persistent-root APIs reject
		// that unsafe late resurrection before mutating the slot. Pointer-free
		// objects remain safe for the existing immediate sweep barrier.
		return errTinyUnsafeSweepRoot
	}
	return nil
}

func (c *Collector) newRootSlot(kind SlotKind, slots *[]Ref, initial Ref) (uint32, error) {
	if err := c.errIfClosed(); err != nil {
		return 0, err
	}
	if err := c.validateStoredRef(initial, true); err != nil {
		return 0, err
	}
	if c.cfg.Profile == ProfileTiny && c.tinySweepActive() {
		if err := c.validateTinySweepRootPublication(initial); err != nil {
			return 0, err
		}
	}
	*slots = append(*slots, initial)
	index := uint32(len(*slots) - 1)
	c.ensureSlotCardBit(kind, index)
	c.WriteBarrierSlot(kind, index, initial)
	return index, nil
}

// NewGlobalSlot creates a nullable global root slot for trusted/test setup. It
// panics if initial is not null, i31, or a live object ref owned by this
// collector, or if Tiny sweep cannot safely publish its pointerful graph;
// production decoding/instantiation paths must use NewCheckedGlobalSlot so
// rejected refs are reported as errors.
func (c *Collector) NewGlobalSlot(initial Ref) uint32 {
	i, err := c.NewCheckedGlobalSlot(initial)
	if err != nil {
		panic("gc: invalid initial global ref: " + err.Error())
	}
	return i
}

// NewCheckedGlobalSlot creates a nullable global root slot after validating the
// initial ref. Rejected refs, including unsafe Tiny sweep publications, do not
// append a slot.
func (c *Collector) NewCheckedGlobalSlot(initial Ref) (uint32, error) {
	return c.newRootSlot(SlotGlobal, &c.globalSlots, initial)
}

// NewCheckedClassifiedGlobalSlot creates a collector-owned persistent slot and
// assigns its telemetry ownership. Classification is inert without
// wago_gcstats or an attached recorder.
func (c *Collector) NewCheckedClassifiedGlobalSlot(initial Ref, class RootClass) (uint32, error) {
	if class >= rootClassCount {
		return 0, errors.New("gc: invalid root telemetry class")
	}
	i, err := c.newRootSlot(SlotGlobal, &c.globalSlots, initial)
	if err == nil && c.telemetryEnabled() {
		c.cfg.Telemetry.setGlobalRootClass(i, class)
	}
	return i, err
}

// SetGlobalSlot validates and publishes a collector-owned global root. Tiny
// rejects an unmarked pointerful graph during sweep because a one-pass sweep may
// already have reclaimed one of its descendants.
func (c *Collector) SetGlobalSlot(i uint32, r Ref) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if !slotIndexOK(i, len(c.globalSlots)) {
		return errRange
	}
	// Module-global synchronization commonly republishes unchanged roots at
	// allocation safepoints. The stored value was already validated and its
	// existing card remains conservative, so avoid repeating validation and the
	// write barrier when no publication occurs.
	if c.globalSlots[i] == r {
		return nil
	}
	if err := c.validateStoredRef(r, true); err != nil {
		return err
	}
	if c.cfg.Profile == ProfileTiny && c.tinySweepActive() {
		if err := c.validateTinySweepRootPublication(r); err != nil {
			return err
		}
	}
	c.globalSlots[i] = r
	c.WriteBarrierSlot(SlotGlobal, i, r)
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
// collector, or if Tiny sweep cannot safely publish its pointerful graph;
// production decoding/instantiation paths must use NewCheckedTableSlot so
// rejected refs are reported as errors.
func (c *Collector) NewTableSlot(initial Ref) uint32 {
	i, err := c.NewCheckedTableSlot(initial)
	if err != nil {
		panic("gc: invalid initial table ref: " + err.Error())
	}
	return i
}

// NewCheckedTableSlot creates a nullable table root slot after validating the
// initial ref. Rejected refs, including unsafe Tiny sweep publications, do not
// append a slot.
func (c *Collector) NewCheckedTableSlot(initial Ref) (uint32, error) {
	return c.newRootSlot(SlotTable, &c.tableSlots, initial)
}

// SetTableSlot validates and publishes a collector-owned table root. It has the
// same fail-closed Tiny sweep rule as SetGlobalSlot.
func (c *Collector) SetTableSlot(i uint32, r Ref) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if !slotIndexOK(i, len(c.tableSlots)) {
		return errRange
	}
	if c.tableSlots[i] == r {
		return nil
	}
	if err := c.validateStoredRef(r, true); err != nil {
		return err
	}
	if c.cfg.Profile == ProfileTiny && c.tinySweepActive() {
		if err := c.validateTinySweepRootPublication(r); err != nil {
			return err
		}
	}
	c.tableSlots[i] = r
	c.WriteBarrierSlot(SlotTable, i, r)
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
