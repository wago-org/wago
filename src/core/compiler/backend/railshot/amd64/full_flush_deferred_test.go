//go:build amd64

package amd64

import (
	"fmt"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestFullFlushStagesOverlappingDeferredInputs(t *testing.T) {
	for _, source := range []int{0, 1, 4} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("source%d/nested%v", source, nested), func(t *testing.T) {
				f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
				f.pushValue(storage{kind: stConst, typ: mtI64, cval: 53})
				if nested {
					f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
				}
				f.pushValue(storage{kind: stSlot, typ: mtI32, slot: uint32(source)})
				if nested {
					f.pushBinOp(opAdd, mtI32)
				}
				f.pushUnOp(opClz, mtI32)
				roots := f.rootsBottomToTop()
				staged := f.flushWideStack(roots, nil, false)
				if want := source == 0; staged != want {
					t.Fatalf("staged=%v want%v", staged, want)
				}
				if f.spillFloor != 0 {
					t.Fatalf("spill floor not restored: %d", f.spillFloor)
				}
				if staged {
					if top := f.s.back(); top.st.kind != stSlot || top.st.slotIndex() != 1 || top.st.typ != mtI32 {
						t.Fatal("staging did not preserve canonical result type/home")
					}
				} else if len(f.a.B) != 0 {
					t.Fatal("non-overlapping inputs emitted staging code")
				}
			})
		}
	}
}

func TestFullFlushStagesOverlappingRootSpills(t *testing.T) {
	for _, typ := range []machineType{mtI64, mtV128} {
		for _, source := range []int{0, 1, 4} {
			t.Run(fmt.Sprintf("type%d/source%d", typ, source), func(t *testing.T) {
				f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
				f.pushValue(storage{kind: stConst, typ: mtI64, cval: 53})
				f.pushValue(storage{kind: stSlot, typ: typ, slot: uint32(source)})
				staged := f.flushWideStack(f.rootsBottomToTop(), nil, false)
				if want := source == 0; staged != want {
					t.Fatalf("staged=%v want%v", staged, want)
				}
				if !staged && len(f.a.B) != 0 {
					t.Fatal("non-overlapping root emitted staging code")
				}
			})
		}
	}
}

func TestFullFlushSpillInspectionAtDepthLimit(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
	leaf := f.pushValue(storage{kind: stSlot, typ: mtI32, slot: 4})
	for i := 0; i < maxDeferDepth; i++ {
		f.pushUnOp(opClz, mtI32)
	}
	root := f.s.back()
	if got := int(deferDepthOf(root)); got != maxDeferDepth {
		t.Fatalf("depth=%d want%d", got, maxDeferDepth)
	}
	if f.flushSpillBefore(root, 1) {
		t.Fatal("non-overlapping deepest leaf was marked hazardous")
	}
	leaf.st.slot = 0
	if !f.flushSpillBefore(root, 1) {
		t.Fatal("overlapping deepest leaf was missed")
	}
	if got := testing.AllocsPerRun(100, func() { f.flushSpillBefore(root, 1) }); got != 0 {
		t.Fatalf("depth-limited inspection allocated %g times", got)
	}
}
