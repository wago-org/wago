package wago

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestPreparedSessionDirectReservation(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !s.state.fast {
		t.Fatal("isolated direct function did not acquire fast session reservation")
	}
	for i := uint64(0); i < 10; i++ {
		got, err := s.Invoke(i)
		if err != nil || len(got) != 1 || got[0] != i+1 {
			t.Fatalf("invoke(%d) = %v, %v", i, got, err)
		}
	}

	shared := make(chan struct{})
	go func() {
		in.markNativeControlShared()
		close(shared)
	}()
	select {
	case <-shared:
		t.Fatal("resource sharing completed while a fast session was reserved")
	case <-time.After(20 * time.Millisecond):
	}
	s.Close()
	select {
	case <-shared:
	case <-time.After(time.Second):
		t.Fatal("resource sharing did not resume after session close")
	}
	s.Close()
	if _, err := s.Invoke(1); err == nil {
		t.Fatal("closed session invocation succeeded")
	}
}

func TestPreparedSessionGeneralReservation(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	in.markNativeControlShared()
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.state.fast {
		t.Fatal("shared instance acquired fast session reservation")
	}
	got, err := s.Invoke(41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("general session invoke = %v, %v", got, err)
	}
}

func TestPreparedSessionDoesNotReserveProcessNativeLease(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	defer in.Close()
	fn.directIntFast = false
	fn.directIsolated = false
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	acquired := make(chan struct{})
	go func() {
		nativeExecutionMu.Lock()
		nativeExecutionMu.Unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("session retained the process-wide native lease between calls")
	}
	if got, err := session.Invoke1(41); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("fallback session invoke = %v, %v", got, err)
	}
}

