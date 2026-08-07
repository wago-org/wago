//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestPreserveIndirectCallTargetAcrossArgumentStaging(t *testing.T) {
	for paramCount, indirect := range intArgRegs {
		t.Run(fmt.Sprintf("reg%d", indirect), func(t *testing.T) {
			a := &encoderamd64.Asm{}
			f := &fn{a: a, s: newStack()}
			target := f.preserveIndirectCallTarget(indirect, paramCount+1)

			argRegs := maskOf(intArgRegs[:paramCount+1]...)
			if target == indirect || argRegs.has(target) {
				t.Fatalf("preserved target = %v, want a non-argument register distinct from %v", target, indirect)
			}
			if !f.pinned.has(target) {
				t.Fatalf("preserved target %v is not pinned", target)
			}
			if len(a.B) == 0 {
				t.Fatal("preserving target emitted no register move")
			}
		})
	}
}

func TestPreserveIndirectCallTargetLeavesSafeRegisterAlone(t *testing.T) {
	a := &encoderamd64.Asm{}
	f := &fn{a: a, s: newStack()}
	if target := f.preserveIndirectCallTarget(RSI, len(intArgRegs)); target != RSI {
		t.Fatalf("safe target = %v, want RSI", target)
	}
	if len(a.B) != 0 {
		t.Fatalf("safe target emitted %d byte(s), want none", len(a.B))
	}
	if !f.pinned.has(RSI) {
		t.Fatal("safe target RSI is not pinned")
	}
}
