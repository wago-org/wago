//go:build wago_regalloccheck

package arm64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"testing"
)

func TestRegallocScalarFPDefinitions(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			emit func(*Asm)
		}{
			{"add", func(a *Asm) { a.Fadd(8, 9, 10, f64) }},
			{"sub", func(a *Asm) { a.Fsub(8, 9, 10, f64) }},
			{"mul", func(a *Asm) { a.Fmul(8, 9, 10, f64) }},
			{"div", func(a *Asm) { a.Fdiv(8, 9, 10, f64) }},
			{"sqrt", func(a *Asm) { a.Fsqrt(8, 9, f64) }},
			{"min", func(a *Asm) { a.Fmin(8, 9, 10, f64) }},
			{"max", func(a *Asm) { a.Fmax(8, 9, 10, f64) }},
			{"signed conversion", func(a *Asm) { a.Scvtf(8, X9, f64, true) }},
			{"unsigned conversion", func(a *Asm) { a.Ucvtf(8, X9, f64, true) }},
			{"promote", func(a *Asm) { a.FcvtS2D(8, 9) }},
			{"demote", func(a *Asm) { a.FcvtD2S(8, 9) }},
			{"nearest", func(a *Asm) { a.Frint(8, 9, f64, 'n') }},
			{"floor", func(a *Asm) { a.Frint(8, 9, f64, 'm') }},
			{"ceil", func(a *Asm) { a.Frint(8, 9, f64, 'p') }},
			{"trunc", func(a *Asm) { a.Frint(8, 9, f64, 'z') }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var a Asm
				var s regalloccheck.State
				dst, src := regalloccheck.Register(regalloccheck.FP, 8), regalloccheck.Register(regalloccheck.FP, 9)
				oldDst, oldSrc := s.Seed(dst, 16), s.Seed(src, 16)
				a.ObserveRegalloc(s.Apply)
				tc.emit(&a)
				requireVectorLost(t, func() { s.Expect("result overwritten", dst, oldDst[:4]) })
				upper := dst
				upper.Byte = 8
				requireVectorLost(t, func() { s.Expect("old upper lanes cleared", upper, oldDst[8:]) })
				s.Expect("source preserved", src, oldSrc)
			})
		}
	}
}

func TestRegallocScalarFPGraphConsumesPhysicalEffects(t *testing.T) {
	for _, newDefinition := range []bool{false, true} {
		var a Asm
		fp := regalloccheck.Register(regalloccheck.FP, 8)
		g := regalloccheck.Graph{Widths: []uint8{8, 8}, Inputs: []regalloccheck.Binding{{Location: fp, Value: 1}}, Blocks: []regalloccheck.Block{{}}}
		a.ObserveRegalloc(func(e regalloccheck.Effect) {
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
		})
		a.Fadd(8, 9, 10, true)
		id := regalloccheck.ValueID(1)
		if newDefinition {
			id = 2
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Define, Location: fp, Value: id})
		}
		g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: fp, Value: id})
		r := g.Verify(regalloccheck.Limits{})
		if newDefinition && r.Verdict != regalloccheck.Verified || !newDefinition && r.Verdict != regalloccheck.Rejected {
			t.Fatalf("newDefinition=%v: %+v", newDefinition, r)
		}
	}
}

func TestRegallocFPReadOnlyInstructionsDoNotDefine(t *testing.T) {
	var a Asm
	var s regalloccheck.State
	fp := regalloccheck.Register(regalloccheck.FP, 8)
	value := s.Seed(fp, 16)
	a.ObserveRegalloc(s.Apply)
	a.FStoreDisp(SP, 32, 8, true)
	a.Fcmp(8, 9, true)
	a.FmovToGpr(X9, 8, true)
	a.Fcvtzs(X9, 8, true, true)
	s.Expect("FP source unchanged", fp, value)
}
