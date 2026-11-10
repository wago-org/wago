//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"runtime"
	"strings"
	"testing"
)

func TestPrivateViewFixedOwnerActualEntry(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32)(result i32)))
 (func (export "run") (param i64 i32)(result i32)
 local.get 0 i32.wrap_i64 local.get 1 i32.add call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			var s *PreparedSession
			var retained Caller
			panicNext, closeNext := false, false
			step := func(v int32) int32 {
				var pcs [32]uintptr
				frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
				owner, bridge := false, false
				for {
					f, more := frames.Next()
					owner = owner || strings.Contains(f.Function, "tryPrivateViewFixed2")
					bridge = bridge || strings.Contains(f.Function, "inlineHostIntegerViewContextEnter")
					if !more {
						break
					}
				}
				if !owner || !bridge {
					panic("view owner/bridge absent")
				}
				if _, err := s.Invoke2(0, 0); err == nil {
					panic("view reentry admitted")
				}
				if inlineWagoGrow(20) != 210 {
					panic("view stack growth")
				}
				runtime.GC()
				if closeNext {
					s.Close()
				}
				if panicNext {
					panicNext = false
					panic("private-view-panic")
				}
				return v + 1
			}
			var callback any = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
			if kind == "Caller" {
				callback = func(caller Caller, call HostCall) {
					if retained.valid() || !caller.valid() {
						panic("Caller lifetime")
					}
					retained = caller
					call.SetI32(0, step(call.I32(0)))
				}
			}
			imports := NewImports()
			imports.HostFunc("env", "step", callback).Params(ValI32).Results(ValI32)
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
			if s.state.privateHost == nil || s.state.privateHost.viewOwner == nil {
				t.Fatal("view owner not admitted")
			}
			check := func(a uint64) {
				got, err := s.Invoke2(a, 0xffff000000000002)
				want := uint64(uint32(a) + 3)
				if err != nil || len(got) != 1 || got[0] != want || s.state.active.Load() || retained.valid() || !s.state.privateHost.activation.invocation.empty() {
					t.Fatalf("input%x got%v err%v want%x", a, got, err, want)
				}
			}
			for _, a := range []uint64{0, 0xffffffff, 0x80000000, 0xabcdef017fffffff} {
				check(a)
			}
			panicNext = true
			var recovered any
			func() { defer func() { recovered = recover() }(); _, _ = s.Invoke2(0, 0) }()
			if recovered != "private-view-panic" || s.state.active.Load() || retained.valid() {
				t.Fatalf("panic%v active%v", recovered, s.state.active.Load())
			}
			check(41)
			closeNext = true
			check(0)
			if !s.state.closed.Load() {
				t.Fatal("view deferred close not applied")
			}
		})
	}
}
