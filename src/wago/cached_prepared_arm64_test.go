//go:build (linux || darwin) && arm64 && !tinygo

package wago

import "testing"

func TestCachedBoundedTypedReusesPrivateContextARM64(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 { calls++; return v + 1 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	invoke := func() {
		t.Helper()
		got, err := in.Invoke("run", 2, 0)
		if err != nil || len(got) != 1 || got[0] != 2 {
			t.Fatalf("result %v %v", got, err)
		}
	}
	invoke()
	invoke()
	state := in.ensurePluginState()
	ic := in.findInvokeCache("run")
	if codeProfileEnabled && (ic == nil || state.hostInvokeCache == nil || state.hostInvokeCache[ic.slotIndex] == nil) {
		t.Fatal("warm typed invocation did not retain prepared context")
	}
	var fn *WasmFunc
	if codeProfileEnabled {
		fn = state.hostInvokeCache[ic.slotIndex]
	} else if !numericContextDetached(in) && state.boundedViewVersion == 0 {
		t.Fatal("warm typed invocation did not record private context")
	}
	version := state.nativeContextVersion.Load()
	invoke()
	if state.nativeContextVersion.Load() != version {
		t.Fatal("unchanged context rebound")
	}
	if codeProfileEnabled && fn != state.hostInvokeCache[ic.slotIndex] {
		t.Fatal("prepared handle replaced")
	}
	in.acquireInstanceNativeStateForHostAccess().Unlock()
	version = state.nativeContextVersion.Load()
	invoke()
	if !numericContextDetached(in) && state.nativeContextVersion.Load() <= version {
		t.Fatal("guarded access did not force rebind")
	}
	if calls != 8 {
		t.Fatal("callback count", calls)
	}
	if state.invocationID != 0 || in.invocationState.Load() != 0 {
		t.Fatal("invocation authority retained")
	}
}

func TestCachedBoundedTypedPanicReleasesPreparedEntryARM64(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sentinel := &struct{ message string }{"cached typed panic"}
	shouldPanic := false
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		if shouldPanic {
			panic(sentinel)
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2; i++ {
		if _, err := in.Invoke("run", 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	shouldPanic = true
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = in.Invoke("run", 1, 0)
	}()
	if recovered != sentinel {
		t.Fatalf("panic = %v", recovered)
	}
	if in.invocationState.Load() != 0 || in.ensurePluginState().invocationID != 0 {
		t.Fatal("panic retained invocation authority")
	}
	shouldPanic = false
	if got, err := in.Invoke("run", 2, 0); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("reuse after panic: %v, %v", got, err)
	}
}
