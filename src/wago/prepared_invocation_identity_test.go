//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import "testing"

func TestPreparedBoundedInvocationIdentityAcrossCalls(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains general entry")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			var in *Instance
			seen := make(map[invocationID]bool)
			var stale Caller
			fail := false
			sentinel := &struct{}{}
			step := func(v int32) int32 {
				id := in.currentInvocationID()
				if id == 0 || seen[id] {
					t.Fatalf("zero or reused invocation identity %d", id)
				}
				seen[id] = true
				if fail {
					panic(sentinel)
				}
				return v + 1
			}
			var callback any = step
			if kind == "HostCall" {
				callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
			} else if kind == "Caller" {
				callback = func(caller Caller, call HostCall) {
					if !caller.valid() || stale.valid() {
						t.Fatal("Caller generation leaked across invocations")
					}
					stale = caller
					call.SetI32(0, step(call.I32(0)))
				}
			}
			imports := NewImports()
			imports.HostFunc("env", "step", callback).Params(ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 260; i++ {
				var result []uint64
				if i%2 == 0 {
					result, err = fn.Invoke(1, 0)
				} else {
					result, err = in.Invoke("run", 1, 0)
				}
				if err != nil || len(result) != 1 || result[0] != 1 || stale.valid() {
					t.Fatalf("result %v %v, valid stale Caller %t", result, err, stale.valid())
				}
			}
			state := in.ensurePluginState()
			fail = true
			var recovered any
			func() { defer func() { recovered = recover() }(); _, _ = fn.Invoke(1, 0) }()
			if recovered != sentinel || state.invocationID != 0 || stale.valid() {
				t.Fatal("panic did not clear invocation authority")
			}
			fail = false
			if result, err := fn.Invoke(1, 0); err != nil || len(result) != 1 || result[0] != 1 {
				t.Fatalf("reuse %v %v", result, err)
			}
			if len(seen) != 262 {
				t.Fatalf("got %d identities", len(seen))
			}
		})
	}
}
