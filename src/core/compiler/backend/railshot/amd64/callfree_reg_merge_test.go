//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestCallFreeRegionalMergeRestoresOnlyMissingPins(t *testing.T) {
	saved := callFreeRegMergesEnabled
	callFreeRegMergesEnabled = true
	defer func() { callFreeRegMergesEnabled = saved }()
	f := &fn{
		a: &encoder.Asm{}, s: newStack(), usesCalls: true, intervalControl: true,
		localType: []machineType{mtI32, mtI64, mtF64}, localSlot: []uint32{0, 1, 2},
		locals:       []localDef{{typ: mtI32, reg: R12, state: lsReg}, {typ: mtI64, reg: R13, state: lsMem}, {typ: mtF64, reg: 12, isFloat: true, state: lsMem}},
		pinnedLocals: []int{0, 1, 2},
	}
	var target []locState
	f.convergeEdgeTo(&target)
	want := &encoder.Asm{}
	want.Load64(R13, RSP, f.localAddr(1))
	want.FLoadDisp(12, RSP, f.localAddr(2), true)
	if !bytes.Equal(f.a.B, want.B) {
		t.Fatalf("first edge = %x, want only missing-pin reload %x", f.a.B, want.B)
	}
	if len(target) != 3 || target[0] != lsReg || target[1] != lsReg || target[2] != lsReg {
		t.Fatalf("merge contract = %v, want three register homes", target)
	}
	// A later edge must restore a different reclaimed pin without storing the
	// dirty register that already satisfies the contract.
	f.a.B = nil
	f.locals[0].state = lsMem
	f.convergeEdgeTo(&target)
	want.B = nil
	want.Load32(R12, RSP, f.localAddr(0))
	if !bytes.Equal(f.a.B, want.B) || f.locals[0].state != lsReg || f.locals[1].state != lsReg {
		t.Fatalf("later edge = %x, want %x; locals=%+v", f.a.B, want.B, f.locals)
	}
	f.a.B = nil
	f.reconcileLocals()
	if f.a.Len() != 0 {
		t.Fatalf("resident loop-entry pins emitted %x", f.a.B)
	}
	f.hasCalls = true
	if f.callFreeRegMerges() {
		t.Fatal("call-making function admitted")
	}
	f.hasCalls, f.intervalControl = false, false
	if f.callFreeRegMerges() {
		t.Fatal("non-regional function admitted")
	}
	f.intervalControl, callFreeRegMergesEnabled = true, false
	if f.callFreeRegMerges() {
		t.Fatal("disabled policy admitted")
	}
}
