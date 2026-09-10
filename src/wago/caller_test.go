package wago

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Adapt test callbacks only. Production concrete dispatch must not make this
// interface conversion; it deliberately remains visible to helper API tests.
func callerTestCallback(concrete bool, fn HostFunc) any {
	if concrete {
		return CallerHostFunc(func(c Caller, p, r []uint64) { fn(c, p, r) })
	}
	return fn
}

func callerTestDeclare(module *ImportModuleBuilder, concrete bool) func(string, HostFunc) *ImportFuncBuilder {
	if concrete {
		return func(name string, fn HostFunc) *ImportFuncBuilder {
			return module.CallerFunc(name, func(c Caller, p, r []uint64) { fn(c, p, r) })
		}
	}
	return module.Func
}

func BenchmarkInvokeCallerHostFuncDirect(b *testing.B) {
	c := benchMustCompile(b, benchReturningImportModule())
	defer c.Close()
	in, err := Instantiate(c, Imports{"env.f": CallerHostFunc(func(_ Caller, p, r []uint64) { r[0] = p[0] + 1 })})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func TestCallerRetainedAndNested(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	var in *Instance
	var outer, nested, retained Caller
	var seq uint64
	calls := 0
	checkExpired := func(h Caller) {
		assertExpiredHostToken(t, h.instanceHostModule)
		if _, err := in.InvokeFromHost(context.Background(), h, "g", 0); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("concrete stale re-entry: %v", err)
		}
	}
	var err error
	in, err = Instantiate(c, Imports{"env.f": CallerHostFunc(func(h Caller, p, r []uint64) {
		calls++
		if h.generation <= seq {
			t.Fatal("generation reused")
		}
		seq = h.generation
		if calls > 2 {
			checkExpired(retained)
		}
		if p[0] == 1 {
			outer = h
			got, err := in.InvokeFromHost(context.Background(), h, "g", 0)
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("nested = %v, %v", got, err)
			}
			if !outer.valid() {
				t.Fatal("outer not restored")
			}
			checkExpired(nested)
		} else {
			nested = h
			if outer.valid() {
				t.Fatal("outer valid inside nested callback")
			}
		}
		r[0] = p[0] + 1
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2; i++ {
		if _, err := in.Invoke("g", 1); err != nil {
			t.Fatal(err)
		}
		checkExpired(outer)
		checkExpired(nested)
		retained = outer
	}
	checkExpired(Caller{})
}

func TestCallerScalarDispatchAllocations(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	in, err := Instantiate(c, Imports{"env.f": CallerHostFunc(func(_ Caller, p, r []uint64) { r[0] = p[0] + 1 })})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := testing.AllocsPerRun(1000, func() {
		results, err := in.Invoke("g", 1)
		if err != nil || len(results) != 1 || results[0] != 2 {
			t.Fatalf("invoke = %v, %v", results, err)
		}
	}); got != 0 {
		t.Fatalf("runtime allocations = %g; want 0", got)
	}
}

func TestCallerConcurrentIndependentInstances(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	t.Cleanup(func() { c.Close() })
	for worker := 0; worker < 16; worker++ {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			t.Parallel()
			var retained Caller
			in, err := Instantiate(c, Imports{"env.f": CallerHostFunc(func(h Caller, p, r []uint64) {
				if !h.valid() || retained.valid() || h.generation <= retained.generation {
					t.Error("independent callback lost authority or revived an expired generation")
				}
				retained = h
				r[0] = p[0] + 1
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for n := 0; n < 128; n++ {
				got, err := in.Invoke("g", uint64(n))
				if err != nil || len(got) != 1 || got[0] != uint64(n+1) || retained.valid() {
					t.Fatalf("independent invocation %d: result=%v error=%v active=%v", n, got, err, retained.valid())
				}
			}
		})
	}
}

func TestCallerCapabilityHelpers(t *testing.T) {
	state := &invocationContextTestState{concrete: true}
	rt := newInvocationContextTestRuntime(t, state)
	defer rt.Close()
	var invoker CallerInvoker
	invoker.activate(rt)
	defer invoker.close()
	other := NewRuntime()
	defer other.Close()
	var wrong CallerResolver
	wrong.activate(other)
	var retained Caller
	var in *Instance
	depth := 0
	expired := func(h Caller) {
		assertExpiredHostToken(t, h.instanceHostModule)
		if _, err := state.resolver.Resolve(h); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expired resolver: %v", err)
		}
		if _, err := state.resolver.InvocationContext(h); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expired context: %v", err)
		}
		if _, err := invoker.Invoke(context.Background(), h, "call"); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expired invoker: %v", err)
		}
		if _, _, err := state.manager.WatchCaller(h); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expired watcher: %v", err)
		}
	}
	state.outer = func(module HostModule, _, _ []uint64) {
		h := module.(Caller)
		if retained.in != nil {
			expired(retained)
		}
		identity, err := state.resolver.Resolve(h)
		if err != nil || identity.value != in {
			t.Fatalf("resolver = %v, %v", identity, err)
		}
		if _, err := wrong.Resolve(h); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("cross-runtime resolver: %v", err)
		}
		ctx, err := state.resolver.InvocationContext(h)
		if err != nil || ctx.Err() != nil {
			t.Fatalf("context = %v, %v", ctx, err)
		}
		ref, err := h.NewExternRef("caller")
		if err != nil {
			t.Fatal(err)
		}
		if retained.in != nil {
			if _, ok := retained.ExternRefValue(ref); ok {
				t.Fatal("expired caller read a live later callback's externref")
			}
			if retained.ReleaseExternRef(ref) {
				t.Fatal("expired caller released a live later callback's externref")
			}
		}
		if value, ok := h.ExternRefValue(ref); !ok || value != "caller" {
			t.Fatalf("externref = %v, %v", value, ok)
		}
		if !h.ReleaseExternRef(ref) {
			t.Fatal("externref release failed")
		}
		if depth == 0 {
			wake, cancel, err := state.manager.WatchCaller(h)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			depth++
			if _, err := invoker.Invoke(context.Background(), h, "call"); err != nil {
				t.Fatal(err)
			}
			depth--
			if !h.valid() {
				t.Fatal("outer helper authority not restored")
			}
			select {
			case <-wake:
				t.Fatal("outer watcher expired during nested call")
			default:
			}
		}
		retained = h
	}
	mod, err := rt.Compile(invocationContextImportModule("outer"))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err = rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2; i++ {
		if _, err := in.Call(context.Background(), "call"); err != nil {
			t.Fatal(err)
		}
		expired(retained)
	}
	expired(Caller{})
}

func TestCallerImportedStart(t *testing.T) {
	c := MustCompile(importedStartModule())
	defer c.Close()
	var retained Caller
	calls := 0
	in, err := Instantiate(c, Imports{"env.start": CallerHostFunc(func(h Caller, p, r []uint64) {
		calls++
		retained = h
		if !h.valid() || len(p) != 0 || len(r) != 0 {
			t.Fatal("invalid start callback")
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if calls != 1 || retained.valid() {
		t.Fatal("start callback lifetime")
	}
}

func TestCallerReexport(t *testing.T) {
	state := &invocationContextTestState{concrete: true}
	rt := newInvocationContextTestRuntime(t, state)
	defer rt.Close()
	var retained Caller
	state.outer = func(m HostModule, _, _ []uint64) {
		retained = m.(Caller)
		if !retained.valid() {
			t.Fatal("invalid reexport callback")
		}
	}
	mod, err := rt.Compile(invocationContextReexportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Call(context.Background(), "call"); err != nil {
		t.Fatal(err)
	}
	if retained.in != in || retained.valid() {
		t.Fatal("reexport callback lifetime")
	}
}
