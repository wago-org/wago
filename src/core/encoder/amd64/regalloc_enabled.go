//go:build wago_regalloccheck

package amd64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = true

type regallocState struct{ regallocObserver func(regalloccheck.Effect) }

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

// Legacy scalar FP register moves preserve their untouched upper lanes;
// scalar memory loads and GP-to-FP moves do not.
func (a *Asm) regallocCopy(dst, src Reg, fp bool, size int) {
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Register(regallocBank(fp), uint8(dst)), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size})
	}
}
func (a *Asm) regallocSwap(dst, src Reg, size int) {
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Swap, Dst: regalloccheck.Register(regalloccheck.GP, uint8(dst)), Src: regalloccheck.Register(regalloccheck.GP, uint8(src)), Size: size})
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
	if base == RSP {
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
	if a.regallocObserver != nil && base == RSP {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Slot(offset), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size})
	}
}
func regallocWidth(wide bool) int {
	if wide {
		return 8
	}
	return 4
}

func (a *Asm) regallocRead(base Reg, offset int32, size int) {
	if a.regallocObserver != nil {
		src := regalloccheck.Location{Bank: regalloccheck.Unknown} // non-frame memory cannot prove a frame input
		if base == RSP {
			src = regalloccheck.Slot(offset)
		}
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Read, Src: src, Size: size})
	}
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
