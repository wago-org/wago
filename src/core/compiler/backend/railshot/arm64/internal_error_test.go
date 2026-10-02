//go:build arm64

package arm64

import (
	"strings"
	"testing"
)

// A missing compiler hint is an internal invariant violation, not a Wasm
// rejection. Inject it at the real recovery boundary without relying on a
// particular unfixed code-generation bug or adding a production test hook.
func TestCompilePanicHasInternalClassification(t *testing.T) {
	t.Setenv("WAGO_DEBUG_PANIC", "")
	m := mod1(t, nil, nil, []byte{0, 0x0b})
	for _, index := range []int{0, 1} {
		_, _, _, err := compileFuncAttempt(m, nil, index,
			false, false, false, false,
			nil, nil, immutableTableHint{}, nil, false, 0,
			false, false, false, nil, nil, nil,
			false, inlineTargetTable{}, nil, CodegenPolicy{}, nil)
		if index == 1 {
			if err == nil || strings.Contains(err.Error(), "internal compiler error") {
				t.Fatalf("ordinary unknown-function rejection = %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "internal compiler error") || !strings.Contains(err.Error(), "function 0") {
			t.Fatalf("recovered invariant failure = %v; want distinct ICE with function context", err)
		}
	}
}
