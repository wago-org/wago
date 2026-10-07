//go:build wago_regalloccheck

package amd64

import "testing"

// These controls inspect observer masks; emitted bytes are never executed.
func TestFPWriteDestinations(t *testing.T) {
	for _, tt := range []struct {
		name string
		want uint32
		emit func(*Asm)
	}{
		{"scalar", 1 << 3, func(a *Asm) { a.FAdd(3, 4, true) }},
		{"partial", 1 << 3, func(a *Asm) { a.FMov(3, 4, false) }},
		{"lane", 1 << 3, func(a *Asm) { a.Pinsrq(3, RAX, 1) }},
		{"legacy-rm", 1 << 3, func(a *Asm) { a.SseRR(0x66, 0xd6, 4, 3, false) }},
		{"legacy-shift-rm", 1 << 3, func(a *Asm) { a.SseMapRRI(0x66, 0, 0x72, 6, 3, 1) }},
		{"vex-shift-vvvv", 1 << 3, func(a *Asm) { a.VPsllwImm(3, 4, 1) }},
		{"vex-extract-rm", 1 << 3, func(a *Asm) { a.VexMapRR(0x3a, 1, 0x39, 4, 3) }},
		{"ymm", 1 << 3, func(a *Asm) { a.YPaddb(3, 4, 5) }},
		{"evex-ternary", 1 << 3, func(a *Asm) { a.XPternlogd(3, 4, 5, 0x96) }},
		{"evex-rotate", 1 << 3, func(a *Asm) { a.XPrordImm(3, 4, 1) }},
		{"evex-load", 1 << 3, func(a *Asm) { a.ZMovdqu64LoadIdx(3, RAX, RCX, 64) }},
		{"scalar-load", 1 << 3, func(a *Asm) { a.FLoadIdx(3, RAX, RCX, 8, true) }},
		{"scalar-rip", 1 << 3, func(a *Asm) { a.MovsRipPlaceholder(3, true) }},
		{"vector-rip", 1 << 3, func(a *Asm) { a.MovdquRipPlaceholder(3) }},
		{"broadcast-rip", 1 << 3, func(a *Asm) { a.YBroadcastSDRipPlaceholder(3) }},
		{"modrm-alias", 1 << 11, func(a *Asm) { a.FAdd(19, 4, true) }},
		{"vvvv-alias", 1 << 3, func(a *Asm) { a.VPsllwImm(19, 4, 1) }},
		{"evex-modrm-alias", 1 << 11, func(a *Asm) { a.XPternlogd(19, 4, 5, 0x96) }},
		{"call-reg", 0xffff, func(a *Asm) { a.CallReg(RAX) }},
		{"call-memory", 0xffff, func(a *Asm) { a.CallMem(RAX, 0) }},
		{"call-relative", 0xffff, func(a *Asm) { a.CallRel32() }},
		{"gp-bank", 0, func(a *Asm) { a.MovImm64(3, 42) }},
		{"scalar-store", 0, func(a *Asm) { a.FStoreIdx(RAX, RCX, 3, 8, true) }},
		{"vector-store", 0, func(a *Asm) { a.VMovdquStoreDisp(RSP, 0, 3) }},
		{"evex-store", 0, func(a *Asm) { a.ZMovdqu64StoreIdx(RAX, RCX, 3, 64) }},
		{"extract-gp", 0, func(a *Asm) { a.Pextrq(RAX, 3, 1) }},
		{"flags", 0, func(a *Asm) { a.Ucomis(3, 4, true) }},
		{"ptest", 0, func(a *Asm) { a.SseMapRR(0x66, 0x38, 0x17, 3, 4) }},
		{"upper-only", 0, func(a *Asm) { a.VZeroUpper() }},
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

func TestFPUnknownGenericFormsConservative(t *testing.T) {
	// Classifier-only controls: unknown forms do not produce machine code.
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.regallocFPShift(0, 6, 3) },
		func(a *Asm) { a.regallocFPShift(0x71, 0, 3) },
		func(a *Asm) { a.regallocFPVEX(vexMap0F, 0x81, 0xfe, 3, 4, 5, false) },
		func(a *Asm) { a.regallocFPSSE(0x66, 0x38, 0xff, 3, 4, false) },
		func(a *Asm) { a.regallocFPSSE(0x66, 0, 0x71, 0, 3, false) },
		func(a *Asm) { a.regallocGPRR(0x0f, 0, 1, false) },
		func(a *Asm) { a.regallocGPMem(0x0f, 0, false) },
		func(a *Asm) { a.regallocGPRR(0xff, R11, 2, true) },
	} {
		a := &Asm{}
		var got uint32
		a.ObserveFPWrites(func(mask uint32) { got |= mask })
		emit(a)
		if got != 0xffff {
			t.Fatalf("unknown form mask %#x", got)
		}
		if len(a.B) != 0 {
			t.Fatal("classification emitted bytes")
		}
	}
}
