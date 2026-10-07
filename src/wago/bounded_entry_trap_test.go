//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// A host trap needs annotation after recovery, including on a warmed direct
// entry. The following invocation must reacquire the released native lease.
func TestBoundedDirectRecoveredTrapAnnotation(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, api := range []string{"instance", "prepared", "session"} {
		for _, kind := range []string{"typed", "HostCall", "Caller"} {
			t.Run(api+"/"+kind, func(t *testing.T) {
				var failure *coreruntime.TrapError
				step := func(v int32) int32 {
					if failure != nil {
						panic(HostTrap{Err: failure})
					}
					return v + 1
				}
				var fn any = step
				if kind == "HostCall" {
					fn = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
				} else if kind == "Caller" {
					fn = func(_ Caller, call HostCall) { call.SetI32(0, step(call.I32(0))) }
				}
				imports := NewImports()
				imports.HostFunc("env", "step", fn).Params(ValI32).Results(ValI32)
				in, err := Instantiate(c, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				call := func() ([]uint64, error) { return in.Invoke("run", 1, 0) }
				if api != "instance" {
					resolved, err := in.WasmFunc("run")
					if err != nil {
						t.Fatal(err)
					}
					call = func() ([]uint64, error) { return resolved.Invoke(1, 0) }
					if api == "session" {
						session, err := resolved.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						defer session.Close()
						call = func() ([]uint64, error) { return session.Invoke2(1, 0) }
					}
				}
				invoke := func() {
					t.Helper()
					got, err := call()
					if err != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("invoke: %v, %v", got, err)
					}
				}
				invoke()
				invoke()
				state := in.ensurePluginState()
				wantLifecycle := in.invocationState.Load()
				wantID := state.invocationID
				wantGate := state.invokeMu.state.Load()
				failure = &coreruntime.TrapError{Code: coreruntime.TrapUnreachable, Frames: []coreruntime.TrapFrame{{FunctionIndex: 1}}}
				if _, err := call(); err != failure {
					t.Fatalf("recovered trap: %v; want %v", err, failure)
				}
				if failure.Frames[0].FunctionName != "run" {
					t.Fatalf("missing recovered frame annotation: %+v", failure.Frames)
				}
				if in.invocationState.Load() != wantLifecycle || state.invocationID != wantID || state.invokeMu.state.Load() != wantGate || state.activations.boundedID.Load() != 0 {
					t.Fatal("trap changed entry ownership or retained callback authority")
				}
				failure = nil
				invoke()
			})
		}
	}
}
