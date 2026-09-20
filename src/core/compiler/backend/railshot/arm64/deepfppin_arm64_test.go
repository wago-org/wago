//go:build arm64

package arm64

import "testing"

func TestSeventeenthCallFreeFPPinARM64(t *testing.T) {
	// Seventeen f64 locals are all read in a syntactic loop, making every one a
	// hot pin candidate while leaving the function call-free.
	body := []byte{0x01, 0x11, 0x7c, 0x03, 0x40}
	for i := byte(0); i < 17; i++ {
		body = append(body, 0x20, i, 0x1a) // local.get i; drop
	}
	body = append(body, 0x0b, 0x0b)
	m := mod1(t, nil, nil, body)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.PinnedLocals; got != callFreePinnedFLocalRegs {
		t.Fatalf("pinned locals = %d, want %d", got, callFreePinnedFLocalRegs)
	}
}
