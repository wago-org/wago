//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// scalarArraySetShortcut consumes a validator-proven concrete array operand.
// Public ingress checks its type/domain; the nonallocating shared leaf checks
// the full handle and physical object. All checks precede the write; failed
// checks retain the typed helper and its reference/type/bounds behavior.
func (f *fn) scalarArraySetShortcut(typeIndex uint32, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray)
	if !ok || !layout.PointerFree || !layout.ElemLayout.DirectAccess || !layout.ElemLayout.Mutable {
		return -1
	}
	switch layout.ElemLayout.Storage {
	case gc.StorageI32, gc.StorageI64, gc.StorageF32, gc.StorageF64:
	default:
		return -1
	}
	size := layout.ElemLayout.Size
	if size != 4 && size != 8 {
		return -1
	}
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	f.ld64(X16, SP, f.spillOff(argSlot))
	f.a.MovImm32(X11, int32(gc.PayloadOffset))
	site := f.a.Bl()
	invalidObject := f.a.Cbnz64(X17)
	f.ld32(X16, SP, f.spillOff(argSlot+1))
	f.ld32(X17, X10, 8) // logical array length
	f.cmpRR(X16, X17, false)
	invalidIndex := f.a.Bcond(condAE)
	shift := uint8(2)
	if size == 8 {
		shift = 3
	}
	// W loads zero-extend the index. Its full byte extent fits within 2^36;
	// keep the arithmetic and comparison wide to reject large indexes.
	f.a.MovImm64(X9, uint64(gc.PayloadOffset)+uint64(size))
	f.a.AddShifted(X9, X9, X16, shift, false)
	f.ld32(X17, X10, 4) // physical object bytes, checked by the leaf
	f.cmpRR(X9, X17, true)
	invalidExtent := f.a.Bcond(condA)
	f.a.AddShifted(X10, X10, X16, shift, false)
	if size == 4 {
		f.ld32(X16, SP, f.spillOff(argSlot+2))
		f.st32(X10, int32(gc.PayloadOffset), X16)
	} else {
		f.ld64(X16, SP, f.spillOff(argSlot+2))
		f.st64(X10, int32(gc.PayloadOffset), X16)
	}
	done := f.a.Branch()
	for _, branch := range [...]int{invalidObject, invalidIndex, invalidExtent} {
		f.patchBranch19(branch, f.a.Len())
	}
	f.relocs = append(f.relocs, callReloc{at: f.compactCallRelocField(site), target: f.compactCallRelocField(f.a.Len() - site), gcObject: true})
	f.stats.peep("gc-array-general-scalar-set-native")
	return done
}
