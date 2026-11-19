//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// packedStructGetShortcut reads a validator-proven concrete struct field.
// Public structural ingress and validated stores preserve the type/domain and
// subtype field-prefix contract. The leaf checks the complete physical extent;
// failed checks retain the original typed helper, including null/stale traps.
func (f *fn) packedStructGetShortcut(typeIndex, fieldIndex uint32, signed bool, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || int(fieldIndex) >= len(layout.FieldLayout) || int(fieldIndex) >= len(layout.Type.Comp.Fields) {
		return -1
	}
	storage := layout.Type.Comp.Fields[fieldIndex].Storage()
	field := layout.FieldLayout[fieldIndex]
	if !storage.Packed() || !field.DirectAccess || field.Size != 1 && field.Size != 2 {
		return -1
	}
	offset := uint64(gc.PayloadOffset) + uint64(field.Offset)
	if offset+uint64(field.Size) > math.MaxInt32 {
		return -1
	}
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	f.ld64(X16, SP, f.spillOff(argSlot))
	f.a.MovImm32(X11, int32(offset+uint64(field.Size)))
	site := f.a.Bl()
	fallback := f.a.Cbnz64(X17)
	base, disp := Reg(X10), uint32(offset)
	if offset%uint64(field.Size) != 0 || offset > 0xfff*uint64(field.Size) {
		f.leaDisp(X9, X10, int32(offset), true)
		base, disp = X9, 0
	}
	if field.Size == 1 {
		if !f.a.Ldrb(X16, base, disp) {
			panic("arm64: packed byte field offset is not encodable")
		}
		if signed {
			f.a.Sxtb(X16, X16, true)
		}
	} else {
		if !f.a.Ldrh(X16, base, disp) {
			panic("arm64: packed half field offset is not encodable")
		}
		if signed {
			f.a.Sxth(X16, X16, true)
		}
	}
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, X16)
	done := f.a.Branch()
	f.patchBranch19(fallback, f.a.Len())
	f.relocs = append(f.relocs, callReloc{at: f.compactCallRelocField(site), target: f.compactCallRelocField(f.a.Len() - site), gcObject: true})
	f.stats.peep("gc-struct-packed-get-native")
	return done
}
