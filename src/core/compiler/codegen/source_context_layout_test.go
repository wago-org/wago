//go:build !wago_regalloccheck

package codegen

import (
	"reflect"
	"testing"
)

type optionsBeforeSourceContext struct {
	Runtime RuntimeABI
	Heap    HeapABI
	Module  ModuleInfo
}

func TestSourceContextOrdinaryOptionsLayout(t *testing.T) {
	old, now := reflect.TypeOf(optionsBeforeSourceContext{}), reflect.TypeOf(Options{})
	if now.Name() != "Options" || now.PkgPath() != old.PkgPath() || old.Size() != now.Size() || old.Align() != now.Align() || old.NumField() != now.NumField() || now.NumMethod() != 0 || reflect.PointerTo(now).NumMethod() != 0 {
		t.Fatalf("ordinary Options identity/layout/methods changed: %v/%v", old, now)
	}
	for i := 0; i < old.NumField(); i++ {
		a, b := old.Field(i), now.Field(i)
		if a.Name != b.Name || a.Type != b.Type || a.Offset != b.Offset || a.Tag != b.Tag || a.Anonymous != b.Anonymous || a.PkgPath != b.PkgPath {
			t.Fatalf("ordinary Options field changed: %s", a.Name)
		}
	}
}
