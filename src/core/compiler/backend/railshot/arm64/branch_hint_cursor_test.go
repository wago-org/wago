//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBranchHintCursorSparseOffsets(t *testing.T) {
	s := byteBodyScanner{branchHints: []wasm.BranchHint{{Offset: 2, Likely: true}, {Offset: 7, Likely: false}}}
	for _, tc := range []struct {
		off           uint32
		likely, found bool
	}{{0, false, false}, {2, true, true}, {2, true, true}, {3, false, false}, {7, false, true}, {8, false, false}} {
		likely, found := s.branchHintAt(tc.off)
		if likely != tc.likely || found != tc.found {
			t.Fatalf("offset %d: %v/%v", tc.off, likely, found)
		}
	}
}
