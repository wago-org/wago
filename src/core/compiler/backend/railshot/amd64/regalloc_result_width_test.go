//go:build amd64 && wago_regalloccheck

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Swift's failing function combines spilled i64 comparison results with i32.and.
// The input width stays i64 until condensation, but the result identity is i32
// from the start of the checked window.
func TestRegallocCheckFoldedI64ComparisonResult(t *testing.T) {
	for _, op := range []wOp{opEq, opNe, opLtS, opLtU, opGtS, opGtU, opLeS, opLeU, opGeS, opGeU, opEqz} {
		for _, kind := range []string{"correct", "wrong-slot", "missing-read"} {
			t.Run(fmt.Sprintf("op%d/%s", op, kind), func(t *testing.T) {
				f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
				f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
				if op == opEqz {
					f.pushUnOp(op, mtI64)
				} else {
					f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
					f.pushBinOp(op, mtI64)
				}
				root := f.s.back()
				f.checkBeginFlush([]*elem{root})
				defer f.a.ObserveRegalloc(nil)
				f.materialize(root)
				f.spill(root)
				observed := f.allocationCheck.observe
				reads := 0
				f.a.ObserveRegalloc(func(effect regalloccheck.Effect) {
					if effect.Kind == regalloccheck.Read {
						reads++
						if effect.Size != 4 {
							t.Fatalf("folded boolean read = %d bytes, want 4", effect.Size)
						}
						switch kind {
						case "wrong-slot":
							effect.Src = regalloccheck.Slot(f.spillOff(root.st.slotIndex() + 1))
						case "missing-read":
							return
						}
					}
					observed(effect)
				})
				consume := func() { f.applyALU(aluTable[opAnd], R8, root, false) }
				switch kind {
				case "correct":
					consume()
					f.checkEndFlush()
				case "wrong-slot":
					requireAllocationFailure(t, "folded machine input", consume)
				case "missing-read":
					consume()
					requireAllocationFailure(t, "missing folded", func() { f.checkOccupy(root, R8, false) })
				}
				if reads != 1 {
					t.Fatalf("folded reads = %d, want 1", reads)
				}
			})
		}
	}
}

func TestRegallocCheckRejectsShortFoldedI64Value(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	leaf := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
	f.checkBeginFlush([]*elem{leaf})
	defer f.a.ObserveRegalloc(nil)
	f.checkFoldedUse(leaf)
	requireAllocationFailure(t, "short folded input", func() {
		f.a.AluRM(cmpRMcode, R8, RSP, f.spillOff(3), false)
	})
}

// A canonical frame carrier can be wider than an i32 consumer. Late operand
// selection must keep its full-width materialization instead of folding a
// narrow read of a value whose transfer contract still covers eight bytes.
func TestRegallocCheckLateFrameCarrierWidth(t *testing.T) {
	saved := lateFrameCommuteEnabled
	defer func() { lateFrameCommuteEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		for _, typ := range []machineType{mtI32, mtI64} {
			t.Run(fmt.Sprintf("enabled%v/type%d", enabled, typ), func(t *testing.T) {
				lateFrameCommuteEnabled = enabled
				f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
				f.pushValue(storage{kind: stSlot, typ: typ, slot: 3})
				f.pushValue(storage{kind: stSlot, typ: mtI32, slot: 4})
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
				f.pushBinOp(opAdd, mtI32)
				f.pushBinOp(opXor, mtI32)
				root := f.s.back()
				f.checkBeginFlush([]*elem{root})
				defer f.a.ObserveRegalloc(nil)
				r := f.materialize(root)
				f.a.Store64(RSP, f.spillOff(0), r)
				f.checkEndFlush()
			})
		}
	}
}

