//go:build linux && !tinygo && !wago_regalloccheck

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"reflect"
	"testing"
	"unsafe"
)

func TestRegallocOrdinaryTargetLayout(t *testing.T) {
	f := target{}
	if unsafe.Offsetof(f.regallocFnState) != 0 {
		t.Fatal("ordinary placeholder must lead target")
	}
	f.a.ObserveGPWrites(func(uint32) { t.Fatal("ordinary target retained observer") })
	f.a.Constant64(enc.X9, 42)
	f.checkImmutable(enc.X9)
	saved := f.checkTerminalGPWrites()
	f.a.Constant64(enc.X9, 0)
	f.checkRestoreGPWrites(saved)
	f.a.Constant64(enc.X9, 1)
	f.checkEndLifetimes()
	typ := reflect.TypeOf(target{})
	fields := []reflect.StructField{}
	for i := 1; i < typ.NumField(); i++ {
		f := typ.Field(i)
		f.Offset = 0
		fields = append(fields, f)
	}
	bare := reflect.StructOf(fields)
	if typ.Size() != bare.Size() || typ.Align() != bare.Align() || unsafe.Sizeof(regallocFnState{}) != 0 || unsafe.Sizeof(regallocGPWriteMask{}) != 0 {
		t.Fatal("ordinary target/mask layout grew")
	}
	for i := 1; i < typ.NumField(); i++ {
		if typ.Field(i).Offset != bare.Field(i-1).Offset {
			t.Fatal("ordinary target offset changed")
		}
	}
}
