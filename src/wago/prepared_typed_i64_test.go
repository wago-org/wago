//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestPreparedTypedI64PrivateRoute(t *testing.T) {
	if codeProfileEnabled || !detachedNumericHostEnabled || !privateNumericLiveRouteEnabled || !integerNumericHostEnabled {
		t.Skip("private numeric route disabled")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64)
  local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.integerHostContextAllowed() {
		t.Fatal("integer-only fixture lacks integer context proof")
	}

	for _, api := range []string{"instance", "function", "session"} {
		t.Run(api, func(t *testing.T) {
			const mask = uint64(0x9e3779b97f4a7c15)
			sentinel := errors.New("typed i64 host trap")
			var failure any
			closeNext := false
			calls := 0
			var session *PreparedSession
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int64) int64 {
				calls++
				if failure != nil {
					panic(failure)
				}
				if closeNext {
					closeNext = false
					copy := *session
					copy.Close()
				}
				if inlineWagoGrow(8) != 36 {
					panic("stack growth")
				}
				runtime.GC()
				return int64(uint64(v) ^ mask)
			}).Params(ValI64).Results(ValI64)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			p := in.eng.PreparedScalarHost()
			if p == nil || !p.DetachedNumericContext() || !p.IntegerGuestContext() {
				t.Fatal("private integer bridge unavailable")
			}
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if !fn.boundedNumericHost || !in.preparedBoundedNumericEligible() || in.syncHosts[0].typedI64 == nil {
				t.Fatal("typed i64 callback was not admitted to the bounded private route")
			}

			invoke := func(v uint64) ([]uint64, error) { return in.Invoke("run", v) }
			switch api {
			case "function":
				invoke = func(v uint64) ([]uint64, error) { return fn.Invoke(v) }
			case "session":
				session, err = fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				if session.state.privateHost == nil || session.state.privateHost.scalar == nil || session.state.privateHost.directI32 != nil {
					t.Fatal("session did not select the generic integer i64 adapter")
				}
				invoke = func(v uint64) ([]uint64, error) { return session.Invoke1(v) }
			}
			// Name-based Invoke resolves its bounded cache lazily. Warm each API
			// once before asserting the steady-state private route.
			if got, err := invoke(0); err != nil || len(got) != 1 || got[0] != mask {
				t.Fatalf("warm route: got %v, %v", got, err)
			}
			calls = 0

			// The dedicated field is the only callback target used by the new
			// adapters. Removing the expanded-path interface value makes this a
			// route assertion: falling back to the old portal would panic.
			in.syncHosts[0].fn = nil
			for _, input := range []uint64{0, 1, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffffffffffff} {
				got, err := invoke(input)
				if err != nil || len(got) != 1 || got[0] != input^mask {
					t.Fatalf("input %#x: got %v, %v; want %#x", input, got, err, input^mask)
				}
			}

			for _, outcome := range []any{HostTrap{Err: sentinel}, HostExit{Code: 7}, "typed-i64-panic"} {
				failure = outcome
				var got []uint64
				var invokeErr error
				var recovered any
				func() {
					defer func() { recovered = recover() }()
					got, invokeErr = invoke(0)
				}()
				switch outcome.(type) {
				case HostTrap:
					if recovered != nil || !errors.Is(invokeErr, sentinel) {
						t.Fatalf("host trap: got %v, %v; panic %v", got, invokeErr, recovered)
					}
				case HostExit:
					var exit *ExitError
					if recovered != nil || !errors.As(invokeErr, &exit) || exit.Code != 7 {
						t.Fatalf("host exit: got %v, %v; panic %v", got, invokeErr, recovered)
					}
				default:
					if recovered != outcome {
						t.Fatalf("callback panic changed: %v", recovered)
					}
				}
				if session == nil && in.invocationState.Load() != 0 || session != nil && session.state.active.Load() {
					t.Fatal("failure retained per-call invocation ownership")
				}
				failure = nil
				got, invokeErr = invoke(0xffffffffffffffff)
				if invokeErr != nil || len(got) != 1 || got[0] != ^mask {
					t.Fatalf("after failure: got %v, %v", got, invokeErr)
				}
			}
			if calls != 11 {
				t.Fatalf("callback count %d, want 11", calls)
			}
			if session != nil {
				closeNext = true
				got, err := invoke(1)
				if err != nil || len(got) != 1 || got[0] != 1^mask || !session.state.closed.Load() || session.state.active.Load() {
					t.Fatalf("deferred close: got %v, %v; closed=%t active=%t", got, err, session.state.closed.Load(), session.state.active.Load())
				}
				if _, err := invoke(1); err == nil {
					t.Fatal("closed session invoked guest")
				}
			}
		})
	}
}

func TestPreparedTypedI64DoesNotClaimIntegerBridgeWithoutProof(t *testing.T) {
	if codeProfileEnabled || !detachedNumericHostEnabled {
		t.Skip("private numeric route disabled")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64) (local f64)
  local.get 0 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.integerHostContextAllowed() {
		t.Fatal("floating-point local unexpectedly passed integer context proof")
	}
	for _, api := range []string{"instance", "function", "session"} {
		t.Run(api, func(t *testing.T) {
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int64) int64 { return v + 1 }).Params(ValI64).Results(ValI64)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			p := in.eng.PreparedScalarHost()
			if p == nil || !p.DetachedNumericContext() {
				t.Skip("detached bridge unavailable")
			}
			if p.IntegerGuestContext() {
				t.Fatal("i64 callback claimed integer bridge without proof")
			}
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			invoke := func() ([]uint64, error) { return in.Invoke("run", 41) }
			if api == "function" {
				invoke = func() ([]uint64, error) { return fn.Invoke(41) }
			}
			if api == "session" {
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				if session.state.privateHost != nil && session.state.privateHost.scalar == nil {
					t.Fatal("ordinary prepared context retained the wrong i64 adapter")
				}
				invoke = func() ([]uint64, error) { return session.Invoke1(41) }
			}
			got, err := invoke()
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("fallback context: got %v, %v", got, err)
			}
		})
	}
}

