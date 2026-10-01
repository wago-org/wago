//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func callfreeSpilledPin(typ machineType) *fn {
	reg := Reg(12)
	if !typ.isXMM() {
		reg = R12
	}
	return &fn{a: &encoderamd64.Asm{}, s: newStack(), nLocals: 1, globalCellReg: regNone,
		localType: []machineType{typ}, localSlot: []uint32{0},
		locals:       []localDef{{typ: typ, reg: reg, isFloat: typ.isXMM(), state: lsMem}},
		pinnedLocals: []int{0}, pinRelinquished: true,
	}
}

func TestCallFreeRelinquishedPinEdges(t *testing.T) {
	for _, typ := range []machineType{mtI32, mtI64, mtF32, mtF64, mtV128} {
		for _, edge := range []string{"reconcile", "converge"} {
			t.Run(fmt.Sprintf("type=%d/%s", typ, edge), func(t *testing.T) {
				f := callfreeSpilledPin(typ)
				var target []locState
				if edge == "reconcile" {
					f.reconcileLocals()
				} else {
					f.convergeEdgeTo(&target)
				}
				if f.locals[0].state != lsReg || f.a.Len() == 0 {
					t.Fatalf("edge left spilled pin in state %v, emitted %d bytes", f.locals[0].state, f.a.Len())
				}
				if target != nil {
					t.Fatal("call-free edge allocated a merge snapshot")
				}
			})
		}
	}
}

func TestCallFreeRecoveredPinBecomesDirtyOnWrite(t *testing.T) {
	for _, typ := range []machineType{mtI32, mtI64, mtF32, mtF64, mtV128} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			f := callfreeSpilledPin(typ)
			f.recoverLocal(0)
			if f.locals[0].state != lsStackReg {
				t.Fatal("fixture did not recover the spilled local")
			}
			f.markLocalDirty(0)
			if f.locals[0].state != lsReg {
				t.Fatalf("rewritten local state = %v, want dirty", f.locals[0].state)
			}
			before := f.a.Len()
			var reg Reg
			if typ.isXMM() {
				reg = f.relinquishPinnedFLocal(0)
			} else {
				reg = f.relinquishPinnedLocal(0)
			}
			if reg != f.locals[0].reg || f.a.Len() == before {
				t.Fatal("second relinquishment did not store the rewritten local")
			}
		})
	}
}

func TestCallFreePinMergeInvalidatesCleanSlot(t *testing.T) {
	for _, state := range []locState{lsMem, lsStackReg, lsReg} {
		t.Run(fmt.Sprint(state), func(t *testing.T) {
			f := callfreeSpilledPin(mtI64)
			f.locals[0].state = state
			// Call-free edges agree only on the register. The last emitted arm's
			// slot may be stale on another incoming path, even if that arm reloaded.
			f.setLocalsState(nil)
			if f.locals[0].state != lsReg || f.a.Len() != 0 {
				t.Fatalf("merge state = %v, emitted %d bytes; want dirty metadata only", f.locals[0].state, f.a.Len())
			}
		})
	}
}

func TestCallFreePinEdgesDoNotStoreRegisterLocals(t *testing.T) {
	for _, state := range []locState{lsReg, lsStackReg} {
		t.Run(fmt.Sprint(state), func(t *testing.T) {
			f := callfreeSpilledPin(mtI64)
			f.locals[0].state = state
			var target []locState
			f.reconcileLocals()
			f.convergeEdgeTo(&target)
			if f.locals[0].state != lsReg || f.a.Len() != 0 || target != nil {
				t.Fatalf("edge state = %v, emitted %d bytes, snapshot %v; want dirty register without stores or snapshot", f.locals[0].state, f.a.Len(), target)
			}
		})
	}
}

func callfreeEdgePressure() *fn {
	f := &fn{
		a: &encoderamd64.Asm{}, s: newStack(),
		nLocals: len(pinnedFLocalRegs), nLocalSlots: len(pinnedFLocalRegs),
		fconsts: []floatConstReg{{typ: mtF64, reg: 0}, {typ: mtF64, reg: 1}},
		vconsts: []v128ConstReg{{reg: 2}, {reg: 3}},
	}
	for i, reg := range pinnedFLocalRegs {
		f.localType = append(f.localType, mtF64)
		f.localSlot = append(f.localSlot, uint32(8*i))
		f.locals = append(f.locals, localDef{typ: mtF64, reg: reg, isFloat: true, state: lsReg})
		f.pinnedLocals = append(f.pinnedLocals, i)
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(reg)
	}
	return f
}

