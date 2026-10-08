//go:build arm64 && wago_regalloccheck && !tinygo && !wago_profile

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
)

// Compose with the established immutable-GP and transfer observers. The graph
// owns no production state and restores both enclosing observers on every exit.
func (f *fn) ObserveScalarGraph(effect func(regalloccheck.Effect), writes func(uint32)) func() {
	var previousEffect func(regalloccheck.Effect)
	previousEffect = f.a.ObserveRegalloc(func(e regalloccheck.Effect) {
		if previousEffect != nil {
			previousEffect(e)
		}
		effect(e)
	})
	var previousWrites func(uint32)
	previousWrites = f.a.ObserveGPWrites(func(mask uint32) {
		if previousWrites != nil {
			previousWrites(mask)
		}
		writes(mask)
	})
	return func() { f.a.ObserveRegalloc(previousEffect); f.a.ObserveGPWrites(previousWrites) }
}
func (f *fn) ScalarGraphReturnLocation() regalloccheck.Location {
	if f.singleRegResult {
		return regalloccheck.Register(regalloccheck.GP, uint8(X0))
	}
	return regalloccheck.Slot(f.spillOff(0))
}

func (f *fn) ScalarGraphLocalOffset(i int) int32 { return f.localOff(i) }
func (f *fn) ScalarGraphBranchDestination(site int) (int, bool, bool) {
	if site < 0 || site > len(f.a.B)-4 || site%4 != 0 {
		return 0, false, false
	}
	op := binary.LittleEndian.Uint32(f.a.B[site:])
	if op&0xfc000000 == 0x14000000 {
		return site + int(int32(op<<6)>>6)*4, false, true
	}
	if op&0xff000010 == 0x54000000 || op&0x7e000000 == 0x34000000 {
		return site + int(int32(op<<8)>>13)*4, true, true
	}
	return 0, false, false
}
