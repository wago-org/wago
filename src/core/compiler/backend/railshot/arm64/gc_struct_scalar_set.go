//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// scalarStructSetShortcut consumes a validator-proven concrete struct operand.
// Public ingress and validated stores preserve its structural type/domain. Only
// numeric mutable fields are admitted; they need no reference write barrier.
// All physical checks precede the store. Failure keeps the original typed helper.
func (f *fn) scalarStructSetShortcut(typeIndex, fieldIndex uint32, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || int(fieldIndex) >= len(layout.FieldLayout) || int(fieldIndex) >= len(layout.Type.Comp.Fields) {
		return -1
	}
	declared := layout.Type.Comp.Fields[fieldIndex]
	storage := declared.Storage()
	field := layout.FieldLayout[fieldIndex]
	if declared.Mut() != wasm.Var || storage.Packed() || storage.Val().Kind() != wasm.ValNum || !field.DirectAccess || field.Size != 4 && field.Size != 8 {
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
	base, disp := Reg(X10), int32(offset)
	if offset%uint64(field.Size) != 0 || offset > 0xfff*uint64(field.Size) {
		f.leaDisp(X9, X10, int32(offset), true)
		base, disp = X9, 0
	}
	// Large-offset address formation may use X16. Load the value after it.
	if field.Size == 4 {
		f.ld32(X16, SP, f.spillOff(argSlot+1))
		f.st32(base, disp, X16)
	} else {
		f.ld64(X16, SP, f.spillOff(argSlot+1))
		f.st64(base, disp, X16)
	}
	done := f.a.Branch()
	f.patchBranch19(fallback, f.a.Len())
	f.relocs = append(f.relocs, callReloc{at: f.compactCallRelocField(site), target: f.compactCallRelocField(f.a.Len() - site), gcObject: true})
	f.stats.peep("gc-struct-scalar-set-native")
	return done
}