func TestCallFreePinEdgesRestoreAfterMaterialization(t *testing.T) {
	for _, edge := range []string{"branch", "conditional", "table", "else", "end", "loop", "if"} {
		t.Run(edge, func(t *testing.T) {
			f := callfreeEdgePressure()
			fr := ctrlFrame{kind: cfBlock, resultN: 1, branchN: 1, res0: mtF64}
			if edge == "else" {
				fr.kind = cfIf
				fr.controlSite = f.a.JccPlaceholder(condE)
			}
			f.ctrl = []ctrlFrame{fr}
			// All XMM registers are reserved. Materializing this result/base value
			// must relinquish a pin after the edge's initial convergence.
			f.pushValue(storage{kind: stConst, typ: mtF64, cval: 0x4059000000000000}) // 100
			var err error
			switch edge {
			case "branch":
				f.branchToFrame(0)
			case "conditional":
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
				err = f.opBr(wasm.NewReader([]byte{0}), true)
			case "table":
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
				err = f.opBrTable(wasm.NewReader([]byte{0, 0}))
			case "else":
				err = f.opElse()
			case "end":
				err = f.opEnd()
			case "loop":
				err = f.opBlock(wasm.NewReader([]byte{0x40}), 0x03)
			case "if":
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
				err = f.opBlock(wasm.NewReader([]byte{0x40}), 0x04)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !f.pinRelinquished {
				t.Fatal("fixture did not relinquish a pin")
			}
			for _, x := range f.pinnedLocals {
				if f.locals[x].state != lsReg {
					t.Fatalf("local %d left in state %v at edge", x, f.locals[x].state)
				}
			}
			// Metadata reset alone is insufficient: the actual edge needs a load.
			last := len(f.locals) - 1
			load := &encoderamd64.Asm{}
			load.FLoadDisp(f.locals[last].reg, RSP, f.localAddr(last), true)
			if !bytes.Contains(f.a.B, load.B) {
				t.Fatal("edge did not reload the pressure-spilled pin")
			}
		})
	}
}

func TestCallFreePinRelinquishmentRetainsFrame(t *testing.T) {
	for _, relinquished := range []bool{false, true} {
		f := callfreeSpilledPin(mtI64)
		f.ft = &wasm.CompType{}
		f.moduleGlobalRegionalLease = regNone
		f.policy = currentCodegenPolicy()
		f.pinRelinquished = relinquished
		f.locals[0].state = lsReg
		if got := f.elideRegisterOnlyFrame(); got == relinquished {
			t.Fatalf("elide frame with pinRelinquished=%v: got %v, want %v", relinquished, got, !relinquished)
		}
	}
}

func TestCallFreePinConditionSurvivesRelinquishment(t *testing.T) {
	for _, edge := range []string{"if", "branch", "fused-branch", "table"} {
		t.Run(edge, func(t *testing.T) {
			f := callfreeSpilledPin(mtI32)
			f.locals[0].state = lsReg
			f.pinRelinquished = false
			f.pinnedLocalMask = maskOf(R12)
			for _, reg := range gpAlloc {
				if reg != R12 {
					f.pinned = f.pinned.add(reg)
				}
			}
			f.ctrl = []ctrlFrame{{kind: cfBlock}}
			condition := f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
			if edge == "fused-branch" {
				node := f.s.alloc()
				node.setElemKind(ekDeferred)
				node.setDeferredOp(opEqz)
				node.setValueType(mtI32)
				node.arg0 = condition
				f.s.pushDeferred(node)
			}
			var err error
			switch edge {
			case "if":
				err = f.opBlock(wasm.NewReader([]byte{0x40}), 0x04)
			case "table":
				err = f.opBrTable(wasm.NewReader([]byte{1, 0, 0}))
			default:
				err = f.opBr(wasm.NewReader([]byte{0}), true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !f.pinRelinquished || f.locals[0].state != lsReg {
				t.Fatal("condition did not borrow and restore the local register")
			}
			test, load := &encoderamd64.Asm{}, &encoderamd64.Asm{}
			load.Load32(R12, RSP, f.localAddr(0))
			loadAt := bytes.Index(f.a.B, load.B)
			if edge == "table" {
				move := &encoderamd64.Asm{}
				move.MovRegReg32(RDX, R12)
				test.AluRI(cmpDigit, RDX, 0, false)
				moveAt, testAt := bytes.Index(f.a.B, move.B), bytes.Index(f.a.B, test.B)
				if moveAt < 0 || loadAt < moveAt+len(move.B) || testAt < loadAt+len(load.B) {
					t.Fatalf("index move/restore/compare out of order: %d/%d/%d", moveAt, loadAt, testAt)
				}
			} else {
				test.TestSelf(R12, false)
				testAt := bytes.Index(f.a.B, test.B)
				if testAt < 0 || loadAt < testAt+len(test.B) {
					t.Fatalf("condition test at %d must precede local restore at %d", testAt, loadAt)
				}
			}
		})
	}
}
