//go:build arm64

package arm64

// SP-relative (and general base+disp) load/store helpers the port references as
// f.ld64/ld32/st64/st32. disp is a signed byte offset: a non-negative,
// size-aligned, imm12-scalable offset uses the scaled LDR/STR form; a small
// signed offset uses LDUR/STUR. Larger negative loads within the add/sub
// immediate range materialize the address in the destination register; stores
// must materialize an address explicitly so they cannot clobber a live source.
// Large SP-relative offsets also need an address register when EH frames grow
// beyond the scaled load/store immediate range.

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
	case !store && disp < -256 && disp >= -0xfff:
		f.a.SubImm64(rt, base, uint32(-disp))
		if size == 8 {
			f.a.Load64(rt, rt, 0)
		} else {
			f.a.Load32(rt, rt, 0)
		}
	case base == SP && disp >= 0:
		addr := rt
		remaining := uint32(disp)
		if store {
			addr = X16
			if rt == X16 {
				// X17 can hold an exception root or indirect call target here.
				// Preserve it across the address calculation without enlarging
				// the function's permanent frame.
				f.a.SubSP64(16)
				previous := f.a.ObserveRegalloc(nil)
				f.a.Store64(X17, SP, 0)
				f.a.ObserveRegalloc(previous)
				addr = X17
				remaining += 16
			}
		}
		from := SP
		for remaining >= 0x1000 {
			step := remaining &^ uint32(0xfff)
			if step > 0xfff000 {
				step = 0xfff000
			}
			f.a.AddImm64LSL12(addr, from, step)
			remaining -= step
			from = addr
		}
		if remaining != 0 {
			f.a.AddImm64(addr, from, remaining)
		}
		switch {
		case store && size == 8:
			f.a.Store64(rt, addr, 0)
		case store:
			f.a.Store32(rt, addr, 0)
		case size == 8:
			f.a.Load64(rt, addr, 0)
		default:
			f.a.Load32(rt, addr, 0)
		}
		if store {
			f.a.ObserveFrameStore(rt, disp, size)
		} else {
			f.a.ObserveFrameLoad(rt, disp, size)
		}
		if store && addr == X17 {
			previous := f.a.ObserveRegalloc(nil)
			f.a.Load64(X17, SP, 0)
			f.a.ObserveRegalloc(previous)
			f.a.AddSP64(16)
		}
	case !store:
		// Global slot tables and other metadata bases can exceed the scaled
		// load immediate. The destination owns the temporary address, even
		// when it initially also holds the base pointer.
		remaining := uint32(disp)
		if disp < 0 {
			remaining = uint32(-int64(disp))
		}
		from := base
		for remaining >= 0x1000 {
			step := min(remaining&^uint32(0xfff), uint32(0xfff000))
			if disp < 0 {
				f.a.SubImm64LSL12(rt, from, step)
			} else {
				f.a.AddImm64LSL12(rt, from, step)
			}
			remaining -= step
			from = rt
		}
		if remaining != 0 {
			if disp < 0 {
				f.a.SubImm64(rt, from, remaining)
			} else {
				f.a.AddImm64(rt, from, remaining)
			}
		}
		if size == 8 {
			f.a.Load64(rt, rt, 0)
		} else {
			f.a.Load32(rt, rt, 0)
		}
	default:
		panic("arm64 ldst: byte offset out of range for a single load/store")
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
