//go:build arm64

package arm64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// packedStructSetShortcut consumes a validator-proven concrete struct operand.
// Public ingress and validated stores preserve its structural type/domain.
// Packed numeric mutable fields need no reference write barrier. All physical
// checks precede the truncating store; failure keeps the original typed helper.
func (f *fn) packedStructSetShortcut(typeIndex, fieldIndex uint32, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || int(fieldIndex) >= len(layout.FieldLayout) || int(fieldIndex) >= len(layout.Type.Comp.Fields) {
		return -1
	}
	declared := layout.Type.Comp.Fields[fieldIndex]
	field := layout.FieldLayout[fieldIndex]
	if declared.Mut() != wasm.Var || !declared.Storage().Packed() || !field.DirectAccess || field.Size != 1 && field.Size != 2 {
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
	// Large-offset address formation may use X16. Load the value after it.
	f.ld32(X16, SP, f.spillOff(argSlot+1))
	encoded := false
	if field.Size == 1 {
		encoded = f.a.Strb(X16, base, disp)
	} else {
		encoded = f.a.Strh(X16, base, disp)
	}
	if !encoded {
		panic("arm64: packed struct store offset was not encodable")
	}
	done := f.a.Branch()
	f.patchBranch19(fallback, f.a.Len())
	f.relocs = append(f.relocs, callReloc{at: f.compactCallRelocField(site), target: f.compactCallRelocField(f.a.Len() - site), gcObject: true})
	f.stats.peep("gc-struct-packed-set-native")
	return done
}
