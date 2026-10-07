//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func TestPreparedBoundedViewContextAndPanicCleanup(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains general entry")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, api := range []string{"prepared", "session"} {
		for _, kind := range []string{"typed", "HostCall", "Caller"} {
			t.Run(api+"/"+kind, func(t *testing.T) {
				calls := 0
				var failure any
				var stale Caller
				sentinel := errors.New("prepared view callback failure")
				step := func(v int32) int32 {
					if failure != nil {
						panic(failure)
					}
					if calls == 0 {
						if inlineWagoGrow(32) != 528 {
							t.Fatal("stack growth")
						}
						runtime.GC()
					}
					calls++
					return v + 1
				}
				var callback any = step
				if kind == "HostCall" {
					callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
				}
				if kind == "Caller" {
					callback = func(caller Caller, call HostCall) {
						if !caller.valid() || stale.valid() {
							t.Fatal("Caller generation")
						}
						stale = caller
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
				invoke := func() ([]uint64, error) { return fn.Invoke(2, 0) }
				if api == "session" {
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					invoke = func() ([]uint64, error) { return session.Invoke2(2, 0) }
				}
				ctrl := offHeapSlicePtr(in.ctrl)
				outer := hostInvocationContext{id: newInvocationID(), parent: context.Background()}
				restore := bindHostInvocationContext(ctrl, outer)
				defer restore()
				checkContext := func() {
					t.Helper()
					value, ok := hostInvocationContexts.Load(ctrl)
					if !ok || value.(hostInvocationContext).id != outer.id || value.(hostInvocationContext).parent != outer.parent {
						t.Fatal("outer host context changed")
					}
				}
				check := func() {
					t.Helper()
					got, err := invoke()
					if err != nil || len(got) != 1 || got[0] != 2 {
						t.Fatalf("result %v %v", got, err)
					}
					if stale.valid() {
						t.Fatal("expired Caller survived")
					}
					checkContext()
				}
				check()
				check()
				state := in.ensurePluginState()
				if !numericContextDetached(in) && state.boundedViewVersion == 0 {
					t.Fatal("prepared view did not record private context")
				}
				version := state.nativeContextVersion.Load()
				id := state.invocationID
				check()
				if state.nativeContextVersion.Load() != version {
					t.Fatal("unchanged context rebound")
				}
				in.acquireInstanceNativeStateForHostAccess().Unlock()
				version = state.nativeContextVersion.Load()
				check()
				if !numericContextDetached(in) && state.nativeContextVersion.Load() <= version {
					t.Fatal("guarded access failed to rebind")
				}
				for _, outcome := range []any{HostTrap{Err: sentinel}, HostExit{Code: 7}, sentinel} {
					failure = outcome
					var recovered any
					var invokeErr error
					func() { defer func() { recovered = recover() }(); _, invokeErr = invoke() }()
					switch outcome.(type) {
					case HostTrap:
						if recovered != nil || !errors.Is(invokeErr, sentinel) {
							t.Fatalf("trap %v panic %v", invokeErr, recovered)
						}
					case HostExit:
						var exit *ExitError
						if recovered != nil || !errors.As(invokeErr, &exit) || exit.Code != 7 {
							t.Fatalf("exit %v panic %v", invokeErr, recovered)
						}
					default:
						if recovered != sentinel {
							t.Fatal("callback panic lost", recovered)
						}
					}
					checkContext()
					if stale.valid() || state.activations.boundedID.Load() != 0 || state.invocationID != id {
						t.Fatal("failure retained callback authority or lost reservation")
					}
				}
				failure = nil
				check()
				if calls != 10 {
					t.Fatal("wrong callback count", calls)
				}
			})
		}
	}
}

func TestPreparedBoundedViewCallerReentryAndPublication(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains general entry")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (memory (export "memory") 1)
 (func (export "run") (param i32) (result i32) local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, api := range []string{"prepared", "session"} {
		t.Run(api, func(t *testing.T) {
			var in *Instance
			publish := false
			var stale []Caller
			calls := 0
			imports := NewImports()
			imports.HostFunc("env", "step", func(caller Caller, call HostCall) {
				if !caller.valid() {
					t.Fatal("inactive Caller")
				}
				stale = append(stale, caller)
				calls++
				n := call.I32(0)
				if n == 0 {
					call.SetI32(0, 1)
					return
				}
				if publish && n == 3 {
					if !numericContextDetached(in) && in.ensurePluginState().boundedViewVersion == 0 {
						t.Fatal("publication was not warmed")
					}
					if _, err := in.ExportedMemory("memory"); err != nil {
						t.Fatal(err)
					}
				}
				got, err := in.InvokeFromHost(context.Background(), caller, "run", uint64(n-1))
				if err != nil || len(got) != 1 || !caller.valid() {
					t.Fatalf("reentry %v %v", got, err)
				}
				call.SetI32(0, int32(got[0])+1)
			}).Params(ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			invoke := func() ([]uint64, error) { return fn.Invoke(3) }
			if api == "session" {
				s, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				invoke = func() ([]uint64, error) { return s.Invoke1(3) }
			}
			for i := 0; i < 4; i++ {
				publish = i >= 2
				got, err := invoke()
				if err != nil || len(got) != 1 || got[0] != 4 {
					t.Fatalf("outer %v %v", got, err)
				}
				for _, caller := range stale {
					if caller.valid() {
						t.Fatal("retired scope valid")
					}
				}
				if in.ensurePluginState().activations.boundedID.Load() != 0 {
					t.Fatal("callback authority retained")
				}
			}
			if in.usesIndependentExecution() || in.preparedBoundedNumericEligible() || calls != 16 {
				t.Fatal("publication retained private driver or wrong count", calls)
			}
		})
	}
}

func TestPreparedBoundedViewArityAndClose(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, api := range []string{"prepared", "session"} {
		t.Run(api, func(t *testing.T) {
			calls := 0
			imports := NewImports()
			imports.HostFunc("env", "step", func(call HostCall) { calls++; call.SetI32(0, call.I32(0)+1) }).Params(ValI32).Results(ValI32)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			var s *PreparedSession
			if api == "session" {
				s, err = fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
			}
			if s == nil {
				_, err = fn.Invoke(1)
			} else {
				_, err = s.Invoke1(1)
			}
			if err == nil || calls != 0 {
				t.Fatal("wrong arity reached callback", err, calls)
			}
			if s == nil {
				_, err = fn.Invoke(2, 0)
			} else {
				_, err = s.Invoke2(2, 0)
			}
			if err != nil || calls != 2 {
				t.Fatal("correct invocation", err, calls)
			}
			if s != nil {
				s.Close()
			} else if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if s == nil {
				_, err = fn.Invoke(2, 0)
			} else {
				_, err = s.Invoke2(2, 0)
			}
			if err == nil || calls != 2 {
				t.Fatal("closed handle reached callback", err, calls)
			}
		})
	}
}
