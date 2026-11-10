//go:build wago_regalloccheck

package amd64

import "testing"

// Encoder-only controls: these bytes are never executed.
func TestGPWritesPhysicalAliases(t *testing.T) {
	for _, reg := range []Reg{16, 23, 32, 255} {
		want := uint32(1) << (8 | (reg & 7))
		checkGPWrites(t, func(a *Asm) { a.MovImm32(reg, 1) }, want, true)
		checkGPWrites(t, func(a *Asm) { a.Load64(reg, RSP, 0) }, want, true)
		checkGPWrites(t, func(a *Asm) { a.AluRR(0x03, RAX, reg, true) }, want, true)
		checkGPWrites(t, func(a *Asm) { a.SseRR(0xf2, 0x2c, reg, 0, true) }, want, true)
	}
}

func TestGPWritesUnknownGenericEffects(t *testing.T) {
	// Call classifiers directly so unknown forms do not produce instructions.
	for _, classify := range []func(*Asm){
		func(a *Asm) { a.regallocGPRR(0x0f, 0, 1, false) },
		func(a *Asm) { a.regallocGPMem(0x0f, 0, false) },
		func(a *Asm) { a.regallocGPSSE(0x66, 0x38, 0xff, 0, 1, false) },
		func(a *Asm) { a.regallocGPVEX(0xff, 1, 0x58, 0, 1, false) },
		func(a *Asm) { a.regallocGPVEX(vexMap0F, 0x81, 0x58, 0, 1, false) },
		func(a *Asm) { a.regallocFPShift(0x71, 0, 3) },
	} {
		checkGPWrites(t, classify, 0xffff, false)
	}
}

func TestGPWritesUnknownEVEXRefusesBeforeEmission(t *testing.T) {
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.evexRRR(0xff, 1, 0x58, false, 0, 1, 2) },
		func(a *Asm) { a.evexRR(0xff, 1, 0x58, false, 0, 1) },
		func(a *Asm) { a.evexMemIdx(0xff, 1, 0x58, false, 0, RSP, RCX, 0, 16) },
		func(a *Asm) { a.LockAluIdx(0x0f, RSP, RCX, RAX, 0, 8) },
	} {
		a := &Asm{}
		refused := false
		a.ObserveGPWrites(func(mask uint32) {
			if mask != 0xffff {
				t.Fatalf("unknown EVEX mask = %#x", mask)
			}
			refused = true
			panic("refused")
		})
		func() {
			defer func() {
				if recover() != "refused" {
					t.Fatal("missing conservative refusal")
				}
			}()
			emit(a)
		}()
		if !refused || len(a.B) != 0 {
			t.Fatal("unknown EVEX emitted before refusal")
		}
	}
}

func TestGPWritesGenericMemoryImplicitEffects(t *testing.T) {
	checkGPWrites(t, func(a *Asm) { a.regallocGPMem(0x86, 4, false) }, 1<<RAX, false)
	checkGPWrites(t, func(a *Asm) { a.regallocGPMem(0x87, 20, true) }, 1<<R12, false)
	checkGPWrites(t, func(a *Asm) { a.regallocGPMem(0xff, 6, true) }, 1<<RSP, false)
	checkGPWrites(t, func(a *Asm) { a.regallocGPMem(0xff, 10, true) }, 0xffff, false)
	checkGPWrites(t, func(a *Asm) { a.regallocGPRR(0xff, R11, 2, true) }, 0xffff, false)
	checkGPWrites(t, func(a *Asm) { a.LockAluIdx(0x21, RSP, RCX, RAX, 0, 8) }, 0, true)
}