func TestPreparedTypedI64PublicationRevokesPrivateRoute(t *testing.T) {
	if codeProfileEnabled || !detachedNumericHostEnabled || !privateNumericLiveRouteEnabled {
		t.Skip("private numeric route disabled")
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
	entered, leave := make(chan struct{}), make(chan struct{})
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int64) int64 {
		calls++
		if calls == 1 {
			close(entered)
			<-leave
			runtime.GC()
		}
		return v + 1
	}).Params(ValI64).Results(ValI64)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !numericContextDetached(in) {
		t.Skip("detached numeric context unavailable")
	}
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.boundedNumericHost {
		t.Fatal("typed i64 callback lacks private eligibility")
	}
	type result struct {
		values []uint64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		got, callErr := fn.Invoke(41)
		done <- result{values: got, err: callErr}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("typed i64 callback did not enter")
	}
	if _, err := in.ExportedMemory("memory"); err != nil {
		close(leave)
		<-done
		t.Fatal(err)
	}
	guard := in.acquireInstanceNativeStateForHostAccess()
	close(leave)
	select {
	case got := <-done:
		guard.Unlock()
		if got.err != nil || len(got.values) != 1 || got.values[0] != 42 {
			t.Fatalf("published continuation: got %v, %v", got.values, got.err)
		}
	case <-time.After(3 * time.Second):
		guard.Unlock()
		<-done
		t.Fatal("private continuation waited for published resource context")
	}
	if _, err, admitted := fn.tryInvokeBoundedNumeric([]uint64{41}); admitted || err != nil {
		t.Fatalf("revoked route admitted=%t err=%v", admitted, err)
	}
	got, err := fn.Invoke(41)
	if err != nil || len(got) != 1 || got[0] != 42 || calls != 2 {
		t.Fatalf("post-publication fallback: got %v, %v; calls=%d", got, err, calls)
	}
}

// BenchmarkPreparedTypedI64Route measures one complete host -> guest -> host
// round trip. Setup and session open/close stay outside the timed region; the
// signature-matrix benchmark separately measures 1024 callbacks per invoke.
// Keep HostCall as an ABI-equivalent control, not as evidence for this route.
func BenchmarkPreparedTypedI64Route(b *testing.B) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(b, `(module
 (import "env" "step" (func $step (param i64) (result i64)))
 (func (export "run") (param i64) (result i64)
  local.get 0 call $step))`))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for _, callback := range []struct {
		name string
		fn   any
	}{
		{name: "typed", fn: func(v int64) int64 { return v + 1 }},
		{name: "HostCall", fn: HostCallFunc(func(call HostCall) { call.SetI64(0, call.I64(0)+1) })},
	} {
		for _, api := range []string{"instance", "function", "session"} {
			b.Run(callback.name+"/"+api, func(b *testing.B) {
				imports := NewImports()
				imports.HostFunc("env", "step", callback.fn).Params(ValI64).Results(ValI64)
				in, err := Instantiate(c, InstantiateOptions{Imports: imports})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				invoke := func() ([]uint64, error) { return in.Invoke("run", 7) }
				var session *PreparedSession
				switch api {
				case "function":
					invoke = func() ([]uint64, error) { return fn.Invoke(7) }
				case "session":
					session, err = fn.OpenSession()
					if err != nil {
						b.Fatal(err)
					}
					defer session.Close()
					invoke = func() ([]uint64, error) { return session.Invoke1(7) }
				}
				if got, err := invoke(); err != nil || len(got) != 1 || got[0] != 8 {
					b.Fatalf("warm route: got %v, %v", got, err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if got, err := invoke(); err != nil || len(got) != 1 || got[0] != 8 {
						b.Fatalf("invoke: got %v, %v", got, err)
					}
				}
			})
		}
	}
}

// BenchmarkPreparedTypedI64GuestEntry is the signature-matched entry/return
// control for BenchmarkPreparedTypedI64Route; it performs no host callback.
func BenchmarkPreparedTypedI64GuestEntry(b *testing.B) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(b, `(module
 (func (export "run") (param i64) (result i64) local.get 0))`))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		b.Fatal(err)
	}
	for _, api := range []string{"instance", "function", "session"} {
		b.Run(api, func(b *testing.B) {
			invoke := func() ([]uint64, error) { return in.Invoke("run", 7) }
			var session *PreparedSession
			switch api {
			case "function":
				invoke = func() ([]uint64, error) { return fn.Invoke(7) }
			case "session":
				session, err = fn.OpenSession()
				if err != nil {
					b.Fatal(err)
				}
				defer session.Close()
				invoke = func() ([]uint64, error) { return session.Invoke1(7) }
			}
			if got, err := invoke(); err != nil || len(got) != 1 || got[0] != 7 {
				b.Fatalf("warm route: got %v, %v", got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if got, err := invoke(); err != nil || len(got) != 1 || got[0] != 7 {
					b.Fatalf("invoke: got %v, %v", got, err)
				}
			}
		})
	}
}
