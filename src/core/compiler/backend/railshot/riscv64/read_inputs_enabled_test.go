//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func expectReadClobber(t *testing.T, bank string, emit func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "read-only "+bank+" input overwritten") {
			t.Fatalf("want %s input clobber, got %v", bank, r)
		}
	}()
	emit()
}

func TestReadGuardRejectsPreparationClobbers(t *testing.T) {
	for name, emit := range map[string]func(*target){
		"large store value":      func(f *target) { f.Store(4096, uint8(enc.Scratch1), true) },
		"large address base":     func(f *target) { f.addressAt(enc.Scratch1, 4096, enc.Scratch1) },
		"signed compare right":   func(f *target) { f.compareOperands(shared.IntLtS, false, enc.X5, enc.Scratch0) },
		"unsigned compare right": func(f *target) { f.compareOperands(shared.IntLtU, false, enc.X5, enc.Scratch0) },
	} {
		t.Run(name, func(t *testing.T) {
			f := &target{}
			t.Cleanup(f.checkEndLifetimes)
			expectReadClobber(t, "GP", func() { emit(f) })
			if f.a.Len() != 0 {
				t.Fatal("clobber emitted before detection")
			}
			if f.readCheck != (regallocReadMask{}) {
				t.Fatal("panic leaked a read guard")
			}
		})
	}
	for _, wide := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepared FP/%v", wide), func(t *testing.T) {
			f := &integerEmitter{}
			t.Cleanup(f.checkEndLifetimes)
			// Inject a write while preparing F1, before its actual encoding. F0
			// must remain live through this real second-input path.
			f.a.ObserveFPWrites(func(mask uint32) {
				if mask == 1<<enc.F1 {
					f.a.FmvDX(enc.F0, enc.X7)
				}
			})
			f.floatInput(enc.F0, enc.X5, wide)
			before := f.a.Len()
			expectReadClobber(t, "FP", func() { f.floatSecondInput(wide) })
			if f.a.Len() != before || f.readCheck != (regallocReadMask{}) {
				t.Fatal("FP guard did not stop and unwind the write")
			}
		})
	}
}

func TestReadGuardsNestedBanksCallsAndCleanup(t *testing.T) {
	f := &target{}
	gp, fp := 0, 0
	f.a.ObserveGPWrites(func(uint32) { gp++ })
	f.a.ObserveFPWrites(func(uint32) { fp++ })
	saved := f.checkProtectRead(uint8(enc.X5), false)
	f.a.FmvDX(enc.F5, enc.X5) // The same number in the other bank is allowed.
	inner := f.checkProtectRead(uint8(enc.F0), true)
	expectReadClobber(t, "FP", func() { f.a.FmvWX(enc.F0, enc.X6) })
	expectReadClobber(t, "GP", func() { f.a.Add(enc.X5, enc.X6, enc.X7) })
	f.checkRestoreRead(inner)
	f.a.FmvWX(enc.F0, enc.X6)
	expectReadClobber(t, "GP", func() { f.a.Jal(enc.RA, 4) })
	f.checkRestoreRead(saved)
	f.a.Add(enc.X5, enc.X5, enc.X6) // Consuming output aliases are legal after restore.
	f.checkProtectRead(uint8(enc.F0), true)
	expectReadClobber(t, "FP", func() { f.a.Jalr(enc.X5, enc.X6, 0) })
	f.checkEndLifetimes()
	f.checkEndLifetimes()
	beforeGP, beforeFP := gp, fp
	f.a.Add(enc.X5, enc.X6, enc.X7)
	f.a.FmvDX(enc.F0, enc.X5)
	if gp != beforeGP+1 || fp != beforeFP+1 || f.gpObserverActive || f.fpObserverActive || f.readCheck != (regallocReadMask{}) {
		t.Fatal("enclosing observers or cleanup state lost")
	}
}

func TestReadGuardsPermitActualPreparation(t *testing.T) {
	f := &target{}
	defer f.checkEndLifetimes()
	f.Store(4096, uint8(enc.X5), true)
	f.addressAt(enc.X5, 4096, enc.Scratch1)
	for _, op := range []shared.IntOp{shared.IntLtS, shared.IntLtU} {
		f.compareOperands(op, false, enc.X5, enc.X6)
		f.Binary(op, false, uint8(enc.X5), uint8(enc.X5), shared.ScalarOperand{Kind: shared.ScalarRegister, Reg: uint8(enc.X6)})
	}
	g := &integerEmitter{}
	defer g.checkEndLifetimes()
	for _, wide := range []bool{false, true} {
		g.floatInput(enc.F0, enc.X5, wide)
		g.floatSecondInput(wide)
		g.a.FaddD(enc.F0, enc.F0, enc.F1, enc.RNE)
	}
	f.checkProtectRead(uint8(enc.Zero), false)
	f.a.Addi(enc.Zero, enc.Zero, 1)
	if f.a.Err() != nil || g.a.Err() != nil || f.readCheck != (regallocReadMask{}) || g.readCheck != (regallocReadMask{}) {
		t.Fatal("valid preparation rejected")
	}
}
