//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Check header widths at the native emission boundary.
func TestIndexedMemoryReadsBackingHeaders(t *testing.T) {
	for _, size := range []bool{false, true} {
		sc := newScratch()
		f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: make([]wasm.MemType, 2)}, memSizeReg: regNone, memLimitReg: regNone}
		if size {
			r := wasm.ReaderFrom([]byte{1})
			if err := f.memorySize(&r); err != nil {
				t.Fatal(err)
			}
		} else {
			f.absoluteBulkAddr(1, X0, X1)
		}
		found := false
		for dst := Reg(0); dst < 31; dst++ {
			for base := Reg(0); base < 31; base++ {
				var want enc.Asm
				if size {
					want.Ldur32(dst, base, -int32(bdCurPages))
				} else {
					want.SubImm64(dst, base, uint32(bdCurBytes))
					want.Load64(dst, dst, 0)
				}
				found = found || bytes.Contains(f.a.B, want.B)
			}
		}
		if !found {
			t.Fatalf("indexed size=%t did not read its backing header: %x", size, f.a.B)
		}
	}
}

func TestIndexedGrowReloadsPrimaryBound(t *testing.T) {
	sc := newScratch()
	f := fn{a: sc.asm, sc: sc, s: sc.stack, m: &wasm.Module{Memories: make([]wasm.MemType, 2)}, memSizeReg: X27, memLimitReg: X25, memLimitExtent: 4, reserved: maskOf(X25, X27)}
	f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	r := wasm.ReaderFrom([]byte{1})
	if err := f.memoryGrow(&r); err != nil {
		t.Fatal(err)
	}
	var want enc.Asm
	want.SubImm64(X27, linMemReg, uint32(bdCurBytes))
	want.Load64(X27, X27, 0)
	want.SubImm64(X25, X27, 4)
	if !bytes.Contains(f.a.B, want.B) {
		t.Fatal("indexed grow did not reload the primary bound and adjusted limit")
	}
}
