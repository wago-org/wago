//go:build linux && (amd64 || arm64)

package runtime

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

type filePageSnapshot struct {
	mu     sync.RWMutex
	file   *os.File
	closed bool
}

func newPageSnapshotBacking(data []byte) (pageSnapshotBacking, error) {
	if len(data) == 0 || len(data)%pageSize != 0 {
		return nil, fmt.Errorf("wago: page snapshot size %d is not page aligned", len(data))
	}
	f, err := os.CreateTemp("", "wago-page-snapshot-*")
	if err != nil {
		return nil, fmt.Errorf("wago: create page snapshot: %w", err)
	}
	name := f.Name()
	_ = os.Remove(name)
	fail := func(cause error) (pageSnapshotBacking, error) {
		_ = f.Close()
		return nil, cause
	}
	if err = f.Truncate(int64(len(data))); err != nil {
		return fail(fmt.Errorf("wago: size page snapshot: %w", err))
	}
	if _, err = f.WriteAt(data, 0); err != nil {
		return fail(fmt.Errorf("wago: write page snapshot: %w", err))
	}
	return &filePageSnapshot{file: f}, nil
}

func (s *filePageSnapshot) reset(addr uintptr, size int) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("wago: page snapshot is closed")
	}
	mapped, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		addr,
		uintptr(size),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_FIXED|syscall.MAP_PRIVATE,
		s.file.Fd(),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("wago: remap page snapshot: %w", errno)
	}
	if mapped != addr {
		return fmt.Errorf("wago: page snapshot mapped at %#x, want %#x", mapped, addr)
	}
	return nil
}

func (*filePageSnapshot) pageBacked() bool { return true }

func (s *filePageSnapshot) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.file.Close()
}
