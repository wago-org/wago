//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"runtime"
	"testing"
)

func TestPreparedDirectIntegerI32ClosureAndPanic(t *testing.T) {
	if (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") || !directIntegerI32Enabled || !privateNumericSessionEnabled || !integerNumericHostEnabled || !detachedNumericHostEnabled || codeProfileEnabled {
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
		t.Fatal("private integer bridge unavailable")
	}
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint32{0, 0xffffffff, 0x80000000, 0x7fffffff} {
		got, err := fn.Invoke(uint64(v))
		want := uint64(v ^ uint32(*captured))
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("input %#x got %v %v want %#x", v, got, err, want)
		}
	}
	panicNext = true
	var recovered any
	func() { defer func() { recovered = recover() }(); _, _ = fn.Invoke(0) }()
	if recovered != "direct-i32-panic" {
		t.Fatalf("callback panic changed: %v", recovered)
	}
	if in.invocationState.Load() != 0 {
		t.Fatal("panic retained activation")
	}
	got, err := fn.Invoke(0xffffffff)
	if err != nil || len(got) != 1 || got[0] != uint64(uint32(0xffffffff)^uint32(*captured)) || calls != 6 {
		t.Fatalf("after panic %v %v calls=%d", got, err, calls)
	}
}

func TestPreparedFallbackIntegerI32ClosureAndPanic(t *testing.T) {
	if (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") || !directIntegerI32Enabled || !privateNumericSessionEnabled || !integerNumericHostEnabled || !detachedNumericHostEnabled || codeProfileEnabled {
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
		t.Fatal("private integer bridge unavailable")
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
	session.state.privateHost = nil
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
	if session.state.active.Load() {
		t.Fatal("panic retained activation")
	}
	got, err := session.Invoke1(0xffffffff)
	if err != nil || len(got) != 1 || got[0] != uint64(uint32(0xffffffff)^uint32(*captured)) || calls != 6 {
		t.Fatalf("after panic %v %v calls=%d", got, err, calls)
	}
}
