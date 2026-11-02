//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
)

// These are the pre-split RV64 sizes and offsets, captured by execution at
// 563631afba41d58f6de9eb7fd31bb8aa3f16f55e. The target declaration must preserve
// every existing field, including the exact receipt and exception state slots.
func TestManagedFunctionRV64Shape(t *testing.T) {
	for _, shape := range []struct {
		value  any
		size   uintptr
		fields []struct {
			name         string
			offset, size uintptr
		}
	}{
		{managedFunctionCollector{}, 200, []struct {
			name         string
			offset, size uintptr
		}{
			{"mu", 0, 8},
			{"graph", 8, 8},
			{"collector", 16, 8},
			{"typeID", 24, 4},
			{"envs", 32, 8},
			{"byOwner", 40, 8},
			{"byDescriptor", 48, 8},
			{"shadows", 56, 8},
			{"extraShadows", 64, 8},
			{"aggregate", 72, 8},
			{"globalOwners", 80, 8},
			{"tableOwners", 88, 8},
			{"hostEnvSlots", 96, 8},
			{"conversionHostSlots", 104, 8},
			{"conversionFloating", 112, 8},
			{"hostFreeSlots", 120, 24},
			{"authorized", 144, 1},
			{"liveOwners", 152, 8},
			{"reclaimed", 160, 24},
			{"closed", 184, 1},
			{"exception", 192, 8},
		}},
		{managedFunctionMark{}, 208, []struct {
			name         string
			offset, size uintptr
		}{
			{"graph", 0, 8},
			{"epoch", 8, 8},
			{"domains", 16, 24},
			{"byCollector", 40, 8},
			{"live", 48, 8},
			{"owners", 56, 24},
			{"ownerAt", 80, 8},
			{"ownerSlot", 88, 8},
			{"edges", 96, 8},
			{"incoming", 104, 8},
			{"err", 112, 16},
			{"complete", 128, 1},
			{"members", 136, 24},
			{"receipts", 160, 24},
			{"nativeCursor", 184, 16},
			{"nativeDone", 200, 1},
		}},
	} {
		typ := reflect.TypeOf(shape.value)
		if typ.Size() != shape.size || typ.Align() != 8 {
			t.Fatalf("%s size/alignment=%d/%d, want %d/8", typ, typ.Size(), typ.Align(), shape.size)
		}
		for _, want := range shape.fields {
			field, ok := typ.FieldByName(want.name)
			if !ok {
				t.Fatalf("%s lost %s", typ, want.name)
			}
			current := typ
			var offset uintptr
			for _, index := range field.Index {
				nested := current.Field(index)
				offset += nested.Offset
				current = nested.Type
			}
			if offset != want.offset || field.Type.Size() != want.size {
				t.Fatalf("%s.%s offset/size=%d/%d, want %d/%d", typ, want.name, offset, field.Type.Size(), want.offset, want.size)
			}
		}
	}
}
