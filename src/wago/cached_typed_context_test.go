//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import "testing"

func TestCachedBoundedTypedContextReuseAndInvalidation(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains the general driver")
	}
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
			t.Fatalf("invoke: %v, %v", got, err)
		}
	}
	invoke()
	invoke()
	state := in.ensurePluginState()
	if !numericContextDetached(in) && state.boundedViewVersion == 0 {
		t.Fatal("warm typed entry did not record context version")
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
}
