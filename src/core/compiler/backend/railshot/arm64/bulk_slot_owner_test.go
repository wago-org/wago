//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestBulkPhysicalArgumentSlots(t *testing.T) {
	for _, tc := range []struct {
		name      string
		emit      func(*fn, *wasm.Reader) error
		immediate []byte
		valueReg  Reg
		external  bool
	}{
		{"memory.init", (*fn).memoryInit, []byte{0, 0}, X10, false},
		{"memory.copy", (*fn).memoryCopy, []byte{0, 0}, X10, false},
		{"memory.fill", (*fn).memoryFill, []byte{0}, X14, false},
		{"table.init", (*fn).tableInit, []byte{0, 0}, X10, false},
		{"table.copy", (*fn).tableCopy, []byte{0, 0}, X10, false},
		{"table.fill", (*fn).tableFill, []byte{0}, X12, false},
		{"externref.fill", (*fn).tableFill, []byte{0}, X12, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := wasm.AbsRef(wasm.HeapFunc)
			if tc.external {
				ref = wasm.AbsRef(wasm.HeapExtern)
			}
			sc := newScratch()
			f := fn{a: sc.asm, sc: sc, s: sc.stack, syncHostSlots: maxSyncHostSlots, memSizeReg: regNone, m: &wasm.Module{
				Memories: []wasm.MemType{{}}, Tables: []wasm.Table{{Type: wasm.TableType{Ref: ref}}},
			}}
			middleType := mtI32
			if tc.valueReg == X12 {
				middleType = mtI64
			}
			for _, st := range []storage{
				{kind: stSlot, typ: mtV128, slot: 0}, {kind: stSlot, typ: mtV128, slot: 2},
				{kind: stSlot, typ: mtI32, slot: 4}, {kind: stSlot, typ: mtI32, slot: 5},
				{kind: stSlot, typ: middleType, slot: 6}, {kind: stSlot, typ: mtI32, slot: 7},
			} {
				f.s.pushValue(st)
			}
			want := f
			want.a = &a64.Asm{}
			if tc.external {
				// table.fill's GC helper takes (i32 dst, externref value,
				// i32 count, i32 table). Two v128 values below the operands
				// occupy four physical slots; helper arguments start at slot 5.
				want.a.MovImm32(X9, 0)
				want.st64(SP, f.spillOff(8), X9)
				want.ld64(X11, linMemReg, -int32(offCustomCtx))
				want.ld32(X9, SP, f.spillOff(5))
				want.st64(X11, hcArgs, X9)
				want.ld64(X9, SP, f.spillOff(6))
				want.st64(X11, hcArgs+8, X9)
				want.ld32(X9, SP, f.spillOff(7))
				want.st64(X11, hcArgs+16, X9)
				want.ld32(X9, SP, f.spillOff(8))
				want.st64(X11, hcArgs+24, X9)
				want.a.MovImm64(X16, uint64(gcStructDispatchBit|55))
				want.st32(X11, hcImportIdx, X16)
				want.a.MovImm64(X16, 4) // four parameter slots, no results
				want.st32(X11, hcNArgs, X16)
			} else {
				want.ld64(X9, SP, f.spillOff(5))
				want.ld64(tc.valueReg, SP, f.spillOff(6))
				want.ld64(X11, SP, f.spillOff(7))
			}
			if err := tc.emit(&f, wasm.NewReader(tc.immediate)); err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(f.a.B, want.a.B) {
				t.Fatalf("argument loads = %x; want prefix %x", f.a.B[:min(len(f.a.B), len(want.a.B))], want.a.B)
			}
			if f.depth() != 3 || f.s.back().st.slotIndex() != 4 {
				t.Fatalf("remaining depth/last slot = %d/%d; want 3/4", f.depth(), f.s.back().st.slotIndex())
			}
		})
	}
}
