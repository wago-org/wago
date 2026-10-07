//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"runtime"
	"strings"
	"testing"
)

func TestPrivateFixedOwnerActualEntry(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32)(result i32)))
 (func (export "run") (param i64 i32)(result i32)
 local.get 0 i32.wrap_i64 local.get 1 i32.add call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var s *PreparedSession
	panicNext, closeNext := false, false
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		calls++
		var pcs [32]uintptr
		frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
		found := false
		for {
			frame, more := frames.Next()
			if strings.Contains(frame.Function, "tryPrivateFixed2") {
				found = true
			}
			if !more {
				break
			}
		}
		if !found {
			panic("specialized owner absent")
		}
		if _, err := s.Invoke2(0, 0); err == nil {
			panic("reentry admitted")
		}
		if inlineWagoGrow(20) != 210 {
			panic("stack growth")
		}
		runtime.GC()
		if closeNext {
			s.Close()
		}
		if panicNext {
			panicNext = false
			panic("fixed-owner-panic")
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	s, err = fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.state.privateHost == nil || s.state.privateHost.owner == nil {
		t.Fatal("owner not admitted")
	}
	for _, a := range []uint64{0, 0xffffffff, 0x80000000, 0xabcdef017fffffff} {
		got, err := s.Invoke2(a, 0xffff000000000002)
		want := uint64(uint32(a) + 3)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("input%x got%v err%v want%x", a, got, err, want)
		}
	}
	panicNext = true
	var recovered any
	func() { defer func() { recovered = recover() }(); _, _ = s.Invoke2(0, 0) }()
	if recovered != "fixed-owner-panic" || s.state.active.Load() {
		t.Fatalf("panic%v active%v", recovered, s.state.active.Load())
	}
	closeNext = true
	got, err := s.Invoke2(0, 0)
	if err != nil || len(got) != 1 || got[0] != 1 || !s.state.closed.Load() || s.state.active.Load() || calls != 6 {
		t.Fatalf("close got%v err%v calls%d", got, err, calls)
	}
}
