//go:build linux && !tinygo && !wago_profile

package riscv64

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestProfileOrdinaryHoldersPreserveLayout(t *testing.T) {
	if unsafe.Sizeof(profileModuleState{}) != 0 || unsafe.Sizeof(profileMode{}) != 0 {
		t.Fatal("ordinary profiling holders retain state")
	}
	for _, value := range []any{Compiled{}, emissionMode{}, swarModuleImage{}} {
		now := reflect.TypeOf(value)
		if !now.Field(0).Anonymous || now.Field(0).Type.Size() != 0 || now.Field(0).Offset != 0 {
			t.Fatal("optional holder must be empty and first", now)
		}
		fields := make([]reflect.StructField, now.NumField()-1)
		for i := range fields {
			fields[i] = now.Field(i + 1)
		}
		before := reflect.StructOf(fields)
		if now.Size() != before.Size() || now.Align() != before.Align() {
			t.Fatal("ordinary layout grew", now, before.Size(), now.Size())
		}
		for i := range fields {
			if now.Field(i+1).Offset != before.Field(i).Offset {
				t.Fatal("ordinary field shifted", now, fields[i].Name)
			}
		}
	}
	if got := testing.AllocsPerRun(10, func() {
		p := newProfileModule(false, false, MaxFunctions)
		if err := p.prepareProfile(0); err != nil {
			panic(err)
		}
	}); got != 0 {
		t.Fatal("ordinary disabled capture allocated", got)
	}
}
