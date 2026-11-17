//go:build amd64

package amd64

import (
	"runtime"
	"unsafe"
)

// nodeID is a checked index into storage owned by one compiler stack. Zero is
// absent; the low half names a node plus one and the high half names its chunk.
// It never contains a Go address. Keeping chunk slices alive retains every node,
// including the heap fallback when the optional mapped arena is exhausted.
type nodeID uint32

const nodeChunkBits = 16
const nodeChunkMask = (1 << nodeChunkBits) - 1

func (s *stack) node(id nodeID) *elem {
	if id == 0 {
		return nil
	}
	return &s.chunks[uint32(id)>>nodeChunkBits][(uint32(id)&nodeChunkMask)-1]
}

// ref preserves the existing short-lived pointer API while compact links use
// IDs. The common case is the active chunk; cross-chunk references use a cached
// owner then a checked search. No integer is ever converted back to a pointer.
func (s *stack) ref(e *elem) nodeID {
	if e == nil {
		return 0
	}
	if id, ok := nodeRefInChunk(s.chunks[s.cur], s.cur, e); ok {
		return id
	}
	if s.refChunk != s.cur {
		if id, ok := nodeRefInChunk(s.chunks[s.refChunk], s.refChunk, e); ok {
			return id
		}
	}
	return s.refSlow(e)
}

//go:noinline
func (s *stack) refSlow(e *elem) nodeID {
	s.refSearches++
	for i, chunk := range s.chunks {
		if id, ok := nodeRefInChunk(chunk, uint32(i), e); ok {
			s.refChunk = uint32(i)
			return id
		}
	}
	panic("amd64: operand pointer does not belong to active stack storage")
}

func nodeRefInChunk(chunk []elem, index uint32, e *elem) (nodeID, bool) {
	if len(chunk) == 0 {
		return 0, false
	}
	start := uintptr(unsafe.Pointer(unsafe.SliceData(chunk)))
	address := uintptr(unsafe.Pointer(e))
	size := unsafe.Sizeof(elem{})
	delta := address - start
	ok := address >= start && delta < uintptr(len(chunk))*size && delta%size == 0
	runtime.KeepAlive(chunk)
	runtime.KeepAlive(e)
	if !ok {
		return 0, false
	}
	return nodeID(index<<nodeChunkBits | uint32(delta/size) + 1), true
}
