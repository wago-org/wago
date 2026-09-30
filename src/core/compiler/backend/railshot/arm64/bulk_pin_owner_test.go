//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBulkBoundsCheckPreservesCallerPins(t *testing.T) {
	for _, alreadyPinned := range []bool{false, true} {
		name := "temporary"
		if alreadyPinned {
			name = "caller-owned"
		}
		t.Run(name, func(t *testing.T) {
			sc := newScratch()
			sc.asm.B = make([]byte, 0, 1024)
			f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: []wasm.MemType{{}}}, memSizeReg: regNone}
			f.pinned = maskOf(X10)
			if alreadyPinned {
				f.pinned = f.pinned.add(X9)
			}
			want := f.pinned
			f.bulkBoundsCheck(X9, 16, 0)
			if f.pinned != want {
				t.Fatalf("pins = %#x, want caller pins %#x", f.pinned, want)
			}
		})
	}
}
