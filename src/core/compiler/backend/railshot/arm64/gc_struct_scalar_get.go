//go:build arm64

package arm64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// scalarStructGetShortcut consumes a validator-proven concrete struct operand.
// Public reference ingress checks its structural type/domain before execution;
// validated local/global stores and subtype prefixes preserve that contract.
// The shared leaf checks physical extent and cannot collect. Failed resolution
// retains the original typed helper, including null and stale-handle failures.
func (f *fn) scalarStructGetShortcut(typeIndex, fieldIndex uint32, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || int(fieldIndex) >= len(layout.FieldLayout) || int(fieldIndex) >= len(layout.Type.Comp.Fields) {
		return -1
	}
	storage := layout.Type.Comp.Fields[fieldIndex].Storage()
	field := layout.FieldLayout[fieldIndex]
	if storage.Packed() || storage.Val().Kind() != wasm.ValNum || !field.DirectAccess || field.Size != 4 && field.Size != 8 {
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
	if field.Size == 4 {
		f.ld32(X16, base, disp)
	} else {
		f.ld64(X16, base, disp)
	}
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, X16)
	done := f.a.Branch()
	f.patchBranch19(fallback, f.a.Len())
	f.relocs = append(f.relocs, callReloc{at: f.compactCallRelocField(site), target: f.compactCallRelocField(f.a.Len() - site), gcObject: true})
	f.stats.peep("gc-struct-scalar-get-native")
	return done
}
