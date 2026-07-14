//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBranchHintWeightsIfLocalScores(t *testing.T) {
	body := []byte{
		0x04, 0x40, // if
		0x20, 0x00, 0x1a, // then: local.get 0; drop
		0x05,
		0x20, 0x01, 0x1a, // else: local.get 1; drop
		0x0b, 0x0b,
	}
	likelyThen, err := scanBodyBytesWithHints(body, 0, 2, 0, 0, []wasm.BranchHint{{Offset: 0, Likely: true}})
	if err != nil {
		t.Fatalf("scan likely-then: %v", err)
	}
	if got, want := likelyThen.localScore[0], int64(branchHintWeight); got != want {
		t.Fatalf("likely then local score = %d, want %d", got, want)
	}
	if got := likelyThen.localScore[1]; got != 1 {
		t.Fatalf("unlikely else local score = %d, want 1", got)
	}
	likelyElse, err := scanBodyBytesWithHints(body, 0, 2, 0, 0, []wasm.BranchHint{{Offset: 0, Likely: false}})
	if err != nil {
		t.Fatalf("scan likely-else: %v", err)
	}
	if got, want := likelyElse.localScore[1], int64(branchHintWeight); got != want {
		t.Fatalf("likely else local score = %d, want %d", got, want)
	}
}
