//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"
	"time"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

func numericContextDetached(in *Instance) bool {
	p := in.eng.PreparedScalarHost()
	return p != nil && p.DetachedNumericContext()
}

func TestDetachedNumericResourceIsolation(t *testing.T) {
	if !detachedNumericHostEnabled || (goruntime.GOARCH != "amd64" && (goruntime.GOARCH != "arm64" || !armDetachedNumericEnabled)) || codeProfileEnabled {
		t.Skip("detached numeric context")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (memory (export "memory") 1 4)
 (func (export "run") (param i32) (result i32) (local i32)
  loop $again local.get 1 call $step local.set 1
   local.get 1 local.get 0 i32.lt_u br_if $again end local.get 1))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	consumerCode, err := Compile(NewRuntimeConfig(), watToWasm(t, `(module
 (import "owner" "memory" (memory 1 4))
 (func (export "grow") (result i32) i32.const 1 memory.grow))`))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	for _, api := range []string{"instance", "prepared", "session"} {
		for _, kind := range []string{"typed", "HostCall", "Caller"} {
			t.Run(api+"/"+kind, func(t *testing.T) {
				entered, leave := make(chan struct{}), make(chan struct{})
				calls := 0
				step := func(v int32) int32 {
					calls++
					if calls == 1 {
						close(entered)
						<-leave
						if inlineWagoGrow(32) != 528 {
							panic("stack growth")
						}
						goruntime.GC()
					}
					return v + 1
				}
				var callback any = step
				var stale Caller
				if kind == "HostCall" {
					callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
				}
				if kind == "Caller" {
					callback = func(caller Caller, call HostCall) {
						if !caller.valid() {
							panic("expired Caller")
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
				if in.eng.PreparedScalarHost() == nil {
					t.Skip("prepared numeric bridge unavailable")
				}
				if !numericContextDetached(in) {
					t.Fatal("eligible context was not detached")
				}
				invoke := func() ([]uint64, error) { return in.Invoke("run", 5) }
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				if api == "prepared" {
					invoke = func() ([]uint64, error) { return fn.Invoke(5) }
				}
				if api == "session" {
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					invoke = func() ([]uint64, error) { return session.Invoke1(5) }
				}
				// Warm metadata without entering the blocking host callback.
				in.ensurePluginState()
				if _, err := in.fillInvokeCache("run"); err != nil {
					t.Fatal(err)
				}
				wantLifecycle := in.invocationState.Load()
				done := make(chan error, 1)
				go func() {
					got, err := invoke()
					if err == nil && (len(got) != 1 || got[0] != 5) {
						err = fmt.Errorf("result %v", got)
					}
					done <- err
				}()
				select {
				case <-entered:
					if home := uintptr(binary.LittleEndian.Uint64(in.ctrl[inlineWagoSavedHomeOffset():])); home != in.eng.PreparedScalarHost().DetachedNumericContextBase() {
						close(leave)
						<-done
						t.Fatal("invocation did not select private context")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("callback did not enter")
				}
				// Publication and another native instance replace the real basedata while
				// the private numeric continuation is parked. Grow its exported memory.
				mem, err := in.ExportedMemory("memory")
				if err != nil {
					close(leave)
					t.Fatal(err)
				}
				consumerImports := NewImports()
				consumerImports.Memory("owner", "memory", mem)
				consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: consumerImports})
				if err != nil {
					close(leave)
					t.Fatal(err)
				}
				defer consumer.Close()
				got, err := consumer.Invoke("grow")
				if err != nil || len(got) != 1 || got[0] != 1 {
					close(leave)
					t.Fatalf("growth: %v %v", got, err)
				}
				guard := in.acquireInstanceNativeStateForHostAccess()
				close(leave)
				// Every remaining native segment and host callback must finish while the
				// mutable resource context remains locked by someone else.
				select {
				case err := <-done:
					guard.Unlock()
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					guard.Unlock()
					<-done
					t.Fatal("private continuation waited for resource mutex")
				}
				if calls != 5 || stale.valid() || in.invocationState.Load() != wantLifecycle || in.ensurePluginState().activations.boundedID.Load() != 0 {
					t.Fatal("callback count or authority leak")
				}
				// Publication revokes the fast admission; the ordinary next entry still works.
				if got, err := invoke(); err != nil || len(got) != 1 || got[0] != 5 {
					t.Fatalf("post-publication: %v %v", got, err)
				}
			})
		}
	}
}

func TestDetachedNumericInterruptAndRecovery(t *testing.T) {
	if !detachedNumericHostEnabled || (goruntime.GOARCH != "amd64" && (goruntime.GOARCH != "arm64" || !armDetachedNumericEnabled)) || codeProfileEnabled {
		t.Skip("detached numeric context")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	interrupt := true
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		calls++
		if home := uintptr(binary.LittleEndian.Uint64(in.ctrl[inlineWagoSavedHomeOffset():])); home != in.eng.PreparedScalarHost().DetachedNumericContextBase() {
			panic("interrupt invocation used mutable context")
		}
		if interrupt {
			wruntime.RequestInterrupt(in.trap)
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.eng.PreparedScalarHost() == nil {
		t.Skip("prepared numeric bridge unavailable")
	}
	if !numericContextDetached(in) {
		t.Fatal("context not detached")
	}
	in.ensurePluginState()
	if _, err := in.fillInvokeCache("run"); err != nil {
		t.Fatal(err)
	}
	_, err = in.Invoke("run", 100, 0)
	var trap *wruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != wruntime.TrapInterrupted || calls != 1 {
		t.Fatalf("interrupt: %v, calls=%d", err, calls)
	}
	interrupt = false
	// The public context cancellation path clears its interrupt on cleanup;
	// this direct runtime request deliberately leaves it armed until reset.
	for i := range in.trap {
		in.trap[i] = 0
	}
	got, err := in.Invoke("run", 3, 0)
	if err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("recovery: %v %v", got, err)
	}
}

func TestDetachedNumericGuestTraps(t *testing.T) {
	if !detachedNumericHostEnabled || (goruntime.GOARCH != "amd64" && (goruntime.GOARCH != "arm64" || !armDetachedNumericEnabled)) || codeProfileEnabled {
		t.Skip("detached numeric context")
	}
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32) (result i32)))
 (func (export "run") (param i32 i32) (result i32)
  local.get 0 call $step local.get 1 i32.div_s))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 { return v }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.eng.PreparedScalarHost() == nil {
		t.Skip("prepared numeric bridge unavailable")
	}
	if !numericContextDetached(in) {
		t.Fatal("context not detached")
	}
	in.ensurePluginState()
	if _, err := in.fillInvokeCache("run"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][2]int32{{1, 0}, {-2147483648, -1}} {
		_, err := in.Invoke("run", I32(args[0]), I32(args[1]))
		var trap *wruntime.TrapError
		if !errors.As(err, &trap) {
			t.Fatalf("guest trap: %v", err)
		}
		if got, err := in.Invoke("run", 10, 2); err != nil || len(got) != 1 || got[0] != 5 {
			t.Fatalf("trap recovery: %v %v", got, err)
		}
	}
}
