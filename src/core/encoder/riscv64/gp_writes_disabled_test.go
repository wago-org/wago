//go:build !wago_regalloccheck && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"reflect"
	"testing"
	"unsafe"
)

func TestGPWritesOrdinaryLayoutAndInertObservers(t *testing.T) {
	var a Asm
	if unsafe.Offsetof(a.regallocState) != 0 {
		t.Fatal("ordinary placeholder must lead Asm")
	}
	if a.ObserveGPWrites(func(uint32) { t.Fatal("ordinary GP observer") }) != nil {
		t.Fatal("saved ordinary observer")
	}
	if a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("ordinary transfer observer") }) != nil {
		t.Fatal("saved ordinary observer")
	}
	a.Addw(X9, X10, X11)
	a.Ld(X9, SP, 16)
	a.JumpLong(true)
	if a.ObserveGPWrites(nil) != nil || a.ObserveRegalloc(nil) != nil {
		t.Fatal("retained ordinary observer")
	}
	typ := reflect.TypeOf(a)
	fields := []reflect.StructField{}
	for i := 1; i < typ.NumField(); i++ {
		f := typ.Field(i)
		f.Offset = 0
		fields = append(fields, f)
	}
	bare := reflect.StructOf(fields)
	if typ.Size() != bare.Size() || typ.Align() != bare.Align() || unsafe.Sizeof(regallocState{}) != 0 {
		t.Fatal("ordinary encoder layout grew")
	}
	for i := 1; i < typ.NumField(); i++ {
		if typ.Field(i).Offset != bare.Field(i-1).Offset {
			t.Fatal("ordinary encoder field offset changed")
		}
	}
}
