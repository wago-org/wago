//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const regallocCheckEnabled = true

type regallocFnState struct {
	immutableGPMask    uint32
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
	if !f.gpObserverActive {
		f.gpObserverActive = true
		f.gpObserverPrevious = f.a.ObserveGPWrites(func(mask uint32) {
			if mask&f.immutableGPMask != 0 {
				panic(fmt.Sprintf("regalloccheck: immutable GP reservation overwritten (mask %#x)", mask&f.immutableGPMask))
			}
			if f.gpObserverPrevious != nil {
				f.gpObserverPrevious(mask)
			}
		})
	}
}

func (f *target) checkEndLifetimes() {
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
