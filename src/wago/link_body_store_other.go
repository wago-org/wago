//go:build !unix

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Non-Unix fallback: keep the same compact contiguous replay representation.
// Unix production targets use an unlinked file mapping instead.
type linkBodyStore struct {
	data []byte
}

func (s *linkBodyStore) Close() {
	if s != nil {
		s.data = nil
	}
}

func (s *linkBodyStore) isMapped() bool { return false }

func newLinkBodyStore(funcs []wasm.Func) (*linkBodyStore, [][]byte, error) {
	total := 0
	for i := range funcs {
		if len(funcs[i].BodyBytes) > maxInt()-total {
			return nil, nil, fmt.Errorf("function body replay bytes overflow")
		}
		total += len(funcs[i].BodyBytes)
	}
	store := &linkBodyStore{data: make([]byte, total)}
	bodies := make([][]byte, len(funcs))
	off := 0
	for i := range funcs {
		n := copy(store.data[off:], funcs[i].BodyBytes)
		bodies[i] = store.data[off : off+n : off+n]
		off += n
	}
	return store, bodies, nil
}
