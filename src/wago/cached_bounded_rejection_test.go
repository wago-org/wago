//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import "testing"

// Each row changes one mutable eligibility condition after the export has been
// cached. A rejected probe must leave both admission gates free so Invoke can
// take the ordinary route and a later eligible probe can use the cached route.
func TestCachedBoundedNumericRejectionMatrix(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		name  string
		flags uint32
	}{
		{name: "native control shared", flags: executionFlagNativeControlShared},
		{name: "imported GC domain", flags: executionFlagImportedGCDomain},
		{name: "dynamic GC domain", flags: executionFlagDynamicGCDomain},
		{name: "store owned GC collector", flags: executionFlagStoreOwnedGCCollector},
		{name: "non independent execution", flags: executionFlagIndependent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var in *Instance
			observeFallback := false
			fallbackAuthorityCalls := 0
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int32) int32 {
				calls++
				if observeFallback {
					state := in.pluginState.Load()
					if state != nil && state.invocationID != 0 && state.invokeMu.state.Load()&invocationGateHeld != 0 {
						fallbackAuthorityCalls++
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
			if tc.name == "non independent execution" {
				in.executionFlags.Store(initial &^ tc.flags)
			} else {
				in.executionFlags.Store(initial | tc.flags)
			}
			if got, err, admitted := in.tryInvokeCachedBoundedNumeric("run", args); admitted || err != nil || got != nil {
				t.Fatalf("%s admitted: %v, %v, admitted=%t", tc.name, got, err, admitted)
			}
			checkAuthority("rejected probe")
			observeFallback = true
			fallback, err := in.Invoke("run", args...)
			observeFallback = false
			checkResult("ordinary fallback", fallback, err)
			if fallbackAuthorityCalls != 3 {
				t.Fatalf("ordinary fallback callbacks with instance authority = %d; want 3", fallbackAuthorityCalls)
			}
			checkAuthority("ordinary fallback")
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
