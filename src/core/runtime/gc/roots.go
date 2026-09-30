package gc

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
