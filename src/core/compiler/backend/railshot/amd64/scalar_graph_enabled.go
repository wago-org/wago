//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

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
		return regalloccheck.Register(regalloccheck.GP, uint8(RAX))
	}
	return regalloccheck.Slot(f.spillOff(0))
}

func (f *fn) ScalarGraphLocalOffset(i int) int32 { return f.localOff(i) }
func (f *fn) ScalarGraphBranchDestination(site int) (int, bool, bool) {
	b := f.a.B
	if site < 1 || site > len(b)-4 {
		return 0, false, false
	}
	conditional := site >= 2 && b[site-2] == 0x0f && b[site-1]&0xf0 == 0x80
	if !conditional && b[site-1] != 0xe9 {
		return 0, false, false
	}
	return site + 4 + int(int32(binary.LittleEndian.Uint32(b[site:]))), conditional, true
}
