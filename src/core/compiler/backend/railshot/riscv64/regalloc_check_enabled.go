//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const regallocCheckEnabled = true

type regallocFnState struct {
	immutableGPMask    uint32
	readCheck          regallocReadMask
	fpObserverActive   bool
	fpObserverPrevious func(uint32)
	gpObserverActive   bool
	gpObserverPrevious func(uint32)
}

// checkImmutable begins a reservation only after its complete actual preload.
// There is currently no immutable-cache admission in the RV64 shared or fallback
// compiler. This infrastructure must not be used to seed/reseed values at calls
// or to pretend ordinary allocator values have immutable lifetimes.
func (f *target) checkImmutable(reg enc.Reg) {
	if reg == enc.Zero || reg > enc.X31 {
		panic("regalloccheck: invalid immutable GP reservation")
	}
	f.immutableGPMask |= uint32(1) << reg
	f.checkObserveGPWrites()
}

func (f *target) checkObserveGPWrites() {
	if !f.gpObserverActive {
		f.gpObserverActive = true
		f.gpObserverPrevious = f.a.ObserveGPWrites(func(mask uint32) {
			if mask&f.immutableGPMask != 0 {
				panic(fmt.Sprintf("regalloccheck: immutable GP reservation overwritten (mask %#x)", mask&f.immutableGPMask))
			}
			if hit := mask & f.readCheck.gp; hit != 0 {
				panic(fmt.Sprintf("regalloccheck: function %d pc %d: read-only GP input overwritten (mask %#x, code offset %d)", f.mode.functionIndex, f.sourcePC, hit, f.a.Len()))
			}
			if f.gpObserverPrevious != nil {
				f.gpObserverPrevious(mask)
			}
		})
	}
}

// Read guards cover only preparation. Restore them before a consuming operation
// may write its result into an input register. GP x0 is constant; FP f0 is not.
type regallocReadMask struct{ gp, fp uint32 }

func (f *target) checkProtectRead(reg uint8, fp bool) regallocReadMask {
	if reg > 31 {
		panic("regalloccheck: invalid read-only input register")
	}
	saved := f.readCheck
	if fp {
		f.readCheck.fp |= uint32(1) << reg
		if !f.fpObserverActive {
			f.fpObserverActive = true
			f.fpObserverPrevious = f.a.ObserveFPWrites(func(mask uint32) {
				if hit := mask & f.readCheck.fp; hit != 0 {
					panic(fmt.Sprintf("regalloccheck: function %d pc %d: read-only FP input overwritten (mask %#x, code offset %d)", f.mode.functionIndex, f.sourcePC, hit, f.a.Len()))
				}
				if f.fpObserverPrevious != nil {
					f.fpObserverPrevious(mask)
				}
			})
		}
	} else if reg != uint8(enc.Zero) {
		f.readCheck.gp |= uint32(1) << reg
		f.checkObserveGPWrites()
	}
	return saved
}

func (f *target) checkRestoreRead(saved regallocReadMask) { f.readCheck = saved }

func (f *target) checkEndLifetimes() {
	if f.fpObserverActive {
		f.a.ObserveFPWrites(f.fpObserverPrevious)
		f.fpObserverPrevious = nil
		f.fpObserverActive = false
	}
	f.readCheck = regallocReadMask{}
	if f.gpObserverActive {
		f.a.ObserveGPWrites(f.gpObserverPrevious)
		f.gpObserverPrevious = nil
		f.gpObserverActive = false
	}
	f.immutableGPMask = 0
}

// Terminal code can retire the current path's reservations after its last use.
// The emitter must defer restoration before emitting another body path.
type regallocGPWriteMask = uint32

func (f *target) checkTerminalGPWrites() regallocGPWriteMask {
	saved := f.immutableGPMask
	f.immutableGPMask = 0
	return saved
}
func (f *target) checkRestoreGPWrites(saved regallocGPWriteMask) { f.immutableGPMask = saved }
