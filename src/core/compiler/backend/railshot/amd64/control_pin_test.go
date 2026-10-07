//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestBranchPredicateSurvivesPinRestoration(t *testing.T) {
	f := &fn{
		a: &encoder.Asm{}, s: newStack(), usesCalls: true, intervalControl: true,
		pinRelinquished: true, nLocals: 1, nLocalSlots: 1,
		localType: []machineType{mtI32}, localSlot: []uint32{0},
		locals:       []localDef{{typ: mtI32, reg: R12, state: lsMem}},
		pinnedLocals: []int{0}, pinnedLocalMask: maskOf(R12),
		ctrl: []ctrlFrame{{kind: cfLoop, controlSite: 0}},
	}
	f.setFrameBranchState(&f.ctrl[0], []locState{lsReg})
	f.pushReg(R12, mtI32) // predicate temporarily owns the relinquished local pin
	r := wasm.ReaderFrom([]byte{0})
	if err := f.opBr(&r, true); err != nil {
		t.Fatal(err)
	}
	bad := &encoder.Asm{}
	bad.TestSelf(R12, false)
	if bytes.Contains(f.a.B, bad.B) {
		t.Fatalf("branch tests restored local instead of its predicate: %x", f.a.B)
	}
}

func TestBranchEmissionRestoresPinsAfterDeferredEvaluation(t *testing.T) {
	for _, conditional := range []bool{false, true} {
		stats := new(CodegenStats)
		f := &fn{
			a: &encoder.Asm{}, s: newStackWithCap(16), stats: stats,
			usesCalls: true, intervalControl: true, nLocals: 1, nLocalSlots: 1,
			localType: []machineType{mtI64}, localSlot: []uint32{0},
			locals:       []localDef{{typ: mtI64, reg: R12, state: lsReg}},
			pinnedLocals: []int{0}, pinnedLocalMask: maskOf(R12),
			ctrl: []ctrlFrame{{kind: cfLoop, controlSite: 0}},
		}
		f.setFrameBranchState(&f.ctrl[0], []locState{lsReg})
		rhsRelocateFixture(f)
		f.pinned = rhsRelocateFixturePins.remove(R12).add(R8)
		if conditional {
			f.pushValue(storage{kind: stConst, typ: mtI64})
			f.pushBinOp(opNe, mtI64)
			r := wasm.ReaderFrom(nil)
			if err := f.brIfFused(&r, f.s.back(), 0); err != nil {
				t.Fatal(err)
			}
		} else {
			f.branchToFrame(0)
		}
		if diagnosticsEnabled {
			if stats.PinRelinquishments == 0 {
				t.Fatalf("conditional=%t: fixture did not reclaim a pin", conditional)
			}
		}
		if f.locals[0].state != lsReg {
			t.Errorf("conditional=%t: branch leaves local in state %v, want register home", conditional, f.locals[0].state)
		}
	}
}

func TestControlEdgesRestorePinsAfterDeferredEvaluation(t *testing.T) {
	for _, edge := range []string{"loop", "if", "if-fused", "else", "end", "table"} {
		t.Run(edge, func(t *testing.T) {
			stats := new(CodegenStats)
			f := &fn{
				a: &encoder.Asm{}, s: newStackWithCap(16), stats: stats,
				usesCalls: true, intervalControl: true, nLocals: 1, nLocalSlots: 1,
				localType: []machineType{mtI64}, localSlot: []uint32{0},
				locals:       []localDef{{typ: mtI64, reg: R12, state: lsReg}},
				pinnedLocals: []int{0}, pinnedLocalMask: maskOf(R12),
				ctrl: []ctrlFrame{{kind: cfFunc}},
			}
			rhsRelocateFixture(f)
			f.pinned = rhsRelocateFixturePins.remove(R12).add(R8)
			r := wasm.ReaderFrom([]byte{0x40})
			var err error
			switch edge {
			case "loop":
				err = f.opBlock(&r, 0x03)
			case "if", "if-fused":
				if edge == "if-fused" {
					f.pushValue(storage{kind: stConst, typ: mtI64})
					f.pushBinOp(opNe, mtI64)
				} else {
					for e := f.s.head.next; e != f.s.head; e = e.next {
						e.setValueType(mtI32)
					}
				}
				err = f.opBlock(&r, 0x04)
			case "else", "end":
				fr := ctrlFrame{kind: cfBlock, resultN: 1, branchN: 1, res0: mtI64}
				if edge == "else" {
					fr.kind = cfIf
					fr.controlSite = f.a.JccPlaceholder(condE)
				}
				f.ctrl = append(f.ctrl, fr)
				f.setFrameEntryState(&f.ctrl[1], []locState{lsReg})
				f.setFrameBranchState(&f.ctrl[1], []locState{lsReg})
				if edge == "else" {
					err = f.opElse()
				} else {
					err = f.opEnd()
				}
			case "table":
				f.ctrl[0].kind = cfLoop
				f.ctrl = append(f.ctrl, ctrlFrame{kind: cfLoop})
				f.setFrameBranchState(&f.ctrl[0], []locState{lsReg})
				f.setFrameBranchState(&f.ctrl[1], []locState{lsReg})
				for e := f.s.head.next; e != f.s.head; e = e.next {
					e.setValueType(mtI32)
				}
				r = wasm.ReaderFrom([]byte{1, 0, 1})
				err = f.opBrTable(&r)
			}
			if err != nil {
				t.Fatal(err)
			}
			if diagnosticsEnabled {
				if stats.PinRelinquishments == 0 {
					t.Fatal("fixture did not reclaim a pin")
				}
			}
			if edge == "if" {
				state := f.frameEntryState(&f.ctrl[len(f.ctrl)-1])
				if len(state) == 1 && state[0] == lsMem && f.locals[0].state == lsMem {
					// A freshly established lazy merge may keep the pin in its
					// frame home, provided both paths use that actual location.
					return
				}
			}
			restore := &encoder.Asm{}
			restore.Load64(R12, RSP, f.localAddr(0))
			if !bytes.Contains(f.a.B, restore.B) {
				t.Fatalf("edge omits reclaimed-pin reload: %x", f.a.B)
			}
			if edge == "table" {
				dispatch := bytes.Index(f.a.B, []byte{0x0f, 0x85})
				if dispatch < 0 || bytes.Index(f.a.B, restore.B) > dispatch {
					t.Fatalf("table must restore pins before dispatch to either target: %x", f.a.B)
				}
			}
		})
	}
}
