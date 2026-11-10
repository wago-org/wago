//go:build (linux || darwin) && (arm64 || amd64) && !tinygo && go1.22 && !go1.28

package wago

import (
	"context"
	"strings"
	"testing"
)

func TestBoundedCallerExhaustionRestoresMarkerAndLease(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	called := false
	imports := NewImports()
	imports.HostFunc("env", "step", func(_ Caller, call HostCall) { called = true; call.SetI32(0, call.I32(0)+1) }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.boundedHostSegments() || !in.hasBoundedCallerHostView() {
		t.Fatal("fixture did not select bounded Caller view")
	}
	state := in.ensurePluginState()
	state.hostScope.sequence.Store(^uint64(0))
	state.activations.boundedID.Store(123)
	for range 2 {
		_, err := in.Invoke("run", 1, 0)
		if err == nil || !strings.Contains(err.Error(), "generation exhausted") {
			t.Fatalf("exhaustion error: %v", err)
		}
		if called {
			t.Fatal("callback executed after exhaustion")
		}
		if state.hostScope.sequence.Load() != ^uint64(0) || state.hostScope.active.Load() != 0 {
			t.Fatal("exhaustion changed scope authority")
		}
		if state.activations.boundedID.Load() != 123 {
			t.Fatal("bounded invocation marker leaked")
		}
		if !state.nativeExecutionMu.TryLock() {
			t.Fatal("native lease remained locked after error")
		}
		state.nativeExecutionMu.Unlock()
	}
}

// A custom parent can supply a Stop function that panics. Scope expiry must
// still restore the invocation marker and the parked native execution lease.
type callerCleanupPanicContext struct{ context.Context }

func (callerCleanupPanicContext) Value(any) any { return nil }
func (callerCleanupPanicContext) AfterFunc(func()) func() bool {
	return func() bool { panic("callback parent stop panic") }
}

func TestBoundedCallerContextCleanupPanicRestoresMarkerAndLease(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stale Caller
	watch := true
	imports := NewImports()
	imports.HostFunc("env", "step", func(caller Caller, call HostCall) {
		stale = caller
		if watch {
			if _, ok := caller.scope.invocationContext(caller.generation, callerCleanupPanicContext{parent}); !ok {
				t.Fatal("context rejected")
			}
		}
		call.SetI32(0, call.I32(0)+1)
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.boundedHostSegments() || !in.hasBoundedCallerHostView() {
		t.Fatal("fixture did not select bounded Caller view")
	}
	state := in.ensurePluginState()
	state.activations.boundedID.Store(123)
	func() {
		defer func() {
			if got := recover(); got != "callback parent stop panic" {
				t.Fatalf("cleanup panic %v", got)
			}
		}()
		_, _ = in.Invoke("run", 1, 0)
	}()
	if stale.valid() || state.hostScope.active.Load() != 0 {
		t.Fatal("callback scope survived cleanup panic")
	}
	if state.activations.boundedID.Load() != 123 {
		t.Fatal("marker survived cleanup panic")
	}
	if !state.nativeExecutionMu.TryLock() {
		t.Fatal("native lease survived cleanup panic")
	}
	state.nativeExecutionMu.Unlock()
	watch = false
	got, err := in.Invoke("run", 1, 0)
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("reuse result %v %v", got, err)
	}
}
