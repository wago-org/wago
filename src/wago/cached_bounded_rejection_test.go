//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import "testing"

// Each row synthesizes one execution-flag state after the export is cached.
// This tests predicate rejection and gate cleanup, not the publication handshake.
// Public Invoke must still work; it may select another safe entry route.
func TestCachedBoundedNumericRejectionMatrix(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		name  string
		flag  uint32
		clear bool
	}{
		// Sharing blocks both the prepared fast state and independent execution.
		{name: "native control shared (two guards)", flag: executionFlagNativeControlShared},
		{name: "imported GC domain", flag: executionFlagImportedGCDomain},
		{name: "dynamic GC domain", flag: executionFlagDynamicGCDomain},
		{name: "store owned GC collector", flag: executionFlagStoreOwnedGCCollector},
		{name: "non independent execution", flag: executionFlagIndependent, clear: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var in *Instance
			observePublicInvoke := false
			publicInvokeAuthorityCalls := 0
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int32) int32 {
				calls++
				if observePublicInvoke {
					state := in.pluginState.Load()
					if state != nil && state.invocationID != 0 && state.invokeMu.state.Load()&invocationGateHeld != 0 {
						publicInvokeAuthorityCalls++
					}
				}
				return v + 1
			}).Params(ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			args := []uint64{3, 0}
			checkResult := func(label string, got []uint64, err error) {
				t.Helper()
				if err != nil || len(got) != 1 || got[0] != 3 {
					t.Fatalf("%s = %v, %v; want [3]", label, got, err)
				}
			}
			checkAuthority := func(label string) {
				t.Helper()
				state := in.ensurePluginState()
				if state.invocationID != 0 || in.invocationState.Load() != 0 || state.invokeMu.state.Load() != 0 || state.activations.boundedID.Load() != 0 {
					t.Fatalf("%s retained invocation authority", label)
				}
			}
			warm, err := in.Invoke("run", args...)
			checkResult("warm", warm, err)
			ic := in.findInvokeCache("run")
			if ic == nil || !ic.boundedNumericHost {
				t.Fatal("positive control did not cache bounded numeric eligibility")
			}
			if got, err, admitted := in.tryInvokeCachedBoundedNumeric("run", args); !admitted {
				t.Fatal("positive control rejected cached entry")
			} else {
				checkResult("positive", got, err)
			}
			checkAuthority("positive")
			initial := in.executionFlags.Load()
			if tc.clear {
				in.executionFlags.Store(initial &^ tc.flag)
			} else {
				in.executionFlags.Store(initial | tc.flag)
			}
			if got, err, admitted := in.tryInvokeCachedBoundedNumeric("run", args); admitted || err != nil || got != nil {
				t.Fatalf("%s admitted: %v, %v, admitted=%t", tc.name, got, err, admitted)
			}
			checkAuthority("rejected probe")
			observePublicInvoke = true
			result, err := in.Invoke("run", args...)
			observePublicInvoke = false
			checkResult("public Invoke after rejection", result, err)
			if publicInvokeAuthorityCalls != 3 {
				t.Fatalf("public Invoke callbacks with instance authority = %d; want 3", publicInvokeAuthorityCalls)
			}
			checkAuthority("public Invoke after rejection")
			in.executionFlags.Store(initial)
			if got, err, admitted := in.tryInvokeCachedBoundedNumeric("run", args); !admitted {
				t.Fatal("restored positive control rejected cached entry")
			} else {
				checkResult("restored positive", got, err)
			}
			checkAuthority("restored positive")
			if calls != 12 {
				t.Fatalf("host callbacks = %d; want 12", calls)
			}
		})
	}
}
