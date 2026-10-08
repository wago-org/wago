//go:build wago_regalloccheck

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"testing"
)

// These controls use actual encoder callbacks, never backend bookkeeping.
// Unchanged upper lanes retain their original provenance; overwritten result
// bytes cannot prove either their old value or a newly invented semantic one.
func TestRegallocScalarFPDefinitions(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		vex  bool
		emit func(*Asm)
	}{
		{"addss", 4, false, func(a *Asm) { a.FAdd(8, 9, false) }},
		{"addsd", 8, false, func(a *Asm) { a.FAdd(8, 9, true) }},
		{"subss", 4, false, func(a *Asm) { a.FSub(8, 9, false) }},
		{"mulsd", 8, false, func(a *Asm) { a.FMul(8, 9, true) }},
		{"divss", 4, false, func(a *Asm) { a.FDiv(8, 9, false) }},
		{"minsd", 8, false, func(a *Asm) { a.FMin(8, 9, true) }},
		{"maxss", 4, false, func(a *Asm) { a.FMax(8, 9, false) }},
		{"sqrtsd", 8, false, func(a *Asm) { a.FSqrt(8, 9, true) }},
		{"integer to ss", 4, false, func(a *Asm) { a.Cvtsi2f(8, R9, false, true) }},
		{"integer to sd", 8, false, func(a *Asm) { a.Cvtsi2f(8, R9, true, true) }},
		{"ss to sd", 8, false, func(a *Asm) { a.Cvtss2sd(8, 9) }},
		{"sd to ss", 4, false, func(a *Asm) { a.Cvtsd2ss(8, 9) }},
		{"roundss", 4, false, func(a *Asm) { a.SseMapRRI(0x66, 0x3a, 0x0a, 8, 9, 0) }},
		{"roundsd", 8, false, func(a *Asm) { a.SseMapRRI(0x66, 0x3a, 0x0b, 8, 9, 0) }},
		{"addss disp", 4, false, func(a *Asm) { a.FAluDisp(0x58, 8, RSP, 32, false) }},
		{"addsd index", 8, false, func(a *Asm) { a.SseIdx(0xf2, 0x58, 8, RSP, R9, 32) }},
		{"vaddss", 4, true, func(a *Asm) { a.VFAdd(8, 9, 10, false) }},
		{"vaddsd", 8, true, func(a *Asm) { a.VFAdd(8, 9, 10, true) }},
		{"vsubss", 4, true, func(a *Asm) { a.VFSub(8, 9, 10, false) }},
		{"vmulsd", 8, true, func(a *Asm) { a.VFMul(8, 9, 10, true) }},
		{"vdivss", 4, true, func(a *Asm) { a.VFDiv(8, 9, 10, false) }},
		{"vaddss disp", 4, true, func(a *Asm) { a.VSseMemDisp(2, 0x58, 8, 9, RSP, 32) }},
		{"vaddsd index", 8, true, func(a *Asm) { a.VFMemIdx(0x58, 8, 9, RSP, R10, 32, true) }},
		{"vaddss rip", 4, true, func(a *Asm) { a.vex3MemRipPlaceholder(vexMap0F, 2, 0x58, 8, 9) }},
		{"vss to sd", 8, true, func(a *Asm) { a.vex3RRR(2, 0x5a, 8, 9, 10) }},
		{"vsd to ss", 4, true, func(a *Asm) { a.vex3RRR(3, 0x5a, 8, 9, 10) }},
		{"vroundss", 4, true, func(a *Asm) { a.vex3RRIMap(vexMap0F3A, 1, 0x0a, 8, 9, 10, 0) }},
		{"vroundsd", 8, true, func(a *Asm) { a.vex3RRIMap(vexMap0F3A, 1, 0x0b, 8, 9, 10, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			var s regalloccheck.State
			dst, src := regalloccheck.Register(regalloccheck.FP, 8), regalloccheck.Register(regalloccheck.FP, 9)
			oldDst, oldSrc := s.Seed(dst, 16), s.Seed(src, 16)
			a.ObserveRegalloc(s.Apply)
			tc.emit(&a)
			requireVectorLost(t, func() { s.Expect("arithmetic result was overwritten", dst, oldDst[:tc.size]) })
			upper := dst
			upper.Byte = uint8(tc.size)
			want := oldDst[tc.size:]
			if tc.vex {
				want = oldSrc[tc.size:]
			}
			s.Expect("upper lanes", upper, want)
			s.Expect("source preserved", src, oldSrc)
		})
	}
}