func TestPreparedSessionTypedHostTrapAndNestedEntry(t *testing.T) {
	otherCompiled := MustCompile(benchAddOneModule())
	defer otherCompiled.Close()
	other, err := Instantiate(otherCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherFn, err := other.PrepareI32ToI32("f")
	if err != nil {
		t.Fatal(err)
	}

	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	calls := 0
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			calls++
			if calls == 1 {
				panic(HostTrap{Err: errors.New("expected session host trap")})
			}
			got, callErr := otherFn.Call(value)
			if callErr != nil {
				panic(callErr)
			}
			return got
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("g")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !session.state.host {
		t.Fatal("typed host function did not acquire a host-capable session")
	}
	if _, err := session.Invoke1(I32(41)); err == nil || err.Error() != "expected session host trap" {
		t.Fatalf("first session call error = %v", err)
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("session after host trap and nested entry = %v, %v; want 42", got, err)
	}
	session.Close()
}

func TestPreparedSessionCopiesShareCloseState(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	defer in.Close()
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	copy := *session
	session.Close()
	copy.Close()
	if _, err := copy.Invoke1(41); err == nil {
		t.Fatal("copied session invoked after the shared lease was closed")
	}
}

func TestPreparedSessionRejectsCallbackReentryAndDefersCallbackClose(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	var session *PreparedSession
	var reentryErr error
	calls := 0
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			calls++
			if calls == 1 {
				_, reentryErr = session.Invoke1(I32(value))
			} else {
				session.Close()
			}
			return value + 1
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("g")
	if err != nil {
		t.Fatal(err)
	}
	session, err = fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !session.state.host {
		t.Fatal("typed host function did not acquire a host-capable session")
	}
	if got, err := session.Invoke1(I32(40)); err != nil || len(got) != 1 || AsI32(got[0]) != 41 {
		t.Fatalf("outer re-entry call = %v, %v; want 41", got, err)
	}
	if reentryErr == nil || !strings.Contains(reentryErr.Error(), "already active") {
		t.Fatalf("same-session callback re-entry error = %v, want already active", reentryErr)
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("outer callback-close call = %v, %v; want 42", got, err)
	}
	if _, err := session.Invoke1(I32(41)); err == nil || !strings.Contains(err.Error(), "closed prepared session") {
		t.Fatalf("post-callback-close invocation error = %v, want closed session", err)
	}
}

func TestPreparedSessionGuardsDeferredHostEventReplay(t *testing.T) {
	compiled := MustCompile(watToWasm(t, `(module
		(import "env" "event" (func $event (param i32)))
		(func (export "run") (param i32)
			local.get 0
			call $event))`))
	defer compiled.Close()
	var session *PreparedSession
	var reentryErr error
	calls := 0
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.event": I32HostEvent(func(value int32) {
			calls++
			if calls == 1 {
				_, reentryErr = session.Invoke1(I32(value))
			} else {
				session.Close()
			}
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	session, err = fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if in.syncMode || !session.state.guardCalls {
		t.Fatalf("deferred host session guard = sync %v, guarded %v; want false, true", in.syncMode, session.state.guardCalls)
	}
	if got, err := session.Invoke1(I32(40)); err != nil || len(got) != 0 {
		t.Fatalf("outer deferred re-entry call = %v, %v; want no results", got, err)
	}
	if reentryErr == nil || !strings.Contains(reentryErr.Error(), "already active") {
		t.Fatalf("deferred same-session re-entry error = %v, want already active", reentryErr)
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 0 {
		t.Fatalf("outer deferred callback-close call = %v, %v; want no results", got, err)
	}
	if _, err := session.Invoke1(I32(41)); err == nil || !strings.Contains(err.Error(), "closed prepared session") {
		t.Fatalf("post-deferred-callback-close invocation error = %v, want closed session", err)
	}
}

func TestPreparedSessionHostCacheRejectsWasmGCCollector(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 { return value + 1 }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("g")
	if err != nil {
		t.Fatal(err)
	}
	// Admission must key off collector presence itself, not only imported or
	// dynamic domain flags. The full GC integration tests cover registration.
	in.gc = new(gc.Collector)
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if session.state.host {
		session.Close()
		t.Fatal("WasmGC instance acquired a cached host entry")
	}
	session.Close()
	in.gc = nil
}

func TestPreparedSessionReservationDoesNotHoldGCDomain(t *testing.T) {
	collector := new(gc.Collector)
	domain := &gcStoreDomain{id: 1, collector: collector}
	in := &Instance{gc: collector}
	in.refStore = &referenceStore{instances: map[*Instance]*referenceStoreInstance{
		in: {gcDomain: domain},
	}}

	lease := in.lockPreparedSessionInvocation()
	defer lease.unlock()
	domain.invocationState.Lock()
	owner := domain.invocationOwner
	domain.invocationState.Unlock()
	if owner != 0 {
		t.Fatalf("idle session retained GC domain owner %d", owner)
	}

	callLease := in.lockGCInvocation(lease.state.invocationID)
	domain.invocationState.Lock()
	owner = domain.invocationOwner
	domain.invocationState.Unlock()
	if owner != lease.state.invocationID {
		callLease.unlock()
		t.Fatalf("active session GC domain owner = %d, want %d", owner, lease.state.invocationID)
	}
	callLease.unlock()
	domain.invocationState.Lock()
	owner = domain.invocationOwner
	domain.invocationState.Unlock()
	if owner != 0 {
		t.Fatalf("session retained GC domain owner %d after call", owner)
	}
}

func TestPreparedSessionHostSharingRevokesCachedLease(t *testing.T) {
	compiled := MustCompile(watToWasm(t, `(module
		(import "env" "f" (func $f (param i32) (result i32)))
		(memory (export "memory") 1 1)
		(func (export "g") (param i32) (result i32)
			local.get 0
			call $f))`))
	defer compiled.Close()
	var in *Instance
	var exported *Memory
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			var exportErr error
			exported, exportErr = in.ExportedMemory("memory")
			if exportErr != nil {
				panic(HostTrap{Err: exportErr})
			}
			return value + 1
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("g")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !session.state.host {
		t.Fatal("typed host function did not acquire cached host lease")
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("sharing call = %v, %v; want 42", got, err)
	}
	if exported == nil {
		t.Fatal("callback did not export memory")
	}
	if session.state.host {
		t.Fatal("session retained its private host lease after resource sharing")
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("post-sharing fallback call = %v, %v; want 42", got, err)
	}
}

func TestPreparedSessionCapabilityHostUsesGeneralPath(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	var retained instanceHostModule
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(module HostModule, params, results []uint64) {
			retained = module.(instanceHostModule)
			results[0] = params[0] + 1
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("g")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.state.host {
		t.Fatal("capability-bearing HostFunc acquired cached host lease")
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("capability host call = %v, %v; want 42", got, err)
	}
	assertExpiredHostToken(t, retained)
}

func TestPreparedSessionHostPanicAndExitCleanup(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first func()
	}{
		{name: "panic", first: func() { panic("expected session panic") }},
		{name: "exit", first: func() { panic(HostExit{Code: 23}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := MustCompile(benchReturningImportModule())
			defer compiled.Close()
			calls := 0
			in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
				"env.f": I32ToI32HostFunc(func(value int32) int32 {
					calls++
					if calls == 1 {
						tc.first()
					}
					return value + 1
				}),
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("g")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()

			if tc.name == "panic" {
				func() {
					defer func() {
						if got := recover(); got != "expected session panic" {
							t.Fatalf("panic = %v", got)
						}
					}()
					_, _ = session.Invoke1(I32(41))
				}()
			} else if _, err := session.Invoke1(I32(41)); err == nil {
				t.Fatal("HostExit did not stop the first call")
			}
			if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
				t.Fatalf("call after %s = %v, %v; want 42", tc.name, got, err)
			}
		})
	}
}
