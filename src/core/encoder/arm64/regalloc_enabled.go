//go:build wago_regalloccheck

package arm64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = true

type regallocState struct {
	regallocObserver func(regalloccheck.Effect)
	gpWriteObserver  func(uint32)
}

// ObserveGPWrites installs an independent physical GP-write observer. Restore the
// returned observer when the observation scope ends, including on panic. Masks
// name X0..X30 and, where an instruction writes SP, bit 31. Discarded XZR
// destinations are excluded. Calls conservatively report every bit for their
// ABI clobbers. This does not change the allocation-transfer observer.
func (a *Asm) ObserveGPWrites(fn func(uint32)) func(uint32) {
	old := a.gpWriteObserver
	a.gpWriteObserver = fn
	return old
}

func (a *Asm) regallocGPWrites(mask uint32) {
	if mask != 0 && a.gpWriteObserver != nil {
		a.gpWriteObserver(mask)
	}
}

// Register 31 is SP only for the instruction forms that explicitly admit it.
// Match the encoded five-bit register field, including for aliased Reg values.
func regallocGPMask(dst Reg, sp bool) uint32 {
	reg := r(dst)
	if reg == 31 && !sp {
		return 0
	}
	return uint32(1) << reg
}

// ObserveRegalloc scopes an observer to an explicitly checked transfer window.
// The returned observer must be restored, including when codegen panics.
func (a *Asm) ObserveRegalloc(fn func(regalloccheck.Effect)) func(regalloccheck.Effect) {
	old := a.regallocObserver
	a.regallocObserver = fn
	return old
}
func regallocBank(fp bool) regalloccheck.Bank {
	if fp {
		return regalloccheck.FP
	}
	return regalloccheck.GP
}

// Scalar FP register moves clear the remaining vector lanes, so they cannot
// preserve a previous v128 identity.
func (a *Asm) regallocCopy(dst, src Reg, fp bool, size int) {
	if a.regallocObserver != nil {
		e := regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Register(regallocBank(fp), uint8(dst)), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size}
		if fp {
			e.ClearTo = 16
		}
		a.regallocObserver(e)
	}
}

// Only direct stack-pointer-relative memory names tracked frame bytes. Other
// loads destroy the old destination identity; pointer aliases and changing frame
// bases are outside this model. Scalar FP loads also kill old upper lanes.
func (a *Asm) regallocLoad(dst, base Reg, offset int32, fp bool, size int) {
	if a.regallocObserver == nil {
		return
	}
	e := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Register(regallocBank(fp), uint8(dst)), Size: size}
	if base == SP {
		e.Kind = regalloccheck.Copy
		e.Src = regalloccheck.Slot(offset)
	}
	if fp {
		e.ClearTo = 16
	} else if size == 4 {
		e.ClearTo = 8
	}
	a.regallocObserver(e)
}

// Non-frame stores cannot establish or update facts about the tracked frame.
func (a *Asm) regallocStore(base Reg, offset int32, src Reg, fp bool, size int) {
	if a.regallocObserver != nil && base == SP {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Slot(offset), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size})
	}
}

// ObserveFrameLoad and ObserveFrameStore name the original SP-relative slot
// after the backend materializes a large displacement in a scratch register.
func (a *Asm) ObserveFrameLoad(dst Reg, offset int32, size int) {
	a.regallocLoad(dst, SP, offset, false, size)
}
func (a *Asm) ObserveFrameStore(src Reg, offset int32, size int) {
	a.regallocStore(SP, offset, src, false, size)
}

func regallocWidth(wide bool) int {
	if wide {
		return 8
	}
	return 4
}

// A cross-bank scalar move defines only the copied low bytes. GP32 and scalar
// FP writes clear their high carrier bytes on both targets.
func (a *Asm) regallocCrossCopy(dst, src Reg, dstFP bool, size int) {
	if a.regallocObserver == nil {
		return
	}
	clearTo := 8
	if dstFP {
		clearTo = 16
	}
	a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy,
		Dst:  regalloccheck.Register(regallocBank(dstFP), uint8(dst)),
		Src:  regalloccheck.Register(regallocBank(!dstFP), uint8(src)),
		Size: size, ClearTo: clearTo})
}

// An untracked FP definition invalidates the previous vector identity. The
// backend installs a semantic result identity when its contract permits one.
func (a *Asm) regallocKillFP(dst Reg) {
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill,
			Dst: regalloccheck.Register(regalloccheck.FP, uint8(dst)), Size: 16})
	}
}
