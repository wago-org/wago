//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

func TestCachedBoundedViewContextReuseAndInvalidation(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains the general driver")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			callback := func(call HostCall) { calls++; call.SetI32(0, call.I32(0)+1) }
			var fn any = callback
			if kind == "Caller" {
				fn = func(caller Caller, call HostCall) {
					if !caller.valid() {
						t.Fatal("inactive Caller")
					}
					callback(call)
				}
			}
			imports := NewImports()
			imports.HostFunc("env", "step", fn).Params(ValI32).Results(ValI32)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			invoke := func() {
				t.Helper()
				if got, err := in.Invoke("run", 2, 0); err != nil || len(got) != 1 || got[0] != 2 {
					t.Fatalf("invoke: %v, %v", got, err)
				}
			}
			invoke()
			invoke()
			state := in.ensurePluginState()
			ic := in.findInvokeCache("run")
			if ic == nil || !numericContextDetached(in) && state.boundedViewVersion == 0 {
				t.Fatal("warm view entry did not record context version")
			}
			version := state.nativeContextVersion.Load()
			invoke()
			if state.nativeContextVersion.Load() != version {
				t.Fatal("unchanged private context was rebound")
			}
			in.acquireInstanceNativeStateForHostAccess().Unlock()
			version = state.nativeContextVersion.Load()
			invoke()
			if !numericContextDetached(in) && state.nativeContextVersion.Load() <= version {
				t.Fatal("guarded host access did not force rebind")
			}
			if calls != 8 || state.invocationID != 0 || in.invocationState.Load() != 0 || state.activations.boundedID.Load() != 0 {
				t.Fatal("incorrect callback count or retained invocation authority")
			}
		})
	}
}

func TestCachedBoundedViewContextAcrossExportsAndEviction(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains the general driver")
	}
	module := `(module (import "env" "step" (func $step (param i32) (result i32)))`
	for i := 0; i < 6; i++ {
		module += fmt.Sprintf(`(func (export "f%d") (param i32) (result i32)
 local.get 0 call $step i32.const %d i32.add)`, i, i)
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, module+")"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(call HostCall) {
		calls++
		call.SetI32(0, call.I32(0)+1)
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	invoke := func(index int) {
		t.Helper()
		got, err := in.Invoke(fmt.Sprintf("f%d", index), I32(-17))
		if err != nil || len(got) != 1 || AsI32(got[0]) != int32(-16+index) {
			t.Fatalf("export f%d: %v, %v", index, got, err)
		}
	}
	for round := 0; round < 2; round++ {
		for i := 0; i < 6; i++ {
			invoke(i)
			invoke(i)
		}
	}
	state := in.ensurePluginState()
	version := state.nativeContextVersion.Load()
	for _, index := range []int{4, 5, 4, 5} {
		invoke(index)
	}
	if state.nativeContextVersion.Load() != version {
		t.Fatal("warmed exports did not share the valid pointer context")
	}
	if calls != 28 || state.invocationID != 0 || in.invocationState.Load() != 0 {
		t.Fatal("incorrect callback count or retained invocation authority")
	}
}
