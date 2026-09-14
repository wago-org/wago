//go:build arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMemoryGrowFitsTransientRegisterFloor(t *testing.T) {
	for _, memory64 := range []bool{false, true} {
		for _, memoryIndex := range []byte{0, 1} {
			t.Run(fmt.Sprintf("memory64=%t/index=%d", memory64, memoryIndex), func(t *testing.T) {
				sc := newScratch()
				sc.asm.B = make([]byte, 0, 4096)
				m := &wasm.Module{Memories: make([]wasm.MemType, 2)}
				m.Memories[memoryIndex].Limits.Addr64 = memory64
				typ := mtI32
				if memory64 {
					typ = mtI64
				}
				var pinned regMask
				available := maskOf(X0, X1, X2, X3)
				for _, reg := range gpAlloc {
					if !available.has(reg) {
						pinned = pinned.add(reg)
					}
				}
				emit := func() {
					sc.reset()
					f := fn{a: sc.asm, sc: sc, s: sc.stack, m: m, memSizeReg: regNone, pinnedLocalMask: pinned}
					f.s.pushValue(storage{kind: stConst, typ: typ, cval: 1})
					r := wasm.ReaderFrom([]byte{memoryIndex})
					if err := f.memoryGrow(&r); err != nil {
						t.Fatal(err)
					}
					result := f.s.back()
					if f.depth() != 1 || result.st.typ != typ || result.st.kind != stReg || !available.has(result.st.reg) {
						t.Fatalf("invalid memory.grow result: %+v", result.st)
					}
					if f.pinned != 0 || f.pinnedLocalMask != pinned {
						t.Fatal("memory.grow changed register reservations")
					}
				}
				emit()
				if got := testing.AllocsPerRun(20, emit); got != 0 {
					t.Fatalf("memory.grow lowering allocated %g times, want 0", got)
				}
			})
		}
	}
}
