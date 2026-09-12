//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestEHCatchSnapshotPreservesStatesAndPoolOwnership(t *testing.T) {
	// Public EH compilation currently disables pins. Exercise the emitter's
	// snapshot directly so it cannot silently become a limit if that policy changes.
	for _, n := range []int{8, 16, 17, 19} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := fn{a: &x86.Asm{}, s: newStack(), usesCalls: true, nLocals: n,
				locals: make([]localDef, n), pinnedLocals: make([]int, n),
				localType: make([]machineType, n), localSlot: make([]uint32, n),
				ctrl: []ctrlFrame{{kind: cfBlock}},
			}
			active := f.newLocStateBuf()
			for i := range f.locals {
				f.pinnedLocals[i] = i
				f.locals[i] = localDef{reg: R12, state: locState(i % 4)}
				f.localType[i], f.localSlot[i], active[i] = mtI64, uint32(i), lsMem
			}
			f.setFrameBranchState(&f.ctrl[0], active)
			for pass := 0; pass < 3; pass++ {
				f.emitEHCatchRoute(&f.ctrl[0], &ehCatchClause{}, 0)
				for i := range f.locals {
					if got, want := f.locals[i].state, locState(i%4); got != want {
						t.Fatalf("local %d state = %d, want %d", i, got, want)
					}
					if active[i] != lsMem {
						t.Fatalf("active frame snapshot changed at %d", i)
					}
				}
				wantPool := 0
				if n > 16 {
					wantPool = 1
				}
				if len(f.lsPool) != wantPool {
					t.Fatalf("pool buffers = %d, want %d", len(f.lsPool), wantPool)
				}
				if wantPool != 0 && &f.lsPool[0][0] == &active[0] {
					t.Fatal("returned buffer aliases the active frame snapshot")
				}
			}
		})
	}
}
