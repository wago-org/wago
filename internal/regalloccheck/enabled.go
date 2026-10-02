//go:build wago_regalloccheck

package regalloccheck

import "fmt"

const Enabled = true

func (l Location) next(n int) Location {
	if l.Bank == Frame {
		l.Index += int32(n)
	} else {
		l.Byte += uint8(n)
	}
	return l
}

type cell struct {
	value uint64
	byte  uint8
}

// State is confined to checked transfer regions. Missing bytes are unknown,
// never proof that a value is present. No state is rebuilt after a transfer.
type State struct {
	cells map[Location]cell
	next  *uint64
}

func (s *State) Fresh(size int) Value {
	if s.next == nil {
		s.next = new(uint64)
	}
	*s.next++
	v := make(Value, size)
	for i := range v {
		v[i] = cell{*s.next, uint8(i)}
	}
	return v
}

func (s *State) Put(loc Location, v Value) {
	if s.cells == nil {
		s.cells = make(map[Location]cell)
	}
	for i, c := range v {
		s.cells[loc.next(i)] = c
	}
}

func (s *State) Read(loc Location, size int) Value {
	v := make(Value, size)
	for i := range v {
		v[i] = s.cells[loc.next(i)]
	}
	return v
}

// Seed establishes a region's input assumption, retaining existing aliases.
// It must only be used at entry, never to repair an unknown/mismatched reload.
func (s *State) Seed(loc Location, size int) Value {
	v := s.Read(loc, size)
	fresh := s.Fresh(size)
	for i := range v {
		if v[i].value == 0 {
			v[i] = fresh[i]
		}
	}
	s.Put(loc, v)
	return v
}

func (s *State) Apply(e Effect) {
	switch e.Kind {
	case Copy:
		s.Put(e.Dst, s.Read(e.Src, e.Size))
		if e.Dst.Bank == GP && e.Size == 4 {
			// Both targets zero-extend GP32 writes; old high-half identities die.
			s.Put(e.Dst.next(4), make(Value, 4))
		}
	case Swap:
		a, b := s.Read(e.Dst, e.Size), s.Read(e.Src, e.Size)
		s.Put(e.Dst, b)
		s.Put(e.Src, a)
	case Kill:
		s.Put(e.Dst, make(Value, e.Size))
	case Call:
		for loc := range s.cells {
			if loc.Bank != Frame {
				delete(s.cells, loc)
			}
		}
	case Read:
		return
	default:
		panic("regalloccheck: unknown machine effect")
	}
	if e.ClearTo > e.Size {
		s.Put(e.Dst.next(e.Size), make(Value, e.ClearTo-e.Size))
	}
}

func (s *State) Expect(where string, loc Location, want Value) {
	for i, value := range want {
		got := s.cells[loc.next(i)]
		if value.value == 0 || got != value {
			panic(fmt.Sprintf("regalloccheck: %s: %v byte %d has value %d/%d, want %d/%d", where, loc, i, got.value, got.byte, value.value, value.byte))
		}
	}
}

// Meet retains only facts true on both incoming edges. Callers must rename
// branch parameters before meeting states; unequal values are not equal merely
// because allocator locations match.
func (s *State) Meet(other *State) {
	if s.next != other.next {
		panic("regalloccheck: unrelated join identity domains")
	}
	for loc, value := range s.cells {
		if other.cells[loc] != value {
			delete(s.cells, loc)
		}
	}
}

// Clones share the identity counter so independent branch definitions cannot
// acquire the same symbol before Meet.
func (s *State) Clone() *State {
	if s.next == nil {
		s.next = new(uint64)
	}
	out := &State{next: s.next, cells: make(map[Location]cell, len(s.cells))}
	for loc, value := range s.cells {
		out.cells[loc] = value
	}
	return out
}

// ExpectKnown checks a carrier copy without assigning meaning to unspecified
// high bytes of i32/f32 values. An entirely unknown source is never accepted.
func (s *State) ExpectKnown(where string, loc Location, want Value) {
	known := false
	for i, value := range want {
		if value.value == 0 {
			continue
		}
		known = true
		s.Expect(where, loc.next(i), want[i:i+1])
	}
	if !known {
		panic("regalloccheck: " + where + ": unknown source")
	}
}
