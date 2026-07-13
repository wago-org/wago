//go:build !unix

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Non-Unix fallback: keep the same compact contiguous replay representation.
// Unix production targets use an unlinked file mapping instead.
type linkBodyStore struct {
	data  []byte
	sizes []int
	size  int
}

func (s *linkBodyStore) Close() {
	if s != nil {
		s.data = nil
	}
}

func (s *linkBodyStore) isMapped() bool { return false }

func (s *linkBodyStore) withBodies(funcs []wasm.Func, fn func() error) error {
	if s == nil {
		return fn()
	}
	if len(funcs) != len(s.sizes) {
		return fmt.Errorf("link replay function count changed: got %d, want %d", len(funcs), len(s.sizes))
	}
	off := 0
	for i, n := range s.sizes {
		funcs[i].BodyBytes = s.data[off : off+n : off+n]
		off += n
	}
	err := fn()
	for i := range funcs {
		funcs[i].BodyBytes = nil
	}
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
	store := &linkBodyStore{data: make([]byte, total), sizes: make([]int, len(funcs)), size: total}
	bodies := make([][]byte, len(funcs))
	off := 0
	for i := range funcs {
		n := copy(store.data[off:], funcs[i].BodyBytes)
		store.sizes[i] = n
		off += n
	}
	return store, bodies, nil
}
