//go:build arm64 && wago_regalloccheck

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestRegallocCheckDeferredCorruptInputs(t *testing.T) {
	for _, side := range []string{"left", "right"} {
		for _, op := range []wOp{opAdd, opMul, opEq} {
			t.Run(fmt.Sprintf("%s/op%d", side, op), func(t *testing.T) {
				f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
				left := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
				right := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
				f.pushBinOp(op, mtI64)
				root := f.s.back()
				f.checkBeginFlush([]*elem{root})
				defer f.a.ObserveRegalloc(nil)
				source, target := right, left
				if side == "right" {
					source, target = left, right
				}
				f.ld64(X0, SP, f.spillOff(source.st.slotIndex()))
				f.st64(SP, f.spillOff(target.st.slotIndex()), X0)
				requireAllocationFailure(t, "materialize input", func() { f.materialize(root) })
			})
		}
	}
}

func TestRegallocCheckTargetHintChecksTransfer(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack()}
	leaf := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
	other := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
	f.checkBeginFlush([]*elem{leaf, other})
	defer f.a.ObserveRegalloc(nil)
	// Deliberately redirect the emitted reload while leaving leaf metadata intact.
	observed := f.allocationCheck.state.Apply
	f.a.ObserveRegalloc(func(effect regalloccheck.Effect) {
		if effect.Kind == regalloccheck.Copy && effect.Src == regalloccheck.Slot(f.spillOff(3)) {
			effect.Src = regalloccheck.Slot(f.spillOff(4))
		}
		observed(effect)
	})
	requireAllocationFailure(t, "materialize transfer", func() { f.condenseInto(leaf, X0) })
}

func TestRegallocCheckABIObservesActualGPEmission(t *testing.T) {
	for _, kind := range []string{"correct", "no-op", "wrong-source", "wrong-bank"} {
		t.Run(kind, func(t *testing.T) {
			f := fn{a: &encoder.Asm{}}
			moves := []regMove{{dst: X0, src: X1}}
			finish := f.checkBeginRegMoves(moves, false)
			defer f.a.ObserveRegalloc(nil)
			resolveRegMovesWindow(moves, func(dst, src Reg) {
				switch kind {
				case "correct":
					f.a.MovReg64(dst, src)
				case "wrong-source":
					f.a.MovReg64(dst, X2)
				case "wrong-bank":
					f.a.FmovReg(dst, src, true)
				}
			}, func(Reg, Reg) {}, nil)
			if kind == "correct" {
				finish()
			} else {
				requireAllocationFailure(t, "parallel ABI move", finish)
			}
		})
	}
}

func TestRegallocCheckABIObservesActualFPSwapSlots(t *testing.T) {
	for _, kind := range []string{"correct", "no-op", "wrong-slot", "wrong-source"} {
		t.Run(kind, func(t *testing.T) {
			f := fn{a: &encoder.Asm{}}
			moves := []regMove{{dst: 0, src: 1}, {dst: 1, src: 0}}
			finish := f.checkBeginRegMoves(moves, true)
			defer f.a.ObserveRegalloc(nil)
			resolveRegMovesWindow(moves, func(dst, src Reg) { f.a.FmovReg(dst, src, true) }, func(x, y Reg) {
				if kind == "no-op" {
					return
				}
				save := x
				if kind == "wrong-source" {
					save = y
				}
				f.a.FStoreDisp(SP, 40, save, true)
				f.a.FmovReg(x, y, true)
				offset := int32(40)
				if kind == "wrong-slot" {
					offset += 8
				}
				f.a.FLoadDisp(y, SP, offset, true)
			}, nil)
			if kind == "correct" {
				finish()
			} else {
				requireAllocationFailure(t, "parallel ABI move", finish)
			}
		})
	}
}

func TestRegallocCheckABIObservesActualSwapChain(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		f := fn{a: &encoder.Asm{}}
		moves := []regMove{{dst: X0, src: X1}, {dst: X1, src: X2}, {dst: X2, src: X0}}
		finish := f.checkBeginRegMoves(moves, false)
		resolveRegMovesWindow(moves, f.a.MovReg64, func(x, y Reg) {
			f.a.MovReg64(X16, x)
			f.a.MovReg64(x, y)
			f.a.MovReg64(y, X16)
		}, func(a, b, c Reg) {
			f.a.MovReg64(X16, a)
			f.a.MovReg64(a, b)
			f.a.MovReg64(b, c)
			if !corrupt {
				f.a.MovReg64(c, X16)
			}
		})
		if corrupt {
			requireAllocationFailure(t, "parallel ABI move", finish)
		} else {
			finish()
		}
	}
}

// Corrupt the unconsumed right leaf only after the initial tree validation,
// during the left reload. The final arithmetic definition must not bless it.
func TestRegallocCheckDeferredInputClobberDuringMaterialization(t *testing.T) {
	for _, op := range []wOp{opAdd, opMul, opEq} {
		t.Run(fmt.Sprint(op), func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
			f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
			f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
			f.pushBinOp(op, mtI64)
			root := f.s.back()
			f.checkBeginFlush([]*elem{root})
			defer f.a.ObserveRegalloc(nil)
			observed := f.allocationCheck.state.Apply
			f.a.ObserveRegalloc(func(effect regalloccheck.Effect) {
				observed(effect)
				if effect.Kind == regalloccheck.Copy && effect.Src == regalloccheck.Slot(f.spillOff(3)) {
					observed(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Slot(f.spillOff(4)), Size: 8})
				}
			})
			requireAllocationFailure(t, "materialize input", func() { f.materialize(root) })
		})
	}
}

func TestRegallocCheckAcceptsDeferredFlushInputs(t *testing.T) {
	for _, op := range []wOp{opAdd, opMul, opEq} {
		f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
		f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
		f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 4})
		f.pushBinOp(op, mtI64)
		f.flush()
	}
}

func TestRegallocCheckABIRestoresObserverAfterPanic(t *testing.T) {
	f := fn{a: &encoder.Asm{}}
	observed := 0
	f.a.ObserveRegalloc(func(regalloccheck.Effect) { observed++ })
	func() {
		defer func() {
			if got := recover(); got != "encoder failure" {
				t.Fatalf("panic=%v", got)
			}
		}()
		finish := f.checkBeginRegMoves([]regMove{{dst: 0, src: 1}}, false)
		defer finish()
		panic("encoder failure")
	}()
	f.a.MovReg64(X0, X0)
	if observed != 1 {
		t.Fatalf("enclosing observer was not restored: effects=%d", observed)
	}
}