func TestRegallocCheckShiftedI64ComparisonResult(t *testing.T) {
	for _, op := range []wOp{opEq, opLtU, opEqz} {
		for _, corrupt := range []int{-1, 3, 4} {
			if op == opEqz && corrupt == 4 {
				continue
			}
			t.Run(fmt.Sprintf("op%d/corrupt%d", op, corrupt), func(t *testing.T) {
				f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
				f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
				if op == opEqz {
					f.pushUnOp(op, mtI64)
				} else {
					f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
					f.pushBinOp(op, mtI64)
				}
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 2})
				f.pushBinOp(opShl, mtI32)
				root := f.s.back()
				f.checkBeginFlush([]*elem{root})
				defer f.a.ObserveRegalloc(nil)
				if corrupt >= 0 {
					// Only corrupt the input's upper half: a four-byte result must
					// not weaken either original eight-byte comparand obligation.
					f.a.Store32(RSP, f.spillOff(corrupt)+4, R8)
					requireAllocationFailure(t, "materialize input", func() { f.materialize(root) })
					return
				}
				observed := f.allocationCheck.observe
				narrowMoves := 0
				f.a.ObserveRegalloc(func(effect regalloccheck.Effect) {
					if effect.Kind == regalloccheck.Copy && effect.Src.Bank == regalloccheck.GP &&
						effect.Dst.Bank == regalloccheck.GP && effect.Size == 4 {
						narrowMoves++
					}
					observed(effect)
				})
				f.materialize(root)
				f.spill(root)
				f.checkEndFlush()
				wantMoves := 1
				if shiftOwnedDestinationEnabled {
					wantMoves = 0 // the comparison's owned result is already i32
				}
				if narrowMoves != wantMoves {
					t.Fatalf("target-hint 32-bit moves = %d, want %d", narrowMoves, wantMoves)
				}
			})
		}
	}
}

func TestRegallocCheckSeedsSemanticResultWidths(t *testing.T) {
	cases := []struct {
		op         wOp
		input, typ machineType
		want       int
	}{
		{opEq, mtI64, mtI64, 4}, {opNe, mtI64, mtI64, 4},
		{opLtS, mtI64, mtI64, 4}, {opLtU, mtI64, mtI64, 4},
		{opGtS, mtI64, mtI64, 4}, {opGtU, mtI64, mtI64, 4},
		{opLeS, mtI64, mtI64, 4}, {opLeU, mtI64, mtI64, 4},
		{opGeS, mtI64, mtI64, 4}, {opGeU, mtI64, mtI64, 4},
		{opEqz, mtI64, mtI64, 4},
		{opEq, mtI32, mtI32, 4}, {opEqz, mtI32, mtI32, 4},
		{opWrap, mtI64, mtI32, 4},
		{opSExt32, mtI32, mtI64, 8}, {opZExt32, mtI32, mtI64, 8},
		{opSExt8, mtI32, mtI32, 4}, {opSExt16, mtI32, mtI32, 4},
		{opSExt8, mtI64, mtI64, 8}, {opSExt16, mtI64, mtI64, 8},
		{opSExt32, mtI64, mtI64, 8},
		{opClz, mtI64, mtI64, 8}, {opAdd, mtI64, mtI64, 8},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("op%d/input%d/type%d", tc.op, tc.input, tc.typ), func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
			input := f.pushValue(storage{kind: stSlot, typ: tc.input, slot: 3})
			if isCompare(tc.op) || isBinALU(tc.op) {
				f.pushValue(storage{kind: stSlot, typ: tc.input, slot: 4})
				f.pushBinOp(tc.op, tc.typ)
			} else {
				f.pushUnOp(tc.op, tc.typ)
			}
			root := f.s.back()
			f.checkBeginFlush([]*elem{root})
			defer f.a.ObserveRegalloc(nil)
			if got := len(f.allocationCheck.values[root]); got != tc.want {
				t.Errorf("result identity = %d bytes, want %d", got, tc.want)
			}
			if got, want := len(f.allocationCheck.values[input]), checkSize(tc.input); got != want {
				t.Errorf("input identity = %d bytes, want %d", got, want)
			}
		})
	}
}
