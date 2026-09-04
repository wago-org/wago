package gc

import raw "github.com/wago-org/wago/src/core/runtime/gc/native"

type RootSlot interface {
	GetRef() Ref
	SetRef(Ref)
}
type RootSet interface{ RangeRoots(func(RootSlot) bool) }
type Root Ref

func (r *Root) GetRef() Ref      { return Ref(*r) }
func (r *Root) SetRef(value Ref) { *r = Root(value) }

type Slots []RootSlot

func (s Slots) RangeRoots(visit func(RootSlot) bool) {
	for _, slot := range s {
		if !visit(slot) {
			return
		}
	}
}

type RefSliceRoots []Ref

func (s RefSliceRoots) RangeRoots(visit func(RootSlot) bool) {
	for i := range s {
		if !visit((*Root)(&s[i])) {
			return
		}
	}
}

type EmptyRoots struct{}

func (EmptyRoots) RangeRoots(func(RootSlot) bool) {}

func (c *Collector) roots(roots RootSet) (raw.RootSet, error) {
	if err := c.available(); err != nil {
		return nil, err
	}
	if roots == nil {
		return nil, nil
	}
	var refs []Ref
	var failure error
	roots.RangeRoots(func(slot RootSlot) bool {
		if slot == nil {
			failure = ErrInvalidReference
			return false
		}
		refs = append(refs, slot.GetRef())
		return true
	})
	if err := c.available(); err != nil {
		return nil, err
	}
	if failure != nil {
		return nil, failure
	}
	// Validate after every caller callback has returned. No caller code runs
	// while the native collector consumes this snapshot.
	values := make(raw.RefSliceRoots, len(refs))
	for i, ref := range refs {
		value, err := c.unwrap(ref)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}
