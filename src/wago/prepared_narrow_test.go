package wago

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPreparedDirectDoesNotAllocateInvocationIdentity(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	if preparedDirectIntEnabled && (!fn.directIntFast || !fn.directIsolated) {
		t.Fatal("fixture must select isolated direct entry")
	}
	before := nextInvocationID.Load()
	out, err := fn.Invoke1(41)
	if err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("call = %v, %v", out, err)
	}
	after := nextInvocationID.Load()
	if fn.directIntFast {
		if after != before {
			t.Fatalf("isolated direct entry consumed invocation identity: %d -> %d", before, after)
		}
	} else if after == before {
		t.Fatal("disabled direct mode omitted general invocation identity")
	}
}

func narrowPreparedFixture(t *testing.T) (*Instance, *PreparedFunction) {
	t.Helper()
	c := MustCompile(benchAddOneModule())
	t.Cleanup(func() { c.Close() })
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	return in, fn
}

func TestPreparedDirectSharesInvocationGate(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	if err := in.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	defer in.endInvocation()
	if !in.tryPreparedDirect() {
		t.Fatal("private reservation rejected")
	}
	gate := &in.ensurePluginState().invokeMu
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := gate.lockContext(ctx); err != context.DeadlineExceeded {
		t.Fatalf("general admission during direct entry: %v", err)
	}
	gate.Unlock()
	gate.Lock()
	if in.tryPreparedDirect() {
		gate.Unlock()
		t.Fatal("direct entry bypassed general owner")
	}
	gate.Unlock()
	out, err := fn.Invoke1(41)
	if err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("call after conflict = %v, %v", out, err)
	}
}

func TestPreparedDirectRevocation(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	if err := in.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	if !in.tryPreparedDirect() {
		t.Fatal("private reservation rejected")
	}
	done := make(chan struct{})
	go func() { in.markNativeControlShared(); close(done) }()
	select {
	case <-done:
		t.Fatal("sharing completed during direct reservation")
	case <-time.After(20 * time.Millisecond):
	}
	in.ensurePluginState().invokeMu.Unlock()
	in.endInvocation()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sharing did not complete")
	}
	if in.tryPreparedDirect() {
		in.ensurePluginState().invokeMu.Unlock()
		t.Fatal("revoked instance admitted direct entry")
	}
	before := nextInvocationID.Load()
	out, err := fn.Invoke1(41)
	if err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("fallback = %v, %v", out, err)
	}
	if nextInvocationID.Load() == before {
		t.Fatal("fallback omitted general identity")
	}
}

func TestPreparedDirectRejectsGCModes(t *testing.T) {
	for _, flag := range []uint32{executionFlagImportedGCDomain, executionFlagDynamicGCDomain, executionFlagStoreOwnedGCCollector} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			in, _ := narrowPreparedFixture(t)
			// Registration publishes these bits before public entry. Exercise each
			// exclusion independently; a numeric signature does not override it.
			in.executionFlags.Store(in.executionFlags.Load() | flag)
			if in.tryPreparedDirect() {
				in.ensurePluginState().invokeMu.Unlock()
				t.Fatal("GC ownership admitted direct entry")
			}
			if in.ensurePluginState().invokeMu.state.Load()&invocationGateHeld != 0 {
				t.Fatal("rejected entry retained gate")
			}
		})
	}
}

func TestPreparedDirectLifetimeDuringClose(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			var in *Instance
			var rt *Runtime
			if owned {
				rt = NewRuntime()
				t.Cleanup(func() { rt.Close() })
				mod, err := rt.Compile(benchAddOneModule())
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				in, err = rt.Instantiate(context.Background(), mod)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				in, _ = narrowPreparedFixture(t)
			}
			fn, err := in.PrepareFunction("f")
			if err != nil {
				t.Fatal(err)
			}
			if err := in.beginInvocation(); err != nil {
				t.Fatal(err)
			}
			if !in.tryPreparedDirect() {
				t.Fatal("private reservation rejected")
			}
			var once sync.Once
			release := func() { once.Do(func() { in.ensurePluginState().invokeMu.Unlock(); in.endInvocation() }) }
			defer release()
			done := make(chan error, 1)
			go func() {
				if rt != nil {
					done <- rt.Close()
				} else {
					done <- in.Close()
				}
			}()
			// Close publishes its lifetime bit before it can release native storage.
			deadline := time.Now().Add(time.Second)
			for in.invocationState.Load()&instanceInvocationClosed == 0 {
				if time.Now().After(deadline) {
					t.Fatal("close did not publish")
				}
				time.Sleep(time.Millisecond)
			}
			in.lifeMu.Lock()
			released := in.resourcesClosed
			in.lifeMu.Unlock()
			if released || in.eng == nil || in.base == 0 {
				t.Fatal("close released an active direct lease")
			}
			release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("close did not finish")
			}
			if _, err := fn.Invoke1(41); err == nil {
				t.Fatal("closed instance accepted prepared call")
			}
		})
	}
}
