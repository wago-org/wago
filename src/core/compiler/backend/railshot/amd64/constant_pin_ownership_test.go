//go:build amd64

package amd64

import (
	"encoding/binary"
	"testing"

	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func constantPinOwnershipFixture() *fn {
	f := &fn{a: &encoderamd64.Asm{}, s: newStack(), policy: currentCodegenPolicy()}
	for i, reg := range pinnedFLocalRegs {
		f.localType = append(f.localType, mtF64)
		f.localSlot = append(f.localSlot, uint32(8*i))
		f.locals = append(f.locals, localDef{typ: mtF64, reg: reg, isFloat: true})
		f.pinnedLocals = append(f.pinnedLocals, i)
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(reg)
	}
	return f
}

func TestVectorConstantReservationDoesNotRelinquishLocals(t *testing.T) {
	before := v128ConstCacheEnabled
	v128ConstCacheEnabled = true
	defer func() { v128ConstCacheEnabled = before }()
	f := constantPinOwnershipFixture()
	f.fconsts = []floatConstReg{{typ: mtF64, bits: 1, reg: 0}, {typ: mtF64, bits: 2, reg: 1}}
	var body []byte
	for i := uint64(1); i <= 4; i++ {
		body = append(body, 0xfd, 12)
		body = binary.LittleEndian.AppendUint64(body, i)
		body = binary.LittleEndian.AppendUint64(body, i+10)
		body = append(body, 0x1a)
	}
	f.preloadV128Consts(body)
	for _, c := range f.vconsts {
		if f.fpinnedLocalMask.has(c.reg) {
			t.Errorf("constant %#x aliases dedicated local register %v", c.lo, c.reg)
		}
	}
	if len(f.vconsts) != 2 || f.pinRelinquished {
		t.Errorf("cached %d constants, relinquished=%v; want only two free registers", len(f.vconsts), f.pinRelinquished)
	}
	for _, d := range f.locals {
		if d.state != lsReg {
			t.Fatalf("reservation changed a local's state to %v", d.state)
		}
	}
}

func TestFloatConstantReservationDoesNotRelinquishLocals(t *testing.T) {
	f := constantPinOwnershipFixture()
	// Vector cache entries occupy the only registers not dedicated to locals.
	for r := Reg(0); r < 4; r++ {
		f.vconsts = append(f.vconsts, v128ConstReg{lo: uint64(r) + 1, reg: r})
	}
	if r, ok := f.preloadFloatConst(storage{kind: stConst, typ: mtF64, cval: 1}); ok || r != regNone {
		t.Errorf("float constant reserved local register %v", r)
	}
	if len(f.fconsts) != 0 || f.pinRelinquished || f.a.Len() != 0 {
		t.Fatalf("reservation altered state: constants=%v, relinquished=%v, code=%d", f.fconsts, f.pinRelinquished, f.a.Len())
	}
}

func TestConstantReservationDoesNotSpillOperands(t *testing.T) {
	f := constantPinOwnershipFixture()
	for r := Reg(0); r < 4; r++ {
		f.pushFReg(r, mtF64)
	}
	if r, ok := f.preloadFloatConst(storage{kind: stConst, typ: mtF64, cval: 1}); ok || r != regNone {
		t.Errorf("float constant spilled a live operand into register %v", r)
	}
	if f.a.Len() != 0 || f.maxSpill != 0 {
		t.Fatalf("reservation emitted %d bytes and allocated %d spill slots", f.a.Len(), f.maxSpill)
	}
}
