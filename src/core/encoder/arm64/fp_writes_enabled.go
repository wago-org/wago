//go:build wago_regalloccheck

package arm64

// ObserveFPWrites observes possible writes to V0..V31 independently of transfer
// windows. Partial/lane writes count; calls conservatively report every bit.
// Unlike XZR, V31 is a physical destination. Restore the returned observer on
// normal completion and panic.
func (a *Asm) ObserveFPWrites(fn func(uint32)) func(uint32) {
	old := a.fpWriteObserver
	a.fpWriteObserver = fn
	return old
}

func (a *Asm) regallocFPWrites(mask uint32) {
	if mask != 0 && a.fpWriteObserver != nil {
		a.fpWriteObserver(mask)
	}
}
