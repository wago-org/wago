//go:build wago_regalloccheck && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func TestRegallocFPWritesIndependentAndBeforeEmission(t *testing.T) {
	for name, emit := range map[string]func(*Asm){
		"single arithmetic": func(a *Asm) { a.FaddS(F0, F1, F2, RNE) },
		"double arithmetic": func(a *Asm) { a.FaddD(F0, F1, F2, RNE) },
		"single input":      func(a *Asm) { a.FmvWX(F0, X5) },
		"double input":      func(a *Asm) { a.FmvDX(F0, X5) },
		"conversion":        func(a *Asm) { a.FcvtSD(F0, F1, RNE) },
		"direct call":       func(a *Asm) { a.Jal(RA, 4) },
		"indirect call":     func(a *Asm) { a.Jalr(X5, X6, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			var a, plain Asm
			var masks []uint32
			a.ObserveFPWrites(func(mask uint32) {
				if a.Len() != 0 {
					t.Fatal("write reported after emission")
				}
				masks = append(masks, mask)
			})
			// Replacing either other channel must not remove FP observation.
			gp := a.ObserveGPWrites(func(uint32) {})
			transfer := a.ObserveRegalloc(func(regalloccheck.Effect) {})
			emit(&a)
			a.ObserveRegalloc(transfer)
			a.ObserveGPWrites(gp)
			emit(&plain)
			want := uint32(1)
			if name == "direct call" || name == "indirect call" {
				want = ^uint32(0)
			}
			if a.Err() != nil || len(masks) != 1 || masks[0] != want || !bytes.Equal(a.B, plain.B) {
				t.Fatalf("masks=%x want=%x error=%v", masks, want, a.Err())
			}
		})
	}
}

func TestRegallocFPWriteValidationAndScope(t *testing.T) {
	for name, emit := range map[string]func(*Asm){
		"invalid destination": func(a *Asm) { a.FaddD(FReg(32), F1, F2, RNE) },
		"invalid source":      func(a *Asm) { a.FmvDX(F0, Reg(32)) },
		"invalid rounding":    func(a *Asm) { a.FaddD(F0, F1, F2, RoundingMode(6)) },
		"unaligned":           func(a *Asm) { a.B = []byte{0}; a.FaddD(F0, F1, F2, RNE) },
		"sticky error":        func(a *Asm) { a.FmvDX(F0, Reg(32)); a.FaddD(F0, F1, F2, RNE) },
	} {
		t.Run(name, func(t *testing.T) {
			var a Asm
			a.ObserveFPWrites(func(uint32) { t.Fatal("invalid instruction reported a write") })
			emit(&a)
			if a.Err() == nil {
				t.Fatal("invalid instruction accepted")
			}
		})
	}
	var a Asm
	outer, inner := 0, 0
	a.ObserveFPWrites(func(uint32) { outer++ })
	prior := a.ObserveFPWrites(func(uint32) { inner++ })
	a.FmvDX(F31, X5)
	a.ObserveFPWrites(prior)
	a.FmvWX(F0, X5)
	a.Add(X5, X6, X7) // A GP definition must not name the same-numbered FP register.
	a.Jal(Zero, 4)    // A zero-link jump is not a call.
	if outer != 1 || inner != 1 {
		t.Fatalf("outer=%d inner=%d", outer, inner)
	}
}

func TestRegallocFPOnlyObserverSeesCalls(t *testing.T) {
	for name, emit := range map[string]func(*Asm){
		"direct":   func(a *Asm) { a.Jal(RA, 4) },
		"indirect": func(a *Asm) { a.Jalr(X5, X6, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			var a Asm
			count := 0
			a.ObserveFPWrites(func(mask uint32) {
				if mask != ^uint32(0) || a.Len() != 0 {
					t.Fatal("call must invalidate FP before emission")
				}
				count++
			})
			emit(&a) // No GP or transfer observer is installed.
			if a.Err() != nil || count != 1 {
				t.Fatalf("call error=%v notifications=%d", a.Err(), count)
			}
		})
	}
}