func TestRegallocVEXScalarAliasAndEncodedSource(t *testing.T) {
	for _, self := range []bool{false, true} {
		var a Asm
		var s regalloccheck.State
		dst := regalloccheck.Register(regalloccheck.FP, 8)
		left := Reg(0)
		if self {
			left = 8
		}
		oldDst := s.Seed(dst, 16)
		source := s.Seed(regalloccheck.Register(regalloccheck.FP, uint8(left)), 16)
		a.ObserveRegalloc(s.Apply)
		// Reg0 encodes vvvv=1111. For this scalar arithmetic opcode it names
		// XMM0, even if a wrapper describes that field as reserved.
		a.vex3RRReserved(vexMap0F, 3, 0x58, 8, 9)
		if self {
			s.Put(dst, oldDst)
			a.VFAdd(8, 8, 9, true)
		}
		upper := dst
		upper.Byte = 8
		s.Expect("encoded first source supplies upper lanes", upper, source[8:])
		requireVectorLost(t, func() { s.Expect("arithmetic low bytes", dst, oldDst[:8]) })
	}
}

func TestRegallocScalarFPKnownEncodingBoundary(t *testing.T) {
	var a Asm
	effects := 0
	a.ObserveRegalloc(func(regalloccheck.Effect) { effects++ })
	a.SseRR(0xf0, 0x58, 8, 9, false)              // LOCK is not a mandatory FP prefix
	a.SseMapRRI(0xf3, 0x3a, 0x0a, 8, 9, 0)        // scalar rounding requires66
	a.vex3RRRMapL(vexMap0F, 2, 0x58, 8, 9, 10, 1) // noncanonical scalarL1
	if effects != 0 {
		t.Fatal("unsupported encoding received a known lane contract")
	}
	a.SseMapRRI(0x66, 0x3a, 0x0a, 8, 9, 0)
	if effects != 1 {
		t.Fatal("canonical round missing destination effect")
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
		a.FAdd(8, 9, true)
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
	a.FStoreDisp(RSP, 32, 8, true)
	a.SseRR(0x66, 0x2e, 8, 9, false) // UCOMISD updates flags only
	a.Cvttf2si(RAX, 8, true, true)   // GP destination, FP source
	s.Expect("FP source unchanged", fp, value)
}

func TestRegallocScalarFPReportsEncodedDestination(t *testing.T) {
	for _, raw := range []Reg{0, 7, 8, 15, 16, 23, 32, 255} {
		for _, vex := range []bool{false, true} {
			var a Asm
			var effects []regalloccheck.Effect
			a.ObserveRegalloc(func(e regalloccheck.Effect) { effects = append(effects, e) })
			if vex {
				a.VFAdd(raw, 9, 10, true)
			} else {
				a.FAdd(raw, 9, true)
			}
			modrm := a.B[len(a.B)-1]
			want := uint8(modrm >> 3 & 7)
			if vex {
				if a.B[1]&0x80 == 0 {
					want |= 8
				}
			} else {
				// Mandatory prefix, followed by optional REX (REX.R is bit2).
				if a.B[1]&0xf0 == 0x40 && a.B[1]&4 != 0 {
					want |= 8
				}
			}
			last := effects[len(effects)-1]
			if last.Kind != regalloccheck.Kill || last.Dst.Index != int32(want) {
				t.Fatalf("raw=%d vex=%v bytes=%x effect=%+v encoded=%d", raw, vex, a.B, last, want)
			}
		}
	}
}
