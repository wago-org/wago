//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestFunctionLocalTrapUnwindSharingIsNotProfitableAMD64(t *testing.T) {
	a := &amd64.Asm{}
	start := a.Len()
	a.Load64(RSP, RBX, -offTrapStackReentry)
	a.Ret()
	unwindBytes := a.Len() - start

	b := &amd64.Asm{}
	b.JmpPlaceholder()
	jumpBytes := b.Len()
	if unwindBytes != 5 || jumpBytes != 5 {
		t.Fatalf("trap unwind/jump bytes = %d/%d, want 5/5", unwindBytes, jumpBytes)
	}
	// N local tails cost N*5 bytes; N jumps plus one shared five-byte tail
	// cost N*5+5. Sharing can never cross over without a shorter transfer.
	for groups := 2; groups <= 32; groups++ {
		local := groups * unwindBytes
		shared := groups*jumpBytes + unwindBytes
		if shared <= local {
			t.Fatalf("%d groups unexpectedly cross over: local=%d shared=%d", groups, local, shared)
		}
	}
}
