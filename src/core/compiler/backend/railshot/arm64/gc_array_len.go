//go:build arm64

package arm64

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// arrayLenShortcut reads a validator-proven arrayref from its canonical slot.
// The checked leaf cannot collect. Null, stale and malformed handles retain
// the original helper and its traps. GC hints already require an LR save and
// call-safe local homes for this function, including when the site is inlined.
func (f *fn) arrayLenShortcut(argSlot int) int {
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	f.ld64(X16, SP, f.spillOff(argSlot))
	f.a.MovImm32(X11, int32(gc.HeaderSize))
	site := f.a.Bl()
	fallback := f.a.Cbnz64(X17)
	f.ld32(X16, X10, 8)
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, X16)
	done := f.a.Branch()
	f.patchBranch19(fallback, f.a.Len())
	// Prefix compaction moves the call and helper together. Keep their distance
	// so module finalization can retain the helper if the leaf is out of BL range.
	f.relocs = append(f.relocs, callReloc{at: f.compactCallRelocField(site), target: f.compactCallRelocField(f.a.Len() - site), gcObject: true})
	f.stats.peep("gc-array-len-native")
	return done
}
