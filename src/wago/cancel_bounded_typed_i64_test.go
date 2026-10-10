//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"testing"
)

func TestInvokeContextBoundedTypedI64(t *testing.T) {
	if codeProfileEnabled || !detachedNumericHostEnabled || !privateNumericLiveRouteEnabled || !integerNumericHostEnabled {
		t.Skip("private integer host bridge disabled")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (memory (export "memory") 1 2)
 (func (export "run") (param i64) (result i64)
  local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var cancel context.CancelFunc
	var failure any
	var in *Instance
	var observedParent context.Context
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int64) int64 {
		observedParent = activeHostInvocationContext(in).parent
		if failure != nil {
			panic(failure)
		}
		if cancel != nil {
			cancel()
		}
		return v + 1
	}).Params(ValI64).Results(ValI64)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if got, err := in.InvokeContext(ctx, "run", 41); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("warm context entry: %v, %v", got, err)
	}
	ic := in.findInvokeCache("run")
	if !in.contextBoundedTypedI64Eligible(ic) {
		t.Fatal("private integer context route unavailable")
	}
	// The generic expanded portal needs fn; the new adapter needs typedI64.
	// Removing fn proves that the cancelable entry uses the narrow route.
	original := in.syncHosts[0].fn
	in.syncHosts[0].fn = nil
	if got, err := in.InvokeContext(ctx, "run", 7); err != nil || len(got) != 1 || got[0] != 8 {
		t.Fatalf("narrow context entry: %v, %v", got, err)
	}
	if observedParent != ctx {
		t.Fatalf("narrow callback parent = %v, want invocation context", observedParent)
	}
	cancelCtx, cancelCall := context.WithCancel(context.Background())
	cancel = cancelCall
	if _, err := in.InvokeContext(cancelCtx, "run", 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("callback cancellation: %v", err)
	}
	cancel = nil
	if got, err := in.InvokeContext(ctx, "run", 8); err != nil || len(got) != 1 || got[0] != 9 {
		t.Fatalf("entry after cancellation: %v, %v", got, err)
	}
	sentinel := errors.New("typed i64 host trap")
	for _, outcome := range []any{HostTrap{Err: sentinel}, HostExit{Code: 7}, "typed-i64-panic"} {
		failure = outcome
		var callErr error
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_, callErr = in.InvokeContext(ctx, "run", 7)
		}()
		switch outcome.(type) {
		case HostTrap:
			if recovered != nil || !errors.Is(callErr, sentinel) {
				t.Fatalf("host trap: err=%v panic=%v", callErr, recovered)
			}
		case HostExit:
			var exit *ExitError
			if recovered != nil || !errors.As(callErr, &exit) || exit.Code != 7 {
				t.Fatalf("host exit: err=%v panic=%v", callErr, recovered)
			}
		default:
			if recovered != outcome {
				t.Fatalf("host panic: %v", recovered)
			}
		}
		failure = nil
		if got, err := in.InvokeContext(ctx, "run", 9); err != nil || len(got) != 1 || got[0] != 10 {
			t.Fatalf("entry after host failure: %v, %v", got, err)
		}
	}
	// Publishing host-visible memory revokes private execution. Keep the
	// expanded binding and remove the dedicated callback to prove fallback.
	in.syncHosts[0].fn = original
	if _, err := in.ExportedMemory("memory"); err != nil {
		t.Fatal(err)
	}
	if in.contextBoundedTypedI64Eligible(ic) {
		t.Fatal("published memory retained private context route")
	}
	in.syncHosts[0].typedI64 = nil
	if got, err := in.InvokeContext(ctx, "run", 10); err != nil || len(got) != 1 || got[0] != 11 {
		t.Fatalf("published fallback: %v, %v", got, err)
	}
	if observedParent != ctx {
		t.Fatalf("fallback callback parent = %v, want invocation context", observedParent)
	}
}

func TestInvokeContextBoundedTypedI64HostLoopCancellation(t *testing.T) {
	if codeProfileEnabled || !detachedNumericHostEnabled || !privateNumericLiveRouteEnabled || !integerNumericHostEnabled {
		t.Skip("private integer host bridge disabled")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64)
  (loop $repeat
   local.get 0 call $step drop
   br $repeat)
  i64.const 0))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int64) int64 {
		calls++
		if calls == 64 {
			cancel()
		}
		return v + 1
	}).Params(ValI64).Results(ValI64)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ic, err := in.fillInvokeCache("run")
	if err != nil {
		t.Fatal(err)
	}
	if !in.contextBoundedTypedI64Eligible(ic) {
		t.Fatal("bounded callback loop lacks private integer route")
	}
	in.syncHosts[0].fn = nil
	if _, err := in.InvokeContext(ctx, "run", 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("host loop cancellation after %d callbacks: %v", calls, err)
	}
	if calls < 64 {
		t.Fatalf("host loop stopped after %d callbacks, before cancellation", calls)
	}
}

func BenchmarkInvokeContextBoundedTypedI64(b *testing.B) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(b, `(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64)
  local.get 0 call $step))`))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int64) int64 { return v + 1 }).Params(ValI64).Results(ValI64)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if got, err := in.InvokeContext(ctx, "run", 7); err != nil || len(got) != 1 || got[0] != 8 {
		b.Fatalf("warm context entry: %v, %v", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if got, err := in.InvokeContext(ctx, "run", 7); err != nil || len(got) != 1 || got[0] != 8 {
			b.Fatalf("context entry: %v, %v", got, err)
		}
	}
}
