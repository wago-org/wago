//go:build unix

package wago

import (
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// linkBodyStore owns the replay bytes retained for a module that may need
// deferred function-import linking. Its unlinked file is deliberately not
// mapped until a link-time recompile actually needs bodies: ordinary host-only
// modules must not carry every local body in RSS just in case they are later
// linked cross-instance.
type linkBodyStore struct {
	mu     sync.Mutex
	file   *os.File
	sizes  []int
	size   int
	data   []byte
	mapped bool
}

func (s *linkBodyStore) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data != nil && s.mapped {
		_ = syscall.Munmap(s.data)
	}
	s.data, s.mapped = nil, false
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}

func (s *linkBodyStore) isMapped() bool { return s != nil && s.mapped }

// withBodies maps the compact replay file only while fn needs the function
// bodies, then clears the Module's raw slices and unmaps the file. The store is
// serialized because the root link artifact can be used to build distinct
// host/cross variants concurrently.
func (s *linkBodyStore) withBodies(funcs []wasm.Func, fn func() error) error {
	if s == nil || s.size == 0 {
		return fn()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return fmt.Errorf("link replay store is closed")
	}
	if len(funcs) != len(s.sizes) {
		return fmt.Errorf("link replay function count changed: got %d, want %d", len(funcs), len(s.sizes))
	}
	data, err := syscall.Mmap(int(s.file.Fd()), 0, s.size, syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return fmt.Errorf("map replay bodies: %w", err)
	}
	s.data, s.mapped = data, true
	off := 0
	for i, n := range s.sizes {
		funcs[i].BodyBytes = data[off : off+n : off+n]
		off += n
	}
	err = fn()
	for i := range funcs {
		funcs[i].BodyBytes = nil
	}
	_ = syscall.Munmap(data)
	s.data, s.mapped = nil, false
	return err
}

func newLinkBodyStore(funcs []wasm.Func) (*linkBodyStore, [][]byte, error) {
	total := 0
	for i := range funcs {
		if len(funcs[i].BodyBytes) > maxInt()-total {
			return nil, nil, fmt.Errorf("function body replay bytes overflow")
		}
		total += len(funcs[i].BodyBytes)
	}
	bodies := make([][]byte, len(funcs))
	sizes := make([]int, len(funcs))
	if total == 0 {
		return &linkBodyStore{sizes: sizes}, bodies, nil
	}
	f, err := os.CreateTemp("", "wago-link-bodies-*.wasm")
	if err != nil {
		return nil, nil, fmt.Errorf("create replay spool: %w", err)
	}
	name := f.Name()
	cleanup := func() { _ = f.Close(); _ = os.Remove(name) }
	for i := range funcs {
		if _, err := f.Write(funcs[i].BodyBytes); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("write replay spool: %w", err)
		}
	}
	for i := range funcs {
		n := len(funcs[i].BodyBytes)
		sizes[i] = n
	}
	if err := os.Remove(name); err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("unlink replay spool: %w", err)
	}
	return &linkBodyStore{file: f, sizes: sizes, size: total}, bodies, nil
}
