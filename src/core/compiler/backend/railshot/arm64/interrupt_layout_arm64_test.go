//go:build arm64

package arm64

import (
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestPollFreeLoopHeaderPreservesCooperativeFetchPhase(t *testing.T) {
	for _, prefix := range []int{0, 4, 12, 16, 20, 28} {
		polled := fn{a: &a64.Asm{}, interruptible: true, nLocals: 4}
		polled.a.B = make([]byte, prefix)
		polled.alignLoopHeader()
		polledBodyPhase := (polled.a.Len() + 16) % 32 // four-instruction poll

		pollFree := fn{a: &a64.Asm{}, nLocals: 4}
		pollFree.a.B = make([]byte, prefix)
		pollFree.alignLoopHeader()
		if got := pollFree.a.Len() % 32; got != polledBodyPhase {
			t.Fatalf("prefix %d: poll-free phase = %d, polled body phase = %d", prefix, got, polledBodyPhase)
		}
	}
}

func TestPollFreeLoopHeaderLimitsPaddingForLargeFunctions(t *testing.T) {
	f := fn{a: &a64.Asm{}, nLocals: pollFreeLoopPhaseMaxLocals + 1}
	f.a.B = make([]byte, 4)
	f.alignLoopHeader()
	if got, want := f.a.Len(), 16; got != want {
		t.Fatalf("large-function aligned length = %d, want %d", got, want)
	}
}
