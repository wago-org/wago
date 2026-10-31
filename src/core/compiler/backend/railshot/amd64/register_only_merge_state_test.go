//go:build amd64

package amd64

import (
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestRegisterOnlyMergeStateIsReadOnlyAndNotPooled(t *testing.T) {
	old := callFreeRegMergesEnabled
	callFreeRegMergesEnabled = true
	t.Cleanup(func() { callFreeRegMergesEnabled = old })
	for _, n := range []int{1, 7, 64, 65} {
		f := &fn{a: &encoder.Asm{}, s: newStack(), usesCalls: true, intervalControl: true,
			locals: make([]localDef, n), localType: make([]machineType, n), localSlot: make([]uint32, n), pinnedLocals: make([]int, n)}
		for i := range n {
			f.pinnedLocals[i] = i
			f.locals[i] = localDef{typ: mtI32, reg: R12, state: lsReg}
			f.localType[i] = mtI32
		}
		var state []locState
		f.convergeEdgeTo(&state)
		if len(state) != n {
			t.Fatalf("states=%d want=%d", len(state), n)
		}
		for _, s := range state {
			if s != lsReg {
				t.Fatal("non-register state")
			}
		}
		shared := &state[0] == &registerOnlyMergeStates[0]
		if shared != (n <= len(registerOnlyMergeStates)) {
			t.Fatalf("n=%d shared=%v", n, shared)
		}
		f.freeLocStateBuf(state)
		if shared && (len(f.lsPool) != 0 || f.lsPoolBytes != 0) {
			t.Fatal("read-only state entered mutable pool")
		}
		mutable := f.newLocStateBuf()
		mutable[0] = lsMem
		if registerOnlyMergeStates[0] != lsReg {
			t.Fatal("mutable buffer aliases shared contract")
		}
		// A later general edge may read the register-only contract, but cannot
		// rewrite it even when the source local has moved to memory.
		if shared {
			f.makesCalls = true
			f.locals[0].state = lsMem
			f.convergeEdgeTo(&state)
			if state[0] != lsReg || f.locals[0].state != lsReg {
				t.Fatal("later edge broke register contract")
			}
		}
	}
}
