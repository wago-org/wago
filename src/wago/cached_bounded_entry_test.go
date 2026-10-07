//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"testing"
)

func TestCachedBoundedHostFailureRecovery(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sentinel := errors.New("cached host callback failure")
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			var failure any
			callback := func(call HostCall) {
				if failure != nil {
					panic(failure)
				}
				call.SetI32(0, call.I32(0)+1)
			}
			var fn any = callback
			if kind == "typed" {
				fn = func(v int32) int32 {
					if failure != nil {
						panic(failure)
					}
					return v + 1
				}
			}
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
			ctrl := offHeapSlicePtr(in.ctrl)
			outer := hostInvocationContext{id: newInvocationID(), parent: context.Background()}
			restore := bindHostInvocationContext(ctrl, outer)
			defer restore()
			for _, outcome := range []any{HostTrap{Err: sentinel}, HostExit{Code: 7}, sentinel} {
				if _, err := in.Invoke("run", 1, 0); err != nil {
					t.Fatal(err)
				}
				failure = outcome
				var recovered any
				func() {
					defer func() { recovered = recover() }()
					_, err = in.Invoke("run", 1, 0)
				}()
				switch outcome.(type) {
				case HostTrap:
					if recovered != nil || !errors.Is(err, sentinel) {
						t.Fatalf("trap: %v, panic %v", err, recovered)
					}
				case HostExit:
					var exit *ExitError
					if recovered != nil || !errors.As(err, &exit) || exit.Code != 7 {
						t.Fatalf("exit: %v, panic %v", err, recovered)
					}
				default:
					if recovered != sentinel {
						t.Fatalf("panic: %v", recovered)
					}
				}
				state := in.ensurePluginState()
				if state.invocationID != 0 || in.invocationState.Load() != 0 || state.activations.boundedID.Load() != 0 {
					t.Fatal("failure retained invocation authority")
				}
				if value, ok := hostInvocationContexts.Load(ctrl); !ok || value.(hostInvocationContext).id != outer.id || value.(hostInvocationContext).parent != outer.parent {
					t.Fatal("failure changed outer host invocation context")
				}
				failure = nil
				if got, err := in.Invoke("run", 2, 0); err != nil || len(got) != 1 || got[0] != 2 {
					t.Fatalf("reuse: %v, %v", got, err)
				}
			}
		})
	}
}

func TestCachedBoundedTypedTwoParameterMarshalling(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32 i32) (result i32)))
 (func (export "run") (param i32 i32) (result i32)
  local.get 0 local.get 1 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(a, b int32) int32 { calls++; return a + b }).Params(ValI32, ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, input := range [][2]uint64{{0xdeadbeeffffffffc, 0xffffffff00000007}, {0x123456787fffffff, 0x8765432100000001}} {
		got, err, admitted := in.tryInvokeCachedBoundedNumeric("run", input[:])
		want := uint64(uint32(input[0]) + uint32(input[1]))
		if !admitted || err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("inputs %x: %v, %v, admitted=%v; want %x", input, got, err, admitted, want)
		}
	}
	if calls != 3 {
		t.Fatalf("callbacks=%d; want 3", calls)
	}
}

func TestCachedBoundedNumericEntry(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			var in *Instance
			var retained Caller
			calls := 0
			step := func(v int32) int32 { calls++; return v + 1 }
			var fn any = step
			if kind == "HostCall" {
				fn = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
			}
			if kind == "Caller" {
				fn = func(caller Caller, call HostCall) {
					retained = caller
					got, err := in.InvokeFromHost(context.Background(), caller, "run", 0, 0)
					if err != nil || len(got) != 1 || got[0] != 0 {
						t.Fatalf("nested: %v, %v", got, err)
					}
					call.SetI32(0, step(call.I32(0)))
				}
			}
			imports := NewImports()
			imports.HostFunc("env", "step", fn).Params(ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run", 4, 0); err != nil || len(got) != 1 || got[0] != 4 {
				t.Fatalf("warm: %v, %v", got, err)
			}
			got, err, ok := in.tryInvokeCachedBoundedNumeric("run", []uint64{0xdeadbeef00000002, 0xffffffff00000000})
			if !ok || err != nil || len(got) != 1 || got[0] != 2 {
				t.Fatalf("cached: %v, %v, admitted=%v", got, err, ok)
			}
			if calls != 6 {
				t.Fatalf("callbacks=%d; want 6", calls)
			}
			if retained.valid() || in.pluginState.Load().invocationID != 0 || in.invocationState.Load() != 0 {
				t.Fatal("cached entry retained invocation authority")
			}
			if _, _, ok := in.tryInvokeCachedBoundedNumeric("run", []uint64{1}); ok {
				t.Fatal("wrong arity admitted")
			}
			if _, err := in.Invoke("run", 1); err == nil {
				t.Fatal("wrong arity accepted")
			}
		})
	}
}

func TestCachedBoundedNumericPublicationRevokesEntry(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (memory (export "memory") 1)
 (func (export "run") (param i32) (result i32) local.get 0 call $step call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	publish := false
	imports := NewImports()
	imports.HostFunc("env", "step", func(caller Caller, call HostCall) {
		if !caller.valid() {
			t.Fatal("invalid cached Caller")
		}
		if publish {
			publish = false
			if _, err := in.ExportedMemory("memory"); err != nil {
				t.Fatal(err)
			}
		}
		call.SetI32(0, call.I32(0)+1)
	}).Params(ValI32).Results(ValI32)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run", 1); err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("warm: %v, %v", got, err)
	}
	publish = true
	if got, err, ok := in.tryInvokeCachedBoundedNumeric("run", []uint64{1}); !ok || err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("publish: %v, %v, admitted=%v", got, err, ok)
	}
	if _, _, ok := in.tryInvokeCachedBoundedNumeric("run", []uint64{1}); ok {
		t.Fatal("published backing retained cached private entry")
	}
	if got, err := in.Invoke("run", 1); err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("fallback: %v, %v", got, err)
	}
}

func TestCachedBoundedNumericDynamicDomainsRevokeEntry(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 { return v + 1 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", 1, 0); err != nil {
		t.Fatal(err)
	}
	ic := in.findInvokeCache("run")
	if ic == nil || !ic.boundedNumericHost {
		t.Fatal("numeric host eligibility was not cached")
	}
	initial := in.executionFlags.Load()
	defer in.executionFlags.Store(initial)
	for _, revoked := range []uint32{executionFlagNativeControlShared, executionFlagImportedGCDomain, executionFlagDynamicGCDomain, executionFlagStoreOwnedGCCollector} {
		in.executionFlags.Store(initial | revoked)
		if _, _, ok := in.tryInvokeCachedBoundedNumeric("run", []uint64{1, 0}); ok {
			t.Fatalf("cached signature bypassed execution revocation %#x", revoked)
		}
		state := in.pluginState.Load()
		if state.invocationID != 0 || in.invocationState.Load() != 0 || state.invokeMu.state.Load() != 0 {
			t.Fatal("rejected cache entry retained authority")
		}
	}
	in.executionFlags.Store(initial)
	if got, err, ok := in.tryInvokeCachedBoundedNumeric("run", []uint64{1, 0}); !ok || err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("unrevoked numeric entry: %v, %v, admitted=%v", got, err, ok)
	}
}
