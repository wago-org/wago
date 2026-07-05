package wago

import (
	"fmt"
	"sync"
)

// Handle is an opaque integer reference an extension hands to a guest in place of
// a Go pointer (for sockets, files, timers, subscriptions, …). Its upper 32 bits
// are a generation counter and its lower 32 bits a slot index, so a stale handle
// (one whose slot has since been closed and reused) fails cleanly instead of
// aliasing a new resource.
type Handle uint64

func makeHandle(idx int, gen uint32) Handle {
	return Handle(uint64(gen)<<32 | uint64(uint32(idx)))
}

func (h Handle) slot() int   { return int(uint32(uint64(h))) }
func (h Handle) gen() uint32 { return uint32(uint64(h) >> 32) }

// Resource is anything the runtime owns on a guest's behalf and closes when the
// handle is released.
type Resource interface {
	Close() error
}

type handleSlot struct {
	gen  uint32
	kind string
	res  Resource
	used bool
}

// HandleTable maps opaque Handles to host-owned Resources with generation-checked
// slots. It is safe for concurrent use.
type HandleTable struct {
	mu    sync.Mutex
	slots []handleSlot
	free  []int
}

// NewHandleTable returns an empty handle table.
func NewHandleTable() *HandleTable { return &HandleTable{} }

// Insert stores a resource under a kind tag and returns its handle.
func (t *HandleTable) Insert(kind string, r Resource) Handle {
	t.mu.Lock()
	defer t.mu.Unlock()
	var idx int
	if n := len(t.free); n > 0 {
		idx = t.free[n-1]
		t.free = t.free[:n-1]
	} else {
		idx = len(t.slots)
		t.slots = append(t.slots, handleSlot{})
	}
	s := &t.slots[idx]
	s.kind, s.res, s.used = kind, r, true
	return makeHandle(idx, s.gen)
}

// Get returns the resource for a handle, requiring the kind tag to match. It
// returns false for a stale, wrong-kind, or unknown handle.
func (t *HandleTable) Get(h Handle, kind string) (Resource, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.live(h)
	if !ok || s.kind != kind {
		return nil, false
	}
	return s.res, true
}

// Close releases a handle, closing its resource and bumping the slot generation
// so the handle can never be reused. It is idempotent-safe: a stale handle
// returns ErrInvalidHandle without touching the slot.
func (t *HandleTable) Close(h Handle) error {
	t.mu.Lock()
	s, ok := t.live(h)
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("handle %d: %w", uint64(h), ErrInvalidHandle)
	}
	res := s.res
	s.res, s.used = nil, false
	s.gen++ // invalidate outstanding handles to this slot
	s.kind = ""
	t.free = append(t.free, h.slot())
	t.mu.Unlock()
	if res != nil {
		return res.Close()
	}
	return nil
}

// CloseAll closes every live resource. It returns the first close error, if any,
// after attempting all of them.
func (t *HandleTable) CloseAll() error {
	t.mu.Lock()
	var resources []Resource
	for i := range t.slots {
		s := &t.slots[i]
		if s.used {
			resources = append(resources, s.res)
			s.res, s.used, s.kind = nil, false, ""
			s.gen++
			t.free = append(t.free, i)
		}
	}
	t.mu.Unlock()
	var firstErr error
	for _, r := range resources {
		if r != nil {
			if err := r.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Len returns the number of live handles.
func (t *HandleTable) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for i := range t.slots {
		if t.slots[i].used {
			n++
		}
	}
	return n
}

// live returns the slot for a handle if it is in range, in use, and its
// generation matches. Caller holds t.mu.
func (t *HandleTable) live(h Handle) (*handleSlot, bool) {
	idx := h.slot()
	if idx < 0 || idx >= len(t.slots) {
		return nil, false
	}
	s := &t.slots[idx]
	if !s.used || s.gen != h.gen() {
		return nil, false
	}
	return s, true
}
