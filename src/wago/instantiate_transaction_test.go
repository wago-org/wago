package wago

import (
	"os"
	"strings"
	"testing"
)

func TestFailedInstantiationReleasesCodeAndImportState(t *testing.T) {
	compiled := MustCompile(failingLocalStartModule())
	defer compiled.Close()
	imports := testImports("env.f", slotHostFunc(func(HostModule, []uint64, []uint64) {}))

	for attempt := 0; attempt < 8; attempt++ {
		instance, err := Instantiate(compiled, imports)
		if instance != nil || err == nil || !strings.Contains(err.Error(), "start function trapped") {
			t.Fatalf("attempt %d: Instantiate = %p, %v; want nil start trap", attempt, instance, err)
		}
		compiled.codeCache.mu.Lock()
		refs := compiled.codeCache.refs
		compiled.codeCache.mu.Unlock()
		if refs != 0 {
			t.Fatalf("attempt %d: executable mapping refs = %d, want 0 after rollback", attempt, refs)
		}
	}
}

func TestDefinedStartHostPanicCompletes(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{{name: "value", value: "defined start panic"}}
	if os.Getenv("WAGO_DEFINED_START_NIL_PANIC") == "1" {
		cases = append(cases, struct {
			name  string
			value any
		}{name: "nil", value: nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := MustCompile(hostImportStartModule())
			defer compiled.Close()
			fail := true
			calls := 0
			var captured *Instance
			var callerToken Caller
			imports := NewImports().Function("env", "f", CallerHostCallFunc(func(caller Caller, _ HostCall) {
				calls++
				captured = caller.in
				callerToken = caller
				if fail {
					panic(tc.value)
				}
			}))
			in, err := Instantiate(compiled, imports)
			if in != nil {
				defer in.Close()
			}
			wantPanic := "defined start panic"
			if tc.value == nil {
				wantPanic = "<nil>"
			}
			if in != nil || err == nil || calls != 1 || !strings.Contains(err.Error(), "wago: instantiation panicked after instance creation") || !strings.Contains(err.Error(), wantPanic) {
				t.Fatalf("failed defined start = (%p, %v, calls=%d)", in, err, calls)
			}
			if captured == nil || callerToken.valid() || !captured.resourcesClosed || captured.constructionIsActive() || captured.invocationState.Load()&instanceInvocationCount != 0 {
				t.Fatal("failed defined start retained callback or instance lifetime")
			}
			if state := captured.pluginState.Load(); state != nil && (state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0) {
				t.Fatal("failed defined start retained invocation gate")
			}
			compiled.codeCache.mu.Lock()
			refs := compiled.codeCache.refs
			compiled.codeCache.mu.Unlock()
			if refs != 0 {
				t.Fatalf("failed defined start retained code: refs=%d", refs)
			}
			fail = false
			calls = 0
			in, err = Instantiate(compiled, imports)
			retry := in
			if retry != nil {
				t.Cleanup(func() {
					if retry != nil {
						_ = retry.Close()
					}
				})
			}
			if err != nil || in == nil || calls != 1 {
				t.Fatalf("defined start retry = (%p, %v, calls=%d)", in, err, calls)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			retry = nil
		})
	}
}
