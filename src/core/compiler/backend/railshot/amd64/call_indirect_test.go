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
			target, spilled := f.preserveIndirectCallTarget(indirect, paramCount+1)

			argRegs := maskOf(intArgRegs[:paramCount+1]...)
			if spilled {
				t.Fatal("ordinary target preservation unexpectedly spilled")
			}
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
	if target, spilled := f.preserveIndirectCallTarget(RSI, len(intArgRegs)); target != RSI || spilled {
		t.Fatalf("safe target = %v, want RSI", target)
	}
	if len(a.B) != 0 {
		t.Fatalf("safe target emitted %d byte(s), want none", len(a.B))
	}
	if !f.pinned.has(RSI) {
		t.Fatal("safe target RSI is not pinned")
	}
}

func TestPreserveIndirectCallTargetFallsBackToInstanceScratch(t *testing.T) {
	a := &encoderamd64.Asm{}
	f := &fn{a: a, s: newStack()}
	indirect := intArgRegs[len(intArgRegs)-1]
	argRegs := maskOf(intArgRegs...)
	for _, reg := range gpAlloc {
		if !argRegs.has(reg) {
			f.reserved = f.reserved.add(reg)
		}
	}
	target, spilled := f.preserveIndirectCallTarget(indirect, len(intArgRegs))
	if target != regNone || !spilled {
		t.Fatalf("preserved target = %v, spilled=%t, want memory fallback", target, spilled)
	}
	var want encoderamd64.Asm
	want.Store64(RBX, -int32(offSpillRegion), indirect)
	if string(a.B) != string(want.B) {
		t.Fatalf("target spill = %x, want %x", a.B, want.B)
	}
}
