//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestArenaImmediateFreeDensityHint(t *testing.T) {
	for _, count := range []int{0, 1, 64, 300} {
		body := make([]byte, 0, count*4+1)
		for i := 0; i < count; i++ {
			body = append(body, 0x41, 1, 0x45, 0x1a)
		}
		body = append(body, 0x0b)
		h, err := scanBodyBytes(body, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		want := min(2*count, defaultStackArenaCap)
		if int(h.immediateFreeOps) != want {
			t.Fatalf("count %d: density %d, want %d", count, h.immediateFreeOps, want)
		}
	}
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 64)}}}
	sparse := moduleStackArenaCap(m, []funcHints{{}})
	dense := moduleStackArenaCap(m, []funcHints{{immediateFreeOps: 32}})
	if dense != sparse+16 {
		t.Fatalf("dense capacity %d, sparse %d: missing density allowance", dense, sparse)
	}
	// A saturated hint may select the normal growth policy, never an upper bound
	// on live nodes or a reason to reject an otherwise valid function.
	m.Code[0].BodyBytes = make([]byte, 512)
	if got := moduleStackArenaCap(m, []funcHints{{immediateFreeOps: defaultStackArenaCap}}); got != defaultStackArenaCap {
		t.Fatalf("saturated fallback=%d", got)
	}
}
