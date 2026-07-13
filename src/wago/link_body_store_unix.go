//go:build unix

package wago

import (
	"fmt"
	"os"
	"syscall"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// linkBodyStore owns the replay bytes retained for a module that may need
// deferred function-import linking. On the production Unix targets the bytes
// are an unlinked private mapping, not a second Go-heap copy of every body.
type linkBodyStore struct {
	data   []byte
	mapped bool
}

func (s *linkBodyStore) Close() {
	if s != nil && s.data != nil && s.mapped {
		_ = syscall.Munmap(s.data)
	}
	if s != nil {
		s.data = nil
	}
}

func (s *linkBodyStore) isMapped() bool { return s != nil && s.mapped }

func newLinkBodyStore(funcs []wasm.Func) (*linkBodyStore, [][]byte, error) {
	total := 0
	for i := range funcs {
		if len(funcs[i].BodyBytes) > maxInt()-total {
			return nil, nil, fmt.Errorf("function body replay bytes overflow")
		}
		total += len(funcs[i].BodyBytes)
	}
	bodies := make([][]byte, len(funcs))
	if total == 0 {
		return &linkBodyStore{}, bodies, nil
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
	data, err := syscall.Mmap(int(f.Fd()), 0, total, syscall.PROT_READ, syscall.MAP_PRIVATE)
	cleanup() // the mapping keeps the anonymous file object alive
	if err != nil {
		return nil, nil, fmt.Errorf("map replay spool: %w", err)
	}
	off := 0
	for i := range funcs {
		n := len(funcs[i].BodyBytes)
		bodies[i] = data[off : off+n : off+n]
		off += n
	}
	return &linkBodyStore{data: data, mapped: true}, bodies, nil
}
