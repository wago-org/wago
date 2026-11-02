//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestManagedFunctionHostShape(t *testing.T) {
	for _, state := range []any{managedFunctionCollectorTarget{}, managedFunctionMarkTarget{}} {
		typ := reflect.TypeOf(state)
		if typ.Size() != 0 || typ.NumField() != 0 {
			t.Fatalf("host target state %s carries unused storage", typ)
		}
	}
	for _, shape := range []struct {
		value  any
		absent string
		size64 uintptr
	}{
		{managedFunctionCollector{}, "exception", 192},
		{managedFunctionMark{}, "receipts", 168},
	} {
		typ := reflect.TypeOf(shape.value)
		if _, ok := typ.FieldByName(shape.absent); ok {
			t.Fatalf("host %s still carries RV64 %s", typ, shape.absent)
		}
		if unsafe.Sizeof(uintptr(0)) == 8 && typ.Size() != shape.size64 {
			t.Fatalf("host %s size=%d, want %d", typ, typ.Size(), shape.size64)
		}
	}
}
