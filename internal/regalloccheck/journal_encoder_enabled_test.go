//go:build wago_regalloccheck

package regalloccheck_test

import (
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

type journalEncoder struct {
	length        func() int
	truncate      func(int)
	load          func(int32)
	move, call    func()
	observeEffect func(func(regalloccheck.Effect)) func(regalloccheck.Effect)
	observeWrites func(func(uint32)) func(uint32)
	callMask      uint32
}

func journalEncoders() []struct {
	name string
	new  func() journalEncoder
} {
	return []struct {
		name string
		new  func() journalEncoder
	}{
		{"amd64", func() journalEncoder {
			a := new(x86.Asm)
			return journalEncoder{a.Len, func(n int) { a.B = a.B[:n] }, func(off int32) { a.Load32(x86.RCX, x86.RSP, off) }, func() { a.MovRegReg32(x86.RDX, x86.RCX) }, func() { a.CallReg(x86.R9) }, a.ObserveRegalloc, a.ObserveGPWrites, 0xffff}
		}},
		{"arm64", func() journalEncoder {
			a := new(a64.Asm)
			return journalEncoder{a.Len, func(n int) { a.B = a.B[:n] }, func(off int32) {
				if !a.Load32(a64.X1, a64.SP, uint32(off)) {
					panic("test offset rejected")
				}
			}, func() { a.MovReg32(a64.X2, a64.X1) }, func() { a.Blr(a64.X9) }, a.ObserveRegalloc, a.ObserveGPWrites, ^uint32(0)}
		}},
	}
}

func journalEmit(t *testing.T, j *regalloccheck.EmissionJournal, a journalEncoder, emit func()) {
	t.Helper()
	if !j.BeginEmission(a.length()) {
		t.Fatal(j.Result())
	}
	emit()
	if !j.EndEmission(a.length()) {
		t.Fatal(j.Result())
	}
}

// This test adapter admits only these concrete Load32/Mov32/Call encoder
// recipes. It reconciles their GP notifications against Copy/Call effects;
// dropping every GP notification would not be an admitted whole-body recipe.
// No source mapping, instruction-byte coverage or call-frame ABI is claimed.
func journalTransportGraph(t *testing.T, j *regalloccheck.EmissionJournal, callMask uint32) regalloccheck.Graph {
	t.Helper()
	g := regalloccheck.Graph{Widths: []uint8{4, 4}, Inputs: []regalloccheck.Binding{{Location: regalloccheck.Slot(8), Value: 1}, {Location: regalloccheck.Slot(16), Value: 2}}, Blocks: []regalloccheck.Block{{}}}
	spanStart, spanEnd := -1, -1
	var observed, expected uint32
	finish := func() {
		if observed != expected {
			t.Fatalf("GP notifications %#x != admitted recipe writes %#x", observed, expected)
		}
	}
	for i := 0; i < j.Result().Events; i++ {
		e, ok := j.Event(i)
		if !ok {
			t.Fatal("journal event unavailable", j.Result())
		}
		if e.Start != spanStart || e.End != spanEnd {
			if spanStart >= 0 {
				finish()
			}
			spanStart, spanEnd = e.Start, e.End
			observed, expected = 0, 0
		}
		switch e.Kind {
		case regalloccheck.JournalEffect:
			switch e.Effect.Kind {
			case regalloccheck.Copy:
				if e.Effect.Dst.Bank != regalloccheck.GP || e.Effect.Dst.Index < 1 || e.Effect.Dst.Index > 2 || e.Effect.Size != 4 || e.Effect.ClearTo != 0 && e.Effect.ClearTo != 8 {
					t.Fatal("outside admitted test recipe", e)
				}
				expected |= 1 << uint(e.Effect.Dst.Index)
			case regalloccheck.Call:
				expected |= callMask
			default:
				t.Fatal("outside admitted test recipe", e)
			}
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e.Effect})
		case regalloccheck.JournalGPWrites:
			observed |= e.GPWrites
		default:
			t.Fatal("unsupported observation admitted", e)
		}
	}
	finish()
	g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: regalloccheck.Register(regalloccheck.GP, 2), Value: 1, Where: "original input transport"})
	return g
}

