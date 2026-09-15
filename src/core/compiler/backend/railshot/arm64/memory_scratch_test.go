//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMemoryCopyPatchScratchDoesNotAllocate(t *testing.T) {
	sc := newScratch()
	sc.asm.B = make([]byte, 0, 8192)
	f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: []wasm.MemType{{}}}, memSizeReg: regNone}
	emit := func() {
		sc.reset()
		for i := uint32(0); i < 3; i++ {
			f.s.pushValue(storage{kind: stSlot, typ: mtI32, slot: i})
		}
		r := wasm.ReaderFrom([]byte{0, 0})
		if err := f.memoryCopy(&r); err != nil {
			t.Fatal(err)
		}
	}
	emit() // Warm encoder, operand, and trap-site backing before measuring.
	if got := testing.AllocsPerRun(20, emit); got != 0 {
		t.Fatalf("reused dynamic memory.copy lowering = %g allocations, want 0", got)
	}
}
