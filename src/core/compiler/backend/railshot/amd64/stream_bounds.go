//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"os"
	"unsafe"
)

var streamingBoundsFactsEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_BOUNDS_FACTS") == "1"

// A weak certificate names a live immutable value, not a physical register,
// local binding or backing cell. Creation order changes on every arena reuse;
// the existing admission bound prevents uint16 order wrap within a function.
type streamBoundsFact struct {
	extent uint64
	id     streamID
	order  uint16
}
type streamBoundsCache struct {
	entries [4]streamBoundsFact
	next    uint8
}

func (s *streamState) boundsBytes() uint64 {
	if s.bounds == nil {
		return 0
	}
	return uint64(unsafe.Sizeof(*s.bounds))
}
func (s *streamState) clearBoundsFacts() {
	if s.bounds != nil {
		*s.bounds = streamBoundsCache{}
	}
}
func (s *streamState) boundsCover(id streamID, extent uint64) bool {
	if s.bounds == nil || id == 0 || uint64(id) >= uint64(len(s.nodes)) || s.node(id).refs == 0 {
		return false
	}
	order := s.node(id).order
	for _, e := range s.bounds.entries {
		if e.id == id && e.order == order && e.extent >= extent {
			return true
		}
	}
	return false
}

// Record only after an emitted check's successful fallthrough, using the
// address identity captured before release. A dead identity is not retained
// merely to cache a fact. No callback during destructive lowering consults it.
func (s *streamState) rememberBounds(id streamID, order uint16, extent uint64) {
	if s.bounds == nil || id == 0 || uint64(id) >= uint64(len(s.nodes)) || s.node(id).refs == 0 || s.node(id).order != order {
		return
	}
	for i := range s.bounds.entries {
		e := &s.bounds.entries[i]
		if e.id == id && e.order == order {
			e.extent = max(e.extent, extent)
			return
		}
	}
	s.bounds.entries[s.bounds.next] = streamBoundsFact{extent: extent, id: id, order: order}
	s.bounds.next = (s.bounds.next + 1) % uint8(len(s.bounds.entries))
}
