//go:build wago_regalloccheck

package arm64

import "testing"

// Portable encoder controls: no emitted instruction stream is executed.
func TestFPWriteDestinations(t *testing.T) {
	for _, tt := range []struct {
		name string
		want uint32
		emit func(*Asm)
	}{
		{"scalar", 1 << 3, func(a *Asm) { a.Fadd(3, 4, 5, true) }},
		{"partial", 1 << 3, func(a *Asm) { a.FmovReg(3, 4, false) }},
		{"gp-to-fp", 1 << 3, func(a *Asm) { a.FmovFromGpr(3, X0, true) }},
		{"convert", 1 << 3, func(a *Asm) { a.Scvtf(3, X0, true, true) }},
		{"lane", 1 << 3, func(a *Asm) { a.NeonInsD(3, X0, 1) }},
		{"lane-source", 1 << 3, func(a *Asm) { a.NeonInsLaneSFrom(3, 1, 4, 2) }},
		{"packed", 1 << 3, func(a *Asm) { a.NeonAddB(3, 4, 5) }},
		{"reduction", 1 << 3, func(a *Asm) { a.NeonAddvB(3, 4) }},
		{"shuffle", 1 << 3, func(a *Asm) { a.NeonTbl(3, 4, 5) }},
		{"round", 1 << 3, func(a *Asm) { a.NeonFrint(3, 4, true, 'n') }},
		{"vector-compare", 1 << 3, func(a *Asm) { a.NeonFcmp(3, 4, 5, true, 0) }},
		{"literal", 1 << 3, func(a *Asm) { a.LdrLiteralF(3, true) }},
		{"scalar-load", 1 << 3, func(a *Asm) { a.LdrF(3, SP, 8, true) }},
		{"scalar-index", 1 << 3, func(a *Asm) { a.LdrFIdx(3, X0, X1, 0, true) }},
		{"scalar-index-displaced", 1 << 3, func(a *Asm) { a.LdrFIdx(3, X0, X1, 65536, true) }},
		{"scaled", 1 << 3, func(a *Asm) { a.LdrQ(3, SP, 16) }},
		{"unscaled", 1 << 3, func(a *Asm) { a.LdrQ(3, SP, -1) }},
		{"large-offset", 1 << 3, func(a *Asm) { a.LdrQ(3, SP, (1<<20)+16) }},
		{"dense-scalar", 1 << 3, func(a *Asm) { a.DenseIdxDisp = true; a.LdrFIdx(3, X0, X1, 16, true) }},
		{"dense-vector", 1 << 3, func(a *Asm) { a.DenseIdxDisp = true; a.LdrQIdx(3, X0, X1, 16) }},
		{"pair-v31", 1<<3 | 1<<31, func(a *Asm) { a.LdpQ(3, 31, SP, 0) }},
		{"call-relative", 0xffffffff, func(a *Asm) { a.Bl() }},
		{"index", 1 << 3, func(a *Asm) { a.LdrQIdx(3, X0, X1, 0) }},
		{"index-displaced", 1 << 3, func(a *Asm) { a.LdrQIdx(3, X0, X1, 65536) }},
		{"pair", 1<<3 | 1<<4, func(a *Asm) { a.LdpQ(3, 4, SP, 0) }},
		{"v31", 1 << 31, func(a *Asm) { a.FmovReg(31, 4, true) }},
		{"physical-alias", 1 << 3, func(a *Asm) { a.FmovReg(35, 4, true) }},
		{"call-reg", 0xffffffff, func(a *Asm) { a.Blr(X0) }},
		{"gp-bank", 0, func(a *Asm) { a.MovImm64(3, 42) }},
		{"store", 0, func(a *Asm) { a.StrQ(SP, 0, 3) }},
		{"store-index", 0, func(a *Asm) { a.StrQIdx(X0, X1, 3, 65536) }},
		{"pair-store", 0, func(a *Asm) { a.StpQ(3, 4, SP, 0) }},
		{"extract", 0, func(a *Asm) { a.NeonUmovD(X0, 3, 1) }},
		{"fp-to-gp", 0, func(a *Asm) { a.FmovToGpr(X0, 3, true) }},
		{"flags", 0, func(a *Asm) { a.Fcmp(3, 4, true) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &Asm{}
			var got uint32
			calls := 0
			a.ObserveFPWrites(func(mask uint32) { got |= mask; calls++ })
			tt.emit(a)
			if got != tt.want {
				t.Fatalf("mask %#x, want %#x", got, tt.want)
			}
			wantCalls := 0
			if tt.want != 0 {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("callbacks %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestFPInvalidOperandsDoNotReportWrites(t *testing.T) {
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.LdpQ(3, 4, SP, 1) },
		func(a *Asm) { a.LdpQ(3, 4, SP, 1024) },
		func(a *Asm) { a.NeonFrint(3, 4, true, 0) },
	} {
		a := &Asm{}
		calls := 0
		a.ObserveFPWrites(func(uint32) { calls++ })
		func() {
			defer func() {
				if recover() == nil {
					t.Error("expected operand rejection")
				}
			}()
			emit(a)
		}()
		if calls != 0 || len(a.B) != 0 {
			t.Fatal("invalid operands emitted or observed a write")
		}
	}
}

func TestFPScaledOffsetRefusalDoesNotReportWrites(t *testing.T) {
	for _, off := range []uint32{1, 65536} {
		a := &Asm{}
		calls := 0
		a.ObserveFPWrites(func(uint32) { calls++ })
		if a.ldStrScaled(0x3dc00000, 4, 3, SP, off) {
			t.Fatal("invalid offset accepted")
		}
		if calls != 0 || len(a.B) != 0 {
			t.Fatal("refused offset emitted or observed a write")
		}
	}
}