func TestJournalActualEncoderTransport(t *testing.T) {
	for _, target := range journalEncoders() {
		for _, mode := range []string{"correct", "wrong source", "discarded wrong source", "physical call"} {
			t.Run(target.name+"/"+mode, func(t *testing.T) {
				a := target.new()
				j := regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{})
				defer j.Close()
				oldEffects, oldWrites := 0, 0
				a.observeEffect(func(regalloccheck.Effect) { oldEffects++ })
				a.observeWrites(func(uint32) { oldWrites++ })
				var previousEffect func(regalloccheck.Effect)
				previousEffect = a.observeEffect(func(e regalloccheck.Effect) {
					j.ObserveEffect(e)
					if previousEffect != nil {
						previousEffect(e)
					}
				})
				var previousWrites func(uint32)
				previousWrites = a.observeWrites(func(mask uint32) {
					j.ObserveGPWrites(mask)
					if previousWrites != nil {
						previousWrites(mask)
					}
				})
				defer a.observeEffect(previousEffect)
				defer a.observeWrites(previousWrites)
				if mode == "discarded wrong source" {
					at := a.length()
					m := j.Checkpoint(at)
					journalEmit(t, j, a, func() { a.load(16) })
					a.truncate(at)
					if !j.Rollback(m, a.length()) {
						t.Fatal(j.Result())
					}
				}
				off := int32(8)
				if mode == "wrong source" {
					off = 16
				}
				journalEmit(t, j, a, func() { a.load(off) })
				journalEmit(t, j, a, a.move)
				if mode == "physical call" {
					journalEmit(t, j, a, a.call)
				}
				r := j.Finalize(a.length(), a.length(), nil)
				if r.State != regalloccheck.JournalReady {
					t.Fatal(r)
				}
				g := journalTransportGraph(t, j, a.callMask)
				got := g.Verify(regalloccheck.Limits{})
				want := regalloccheck.Verified
				if mode == "wrong source" || mode == "physical call" {
					want = regalloccheck.Rejected
				}
				if got.Verdict != want {
					t.Fatalf("%v, want %v", got, want)
				}
				wantCallbacks := 2
				if mode == "physical call" || mode == "discarded wrong source" {
					wantCallbacks = 3
				}
				if oldEffects != wantCallbacks || oldWrites != wantCallbacks {
					t.Fatalf("observers forwarded %d/%d, want %d", oldEffects, oldWrites, wantCallbacks)
				}
				for i := 0; i < r.Events; i++ {
					e, _ := j.Event(i)
					if e.Start < 0 || e.End <= e.Start || e.End > a.length() {
						t.Fatal("bad actual emission span", e)
					}
				}
				// Bytes are inspected/observed, never executed, in every mode.
			})
		}
	}
}

func TestJournalActualObserverRestorationAfterPanic(t *testing.T) {
	for _, target := range journalEncoders() {
		t.Run(target.name, func(t *testing.T) {
			a := target.new()
			j := regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{})
			oldEffects, oldWrites := 0, 0
			a.observeEffect(func(regalloccheck.Effect) { oldEffects++ })
			a.observeWrites(func(uint32) { oldWrites++ })
			func() {
				defer func() {
					if recover() != "emission panic" {
						t.Fatal("original panic changed")
					}
				}()
				defer j.Close()
				previousEffect := a.observeEffect(j.ObserveEffect)
				defer a.observeEffect(previousEffect)
				previousWrites := a.observeWrites(j.ObserveGPWrites)
				defer a.observeWrites(previousWrites)
				journalEmit(t, j, a, func() { a.load(8) })
				panic("emission panic")
			}()
			if j.Result().State != regalloccheck.JournalClosed {
				t.Fatal("journal not closed")
			}
			a.move()
			if oldEffects != 1 || oldWrites != 1 {
				t.Fatal("enclosing observers not restored", oldEffects, oldWrites)
			}
		})
	}
}
