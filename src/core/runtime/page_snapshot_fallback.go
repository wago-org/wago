//go:build !linux || (!amd64 && !arm64)

package runtime

import (
	"fmt"
	"sync"
	"unsafe"
)

type copiedPageSnapshot struct {
	mu     sync.RWMutex
	data   []byte
	closed bool
}

func newPageSnapshotBacking(data []byte) (pageSnapshotBacking, error) {
	return &copiedPageSnapshot{data: append([]byte(nil), data...)}, nil
}

func (s *copiedPageSnapshot) reset(addr uintptr, size int) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("wago: page snapshot is closed")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(addr)), size), s.data)
	return nil
}

func (s *copiedPageSnapshot) discard(addr uintptr, size int) error {
	return s.reset(addr, size)
}

func (s *copiedPageSnapshot) track(addr uintptr, size, _, _ int) (pageSnapshotDirtyTracker, error) {
	return &copiedPageSnapshotTracker{snapshot: s, addr: addr, size: size}, nil
}

func (*copiedPageSnapshot) pageBacked() bool { return false }

func (s *copiedPageSnapshot) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.data = nil
	return nil
}

type copiedPageSnapshotTracker struct {
	snapshot *copiedPageSnapshot
	addr     uintptr
	size     int
}

func (t *copiedPageSnapshotTracker) reset() error {
	return t.snapshot.reset(t.addr, t.size)
}

func (*copiedPageSnapshotTracker) close() error { return nil }

func (*copiedPageSnapshotTracker) selective() bool { return false }
