//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestPrivateNumericSessionContextAndDeferredClose(t *testing.T) {
	if !privateNumericSessionEnabled || codeProfileEnabled || runtime.GOARCH == "arm64" && !armPrivateNumericSessionEnabled {
		t.Skip("private session prototype disabled")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			var session *PreparedSession
			calls := 0
			closeNext := false
			step := func(v int32) int32 {
				calls++
				if closeNext {
					closeNext = false
					copy := *session
					copy.Close()
				}
				return v + 1
			}
			var callback any = step
			if kind == "HostCall" {
				callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
			}
			if kind == "Caller" {
				callback = func(caller Caller, call HostCall) {
					if !caller.valid() {
						t.Fatal("inactive Caller")
					}
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
			session, err = fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			h := session.state.privateHost
			if h == nil {
				if p := in.eng.PreparedScalarHost(); p == nil || !p.DetachedNumericContext() || runtime.GOARCH == "amd64" && !p.IntegerGuestContext() {
					t.Skip("private bridge unavailable")
				}
				t.Fatal("eligible session did not cache private bridge")
			}
			intercepted := 0
			if h.directI32 != nil {
				original := h.directI32
				h.directI32 = func(v int32) int32 { intercepted++; return original(v) }
			} else if h.scalar != nil {
				original := h.scalar
				h.scalar = func(context unsafe.Pointer, a0, a1 uint64) uint64 {
					intercepted++
					if context != unsafe.Pointer(&h.activation.boundedTypedHostActivation) {
						t.Fatal("wrong persistent scalar context")
					}
					return original(context, a0, a1)
				}
			} else {
				original := h.view
				h.view = func(context unsafe.Pointer, args, results []uint64) {
					intercepted++
					if context != unsafe.Pointer(&h.activation) {
						t.Fatal("wrong persistent view context")
					}
					original(context, args, results)
				}
			}
			for i := 0; i < 2; i++ {
				got, err := session.Invoke2(2, 0)
				if err != nil || len(got) != 1 || got[0] != 2 {
					t.Fatalf("invoke %v %v", got, err)
				}
				if !h.activation.invocation.empty() || session.state.active.Load() {
					t.Fatal("persistent context retained invocation state")
				}
			}
			closeNext = true
			got, err := session.Invoke2(3, 0)
			if err != nil || len(got) != 1 || got[0] != 3 {
				t.Fatalf("close during call %v %v", got, err)
			}
			if !session.state.closed.Load() || session.state.active.Load() || calls != 7 || intercepted != 7 || in.invocationState.Load() != 0 {
				t.Fatal("deferred close or cached dispatcher failed")
			}
			if _, err := session.Invoke2(1, 0); err == nil {
				t.Fatal("closed private session invoked guest")
			}
		})
	}
}

func TestDirectIntegerI32SessionClosureAndPanic(t *testing.T) {
	if runtime.GOARCH != "amd64" || !directIntegerI32Enabled || !privateNumericSessionEnabled || !integerNumericHostEnabled || !detachedNumericHostEnabled || codeProfileEnabled {
		t.Skip("direct adapter disabled")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32)(result i32)))
 (func (export "run") (param i32)(result i32) local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	captured := new(int32)
	*captured = 0x12345678
	panicNext := false
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		calls++
		if panicNext {
			panicNext = false
			panic("direct-i32-panic")
		}
		if inlineWagoGrow(16) != 136 {
			panic("stack growth")
		}
		runtime.GC()
		return v ^ *captured
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if p := in.eng.PreparedScalarHost(); p == nil || !p.DetachedNumericContext() || !p.IntegerGuestContext() {
		t.Skip("private integer bridge unavailable")
	}
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.state.privateHost == nil || session.state.privateHost.directI32 == nil {
		t.Fatal("direct adapter not admitted")
	}
	for _, v := range []uint32{0, 0xffffffff, 0x80000000, 0x7fffffff} {
		got, err := session.Invoke1(uint64(v))
		want := uint64(v ^ uint32(*captured))
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("input %#x got %v %v want %#x", v, got, err, want)
		}
	}
	panicNext = true
	var recovered any
	func() { defer func() { recovered = recover() }(); _, _ = session.Invoke1(0) }()
	if recovered != "direct-i32-panic" {
		t.Fatalf("callback panic changed: %v", recovered)
	}
	if session.state.active.Load() || !session.state.privateHost.activation.invocation.empty() {
		t.Fatal("panic retained activation")
	}
	got, err := session.Invoke1(0xffffffff)
	if err != nil || len(got) != 1 || got[0] != uint64(uint32(0xffffffff)^uint32(*captured)) || calls != 6 {
		t.Fatalf("after panic %v %v calls=%d", got, err, calls)
	}
}
