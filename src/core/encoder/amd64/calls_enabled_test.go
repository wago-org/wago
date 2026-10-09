//go:build wago_regalloccheck

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"testing"
)

// Each physical call form must produce exactly one effect without any backend
// notification. Retaining a stale GP or FP identity is a negative checker test;
// retaining caller-frame bytes is the corresponding positive test.
func TestRegallocPhysicalCall(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
	}{
		{"relative", func(a *Asm) { a.CallRel32() }},
		{"register", func(a *Asm) { a.CallReg(R9) }},
		{"memory", func(a *Asm) { a.CallMem(R12, 16) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			var state regalloccheck.State
			gp := regalloccheck.Register(regalloccheck.GP, 1)
			fp := regalloccheck.Register(regalloccheck.FP, 1)
			frame := regalloccheck.Slot(32)
			oldGP, oldFP, saved := state.Seed(gp, 8), state.Seed(fp, 16), state.Seed(frame, 16)
			effects := 0
			previous := a.ObserveRegalloc(func(e regalloccheck.Effect) {
				if e.Kind != regalloccheck.Call {
					t.Fatalf("unexpected call effect: %+v", e)
				}
				effects++
				state.Apply(e)
			})
			tc.emit(&a)
			if effects != 1 {
				t.Fatalf("got %d effects, want one physical call", effects)
			}
			requireVectorLost(t, func() { state.Expect("GP across call", gp, oldGP) })
			requireVectorLost(t, func() { state.Expect("FP across call", fp, oldFP) })
			state.Expect("caller frame retained", frame, saved)
			a.ObserveRegalloc(previous)
			tc.emit(&a)
			if effects != 1 {
				t.Fatal("restored observer still invoked")
			}
		})
	}
}

func TestRegallocNonCallsDoNotClobber(t *testing.T) {
	var a Asm
	calls := 0
	a.ObserveRegalloc(func(e regalloccheck.Effect) {
		if e.Kind == regalloccheck.Call {
			calls++
		}
	})
	a.JmpReg(R9)
	a.Ret()
	if calls != 0 {
		t.Fatal("jump/return reported a physical call")
	}
}
