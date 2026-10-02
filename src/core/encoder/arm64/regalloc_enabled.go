//go:build wago_regalloccheck

package arm64

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
func (a *Asm) regallocCopy(dst, src Reg, fp bool, size int) {
	if a.regallocObserver != nil {
		e := regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Register(regallocBank(fp), uint8(dst)), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size}
		if fp {
			e.ClearTo = 16
		}
		a.regallocObserver(e)
	}
}
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
func (a *Asm) regallocStore(base Reg, offset int32, src Reg, fp bool, size int) {
	if a.regallocObserver != nil && base == SP {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Slot(offset), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size})
	}
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
func (a *Asm) regallocKillFP(dst Reg) {
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill,
			Dst: regalloccheck.Register(regalloccheck.FP, uint8(dst)), Size: 16})
	}
}
