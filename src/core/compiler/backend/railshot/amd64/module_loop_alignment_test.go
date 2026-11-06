//go:build amd64

package amd64

import "testing"

func TestCompactLoopAlign32PreservesModuleBaseAMD64(t *testing.T) {
	policy := currentCodegenPolicy()
	if !policy.EnabledOption(optCompactLoopAlign32) {
		t.Skip("compact-loop-align32 disabled")
	}
	for off := 0; off < 64; off++ {
		for _, adapter := range []bool{false, true} {
			pad := functionStartPadding(off, 48, adapter, funcHints{flags: hintHasLoop}, policy)
			if (off+pad)&31 != 0 {
				t.Fatalf("offset=%d adapter=%t padding=%d moves a relative32-byte loop off its fetch-block phase", off, adapter, pad)
			}
		}
	}
}
