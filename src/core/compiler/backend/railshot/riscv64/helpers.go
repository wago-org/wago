//go:build riscv64

package riscv64

// SP-relative (and general base+disp) load/store helpers the port references as
// f.ld64/ld32/st64/st32. disp is a signed byte offset: a non-negative,
// size-aligned, imm12-scalable offset uses the scaled LDR/STR form; a small
// signed offset uses LDUR/STUR; anything larger panics (not produced by the
// current frame layout).

func (f *fn) ldst(store bool, size int, rt, base Reg, disp int32) {
	switch {
	case disp >= 0 && disp%int32(size) == 0 && disp/int32(size) <= 0xFFF:
		off := uint32(disp)
		switch {
		case store && size == 8:
			f.a.Store64(rt, base, off)
		case store:
			f.a.Store32(rt, base, off)
		case size == 8:
			f.a.Load64(rt, base, off)
		default:
			f.a.Load32(rt, base, off)
		}
	case disp >= -256 && disp < 256:
		switch {
		case store && size == 8:
			f.a.Stur64(rt, base, disp)
		case store:
			f.a.Stur32(rt, base, disp)
		case size == 8:
			f.a.Ldur64(rt, base, disp)
		default:
			f.a.Ldur32(rt, base, disp)
		}
	default:
		panic("riscv64 ldst: byte offset out of range for a single load/store")
	}
}

func (f *fn) ld64(rt, base Reg, disp int32)     { f.ldst(false, 8, rt, base, disp) }
func (f *fn) ld32(rt, base Reg, disp int32)     { f.ldst(false, 4, rt, base, disp) }
func (f *fn) st64(base Reg, disp int32, rt Reg) { f.ldst(true, 8, rt, base, disp) }
func (f *fn) st32(base Reg, disp int32, rt Reg) { f.ldst(true, 4, rt, base, disp) }

// Float spill load/store helpers (fld/fst/stF) — scalar S/D via the encoder's
// FLoadDisp/FStoreDisp (scaled-imm or address-materialized fallback).
func (f *fn) fld(rt, base Reg, disp int32, f64 bool)     { f.a.FLoadDisp(rt, base, disp, f64) }
func (f *fn) fst(base Reg, disp int32, rt Reg, f64 bool) { f.a.FStoreDisp(base, disp, rt, f64) }
func (f *fn) stF(base Reg, disp int32, rt Reg, f64 bool) { f.a.FStoreDisp(base, disp, rt, f64) }
