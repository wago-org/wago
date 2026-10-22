package wago

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestImportedStartCanInvokeAnotherInstance(t *testing.T) {
	for _, managed := range []bool{false, true} {
		name := "standalone"
		if managed {
			name = "runtime"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			targetBytes := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("initialize", 0, 0), wasmtest.ExportEntry("get", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0x0b}))),
			)
			var rt *Runtime
			var target *Instance
			var start *Instance
			var err error
			if managed {
				rt = NewRuntime()
				defer rt.Close()
				targetModule, compileErr := rt.Compile(targetBytes)
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				defer targetModule.Close()
				target, err = rt.Instantiate(ctx, targetModule)
			} else {
				compiled, compileErr := Compile(nil, targetBytes)
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				defer compiled.Close()
				target, err = Instantiate(compiled)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			var retained Caller
			host := CallerHostCallFunc(func(caller Caller, _ HostCall) {
				retained = caller
				if _, callErr := target.InvokeFromHost(ctx, caller, "initialize"); callErr != nil {
					panic(HostTrap{Err: callErr})
				}
			})
			if managed {
				startModule, compileErr := rt.Compile(importedStartModule())
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				defer startModule.Close()
				start, err = rt.Instantiate(ctx, startModule, WithImports(testImports("env.start", host)))
			} else {
				compiled, compileErr := Compile(nil, importedStartModule())
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				defer compiled.Close()
				start, err = Instantiate(compiled, InstantiateOptions{Imports: testImports("env.start", host)})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer start.Close()
			result, err := target.Invoke("get")
			if err != nil || len(result) != 1 || AsI32(result[0]) != 42 {
				t.Fatalf("initialized value = %v, %v; want 42", result, err)
			}
			if _, err := target.InvokeFromHost(ctx, retained, "initialize"); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("expired start caller = %v", err)
			}
		})
	}
}

func TestImportedStartCanReenterInitializingInstance(t *testing.T) {
	// Reentry does not require cancellation support from TinyGo's scheduler.
	ctx, cancel := invocationContextTestParent(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	bytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "start", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}))),
	)
	compiled, err := Compile(nil, bytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	calls := 0
	host := CallerHostCallFunc(func(caller Caller, _ HostCall) {
		result, err := caller.in.InvokeFromHost(ctx, caller, "get")
		if err != nil {
			panic(HostTrap{Err: err})
		}
		if len(result) != 1 || AsI32(result[0]) != 42 {
			t.Fatalf("start reentry result = %v", result)
		}
		calls++
	})
	in, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.start", host)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if calls != 1 {
		t.Fatalf("start callbacks = %d, want 1", calls)
	}
}

func TestImportedStartInvocationContextParentCancellation(t *testing.T) {
	if !nativeCancellationSupported() {
		t.Skip("parent cancellation requires a concurrent scheduler")
	}
	state := &invocationContextTestState{concrete: true}
	rt := newInvocationContextTestRuntime(t, state)
	defer rt.Close()
	module, err := rt.Compile(invocationContextImportedStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var retained HostModule
	var callbackContext context.Context
	state.outer = func(caller HostModule, _, _ []uint64) {
		retained = caller
		var err error
		callbackContext, err = state.resolver.InvocationContext(caller)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		select {
		case <-callbackContext.Done():
			if callbackContext.Err() != context.Canceled {
				t.Errorf("callback context = %v, want context.Canceled", callbackContext.Err())
			}
		case <-time.After(5 * time.Second):
			t.Error("parent cancellation did not reach the active imported-start callback")
		}
	}
	in, err := rt.Instantiate(parent, module)
	if in != nil {
		defer in.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Instantiate error = %v, want context.Canceled", err)
	}
	if callbackContext == nil || callbackContext.Err() != context.Canceled {
		t.Fatalf("callback context after start = %v, want canceled", callbackContext)
	}
	if _, err := state.resolver.InvocationContext(retained); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired start caller context = %v, want permission denied", err)
	}
}
